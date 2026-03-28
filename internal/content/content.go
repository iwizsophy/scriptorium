package content

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/docs"
	"github.com/silvekt/scriptorium/internal/docsindex"
	"github.com/silvekt/scriptorium/internal/filesafe"
	"github.com/silvekt/scriptorium/internal/ref"
	"github.com/silvekt/scriptorium/internal/source"
	"github.com/silvekt/scriptorium/internal/textutil"
)

const (
	defaultSnippetLines = 40
	defaultMaxLines     = 1000
)

type Request struct {
	RefID        string `json:"refId"`
	Mode         string `json:"mode,omitempty"`
	SnippetLines int    `json:"snippetLines,omitempty"`
	MaxLines     int    `json:"maxLines,omitempty"`
}

type Response struct {
	RefID       string  `json:"refId"`
	Kind        string  `json:"kind"`
	Path        string  `json:"path"`
	ContentType string  `json:"contentType"`
	Mode        string  `json:"mode"`
	MaxLines    int     `json:"maxLines"`
	Truncated   bool    `json:"truncated"`
	Reason      *string `json:"reason"`
	Ranges      []Range `json:"ranges"`
	Content     string  `json:"content"`
}

type Range struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

type normalizedRequest struct {
	RefID        string
	Mode         string
	SnippetLines int
	MaxLines     int
}

var (
	orderedListPattern = regexp.MustCompile(`^\s*\d+([.)、])\s+`)
	stepLabelPattern   = regexp.MustCompile(`^\s*(手順|ステップ)\s*\d+\s*[:：\-]`)
	bulletListPattern  = regexp.MustCompile(`^\s*[-*+]\s+`)
	headingLinePattern = regexp.MustCompile(`^#{1,6}[ \t]+`)

	functionPattern = regexp.MustCompile(`^\s*(export\s+)?(async\s+)?function\s+\w+`)
	arrowPattern    = regexp.MustCompile(`^\s*(const|let|var)\s+\w+\s*=\s*(async\s*)?.*=>`)
	csharpPattern   = regexp.MustCompile(`^\s*(public|private|protected|internal|static|virtual|override|sealed|\s)+[\w<>\[\]\?, ]+\s+\w+\s*\(`)
	classPattern    = regexp.MustCompile(`^\s*(export\s+)?class\s+\w+`)
	minimalPattern  = regexp.MustCompile(`\bMap(Get|Post|Put|Delete|Patch)\s*\(`)
	expressPattern  = regexp.MustCompile(`\.(get|post|put|delete|patch)\s*\(`)
)

func Execute(cfg config.Runtime, request Request) (Response, error) {
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Response{}, err
	}

	parsedRef, err := ref.ParseRefID(normalized.RefID)
	if err != nil {
		return Response{}, err
	}

	switch parsedRef.Kind {
	case "md":
		return resolveMarkdown(cfg, normalized, parsedRef)
	case "code":
		return resolveCode(cfg, normalized, parsedRef)
	}
	return resolveFile(cfg, normalized, parsedRef)
}

func normalizeRequest(request Request) (normalizedRequest, error) {
	refID := strings.TrimSpace(request.RefID)
	if refID == "" {
		return normalizedRequest{}, fmt.Errorf("refId is required")
	}

	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = "snippet"
	}
	switch mode {
	case "snippet", "full", "multi_range":
	default:
		return normalizedRequest{}, fmt.Errorf("invalid mode: %s", request.Mode)
	}

	return normalizedRequest{
		RefID:        refID,
		Mode:         mode,
		SnippetLines: clamp(request.SnippetLines, 1, 200, defaultSnippetLines),
		MaxLines:     clamp(request.MaxLines, 1, 5000, defaultMaxLines),
	}, nil
}

func resolveMarkdown(cfg config.Runtime, request normalizedRequest, parsedRef ref.ParsedRef) (Response, error) {
	if request.Mode != "multi_range" {
		if state := docsindex.OpenRuntimeSet(cfg); state.Enabled {
			if response, ok := resolveMarkdownFromIndexes(state.Indexes, request); ok {
				return response, nil
			}
		}
	}

	root, err := filesafe.NewRoot(cfg.DocsRoot, cfg.AllowSymlinks, cfg.MaxFileBytes)
	if err != nil {
		return Response{}, err
	}

	textFile, err := root.ReadText(parsedRef.Path, cfg.TextEncodingFallbacks)
	if err != nil {
		return Response{}, err
	}

	file := docs.ParseMarkdownFile(textFile.Path, textFile.Text)
	var block *docs.Block
	for idx := range file.Blocks {
		if file.Blocks[idx].HeadingSlug == parsedRef.HeadingSlug {
			block = &file.Blocks[idx]
			break
		}
	}
	if block == nil {
		return Response{}, fmt.Errorf("markdown block not found: %s", request.RefID)
	}

	blockLines := textutil.SplitLines(block.Content)
	switch request.Mode {
	case "snippet":
		return markdownSnippetResponse(request, block, blockLines), nil
	case "full":
		return markdownFullResponse(request, block, blockLines), nil
	case "multi_range":
		return markdownMultiRangeResponse(request, block, blockLines), nil
	default:
		return Response{}, fmt.Errorf("unsupported mode: %s", request.Mode)
	}
}

