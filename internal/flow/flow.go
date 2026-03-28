package flow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/content"
	"github.com/iwizsophy/scriptorium/internal/docs"
	"github.com/iwizsophy/scriptorium/internal/ref"
	"github.com/iwizsophy/scriptorium/internal/textutil"
	"golang.org/x/text/unicode/norm"
)

const defaultMaxSteps = 12

type Request struct {
	Topic    string   `json:"topic,omitempty"`
	Seeds    []string `json:"seeds,omitempty"`
	Sources  []string `json:"sources,omitempty"`
	Style    string   `json:"style,omitempty"`
	MaxSteps int      `json:"maxSteps,omitempty"`
}

type Response struct {
	Flow        []Step   `json:"flow"`
	Assumptions []string `json:"assumptions"`
	Confidence  string   `json:"confidence"`
}

type Step struct {
	Step        int      `json:"step"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Refs        []string `json:"refs"`
}

type normalizedRequest struct {
	Topic    string
	Refs     []string
	Style    string
	MaxSteps int
}

type extractedStep struct {
	Order       int
	Title       string
	Description string
	Refs        []string
	Explicit    bool
	SourceKind  string
}

type mergedStep struct {
	Order       int
	Title       string
	Description string
	Refs        []string
	Explicit    bool
	SourceKinds map[string]struct{}
}

var (
	numberedStepPattern = regexp.MustCompile(`^\s*(\d+([.)、])\s+)(.+)$`)
	stepLabelPattern    = regexp.MustCompile(`^\s*((手順|ステップ)\s*\d+\s*[:：\-]\s*)(.+)$`)
	bulletPattern       = regexp.MustCompile(`^\s*[-*+]\s+(.+)$`)
	functionPattern     = regexp.MustCompile(`^\s*(export\s+)?(async\s+)?function\s+([A-Za-z_][\w]*)`)
	arrowPattern        = regexp.MustCompile(`^\s*(const|let|var)\s+([A-Za-z_][\w]*)\s*=\s*(async\s*)?.*=>`)
	csharpPattern       = regexp.MustCompile(`^\s*(public|private|protected|internal|static|virtual|override|sealed|\s)+[\w<>\[\]\?, ]+\s+([A-Za-z_][\w]*)\s*\(`)
	classPattern        = regexp.MustCompile(`^\s*(export\s+)?class\s+([A-Za-z_][\w]*)`)
	minimalPattern      = regexp.MustCompile(`\bMap(Get|Post|Put|Delete|Patch)\s*\(\s*"([^"]*)"`)
	expressPattern      = regexp.MustCompile(`\.(get|post|put|delete|patch)\s*\(\s*["']([^"']+)["']`)
)

func Execute(cfg config.Runtime, request Request) (Response, error) {
	normalized := normalizeRequest(request)
	steps := make([]extractedStep, 0, normalized.MaxSteps)
	assumptions := make([]string, 0, 4)

	for idx, refID := range normalized.Refs {
		parsed, err := ref.ParseRefID(refID)
		if err != nil {
			return Response{}, err
		}

		switch parsed.Kind {
		case "md":
			response, err := content.Execute(cfg, content.Request{
				RefID:        refID,
				Mode:         "full",
				SnippetLines: 80,
				MaxLines:     1500,
			})
			if err != nil {
				return Response{}, err
			}
			markdownSteps, markdownAssumptions := extractMarkdownSteps(response, idx+1)
			steps = append(steps, markdownSteps...)
			assumptions = append(assumptions, markdownAssumptions...)
		case "code":
			response, err := content.Execute(cfg, content.Request{
				RefID:        refID,
				Mode:         "full",
				SnippetLines: 80,
				MaxLines:     1500,
			})
			if err != nil {
				return Response{}, err
			}
			codeSteps, codeAssumptions := extractCodeSteps(refID, parsed.Line, response, normalized.Style, idx+1)
			steps = append(steps, codeSteps...)
			assumptions = append(assumptions, codeAssumptions...)
		}
	}

	merged := mergeSteps(steps)
	sortMergedSteps(merged)
	if len(merged) > normalized.MaxSteps {
		merged = merged[:normalized.MaxSteps]
	}

	flowSteps := make([]Step, 0, len(merged))
	for idx, item := range merged {
		flowSteps = append(flowSteps, Step{
			Step:        idx + 1,
			Title:       item.Title,
			Description: item.Description,
			Refs:        dedupeStrings(item.Refs),
		})
	}

	if len(flowSteps) == 0 && normalized.Topic != "" {
		return Response{
			Flow: []Step{{
				Step:        1,
				Title:       normalized.Topic,
				Description: "参照がないため topic だけを返しています",
				Refs:        []string{},
			}},
			Assumptions: []string{"参照が与えられていないため、topic のみを返しました。"},
			Confidence:  "low",
		}, nil
	}

	return Response{
		Flow:        flowSteps,
		Assumptions: dedupeStrings(assumptions),
		Confidence:  confidenceFor(merged),
	}, nil
}

