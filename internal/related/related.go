package related

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/content"
	"github.com/iwizsophy/scriptorium/internal/docs"
	"github.com/iwizsophy/scriptorium/internal/filesafe"
	"github.com/iwizsophy/scriptorium/internal/ref"
	"github.com/iwizsophy/scriptorium/internal/source"
	"github.com/iwizsophy/scriptorium/internal/textsearch"
	"github.com/iwizsophy/scriptorium/internal/textutil"
)

const (
	defaultBudget       = 30
	defaultSnippetLines = 40
	maxSeedTokens       = 30
)

type Request struct {
	Seeds        []string `json:"seeds"`
	Extensions   []string `json:"extensions,omitempty"`
	Budget       int      `json:"budget,omitempty"`
	Signals      []string `json:"signals,omitempty"`
	SnippetLines int      `json:"snippetLines,omitempty"`
	Mode         string   `json:"mode,omitempty"`
}

type Response struct {
	Related []Result `json:"related"`
}

type Result struct {
	RefID   string   `json:"refId"`
	Kind    string   `json:"kind"`
	Path    string   `json:"path"`
	Range   Range    `json:"range"`
	Score   float64  `json:"score"`
	Reasons []Reason `json:"reasons"`
}

type Range struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

type Reason struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

type normalizedRequest struct {
	Seeds          []string
	Budget         int
	SnippetLines   int
	Mode           string
	SearchDocs     bool
	CodeExtensions []string
	Signals        map[string]bool
}

type seedContext struct {
	RefID         string
	Kind          string
	Path          string
	OverlapTokens []string
	ImportTokens  map[string]struct{}
	FrameworkTags map[string]struct{}
}

type candidate struct {
	RefID   string
	Kind    string
	Path    string
	Range   Range
	Tokens  []string
	Imports map[string]struct{}
	Tags    map[string]struct{}
}

type signalMatch struct {
	score  float64
	reason *Reason
}

var (
	tokenPattern     = regexp.MustCompile(`[\p{L}\p{N}_./-]+`)
	importPattern    = regexp.MustCompile(`^\s*import\b`)
	usingPattern     = regexp.MustCompile(`^\s*using\s+`)
	requirePattern   = regexp.MustCompile(`\brequire\s*\(`)
	numberOnlyToken  = regexp.MustCompile(`^\d+$`)
	configKeyPattern = regexp.MustCompile(`["'][A-Za-z0-9_.:-]+["']`)
)

func Execute(cfg config.Runtime, request Request) (Response, error) {
	normalized, err := normalizeRequest(request, cfg.CodeExtensions)
	if err != nil {
		return Response{}, err
	}

	seeds, err := loadSeedContexts(cfg, normalized)
	if err != nil {
		return Response{}, err
	}

	results := make([]Result, 0, normalized.Budget)
	seedRefs := make(map[string]struct{}, len(seeds))
	seedCodePaths := make(map[string]struct{}, len(seeds))
	for _, seed := range seeds {
		seedRefs[seed.RefID] = struct{}{}
		if seed.Kind == "code_range" || seed.Kind == "file" {
			seedCodePaths[seed.Path] = struct{}{}
		}
	}

	if normalized.SearchDocs && strings.TrimSpace(cfg.DocsRoot) != "" {
		docsResults, err := relatedDocs(cfg, normalized, seeds, seedRefs)
		if err != nil {
			return Response{}, err
		}
		results = append(results, docsResults...)
	}

	if len(normalized.CodeExtensions) > 0 && (len(cfg.SampleRoots) > 0 || len(cfg.EffectiveGitSnapshotPaths()) > 0) {
		codeResults, err := relatedCode(cfg, normalized, seeds, seedRefs, seedCodePaths)
		if err != nil {
			return Response{}, err
		}
		results = append(results, codeResults...)
	}

	sortResults(results)

	if len(results) > normalized.Budget {
		results = results[:normalized.Budget]
	}
	return Response{Related: results}, nil
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

func normalizeRequest(request Request, defaultCodeExtensions []string) (normalizedRequest, error) {
	seeds := make([]string, 0, len(request.Seeds))
	seenSeeds := map[string]struct{}{}
	for _, seed := range request.Seeds {
		seed = strings.TrimSpace(seed)
		if seed == "" {
			continue
		}
		if _, ok := seenSeeds[seed]; ok {
			continue
		}
		seenSeeds[seed] = struct{}{}
		seeds = append(seeds, seed)
	}
	if len(seeds) == 0 {
		return normalizedRequest{}, fmt.Errorf("seeds are required")
	}

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

	signals := normalizeSignals(request.Signals)
	if len(signals) == 0 {
		signals = map[string]bool{
			"imports":         true,
			"token_overlap":   true,
			"path_proximity":  true,
			"same_file_hits":  true,
			"framework_links": true,
		}
	}

	return normalizedRequest{
		Seeds:          seeds,
		Budget:         clamp(request.Budget, 1, 200, defaultBudget),
		SnippetLines:   clamp(request.SnippetLines, 1, 200, defaultSnippetLines),
		Mode:           normalizeMode(request.Mode),
		SearchDocs:     searchDocs,
		CodeExtensions: codeExtensions,
		Signals:        signals,
	}, nil
}

func normalizeSignals(values []string) map[string]bool {
	signals := map[string]bool{}
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "imports", "token_overlap", "path_proximity", "same_file_hits", "framework_links":
			signals[strings.ToLower(strings.TrimSpace(value))] = true
		}
	}
	return signals
}

func normalizeMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "implementation":
		return "implementation"
	default:
		return "default"
	}
}

func loadSeedContexts(cfg config.Runtime, request normalizedRequest) ([]seedContext, error) {
	seeds := make([]seedContext, 0, len(request.Seeds))
	for _, seed := range request.Seeds {
		response, err := content.Execute(cfg, content.Request{
			RefID:        seed,
			Mode:         "snippet",
			SnippetLines: request.SnippetLines,
			MaxLines:     1000,
		})
		if err != nil {
			return nil, err
		}
		seeds = append(seeds, seedContext{
			RefID:         response.RefID,
			Kind:          response.Kind,
			Path:          response.Path,
			OverlapTokens: overlapTokens(response.Path + "\n" + response.Content),
			ImportTokens:  extractImportTokens(response.Content),
			FrameworkTags: extractFrameworkTags(response.Path, response.Content),
		})
	}
	return seeds, nil
}

func relatedDocs(cfg config.Runtime, request normalizedRequest, seeds []seedContext, seedRefs map[string]struct{}) ([]Result, error) {
	root, err := filesafe.NewRoot(cfg.DocsRoot, cfg.AllowSymlinks, cfg.MaxFileBytes)
	if err != nil {
		return nil, err
	}

	files, err := docs.ScanFilesystem(root, cfg.TextEncodingFallbacks)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, 32)
	for _, file := range files {
		for _, block := range file.Blocks {
			if _, ok := seedRefs[block.RefID]; ok {
				continue
			}

			scored := scoreCandidate(candidate{
				RefID:   block.RefID,
				Kind:    "md_heading_block",
				Path:    block.Path,
				Range:   Range{StartLine: block.StartLine, EndLine: block.EndLine},
				Tokens:  overlapTokens(block.Path + "\n" + block.Heading + "\n" + block.Content),
				Imports: extractImportTokens(block.Content),
				Tags:    extractFrameworkTags(block.Path, block.Content),
			}, seeds, request.Signals)
			if scored == nil {
				continue
			}
			results = append(results, *scored)
		}
	}

	return results, nil
}

func relatedCode(cfg config.Runtime, request normalizedRequest, seeds []seedContext, seedRefs map[string]struct{}, seedCodePaths map[string]struct{}) ([]Result, error) {
	sources, multipleRoots, err := source.BuildCodeSources(cfg)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, 32)
	for _, codeSource := range sources {
		paths, err := codeSource.ListFiles(request.CodeExtensions)
		if err != nil {
			return nil, err
		}

		for _, relativePath := range paths {
			textFile, err := codeSource.ReadText(relativePath, cfg.TextEncodingFallbacks)
			if err != nil {
				return nil, err
			}

			logicalPath := codeSource.BuildLogicalPath(relativePath, multipleRoots)
			lines := textutil.SplitLines(textFile.Text)
			if _, ok := seedCodePaths[logicalPath]; ok {
				continue
			}
			fileRef := ref.BuildFileRefID(logicalPath)
			if _, ok := seedRefs[fileRef]; ok {
				continue
			}

			candidateRange := codeCandidateRange(lines, seeds, request.SnippetLines)
			scored := scoreCandidate(candidate{
				RefID:   ref.BuildCodeRefID(logicalPath, candidateRange.StartLine),
				Kind:    "code_range",
				Path:    logicalPath,
				Range:   candidateRange,
				Tokens:  overlapTokens(logicalPath + "\n" + strings.Join(lines, "\n")),
				Imports: extractImportTokens(strings.Join(lines, "\n")),
				Tags:    extractFrameworkTags(logicalPath, strings.Join(lines, "\n")),
			}, seeds, request.Signals)
			if scored == nil {
				continue
			}
			results = append(results, *scored)
		}
	}

	return results, nil
}

