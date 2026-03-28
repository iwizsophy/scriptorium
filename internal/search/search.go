package search

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/docs"
	"github.com/silvekt/scriptorium/internal/docsindex"
	"github.com/silvekt/scriptorium/internal/filesafe"
	"github.com/silvekt/scriptorium/internal/ref"
	"github.com/silvekt/scriptorium/internal/source"
	"github.com/silvekt/scriptorium/internal/textsearch"
	"github.com/silvekt/scriptorium/internal/textutil"
)

const (
	defaultTopK         = 20
	defaultSnippetLines = 40
)

type Request struct {
	Query        string   `json:"query"`
	Extensions   []string `json:"extensions,omitempty"`
	TopK         int      `json:"topK,omitempty"`
	SnippetLines int      `json:"snippetLines,omitempty"`
	Mode         string   `json:"mode,omitempty"`
}

type Response struct {
	Results []Result `json:"results"`
}

type Result struct {
	RefID   string `json:"refId"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Range   Range  `json:"range"`
	Score   int    `json:"score"`
	Snippet string `json:"snippet"`
}

type Range struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

type normalizedRequest struct {
	Query          string
	Phrase         string
	Tokens         []string
	TopK           int
	SnippetLines   int
	Mode           string
	SearchDocs     bool
	CodeExtensions []string
}

var (
	attrPattern         = regexp.MustCompile(`\[[A-Za-z_][\w]*(Get|Post|Put|Delete|Patch|Route|Authorize)?\]`)
	decoratorPattern    = regexp.MustCompile(`@[A-Za-z_][\w]*(Controller|Get|Post|Inject|Autowired|Configuration|Bean|Module)?`)
	configKeyPattern    = regexp.MustCompile(`["'][A-Za-z0-9_.:-]+["']`)
	routePattern        = regexp.MustCompile(`\b(Map(Get|Post|Put|Delete|Patch)|UseEndpoints|Route|MapControllers)\b|/[\w/{}/-]+`)
	serviceRegPattern   = regexp.MustCompile(`\b(AddSingleton|AddScoped|AddTransient|AddAuthentication|AddAuthorization|GetRequiredService|IServiceProvider|builder\.Services)\b`)
	setupFileNameTokens = []string{"program", "startup", "main", "app", "routes", "router", "config", "settings"}
)

func Execute(cfg config.Runtime, request Request) (Response, error) {
	normalized := normalizeRequest(request, cfg.CodeExtensions)
	if normalized.Query == "" {
		return Response{Results: []Result{}}, nil
	}

	results := make([]Result, 0, 32)
	if normalized.SearchDocs && strings.TrimSpace(cfg.DocsRoot) != "" {
		docsResults, err := searchDocs(cfg, normalized)
		if err != nil {
			return Response{}, err
		}
		results = append(results, docsResults...)
	}

	if len(normalized.CodeExtensions) > 0 && (len(cfg.SampleRoots) > 0 || len(cfg.EffectiveGitSnapshotPaths()) > 0) {
		codeResults, err := searchCode(cfg, normalized)
		if err != nil {
			return Response{}, err
		}
		results = append(results, codeResults...)
	}

	results = dedupeResults(results)
	results = clusterCodeResults(results, normalized.SnippetLines)
	sortResults(results)
	if normalized.Mode == "implementation" {
		results = diversifyResults(results, normalized.TopK)
	}

	if len(results) > normalized.TopK {
		results = results[:normalized.TopK]
	}
	return Response{Results: results}, nil
}

func normalizeRequest(request Request, defaultCodeExtensions []string) normalizedRequest {
	topK := clamp(request.TopK, 1, 200, defaultTopK)
	snippetLines := clamp(request.SnippetLines, 1, 200, defaultSnippetLines)

	query := strings.TrimSpace(request.Query)
	phrase := textsearch.Normalize(query)
	extensions := normalizeExtensions(request.Extensions)

	searchDocs := len(extensions) == 0 || containsString(extensions, ".md")
	codeExtensions := append([]string(nil), defaultCodeExtensions...)
	if len(extensions) > 0 {
		codeExtensions = codeExtensions[:0]
		for _, ext := range extensions {
			if ext == ".md" {
				continue
			}
			codeExtensions = append(codeExtensions, ext)
		}
	}

	return normalizedRequest{
		Query:          query,
		Phrase:         phrase,
		Tokens:         textsearch.TokenizeFullTextSearch(query),
		TopK:           topK,
		SnippetLines:   snippetLines,
		Mode:           normalizeMode(request.Mode),
		SearchDocs:     searchDocs,
		CodeExtensions: codeExtensions,
	}
}

func searchDocs(cfg config.Runtime, request normalizedRequest) ([]Result, error) {
	if state := docsindex.OpenRuntimeSet(cfg); state.Enabled {
		return searchDocsIndexes(state.Indexes, request)
	}

	root, err := filesafe.NewRoot(cfg.DocsRoot, cfg.AllowSymlinks, cfg.MaxFileBytes)
	if err != nil {
		return nil, err
	}

	files, err := docs.ScanFilesystem(root, cfg.TextEncodingFallbacks)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(files))
	for _, file := range files {
		for _, block := range file.Blocks {
			score := scoreDocsBlock(block.Path, block.Heading, block.Content, request)
			if score <= 0 {
				continue
			}

			results = append(results, Result{
				RefID:   block.RefID,
				Kind:    "md_heading_block",
				Path:    block.Path,
				Range:   Range{StartLine: block.StartLine, EndLine: block.EndLine},
				Score:   score + frameworkBoostForDocs(block.Path, block.Heading, block.Content, request),
				Snippet: topSnippet(block.Content, request.SnippetLines),
			})
		}
	}

	return results, nil
}