func normalizeRequest(request Request) normalizedRequest {
	refs := make([]string, 0, len(request.Seeds)+len(request.Sources))
	seen := map[string]struct{}{}
	for _, refID := range append(append([]string{}, request.Seeds...), request.Sources...) {
		refID = strings.TrimSpace(refID)
		if refID == "" {
			continue
		}
		if _, ok := seen[refID]; ok {
			continue
		}
		seen[refID] = struct{}{}
		refs = append(refs, refID)
	}

	style := normalizeStyle(request.Style)
	maxSteps := clamp(request.MaxSteps, 1, 50, defaultMaxSteps)
	if len(refs) > maxSteps {
		refs = refs[:maxSteps]
	}

	return normalizedRequest{
		Topic:    strings.TrimSpace(request.Topic),
		Refs:     refs,
		Style:    style,
		MaxSteps: maxSteps,
	}
}

func normalizeStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "detailed", "detail", "詳細":
		return "detailed"
	case "brief", "concise", "簡潔":
		return "brief"
	default:
		return "default"
	}
}

func extractMarkdownSteps(response content.Response, baseOrder int) ([]extractedStep, []string) {
	file := docs.ParseMarkdownFile(response.Path, response.Content)
	if len(file.Blocks) == 0 {
		return nil, []string{"文書に明示的な手順がないため、見出しと本文から要点を抽出しました。"}
	}

	lines := textutil.SplitLines(response.Content)
	if steps := numberedOrLabeledSteps(lines, response.RefID, baseOrder); len(steps) > 0 {
		return steps, nil
	}
	if steps := bulletSteps(lines, response.RefID, baseOrder); len(steps) > 0 {
		return steps, nil
	}
	if len(file.Blocks) > 1 {
		steps := make([]extractedStep, 0, len(file.Blocks)-1)
		for idx, block := range file.Blocks[1:] {
			description := firstParagraph(block.Content, 1)
			if description == "" {
				description = block.Heading
			}
			steps = append(steps, extractedStep{
				Order:       baseOrder*100 + idx,
				Title:       block.Heading,
				Description: description,
				Refs:        []string{response.RefID},
				Explicit:    false,
				SourceKind:  "md",
			})
		}
		return steps, []string{"文書に明示的な番号付き手順がないため、見出し構造から流れを抽出しました。"}
	}

	baseHeading := file.Blocks[0].Heading
	description := firstParagraph(response.Content, 2)
	if description == "" {
		description = baseHeading
	}
	return []extractedStep{{
		Order:       baseOrder * 100,
		Title:       baseHeading,
		Description: description,
		Refs:        []string{response.RefID},
		Explicit:    false,
		SourceKind:  "md",
	}}, []string{"文書に明示的な手順がないため、見出しと本文から要点を抽出しました。"}
}

func numberedOrLabeledSteps(lines []string, refID string, baseOrder int) []extractedStep {
	steps := make([]extractedStep, 0, 8)
	for idx, line := range lines {
		switch {
		case numberedStepPattern.MatchString(line):
			matches := numberedStepPattern.FindStringSubmatch(strings.TrimSpace(line))
			text := strings.TrimSpace(matches[3])
			steps = append(steps, extractedStep{
				Order:       baseOrder*100 + idx,
				Title:       text,
				Description: text,
				Refs:        []string{refID},
				Explicit:    true,
				SourceKind:  "md",
			})
		case stepLabelPattern.MatchString(line):
			matches := stepLabelPattern.FindStringSubmatch(strings.TrimSpace(line))
			text := strings.TrimSpace(matches[3])
			steps = append(steps, extractedStep{
				Order:       baseOrder*100 + idx,
				Title:       text,
				Description: text,
				Refs:        []string{refID},
				Explicit:    true,
				SourceKind:  "md",
			})
		}
	}
	return steps
}