// Assumption: when no seed token appears in a code file, the representative
// range falls back to the first line so path-only candidates can still surface.
func codeCandidateRange(lines []string, seeds []seedContext, snippetLines int) Range {
	focusLine := 1
	for _, seed := range seeds {
		for _, token := range seed.OverlapTokens {
			for idx, line := range lines {
				if strings.Contains(textsearch.Normalize(line), token) {
					start, end := textutil.CenteredRange(len(lines), idx+1, snippetLines)
					return Range{StartLine: start, EndLine: end}
				}
			}
		}
	}
	start, end := textutil.CenteredRange(len(lines), focusLine, snippetLines)
	return Range{StartLine: start, EndLine: end}
}

func scoreCandidate(item candidate, seeds []seedContext, enabled map[string]bool) *Result {
	total := 0.0
	reasons := make([]Reason, 0, 4)

	if enabled["imports"] {
		best := signalMatch{}
		for _, seed := range seeds {
			overlap := countOverlap(seed.ImportTokens, item.Imports)
			if overlap == 0 {
				continue
			}
			score := 0.50 * minFloat(1, float64(overlap)/3)
			if score > best.score {
				best = signalMatch{
					score: score,
					reason: &Reason{
						Type:   "imports",
						Detail: fmt.Sprintf("%d import overlap", overlap),
					},
				}
			}
		}
		if best.reason != nil {
			total += best.score
			reasons = append(reasons, *best.reason)
		}
	}

	if enabled["token_overlap"] {
		best := signalMatch{}
		for _, seed := range seeds {
			overlap := countSliceOverlap(seed.OverlapTokens, item.Tokens)
			if overlap == 0 {
				continue
			}
			score := 0.30 * minFloat(1, float64(overlap)/6)
			if score > best.score {
				best = signalMatch{
					score: score,
					reason: &Reason{
						Type:   "token_overlap",
						Detail: fmt.Sprintf("%d token overlap", overlap),
					},
				}
			}
		}
		if best.reason != nil {
			total += best.score
			reasons = append(reasons, *best.reason)
		}
	}

	if enabled["path_proximity"] {
		best := signalMatch{}
		for _, seed := range seeds {
			proximity := pathProximity(seed.Path, item.Path)
			if proximity <= 0 {
				continue
			}
			score := 0.15 * proximity
			if score > best.score {
				best = signalMatch{
					score: score,
					reason: &Reason{
						Type:   "path_proximity",
						Detail: fmt.Sprintf("path proximity %.2f", proximity),
					},
				}
			}
		}
		if best.reason != nil {
			total += best.score
			reasons = append(reasons, *best.reason)
		}
	}

	if enabled["same_file_hits"] {
		for _, seed := range seeds {
			if seed.Path == item.Path {
				total += 0.05
				reasons = append(reasons, Reason{
					Type:   "same_file_hits",
					Detail: "same file as seed",
				})
				break
			}
		}
	}

	if enabled["framework_links"] {
		best := signalMatch{}
		for _, seed := range seeds {
			overlap := countOverlap(seed.FrameworkTags, item.Tags)
			score := 0.0
			detail := ""
			if overlap > 0 {
				score = 0.35 * minFloat(1, float64(overlap)/3)
				detail = fmt.Sprintf("%d framework signal overlap", overlap)
			} else if complementaryFrameworkRelation(seed.FrameworkTags, item.Tags) {
				score = 0.2
				detail = "complementary framework wiring"
			}
			if score == 0 {
				continue
			}
			if seed.Kind != item.Kind && score < 0.2 {
				score = 0.2
			}
			if score > best.score {
				best = signalMatch{
					score: score,
					reason: &Reason{
						Type:   "framework_links",
						Detail: detail,
					},
				}
			}
		}
		if best.reason != nil {
			total += best.score
			reasons = append(reasons, *best.reason)
		}
	}

	if total <= 0 || len(reasons) == 0 {
		return nil
	}

	sort.Slice(reasons, func(i, j int) bool {
		return reasons[i].Type < reasons[j].Type
	})

	return &Result{
		RefID:   item.RefID,
		Kind:    item.Kind,
		Path:    item.Path,
		Range:   item.Range,
		Score:   total,
		Reasons: reasons,
	}
}

func overlapTokens(text string) []string {
	return textsearch.TokenizeOverlapText(text, maxSeedTokens)
}

