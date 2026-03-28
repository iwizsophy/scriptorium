package guide

import (
	"fmt"
	"strings"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/content"
	"github.com/iwizsophy/scriptorium/internal/flow"
	"github.com/iwizsophy/scriptorium/internal/related"
	"github.com/iwizsophy/scriptorium/internal/search"
)

const (
	defaultMaxSupportingRefs = 6
	defaultSnippetLines      = 20
)

var (
	searchExecute  = search.Execute
	relatedExecute = related.Execute
	flowExecute    = flow.Execute
	contentExecute = content.Execute
)

type Request struct {
	Topic      string `json:"topic"`
	Framework  string `json:"framework,omitempty"`
	Language   string `json:"language,omitempty"`
	Preference string `json:"preference,omitempty"`
	MaxRefs    int    `json:"maxRefs,omitempty"`
}

type Response struct {
	Question    string          `json:"question"`
	Query       string          `json:"query"`
	Guide       string          `json:"guide"`
	Docs        []SupportingRef `json:"docs"`
	SampleCode  []SupportingRef `json:"sampleCode"`
	Assumptions []string        `json:"assumptions"`
	Confidence  string          `json:"confidence"`
}

type SupportingRef struct {
	RefID   string `json:"refId"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Range   Range  `json:"range"`
	Snippet string `json:"snippet"`
	Origin  string `json:"origin"`
}

type Range struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

type normalizedRequest struct {
	Topic          string
	Framework      string
	Language       string
	Preference     string
	Query          string
	CodeExtensions []string
	MaxRefs        int
}

func Execute(cfg config.Runtime, request Request) (Response, error) {
	normalized, err := normalizeRequest(request, cfg.CodeExtensions)
	if err != nil {
		return Response{}, err
	}

	question := buildQuestion(normalized)
	docsWanted, codeWanted := supportingBudgets(normalized.Preference, normalized.MaxRefs)
	searchTopK := max(4, normalized.MaxRefs*2)

	docsSearch, err := searchExecute(cfg, search.Request{
		Query:        normalized.Query,
		Extensions:   []string{".md"},
		TopK:         searchTopK,
		SnippetLines: defaultSnippetLines,
		Mode:         "implementation",
	})
	if err != nil {
		return Response{}, err
	}

	codeSearch, err := searchExecute(cfg, search.Request{
		Query:        normalized.Query,
		Extensions:   normalized.CodeExtensions,
		TopK:         searchTopK,
		SnippetLines: defaultSnippetLines,
		Mode:         "implementation",
	})
	if err != nil {
		return Response{}, err
	}

	docsRefs := takeSearchRefs(docsSearch.Results, "md_heading_block", docsWanted, "search")
	codeRefs := takeSearchRefs(codeSearch.Results, "code_range", codeWanted, "search")

	seeds := initialSeeds(docsRefs, codeRefs)
	if len(seeds) > 0 {
		relatedRefs, err := loadRelatedRefs(cfg, normalized, seeds)
		if err != nil {
			return Response{}, err
		}
		docsRefs = appendSupportingRefs(docsRefs, filterSupportingRefs(relatedRefs, "md_heading_block"), docsWanted)
		codeRefs = appendSupportingRefs(codeRefs, filterSupportingRefs(relatedRefs, "code_range"), codeWanted)
	}

	flowRefs := supportingRefIDs(docsRefs, codeRefs)
	flowResponse, err := flowExecute(cfg, flow.Request{
		Topic:    normalized.Topic,
		Seeds:    flowRefs,
		Style:    "brief",
		MaxSteps: max(3, normalized.MaxRefs),
	})
	if err != nil {
		return Response{}, err
	}

	assumptions := append([]string{}, flowResponse.Assumptions...)
	if len(docsRefs) == 0 {
		assumptions = append(assumptions, "No supporting docs refs matched the current corpus for this question.")
	}
	if len(codeRefs) == 0 {
		assumptions = append(assumptions, "No supporting sample code refs matched the current corpus for this question.")
	}
	if len(flowRefs) == 0 {
		assumptions = append(assumptions, "The guide falls back to the requested topic because no supporting refs were found.")
	}

	return Response{
		Question:    question,
		Query:       normalized.Query,
		Guide:       renderGuide(normalized.Topic, flowResponse.Flow),
		Docs:        docsRefs,
		SampleCode:  codeRefs,
		Assumptions: dedupeStrings(assumptions),
		Confidence:  adjustedConfidence(flowResponse.Confidence, len(docsRefs) > 0, len(codeRefs) > 0),
	}, nil
}

func normalizeRequest(request Request, defaultCodeExtensions []string) (normalizedRequest, error) {
	topic := strings.TrimSpace(request.Topic)
	if topic == "" {
		return normalizedRequest{}, fmt.Errorf("topic is required")
	}

	preference := normalizePreference(request.Preference)
	framework := strings.TrimSpace(request.Framework)
	language := strings.TrimSpace(request.Language)
	queryParts := []string{topic}
	if framework != "" {
		queryParts = append(queryParts, framework)
	}
	if language != "" {
		queryParts = append(queryParts, language)
	}

	codeExtensions := append([]string(nil), defaultCodeExtensions...)
	if languageExtensions := extensionsForLanguage(language); len(languageExtensions) > 0 {
		codeExtensions = languageExtensions
	}

	return normalizedRequest{
		Topic:          topic,
		Framework:      framework,
		Language:       language,
		Preference:     preference,
		Query:          strings.Join(queryParts, " "),
		CodeExtensions: dedupeStrings(codeExtensions),
		MaxRefs:        clamp(request.MaxRefs, 2, 12, defaultMaxSupportingRefs),
	}, nil
}

func normalizePreference(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "docs", "documentation":
		return "docs"
	case "samples", "sample_code", "code":
		return "samples"
	default:
		return "balanced"
	}
}

func extensionsForLanguage(value string) []string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "go", "golang":
		return []string{".go"}
	case "typescript", "ts":
		return []string{".ts", ".tsx"}
	case "javascript", "js":
		return []string{".js", ".jsx"}
	case "c#", "csharp":
		return []string{".cs"}
	case "python", "py":
		return []string{".py"}
	case "java":
		return []string{".java"}
	default:
		return nil
	}
}

func buildQuestion(request normalizedRequest) string {
	parts := []string{request.Topic}
	if request.Framework != "" {
		parts = append(parts, "in "+request.Framework)
	}
	if request.Language != "" {
		parts = append(parts, "using "+request.Language)
	}
	return strings.Join(parts, " ")
}

func supportingBudgets(preference string, maxRefs int) (int, int) {
	switch preference {
	case "docs":
		return max(1, (maxRefs*2)/3), max(1, maxRefs-max(1, (maxRefs*2)/3))
	case "samples":
		return max(1, maxRefs-max(1, (maxRefs*2)/3)), max(1, (maxRefs*2)/3)
	default:
		docs := maxRefs / 2
		if docs == 0 {
			docs = 1
		}
		return docs, max(1, maxRefs-docs)
	}
}

func takeSearchRefs(results []search.Result, kind string, limit int, origin string) []SupportingRef {
	refs := make([]SupportingRef, 0, min(limit, len(results)))
	for _, result := range results {
		if result.Kind != kind {
			continue
		}
		refs = append(refs, SupportingRef{
			RefID:   result.RefID,
			Kind:    result.Kind,
			Path:    result.Path,
			Range:   Range{StartLine: result.Range.StartLine, EndLine: result.Range.EndLine},
			Snippet: result.Snippet,
			Origin:  origin,
		})
		if len(refs) == limit {
			break
		}
	}
	return refs
}

func initialSeeds(docsRefs, codeRefs []SupportingRef) []string {
	seeds := make([]string, 0, 4)
	if len(docsRefs) > 0 {
		seeds = append(seeds, docsRefs[0].RefID)
	}
	if len(codeRefs) > 0 {
		seeds = append(seeds, codeRefs[0].RefID)
	}
	if len(docsRefs) > 1 {
		seeds = append(seeds, docsRefs[1].RefID)
	}
	if len(codeRefs) > 1 {
		seeds = append(seeds, codeRefs[1].RefID)
	}
	return dedupeStrings(seeds)
}

func loadRelatedRefs(cfg config.Runtime, request normalizedRequest, seeds []string) ([]SupportingRef, error) {
	extensions := append([]string{".md"}, request.CodeExtensions...)
	relatedResponse, err := relatedExecute(cfg, related.Request{
		Seeds:        seeds,
		Extensions:   dedupeStrings(extensions),
		Budget:       max(6, request.MaxRefs*2),
		SnippetLines: defaultSnippetLines,
		Mode:         "implementation",
	})
	if err != nil {
		return nil, err
	}

	refs := make([]SupportingRef, 0, len(relatedResponse.Related))
	for _, item := range relatedResponse.Related {
		contentResponse, err := contentExecute(cfg, content.Request{
			RefID:        item.RefID,
			Mode:         "snippet",
			SnippetLines: defaultSnippetLines,
			MaxLines:     400,
		})
		if err != nil {
			return nil, err
		}
		refRange := Range{}
		if len(contentResponse.Ranges) > 0 {
			refRange = Range{
				StartLine: contentResponse.Ranges[0].StartLine,
				EndLine:   contentResponse.Ranges[0].EndLine,
			}
		}
		refs = append(refs, SupportingRef{
			RefID:   contentResponse.RefID,
			Kind:    item.Kind,
			Path:    item.Path,
			Range:   refRange,
			Snippet: contentResponse.Content,
			Origin:  "related",
		})
	}
	return refs, nil
}

func filterSupportingRefs(refs []SupportingRef, kind string) []SupportingRef {
	filtered := make([]SupportingRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == kind {
			filtered = append(filtered, ref)
		}
	}
	return filtered
}

func appendSupportingRefs(existing, candidates []SupportingRef, limit int) []SupportingRef {
	if len(existing) >= limit {
		return existing[:limit]
	}
	seen := make(map[string]struct{}, len(existing))
	for _, ref := range existing {
		seen[ref.RefID] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := seen[candidate.RefID]; ok {
			continue
		}
		seen[candidate.RefID] = struct{}{}
		existing = append(existing, candidate)
		if len(existing) == limit {
			break
		}
	}
	return existing
}

func supportingRefIDs(groups ...[]SupportingRef) []string {
	refs := make([]string, 0, 8)
	for _, group := range groups {
		for _, ref := range group {
			refs = append(refs, ref.RefID)
		}
	}
	return dedupeStrings(refs)
}

func renderGuide(topic string, steps []flow.Step) string {
	if len(steps) == 0 {
		return fmt.Sprintf("No grounded implementation steps were found for %q.", topic)
	}
	lines := make([]string, 0, len(steps))
	for _, step := range steps {
		description := strings.TrimSpace(step.Description)
		title := strings.TrimSpace(step.Title)
		switch {
		case title == "":
			lines = append(lines, fmt.Sprintf("%d. %s", step.Step, description))
		case description == "" || description == title:
			lines = append(lines, fmt.Sprintf("%d. %s", step.Step, title))
		default:
			lines = append(lines, fmt.Sprintf("%d. %s: %s", step.Step, title, description))
		}
	}
	return strings.Join(lines, "\n")
}

func adjustedConfidence(confidence string, hasDocs bool, hasCode bool) string {
	if hasDocs && hasCode {
		return confidence
	}
	switch confidence {
	case "high":
		return "medium"
	case "medium":
		if hasDocs || hasCode {
			return "medium"
		}
		return "low"
	default:
		return "low"
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
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