func bulletSteps(lines []string, refID string, baseOrder int) []extractedStep {
	steps := make([]extractedStep, 0, 8)
	for idx, line := range lines {
		matches := bulletPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		text := strings.TrimSpace(matches[1])
		steps = append(steps, extractedStep{
			Order:       baseOrder*100 + idx,
			Title:       text,
			Description: text,
			Refs:        []string{refID},
			Explicit:    true,
			SourceKind:  "md",
		})
	}
	return steps
}

func firstParagraph(text string, count int) string {
	paragraphs := make([]string, 0, count)
	current := make([]string, 0, 4)
	for _, line := range textutil.SplitLines(text) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(current) > 0 {
				paragraphs = append(paragraphs, strings.Join(current, " "))
				current = current[:0]
				if len(paragraphs) == count {
					break
				}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		current = append(current, trimmed)
	}
	if len(paragraphs) < count && len(current) > 0 {
		paragraphs = append(paragraphs, strings.Join(current, " "))
	}
	return strings.Join(paragraphs, " ")
}

func extractCodeSteps(refID string, focusLine int, response content.Response, style string, baseOrder int) ([]extractedStep, []string) {
	lines := textutil.SplitLines(response.Content)
	title := codeTitle(lines, focusLine, response.Path)
	operations := codeOperations(lines, focusLine, style)

	description := ""
	if len(operations) > 0 {
		description = strings.Join(operations, " -> ")
	} else {
		description = firstNonEmpty(lines)
	}
	if description == "" {
		description = response.Path
	}

	assumptions := []string{"コード参照はシグネチャと近傍の呼び出しから決定的に要約しています。"}
	if response.Truncated {
		assumptions = append(assumptions, "一部コード参照は抜粋から要約しました。")
	}

	return []extractedStep{{
		Order:       baseOrder * 100,
		Title:       title,
		Description: description,
		Refs:        []string{refID},
		Explicit:    false,
		SourceKind:  "code",
	}}, assumptions
}

func codeTitle(lines []string, focusLine int, path string) string {
	if title := routeDescription(lines, focusLine); title != "" {
		return title
	}
	if title := signatureTitle(lines, focusLine); title != "" {
		return title
	}
	if operations := codeOperations(lines, focusLine, "brief"); len(operations) > 0 {
		return operations[0]
	}
	if line := firstNonEmpty(lines); line != "" {
		return line
	}
	return path
}

func routeDescription(lines []string, focusLine int) string {
	for line := min(len(lines), focusLine); line >= 1; line-- {
		trimmed := strings.TrimSpace(lines[line-1])
		if matches := minimalPattern.FindStringSubmatch(trimmed); matches != nil {
			return fmt.Sprintf("Handle %s %s route", strings.ToUpper(matches[1]), matches[2])
		}
		if matches := expressPattern.FindStringSubmatch(trimmed); matches != nil {
			return fmt.Sprintf("Handle %s %s route", strings.ToUpper(matches[1]), matches[2])
		}
	}
	return ""
}

func signatureTitle(lines []string, focusLine int) string {
	for line := min(len(lines), focusLine); line >= 1; line-- {
		trimmed := strings.TrimSpace(lines[line-1])
		switch {
		case functionPattern.MatchString(trimmed):
			return functionPattern.FindStringSubmatch(trimmed)[3]
		case arrowPattern.MatchString(trimmed):
			return arrowPattern.FindStringSubmatch(trimmed)[2]
		case csharpPattern.MatchString(trimmed):
			return csharpPattern.FindStringSubmatch(trimmed)[2]
		case classPattern.MatchString(trimmed):
			return classPattern.FindStringSubmatch(trimmed)[2]
		}
	}
	return ""
}