func searchDocsIndexes(indexes []docsindex.Artifact, request normalizedRequest) ([]Result, error) {
	results := make([]Result, 0, len(indexes)*request.TopK)
	for _, index := range indexes {
		indexResults, err := index.SearchMarkdown(request.Query, request.SnippetLines, request.TopK)
		if err != nil {
			return nil, err
		}
		for _, block := range indexResults {
			results = append(results, Result{
				RefID:   block.RefID,
				Kind:    "md_heading_block",
				Path:    block.Path,
				Range:   Range{StartLine: block.StartLine, EndLine: block.EndLine},
				Score:   block.Score + frameworkBoostForDocs(block.Path, block.Path, block.Snippet, request),
				Snippet: block.Snippet,
			})
		}
	}
	return results, nil
}

func searchCode(cfg config.Runtime, request normalizedRequest) ([]Result, error) {
	sources, multipleRoots, err := source.BuildCodeSources(cfg)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, 32)
	for _, codeSource := range sources {
		if snapshotResults, ok, err := codeSource.Search(request.Query, request.CodeExtensions, request.TopK, request.SnippetLines); ok {
			if err != nil {
				return nil, err
			}
			for _, result := range snapshotResults {
				results = append(results, Result{
					RefID:   result.RefID,
					Kind:    "code_range",
					Path:    result.LogicalPath,
					Range:   Range{StartLine: result.StartLine, EndLine: result.EndLine},
					Score:   result.Score,
					Snippet: result.Snippet,
				})
			}
			continue
		}

		paths, err := codeSource.ListFiles(request.CodeExtensions)
		if err != nil {
			return nil, err
		}
		labelScore := textsearch.ScoreLabelBoost(codeSource.Labels, request.Phrase, request.Tokens)
		sourceMatches := 0

		for _, path := range paths {
			textFile, err := codeSource.ReadText(path, cfg.TextEncodingFallbacks)
			if err != nil {
				return nil, err
			}

			logicalPath := codeSource.BuildLogicalPath(path, multipleRoots)
			pathScore := textsearch.ScorePathBoost(logicalPath, request.Tokens, request.Phrase)
			structuralBoost := frameworkBoostForCode(logicalPath, textFile.Text, request)

			lines := textutil.SplitLines(textFile.Text)
			fileHadLineHit := false
			for idx, line := range lines {
				score := matchScore(textsearch.Normalize(line), request.Phrase, request.Tokens)
				if score <= 0 {
					continue
				}
				fileHadLineHit = true
				sourceMatches++

				startLine, endLine, snippet := textutil.CenteredSnippet(lines, idx+1, request.SnippetLines)
				results = append(results, Result{
					RefID:   ref.BuildCodeRefID(logicalPath, idx+1),
					Kind:    "code_range",
					Path:    logicalPath,
					Range:   Range{StartLine: startLine, EndLine: endLine},
					Score:   score + labelScore + pathScore + structuralBoost,
					Snippet: snippet,
				})
			}

			if !fileHadLineHit && pathScore+structuralBoost > 0 {
				sourceMatches++
				endLine := min(len(lines), request.SnippetLines)
				results = append(results, Result{
					RefID:   ref.BuildCodeRefID(logicalPath, 1),
					Kind:    "code_range",
					Path:    logicalPath,
					Range:   Range{StartLine: 1, EndLine: endLine},
					Score:   pathScore + labelScore + structuralBoost,
					Snippet: topSnippet(textFile.Text, request.SnippetLines),
				})
			}
		}

		if sourceMatches == 0 && len(paths) > 0 {
			firstPath := paths[0]
			textFile, err := codeSource.ReadText(firstPath, cfg.TextEncodingFallbacks)
			if err != nil {
				return nil, err
			}
			logicalPath := codeSource.BuildLogicalPath(firstPath, multipleRoots)
			structuralBoost := frameworkBoostForCode(logicalPath, textFile.Text, request)
			if labelScore+structuralBoost <= 0 {
				continue
			}
			results = append(results, Result{
				RefID:   ref.BuildCodeRefID(logicalPath, 1),
				Kind:    "code_range",
				Path:    logicalPath,
				Range:   Range{StartLine: 1, EndLine: min(len(textutil.SplitLines(textFile.Text)), request.SnippetLines)},
				Score:   labelScore + structuralBoost,
				Snippet: topSnippet(textFile.Text, request.SnippetLines),
			})
		}
	}

	return results, nil
}