func extractImportTokens(text string) map[string]struct{} {
	tokens := map[string]struct{}{}
	for _, line := range textutil.SplitLines(text) {
		if !isImportLine(line) {
			continue
		}
		for _, token := range tokenPattern.FindAllString(textsearch.Normalize(line), -1) {
			if numberOnlyToken.MatchString(token) {
				continue
			}
			tokens[token] = struct{}{}
		}
	}
	return tokens
}

func extractFrameworkTags(path string, content string) map[string]struct{} {
	tags := map[string]struct{}{}
	lowerPath := strings.ToLower(filepath.Base(path))
	lowerContent := strings.ToLower(content)
	if containsAny(lowerPath, "program", "startup", "main", "app", "routes", "router", "config") {
		tags["setup_file"] = struct{}{}
	}
	if strings.Contains(lowerContent, "builder.services") || strings.Contains(lowerContent, "addsingleton") || strings.Contains(lowerContent, "addscoped") || strings.Contains(lowerContent, "addtransient") || strings.Contains(lowerContent, "addauthentication") {
		tags["di_registration"] = struct{}{}
	}
	if strings.Contains(lowerContent, "getrequiredservice") || strings.Contains(lowerContent, "@inject") || strings.Contains(lowerContent, "[fromservices]") {
		tags["di_resolution"] = struct{}{}
	}
	if strings.Contains(lowerContent, "mapget(") || strings.Contains(lowerContent, "mappost(") || strings.Contains(lowerContent, "mapcontrollers(") || strings.Contains(lowerContent, ".get(") || strings.Contains(lowerContent, "[httpget") || strings.Contains(lowerContent, "@get(") {
		tags["route_decl"] = struct{}{}
	}
	if strings.Contains(lowerContent, "[authorize") || strings.Contains(lowerContent, "@controller") || strings.Contains(lowerContent, "@configuration") || strings.Contains(lowerContent, "[route") {
		tags["attribute"] = struct{}{}
	}
	if strings.Contains(lowerContent, "configuration") || strings.Contains(lowerContent, "appsettings") || configKeyPattern.MatchString(content) {
		tags["config"] = struct{}{}
	}
	return tags
}

func complementaryFrameworkRelation(left, right map[string]struct{}) bool {
	leftSetup := hasTag(left, "setup_file") || hasTag(left, "di_registration")
	rightSetup := hasTag(right, "setup_file") || hasTag(right, "di_registration")
	leftRuntime := hasTag(left, "route_decl") || hasTag(left, "attribute") || hasTag(left, "di_resolution")
	rightRuntime := hasTag(right, "route_decl") || hasTag(right, "attribute") || hasTag(right, "di_resolution")
	leftConfig := hasTag(left, "config")
	rightConfig := hasTag(right, "config")
	return (leftSetup && rightRuntime) || (rightSetup && leftRuntime) || (leftSetup && rightConfig) || (rightSetup && leftConfig)
}

func hasTag(tags map[string]struct{}, tag string) bool {
	_, ok := tags[tag]
	return ok
}

func isImportLine(line string) bool {
	switch {
	case importPattern.MatchString(line):
		return true
	case usingPattern.MatchString(line):
		return true
	case requirePattern.MatchString(line):
		return true
	default:
		return false
	}
}

func countOverlap(left, right map[string]struct{}) int {
	count := 0
	for token := range left {
		if _, ok := right[token]; ok {
			count++
		}
	}
	return count
}

func countSliceOverlap(left, right []string) int {
	rightSet := make(map[string]struct{}, len(right))
	for _, token := range right {
		rightSet[token] = struct{}{}
	}
	count := 0
	for _, token := range left {
		if _, ok := rightSet[token]; ok {
			count++
		}
	}
	return count
}

func pathProximity(left, right string) float64 {
	leftSegments := splitPathSegments(left)
	rightSegments := splitPathSegments(right)

	common := 0
	for common < len(leftSegments) && common < len(rightSegments) && leftSegments[common] == rightSegments[common] {
		common++
	}

	distance := (len(leftSegments) - common) + (len(rightSegments) - common)
	proximity := 1 - (float64(distance) / 10)
	if proximity < 0 {
		return 0
	}
	return proximity
}

func splitPathSegments(value string) []string {
	value = strings.TrimPrefix(ref.NormalizePath(value), "@")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments = append(segments, part)
	}
	if len(segments) > 1 && strings.HasPrefix(segments[0], "@") {
		segments[0] = strings.TrimPrefix(segments[0], "@")
	}
	return segments
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

func containsAny(value string, candidates ...string) bool {
	value = strings.ToLower(value)
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
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

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