func resolveMarkdownFromIndexes(indexes []docsindex.Artifact, request normalizedRequest) (Response, bool) {
	for _, index := range indexes {
		block, ok := index.FindBlock(request.RefID)
		if !ok {
			continue
		}

		blockLines := textutil.SplitLines(block.Content)
		docBlock := &docs.Block{
			RefID:       block.RefID,
			Path:        block.Path,
			HeadingSlug: block.HeadingSlug,
			Heading:     block.Heading,
			Level:       block.Level,
			StartLine:   block.StartLine,
			EndLine:     block.EndLine,
			Content:     block.Content,
		}
		switch request.Mode {
		case "snippet":
			return markdownSnippetResponse(request, docBlock, blockLines), true
		case "full":
			return markdownFullResponse(request, docBlock, blockLines), true
		}
	}
	return Response{}, false
}

func markdownSnippetResponse(request normalizedRequest, block *docs.Block, blockLines []string) Response {
	endOffset := min(len(blockLines), request.SnippetLines)
	endLine := block.StartLine + endOffset - 1
	if endOffset == 0 {
		endLine = block.StartLine
	}

	return Response{
		RefID:       request.RefID,
		Kind:        "md_heading_block",
		Path:        block.Path,
		ContentType: "text/markdown",
		Mode:        "snippet",
		MaxLines:    request.MaxLines,
		Truncated:   false,
		Ranges:      []Range{{StartLine: block.StartLine, EndLine: endLine}},
		Content:     strings.Join(blockLines[:endOffset], "\n"),
	}
}

func markdownFullResponse(request normalizedRequest, block *docs.Block, blockLines []string) Response {
	if len(blockLines) <= request.MaxLines {
		return Response{
			RefID:       request.RefID,
			Kind:        "md_heading_block",
			Path:        block.Path,
			ContentType: "text/markdown",
			Mode:        "full",
			MaxLines:    request.MaxLines,
			Truncated:   false,
			Ranges:      []Range{{StartLine: block.StartLine, EndLine: block.EndLine}},
			Content:     strings.Join(blockLines, "\n"),
		}
	}

	response := markdownSnippetResponse(request, block, blockLines)
	response.Truncated = true
	response.Reason = reasonPtr("full mode exceeds maxLines; fell back to snippet")
	return response
}

// Assumption: representative markdown ranges are emitted as single-line anchors
// because the spec defines the candidate line types but not the exact window size.
func markdownMultiRangeResponse(request normalizedRequest, block *docs.Block, blockLines []string) Response {
	candidates := []int{block.StartLine}
	for offset, line := range blockLines {
		if offset == 0 {
			continue
		}
		absoluteLine := block.StartLine + offset
		switch {
		case orderedListPattern.MatchString(line):
			candidates = append(candidates, absoluteLine)
		case stepLabelPattern.MatchString(line):
			candidates = append(candidates, absoluteLine)
		case bulletListPattern.MatchString(line):
			candidates = append(candidates, absoluteLine)
		case headingLinePattern.MatchString(line):
			candidates = append(candidates, absoluteLine)
		}
	}

	if len(candidates) == 1 {
		for offset, line := range blockLines {
			if offset == 0 || strings.TrimSpace(line) == "" {
				continue
			}
			candidates = append(candidates, block.StartLine+offset)
			break
		}
	}

	ranges := make([]Range, 0, min(4, len(candidates)))
	seen := map[int]struct{}{}
	for _, line := range candidates {
		// COVERAGE_EXCEPTION: the current markdown candidate selection only emits
		// each absolute line once, so exercising this defensive dedupe branch
		// would require test-only candidate injection that does not occur through
		// the public content resolution flow.
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		ranges = append(ranges, Range{StartLine: line, EndLine: line})
		if len(ranges) == 4 {
			break
		}
	}

	return Response{
		RefID:       request.RefID,
		Kind:        "md_heading_block",
		Path:        block.Path,
		ContentType: "text/markdown",
		Mode:        "multi_range",
		MaxLines:    request.MaxLines,
		Truncated:   true,
		Reason:      reasonPtr("representative ranges selected for markdown block"),
		Ranges:      ranges,
		Content:     renderMarkedRanges(blockLines, block.StartLine, ranges),
	}
}