func matchScore(text, phrase string, tokens []string) int {
	score := 0
	if phrase != "" && strings.Contains(text, phrase) {
		score += 2
	}
	for _, token := range tokens {
		if strings.Contains(text, token) {
			score++
		}
	}
	return score
}

func scoreDocsBlock(path, heading, content string, request normalizedRequest) int {
	return textsearch.ScoreTokenMatches(content, request.Tokens, request.Phrase, 1, 2) +
		textsearch.ScoreTokenMatches(heading, request.Tokens, request.Phrase, 2, 4) +
		textsearch.ScorePathBoost(path, request.Tokens, request.Phrase)
}

func normalizeMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "implementation":
		return "implementation"
	default:
		return "default"
	}
}

func frameworkBoostForDocs(path, heading, content string, request normalizedRequest) int {
	if request.Mode != "implementation" {
		return 0
	}
	score := 0
	lowerPath := strings.ToLower(filepath.Base(path))
	normalizedHeading := textsearch.Normalize(heading)
	if wantsStartup(request) && containsAny(lowerPath, "program", "startup", "overview", "getting-started") {
		score += 3
	}
	if wantsRoute(request) && (strings.Contains(normalizedHeading, "route") || strings.Contains(normalizedHeading, "endpoint") || routePattern.MatchString(content)) {
		score += 3
	}
	if wantsDI(request) && (strings.Contains(normalizedHeading, "dependency injection") || serviceRegPattern.MatchString(content)) {
		score += 3
	}
	if wantsConfig(request) && (strings.Contains(normalizedHeading, "configuration") || strings.Contains(normalizedHeading, "options") || configKeyPattern.MatchString(content)) {
		score += 2
	}
	if wantsAttributes(request) && (attrPattern.MatchString(content) || decoratorPattern.MatchString(content)) {
		score += 2
	}
	if wantsSetupFile(request) && containsAny(lowerPath, setupFileNameTokens...) {
		score += 1
	}
	if score == 0 && (serviceRegPattern.MatchString(content) || routePattern.MatchString(content)) {
		score++
	}
	return score
}

func frameworkBoostForCode(path, content string, request normalizedRequest) int {
	if request.Mode != "implementation" {
		return 0
	}
	score := 0
	lowerPath := strings.ToLower(path)
	if wantsSetupFile(request) && containsAny(lowerPath, setupFileNameTokens...) {
		score += 2
	}
	if wantsStartup(request) && containsAny(lowerPath, "program.cs", "startup.cs", "main.", "app.", "bootstrap") {
		score += 3
	}
	if wantsDI(request) && serviceRegPattern.MatchString(content) {
		score += 3
	}
	if wantsRoute(request) && routePattern.MatchString(content) {
		score += 3
	}
	if wantsAttributes(request) && (attrPattern.MatchString(content) || decoratorPattern.MatchString(content)) {
		score += 2
	}
	if wantsConfig(request) && (configKeyPattern.MatchString(content) || strings.Contains(strings.ToLower(content), "configuration")) {
		score += 2
	}
	return score
}

func diversifyResults(results []Result, topK int) []Result {
	if len(results) <= 2 || topK <= 1 {
		return results
	}
	var bestDoc *Result
	var bestCode *Result
	for idx := range results {
		result := results[idx]
		switch result.Kind {
		case "md_heading_block":
			if bestDoc == nil {
				bestDoc = &result
			}
		case "code_range":
			if bestCode == nil {
				bestCode = &result
			}
		}
		if bestDoc != nil && bestCode != nil {
			break
		}
	}
	if bestDoc == nil || bestCode == nil {
		return results
	}
	diversified := make([]Result, 0, len(results))
	diversified = append(diversified, *bestDoc, *bestCode)
	seen := map[string]struct{}{
		bestDoc.RefID:  {},
		bestCode.RefID: {},
	}
	for _, result := range results {
		if _, ok := seen[result.RefID]; ok {
			continue
		}
		diversified = append(diversified, result)
	}
	sortResults(diversified)
	if topK >= 2 {
		if diversified[0].Kind == diversified[1].Kind {
			diversified = append([]Result{*bestDoc, *bestCode}, diversified...)
			deduped := make([]Result, 0, len(diversified))
			seen = map[string]struct{}{}
			for _, result := range diversified {
				key := result.RefID + "|" + result.Path + "|" + strconv.Itoa(result.Range.StartLine)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				deduped = append(deduped, result)
			}
			return deduped
		}
	}
	return diversified
}