func codeOperations(lines []string, focusLine int, style string) []string {
	limit := 2
	switch style {
	case "brief":
		limit = 1
	case "detailed":
		limit = 3
	}

	operations := make([]string, 0, limit)
	start := max(1, focusLine-20)
	end := min(len(lines), focusLine+20)
	for line := start; line <= end; line++ {
		if op := describeOperation(strings.TrimSpace(lines[line-1])); op != "" {
			operations = append(operations, op)
		}
		if len(operations) == limit {
			break
		}
	}
	return operations
}

func describeOperation(line string) string {
	if line == "" {
		return ""
	}
	if matches := minimalPattern.FindStringSubmatch(line); matches != nil {
		return fmt.Sprintf("route handling %s %s", strings.ToUpper(matches[1]), matches[2])
	}
	if matches := expressPattern.FindStringSubmatch(line); matches != nil {
		return fmt.Sprintf("route handling %s %s", strings.ToUpper(matches[1]), matches[2])
	}
	if strings.HasPrefix(line, "return ") {
		return strings.TrimSpace(line)
	}
	if strings.Contains(line, "await ") {
		return strings.TrimSpace(line)
	}
	if strings.Contains(line, "throw ") {
		return strings.TrimSpace(line)
	}
	if strings.Contains(line, " new ") || strings.HasPrefix(line, "new ") {
		return strings.TrimSpace(line)
	}
	if strings.HasPrefix(line, "if (") || strings.Contains(line, "if (") {
		return strings.TrimSpace(line)
	}
	if matched, _ := regexp.MatchString(`\b[A-Za-z_][\w]*\s*=\s*[A-Za-z_][\w.]*\s*\(`, line); matched {
		return strings.TrimSpace(line)
	}
	if matched, _ := regexp.MatchString(`\b[A-Za-z_][\w.]*\s*\(`, line); matched {
		return strings.TrimSpace(line)
	}
	return ""
}

func firstNonEmpty(lines []string) string {
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mergeSteps(steps []extractedStep) []mergedStep {
	mergedByKey := map[string]*mergedStep{}
	order := make([]string, 0, len(steps))
	for _, step := range steps {
		if strings.TrimSpace(step.Title) == "" && strings.TrimSpace(step.Description) == "" {
			continue
		}
		key := normalizeText(step.Title) + "|" + normalizeText(step.Description)
		if existing, ok := mergedByKey[key]; ok {
			if step.Order < existing.Order {
				existing.Order = step.Order
			}
			if len(step.Description) > len(existing.Description) {
				existing.Description = step.Description
			}
			existing.Refs = append(existing.Refs, step.Refs...)
			existing.Explicit = existing.Explicit || step.Explicit
			existing.SourceKinds[step.SourceKind] = struct{}{}
			continue
		}

		mergedByKey[key] = &mergedStep{
			Order:       step.Order,
			Title:       step.Title,
			Description: step.Description,
			Refs:        append([]string(nil), step.Refs...),
			Explicit:    step.Explicit,
			SourceKinds: map[string]struct{}{step.SourceKind: {}},
		}
		order = append(order, key)
	}

	merged := make([]mergedStep, 0, len(order))
	for _, key := range order {
		merged = append(merged, *mergedByKey[key])
	}
	return merged
}

func sortMergedSteps(steps []mergedStep) {
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].Order != steps[j].Order {
			return steps[i].Order < steps[j].Order
		}
		return normalizeText(steps[i].Title) < normalizeText(steps[j].Title)
	})
}

func confidenceFor(steps []mergedStep) string {
	explicitCount := 0
	sourceKinds := map[string]struct{}{}
	for _, step := range steps {
		if step.Explicit {
			explicitCount++
		}
		for kind := range step.SourceKinds {
			sourceKinds[kind] = struct{}{}
		}
	}

	if len(steps) >= 3 && explicitCount >= 2 {
		if _, md := sourceKinds["md"]; md {
			if _, code := sourceKinds["code"]; code {
				return "high"
			}
		}
	}
	if len(steps) >= 2 {
		if explicitCount >= 1 {
			return "medium"
		}
		if _, md := sourceKinds["md"]; md {
			return "medium"
		}
	}
	return "low"
}

func normalizeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
	return strings.Join(strings.Fields(value), " ")
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
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