func resolveCode(cfg config.Runtime, request normalizedRequest, parsedRef ref.ParsedRef) (Response, error) {
	sourceEntry, relativePath, multipleRoots, err := source.ResolveLogicalPath(cfg, parsedRef.Path)
	if err != nil {
		return Response{}, err
	}

	textFile, err := sourceEntry.ReadText(relativePath, cfg.TextEncodingFallbacks)
	if err != nil {
		return Response{}, err
	}

	lines := textutil.SplitLines(textFile.Text)
	if parsedRef.Line < 1 || parsedRef.Line > len(lines) {
		return Response{}, fmt.Errorf("code ref line is out of range: %d", parsedRef.Line)
	}

	logicalPath := sourceEntry.BuildLogicalPath(relativePath, multipleRoots)
	switch request.Mode {
	case "snippet":
		return codeSnippetResponse(request, logicalPath, parsedRef.Line, lines), nil
	case "full":
		return codeFullResponse(request, logicalPath, parsedRef.Line, lines), nil
	case "multi_range":
		return codeMultiRangeResponse(request, logicalPath, parsedRef.Line, lines), nil
	default:
		return Response{}, fmt.Errorf("unsupported mode: %s", request.Mode)
	}
}

func codeSnippetResponse(request normalizedRequest, logicalPath string, targetLine int, lines []string) Response {
	startLine, endLine, snippet := textutil.CenteredSnippet(lines, targetLine, request.SnippetLines)
	return Response{
		RefID:       ref.BuildCodeRefID(logicalPath, targetLine),
		Kind:        "code_range",
		Path:        logicalPath,
		ContentType: "text/plain",
		Mode:        "snippet",
		MaxLines:    request.MaxLines,
		Truncated:   false,
		Ranges:      []Range{{StartLine: startLine, EndLine: endLine}},
		Content:     snippet,
	}
}

func codeFullResponse(request normalizedRequest, logicalPath string, targetLine int, lines []string) Response {
	if len(lines) <= request.MaxLines {
		return Response{
			RefID:       ref.BuildCodeRefID(logicalPath, targetLine),
			Kind:        "code_range",
			Path:        logicalPath,
			ContentType: "text/plain",
			Mode:        "full",
			MaxLines:    request.MaxLines,
			Truncated:   false,
			Ranges:      []Range{{StartLine: 1, EndLine: len(lines)}},
			Content:     strings.Join(lines, "\n"),
		}
	}

	response := codeSnippetResponse(request, logicalPath, targetLine, lines)
	response.Truncated = true
	response.Reason = reasonPtr("full mode exceeds maxLines; fell back to snippet")
	return response
}

// Assumption: representative code ranges use a one-line context radius because
// the spec defines candidate line selection but not the exact excerpt width.
func codeMultiRangeResponse(request normalizedRequest, logicalPath string, targetLine int, lines []string) Response {
	candidates := []int{}
	if signatureLine := nearestSignatureLine(lines, targetLine); signatureLine > 0 {
		candidates = append(candidates, signatureLine)
	}
	candidates = append(candidates, targetLine)
	candidates = append(candidates, interestingCodeLines(lines, targetLine, 20)...)

	ranges := make([]Range, 0, 4)
	seen := map[int]struct{}{}
	for _, line := range candidates {
		if line < 1 || line > len(lines) {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		ranges = append(ranges, Range{
			StartLine: max(1, line-1),
			EndLine:   min(len(lines), line+1),
		})
		if len(ranges) == 4 {
			break
		}
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].StartLine < ranges[j].StartLine
	})
	ranges = mergeRanges(ranges)

	return Response{
		RefID:       ref.BuildCodeRefID(logicalPath, targetLine),
		Kind:        "code_range",
		Path:        logicalPath,
		ContentType: "text/plain",
		Mode:        "multi_range",
		MaxLines:    request.MaxLines,
		Truncated:   true,
		Reason:      reasonPtr("representative ranges selected around code reference"),
		Ranges:      ranges,
		Content:     renderMarkedRanges(lines, 1, ranges),
	}
}