func wantsStartup(request normalizedRequest) bool {
	return containsAny(request.Phrase, "startup", "entrypoint", "setup", "boot", "initialize")
}

func wantsRoute(request normalizedRequest) bool {
	return containsAny(request.Phrase, "route", "endpoint", "controller", "api", "handler")
}

func wantsDI(request normalizedRequest) bool {
	return containsAny(request.Phrase, "dependency injection", "inject", "service", "registration", "resolve", "di")
}

func wantsConfig(request normalizedRequest) bool {
	return containsAny(request.Phrase, "configuration", "config", "settings", "options", "bind")
}

func wantsAttributes(request normalizedRequest) bool {
	return containsAny(request.Phrase, "attribute", "annotation", "decorator")
}

func wantsSetupFile(request normalizedRequest) bool {
	return wantsStartup(request) || wantsDI(request) || wantsRoute(request) || wantsConfig(request)
}

func containsAny(value string, candidates ...string) bool {
	value = strings.ToLower(value)
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func topSnippet(text string, snippetLines int) string {
	lines := textutil.SplitLines(text)
	if len(lines) > snippetLines {
		lines = lines[:snippetLines]
	}
	return strings.Join(lines, "\n")
}

func dedupeResults(results []Result) []Result {
	seen := map[string]struct{}{}
	deduped := make([]Result, 0, len(results))
	for _, result := range results {
		key := strings.Join([]string{
			result.Kind,
			result.Path,
			result.RefID,
			strconv.Itoa(result.Range.StartLine),
			strconv.Itoa(result.Range.EndLine),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	return deduped
}

func clusterCodeResults(results []Result, snippetLines int) []Result {
	codeResults := make([]Result, 0, len(results))
	otherResults := make([]Result, 0, len(results))
	for _, result := range results {
		if result.Kind == "code_range" {
			codeResults = append(codeResults, result)
			continue
		}
		otherResults = append(otherResults, result)
	}

	sort.Slice(codeResults, func(i, j int) bool {
		if codeResults[i].Path != codeResults[j].Path {
			return codeResults[i].Path < codeResults[j].Path
		}
		if codeResults[i].Range.StartLine != codeResults[j].Range.StartLine {
			return codeResults[i].Range.StartLine < codeResults[j].Range.StartLine
		}
		if codeResults[i].Range.EndLine != codeResults[j].Range.EndLine {
			return codeResults[i].Range.EndLine < codeResults[j].Range.EndLine
		}
		return codeResults[i].Score > codeResults[j].Score
	})

	proximityLines := max(1, snippetLines/5)
	clustered := make([]Result, 0, len(codeResults))
	for idx := 0; idx < len(codeResults); {
		cluster := []Result{codeResults[idx]}
		clusterEnd := codeResults[idx].Range.EndLine
		next := idx + 1
		for next < len(codeResults) {
			candidate := codeResults[next]
			if candidate.Path != codeResults[idx].Path || candidate.Range.StartLine > clusterEnd+proximityLines {
				break
			}
			cluster = append(cluster, candidate)
			if candidate.Range.EndLine > clusterEnd {
				clusterEnd = candidate.Range.EndLine
			}
			next++
		}

		best := cluster[0]
		for _, candidate := range cluster[1:] {
			if candidate.Score > best.Score {
				best = candidate
			}
		}
		best.Score += min(3, len(cluster)-1)
		clustered = append(clustered, best)
		idx = next
	}

	return append(otherResults, clustered...)
}

func sortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		if results[i].Range.StartLine != results[j].Range.StartLine {
			return results[i].Range.StartLine < results[j].Range.StartLine
		}
		return results[i].Range.EndLine < results[j].Range.EndLine
	})
}

func normalizeExtensions(extensions []string) []string {
	if len(extensions) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(extensions))
	seen := map[string]struct{}{}
	for _, ext := range extensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if _, ok := seen[ext]; ok {
			continue
		}
		seen[ext] = struct{}{}
		normalized = append(normalized, ext)
	}
	return normalized
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func clamp(value, minValue, maxValue, defaultValue int) int {
	if value == 0 {
		value = defaultValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