func resolveFile(cfg config.Runtime, request normalizedRequest, parsedRef ref.ParsedRef) (Response, error) {
	sourceEntry, relativePath, multipleRoots, err := source.ResolveLogicalPath(cfg, parsedRef.Path)
	if err != nil {
		return Response{}, err
	}

	textFile, err := sourceEntry.ReadText(relativePath, cfg.TextEncodingFallbacks)
	if err != nil {
		return Response{}, err
	}

	logicalPath := sourceEntry.BuildLogicalPath(relativePath, multipleRoots)
	lines := textutil.SplitLines(textFile.Text)
	if len(lines) <= request.MaxLines {
		return Response{
			RefID:       ref.BuildFileRefID(logicalPath),
			Kind:        "file",
			Path:        logicalPath,
			ContentType: "text/plain",
			Mode:        "full",
			MaxLines:    request.MaxLines,
			Truncated:   false,
			Ranges:      []Range{{StartLine: 1, EndLine: len(lines)}},
			Content:     strings.Join(lines, "\n"),
		}, nil
	}

	return Response{
		RefID:       ref.BuildFileRefID(logicalPath),
		Kind:        "file",
		Path:        logicalPath,
		ContentType: "text/plain",
		Mode:        "snippet",
		MaxLines:    request.MaxLines,
		Truncated:   true,
		Reason:      reasonPtr("file exceeds maxLines; returning head snippet"),
		Ranges:      []Range{{StartLine: 1, EndLine: request.MaxLines}},
		Content:     strings.Join(lines[:request.MaxLines], "\n"),
	}, nil
}

func nearestSignatureLine(lines []string, targetLine int) int {
	for line := targetLine; line >= 1; line-- {
		if isSignatureLine(lines[line-1]) {
			return line
		}
	}
	return 0
}

func isSignatureLine(line string) bool {
	switch {
	case functionPattern.MatchString(line):
		return true
	case arrowPattern.MatchString(line):
		return true
	case csharpPattern.MatchString(line):
		return true
	case classPattern.MatchString(line):
		return true
	case minimalPattern.MatchString(line):
		return true
	case expressPattern.MatchString(line):
		return true
	default:
		return false
	}
}

func interestingCodeLines(lines []string, targetLine int, radius int) []int {
	type candidate struct {
		line     int
		distance int
	}

	candidates := make([]candidate, 0, 8)
	start := max(1, targetLine-radius)
	end := min(len(lines), targetLine+radius)
	for line := start; line <= end; line++ {
		if line == targetLine {
			continue
		}
		if !isInterestingCodeLine(lines[line-1]) {
			continue
		}
		candidates = append(candidates, candidate{
			line:     line,
			distance: abs(line - targetLine),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		return candidates[i].line < candidates[j].line
	})

	linesOnly := make([]int, 0, min(4, len(candidates)))
	for _, candidate := range candidates {
		linesOnly = append(linesOnly, candidate.line)
		if len(linesOnly) == 4 {
			break
		}
	}
	return linesOnly
}

func isInterestingCodeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	switch {
	case strings.Contains(trimmed, "await "):
		return true
	case strings.Contains(trimmed, "return "):
		return true
	case strings.Contains(trimmed, "throw "):
		return true
	case strings.Contains(trimmed, " new "):
		return true
	case strings.HasPrefix(trimmed, "new "):
		return true
	case strings.Contains(trimmed, "if ("):
		return true
	case minimalPattern.MatchString(trimmed):
		return true
	case expressPattern.MatchString(trimmed):
		return true
	case regexp.MustCompile(`\b\w+\s*\(`).MatchString(trimmed):
		return true
	default:
		return false
	}
}

func mergeRanges(ranges []Range) []Range {
	if len(ranges) == 0 {
		return nil
	}

	merged := []Range{ranges[0]}
	for _, current := range ranges[1:] {
		last := &merged[len(merged)-1]
		if current.StartLine <= last.EndLine+1 {
			if current.EndLine > last.EndLine {
				last.EndLine = current.EndLine
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func renderMarkedRanges(lines []string, baseLine int, ranges []Range) string {
	parts := make([]string, 0, len(ranges))
	for _, rng := range ranges {
		startOffset := rng.StartLine - baseLine
		endOffset := rng.EndLine - baseLine + 1
		if startOffset < 0 || endOffset > len(lines) || startOffset >= endOffset {
			continue
		}
		parts = append(parts, fmt.Sprintf(
			"@@ L%d-L%d @@\n%s",
			rng.StartLine,
			rng.EndLine,
			strings.Join(lines[startOffset:endOffset], "\n"),
		))
	}
	return strings.Join(parts, "\n\n")
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

func reasonPtr(value string) *string {
	return &value
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

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
