package content

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/docs"
	"github.com/silvekt/scriptorium/internal/docsindex"
	"github.com/silvekt/scriptorium/internal/gitsnapshot"
	"github.com/silvekt/scriptorium/internal/ref"
)

func TestExecuteMarkdownModes(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(
		t,
		filepath.Join(docsRoot, "guide.md"),
		"# Flow\nintro\n1. step one\n- bullet detail\n",
	)

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	snippet, err := Execute(cfg, Request{
		RefID:        "md:guide.md#flow",
		SnippetLines: 2,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if snippet.Mode != "snippet" || snippet.Content != "# Flow\nintro" {
		t.Fatalf("unexpected markdown snippet response: %#v", snippet)
	}

	fullFallback, err := Execute(cfg, Request{
		RefID:    "md:guide.md#flow",
		Mode:     "full",
		MaxLines: 2,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !fullFallback.Truncated || fullFallback.Mode != "snippet" {
		t.Fatalf("expected truncated full fallback, got %#v", fullFallback)
	}
	if fullFallback.Reason == nil || *fullFallback.Reason != "full mode exceeds maxLines; fell back to snippet" {
		t.Fatalf("unexpected full fallback reason: %#v", fullFallback.Reason)
	}

	multiRange, err := Execute(cfg, Request{
		RefID: "md:guide.md#flow",
		Mode:  "multi_range",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !multiRange.Truncated || multiRange.Mode != "multi_range" || len(multiRange.Ranges) < 2 {
		t.Fatalf("unexpected markdown multi-range response: %#v", multiRange)
	}
	if !strings.Contains(multiRange.Content, "@@ L1-L1 @@") {
		t.Fatalf("expected marker content, got %q", multiRange.Content)
	}
}

func TestExecuteCodeAndFileModes(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	mustWriteFile(
		t,
		filepath.Join(sampleRoot, "src", "app.ts"),
		"export function run() {\nconst value = buildValue()\nif (value) {\nreturn value\n}\n}\n",
	)

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	codeFullFallback, err := Execute(cfg, Request{
		RefID:    "code:src/app.ts@L4",
		Mode:     "full",
		MaxLines: 3,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !codeFullFallback.Truncated || codeFullFallback.Mode != "snippet" {
		t.Fatalf("expected code full fallback, got %#v", codeFullFallback)
	}

	codeMultiRange, err := Execute(cfg, Request{
		RefID: "code:src/app.ts@L4",
		Mode:  "multi_range",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if codeMultiRange.Mode != "multi_range" || len(codeMultiRange.Ranges) == 0 {
		t.Fatalf("unexpected code multi-range response: %#v", codeMultiRange)
	}
	if !strings.Contains(codeMultiRange.Content, "@@ L") {
		t.Fatalf("expected code markers, got %q", codeMultiRange.Content)
	}

	fileResponse, err := Execute(cfg, Request{
		RefID:    "file:src/app.ts",
		MaxLines: 2,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if fileResponse.Kind != "file" || fileResponse.Mode != "snippet" || !fileResponse.Truncated {
		t.Fatalf("unexpected file response: %#v", fileResponse)
	}
	if fileResponse.Reason == nil || *fileResponse.Reason != "file exceeds maxLines; returning head snippet" {
		t.Fatalf("unexpected file reason: %#v", fileResponse.Reason)
	}

	fullFileResponse, err := Execute(cfg, Request{
		RefID:    "file:src/app.ts",
		Mode:     "full",
		MaxLines: 20,
	})
	if err != nil {
		t.Fatalf("Execute full file returned error: %v", err)
	}
	if fullFileResponse.Mode != "full" || fullFileResponse.Truncated {
		t.Fatalf("unexpected full file response: %#v", fullFileResponse)
	}
}

func TestExecuteMarkdownUsesDocsIndexAndFallsBackWhenStale(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nindexed body\n")

	index, err := docsindex.Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	indexPath := filepath.Join(docsRoot, "scriptorium-index.sqlite")
	if err := docsindex.Write(indexPath, index); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cfg := config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexPaths:  []string{indexPath},
		DocsIndexVerify: "full",
		CodeExtensions:  []string{".ts"},
		MaxFileBytes:    1_000_000,
	}

	response, err := Execute(cfg, Request{RefID: "md:guide.md#guide", Mode: "full"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if response.Content != "# Guide\nindexed body" {
		t.Fatalf("expected indexed content, got %#v", response)
	}

	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nfresh body\n")
	response, err = Execute(cfg, Request{RefID: "md:guide.md#guide", Mode: "full"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if response.Content != "# Guide\nfresh body" {
		t.Fatalf("expected filesystem fallback content, got %#v", response)
	}
}

func TestExecuteMarkdownResolvesAcrossMultipleDocsIndexes(t *testing.T) {
	docsRoot := t.TempDir()

	firstRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(firstRoot, "a.md"), "# Alpha\nfirst indexed body\n")
	firstIndex, err := docsindex.Build(firstRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	firstIndexPath := filepath.Join(t.TempDir(), "docs-a.sqlite")
	if err := docsindex.Write(firstIndexPath, firstIndex); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	secondRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(secondRoot, "b.md"), "# Beta\nsecond indexed body\n")
	secondIndex, err := docsindex.Build(secondRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	secondIndexPath := filepath.Join(t.TempDir(), "docs-b.sqlite")
	if err := docsindex.Write(secondIndexPath, secondIndex); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cfg := config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexPaths:  []string{firstIndexPath, secondIndexPath},
		DocsIndexVerify: "off",
		CodeExtensions:  []string{".ts"},
		MaxFileBytes:    1_000_000,
	}

	response, err := Execute(cfg, Request{RefID: "md:b.md#beta", Mode: "full"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if response.Content != "# Beta\nsecond indexed body" {
		t.Fatalf("expected content from second docs index, got %#v", response)
	}
}

func TestExecuteResolvesGitSnapshotCodeAndFileRefs(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export function runFeature() {\n  return specialToken\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/content")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/content",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := gitsnapshot.Write(snapshotPath, snapshot); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cfg := config.Runtime{
		DocsRoot:         docsRoot,
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}

	codeResponse, err := Execute(cfg, Request{RefID: "code:@feature-content/src/app.ts@L2"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if codeResponse.Path != "@feature-content/src/app.ts" {
		t.Fatalf("unexpected code response: %#v", codeResponse)
	}

	fileResponse, err := Execute(cfg, Request{RefID: "file:@feature-content/src/app.ts", Mode: "full"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(fileResponse.Content, "specialToken") {
		t.Fatalf("unexpected file response: %#v", fileResponse)
	}
}

func TestNormalizeRequestValidationAndClamp(t *testing.T) {
	normalized, err := normalizeRequest(Request{
		RefID:        " code:src/app.ts@L2 ",
		Mode:         " FULL ",
		SnippetLines: -4,
		MaxLines:     9000,
	})
	if err != nil {
		t.Fatalf("normalizeRequest returned error: %v", err)
	}
	if normalized.RefID != "code:src/app.ts@L2" || normalized.Mode != "full" {
		t.Fatalf("unexpected normalized request: %#v", normalized)
	}
	if normalized.SnippetLines != 1 || normalized.MaxLines != 5000 {
		t.Fatalf("expected clamped request values, got %#v", normalized)
	}

	if _, err := normalizeRequest(Request{}); err == nil {
		t.Fatalf("expected empty refId validation error")
	}
	if _, err := normalizeRequest(Request{RefID: "code:src/app.ts@L2", Mode: "invalid"}); err == nil {
		t.Fatalf("expected invalid mode validation error")
	}
}

func TestExecuteValidationAndResolutionErrors(t *testing.T) {
	cfg := config.Runtime{
		DocsRoot:       t.TempDir(),
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	if _, err := Execute(cfg, Request{}); err == nil {
		t.Fatal("expected empty refId validation error")
	}
	if _, err := Execute(cfg, Request{RefID: "bad-ref"}); err == nil {
		t.Fatal("expected invalid refId parse error")
	}
	if _, err := Execute(cfg, Request{RefID: "code:src/app.ts@L2", Mode: "invalid"}); err == nil {
		t.Fatal("expected invalid mode normalization error")
	}

	mustWriteFile(t, filepath.Join(cfg.DocsRoot, "guide.md"), "# Guide\nbody\n")
	if _, err := Execute(cfg, Request{RefID: "md:guide.md#missing", Mode: "full"}); err == nil {
		t.Fatal("expected missing markdown block error")
	}

	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "const onlyLine = 1\n")
	cfg.SampleRoots = []string{sampleRoot}
	if _, err := Execute(cfg, Request{RefID: "code:src/app.ts@L5"}); err == nil {
		t.Fatal("expected code line out-of-range error")
	}
}

func TestExecutePropagatesRootResolutionErrors(t *testing.T) {
	if _, err := Execute(config.Runtime{
		DocsRoot:       filepath.Join(t.TempDir(), "missing"),
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, Request{RefID: "md:guide.md#guide"}); err == nil {
		t.Fatal("expected markdown root resolution error")
	}

	if _, err := Execute(config.Runtime{
		DocsRoot:       t.TempDir(),
		SampleRoots:    []string{filepath.Join(t.TempDir(), "missing")},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, Request{RefID: "code:src/app.ts@L1"}); err == nil {
		t.Fatal("expected code source resolution error")
	}

	if _, err := Execute(config.Runtime{
		DocsRoot:       t.TempDir(),
		SampleRoots:    []string{filepath.Join(t.TempDir(), "missing")},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, Request{RefID: "file:src/app.ts"}); err == nil {
		t.Fatal("expected file source resolution error")
	}
}

func TestMarkdownAndCodeHelperBranches(t *testing.T) {
	index, err := docsindex.Load(mustWriteDocsIndex(t, "guide.md", "# Guide\nbody\n"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	block, ok := index.FindBlock("md:guide.md#guide")
	if !ok {
		t.Fatalf("expected markdown block in index")
	}

	if response, ok := resolveMarkdownFromIndexes([]docsindex.Artifact{index}, normalizedRequest{
		RefID:        block.RefID,
		Mode:         "snippet",
		SnippetLines: 1,
		MaxLines:     5,
	}); !ok || response.Content != "# Guide" {
		t.Fatalf("expected indexed markdown snippet, got ok=%v response=%#v", ok, response)
	}
	if response, ok := resolveMarkdownFromIndexes([]docsindex.Artifact{index}, normalizedRequest{
		RefID:        block.RefID,
		Mode:         "full",
		SnippetLines: 1,
		MaxLines:     5,
	}); !ok || response.Mode != "full" {
		t.Fatalf("expected indexed markdown full response, got ok=%v response=%#v", ok, response)
	}
	if _, ok := resolveMarkdownFromIndexes([]docsindex.Artifact{index}, normalizedRequest{
		RefID: block.RefID,
		Mode:  "multi_range",
	}); ok {
		t.Fatalf("did not expect index shortcut for multi_range")
	}
	if _, ok := resolveMarkdownFromIndexes([]docsindex.Artifact{index}, normalizedRequest{
		RefID: "md:missing.md#guide",
		Mode:  "full",
	}); ok {
		t.Fatalf("did not expect index hit for missing markdown ref")
	}

	emptySnippet := markdownSnippetResponse(normalizedRequest{RefID: block.RefID, MaxLines: 5}, &docs.Block{
		RefID:     block.RefID,
		Path:      block.Path,
		StartLine: 7,
		EndLine:   7,
		Content:   "",
	}, nil)
	if emptySnippet.Ranges[0].StartLine != 7 || emptySnippet.Ranges[0].EndLine != 7 {
		t.Fatalf("expected empty markdown snippet to stay anchored, got %#v", emptySnippet)
	}

	markdownMulti := markdownMultiRangeResponse(normalizedRequest{RefID: block.RefID, MaxLines: 5}, &docs.Block{
		RefID:     block.RefID,
		Path:      block.Path,
		StartLine: 10,
		EndLine:   12,
		Content:   "# Guide\n\nplain body\n",
	}, []string{"# Guide", "", "plain body"})
	if !slices.Equal(markdownMulti.Ranges, []Range{{StartLine: 10, EndLine: 10}, {StartLine: 12, EndLine: 12}}) {
		t.Fatalf("expected markdown fallback ranges, got %#v", markdownMulti.Ranges)
	}

	lines := []string{
		"const noop = 1",
		"class Handler {",
		"  run() {",
		"    await service.Call()",
		"    value = buildResult()",
		"      return value",
		"    }",
		"  }",
		"}",
	}
	if got := nearestSignatureLine(lines, 6); got != 2 {
		t.Fatalf("expected nearest signature anchor, got %d", got)
	}
	if got := nearestSignatureLine([]string{"plain", "body"}, 2); got != 0 {
		t.Fatalf("expected no signature match, got %d", got)
	}
	if !isSignatureLine("router.get('/x', handler)") {
		t.Fatalf("expected express signature match")
	}
	if !isSignatureLine("const build = async () => value") {
		t.Fatalf("expected arrow-function signature match")
	}
	if !isSignatureLine("public Task RunAsync()") {
		t.Fatalf("expected csharp signature match")
	}
	if !isSignatureLine("class Handler {") {
		t.Fatalf("expected class signature match")
	}
	if !isSignatureLine("app.MapGet(\"/x\", handler)") {
		t.Fatalf("expected minimal-api signature match")
	}
	if isSignatureLine("return value") {
		t.Fatalf("did not expect return statement to be a signature")
	}

	interesting := interestingCodeLines(lines, 4, 3)
	if !slices.Equal(interesting, []int{3, 5, 6}) {
		t.Fatalf("unexpected interesting code lines: %#v", interesting)
	}
	if isInterestingCodeLine("  ") || !isInterestingCodeLine("throw err") || !isInterestingCodeLine("new Service()") {
		t.Fatalf("unexpected interesting code line classification")
	}

	codeMulti := codeMultiRangeResponse(normalizedRequest{RefID: "code:src/app.ts@L4", MaxLines: 20}, "src/app.ts", 4, lines)
	if codeMulti.Mode != "multi_range" || len(codeMulti.Ranges) == 0 {
		t.Fatalf("expected code multi-range response, got %#v", codeMulti)
	}
	if codeMulti.Ranges[0].StartLine != 1 || codeMulti.Ranges[0].EndLine < 4 {
		t.Fatalf("expected merged code ranges around signature and target, got %#v", codeMulti.Ranges)
	}

	fullCode := codeFullResponse(normalizedRequest{RefID: "code:src/app.ts@L2", MaxLines: 20}, "src/app.ts", 2, []string{"one", "two"})
	if fullCode.Mode != "full" || fullCode.Truncated || fullCode.Ranges[0].EndLine != 2 {
		t.Fatalf("expected full code response, got %#v", fullCode)
	}

	targetOnly := codeMultiRangeResponse(normalizedRequest{RefID: "code:src/app.ts@L2", MaxLines: 20}, "src/app.ts", 2, []string{"plain", "value", "done"})
	if !slices.Equal(targetOnly.Ranges, []Range{{StartLine: 1, EndLine: 3}}) {
		t.Fatalf("expected target-only multi-range response, got %#v", targetOnly.Ranges)
	}
}

func TestResolveFileAndCodeHelperDirectBranches(t *testing.T) {
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "short.ts"), "one\ntwo\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "empty.ts"), "")
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	cfg := config.Runtime{
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	fullFile, err := resolveFile(cfg, normalizedRequest{RefID: "file:src/short.ts", MaxLines: 10}, ref.ParsedRef{Kind: "file", Path: "src/short.ts"})
	if err != nil {
		t.Fatalf("resolveFile returned error: %v", err)
	}
	if fullFile.Mode != "full" || fullFile.Truncated || fullFile.Ranges[0].EndLine != 2 {
		t.Fatalf("expected full file response, got %#v", fullFile)
	}

	emptyFile, err := resolveFile(cfg, normalizedRequest{RefID: "file:empty.ts", MaxLines: 10}, ref.ParsedRef{Kind: "file", Path: "empty.ts"})
	if err != nil {
		t.Fatalf("resolveFile empty returned error: %v", err)
	}
	if len(emptyFile.Ranges) != 1 || emptyFile.Ranges[0].StartLine != 1 || emptyFile.Ranges[0].EndLine != 0 || emptyFile.Content != "" {
		t.Fatalf("unexpected empty file response: %#v", emptyFile)
	}

	fullCode, err := resolveCode(cfg, normalizedRequest{RefID: "code:src/short.ts@L1", Mode: "full", MaxLines: 10}, ref.ParsedRef{Kind: "code", Path: "src/short.ts", Line: 1})
	if err != nil {
		t.Fatalf("resolveCode returned error: %v", err)
	}
	if fullCode.Mode != "full" || fullCode.Truncated || fullCode.Content != "one\ntwo" {
		t.Fatalf("unexpected full code response: %#v", fullCode)
	}

	if _, err := resolveMarkdown(config.Runtime{
		DocsRoot:     sampleRoot,
		MaxFileBytes: 1_000_000,
	}, normalizedRequest{RefID: "md:missing.md#guide", Mode: "snippet", MaxLines: 20, SnippetLines: 5}, ref.ParsedRef{
		Kind:        "md",
		Path:        "missing.md",
		HeadingSlug: "guide",
	}); err == nil {
		t.Fatalf("expected resolveMarkdown to propagate missing-file read error")
	}

	if _, err := resolveMarkdown(config.Runtime{
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}, normalizedRequest{RefID: "md:guide.md#guide", Mode: "other", MaxLines: 20, SnippetLines: 5}, ref.ParsedRef{
		Kind:        "md",
		Path:        "guide.md",
		HeadingSlug: "guide",
	}); err == nil {
		t.Fatalf("expected resolveMarkdown to reject unsupported mode")
	}

	if _, err := resolveCode(config.Runtime{
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, normalizedRequest{RefID: "code:src/short.ts@L1", Mode: "other", MaxLines: 20, SnippetLines: 5}, ref.ParsedRef{
		Kind: "code",
		Path: "src/short.ts",
		Line: 1,
	}); err == nil {
		t.Fatalf("expected resolveCode to reject unsupported mode")
	}
}

func TestAdditionalContentHelperBranches(t *testing.T) {
	markdownMulti := markdownMultiRangeResponse(normalizedRequest{RefID: "md:guide.md#guide", MaxLines: 20}, &docs.Block{
		RefID:     "md:guide.md#guide",
		Path:      "guide.md",
		StartLine: 20,
		EndLine:   26,
		Content:   "# Guide\n1. first\n手順1: second\n- third\n## Nested\n2. fourth\n",
	}, []string{"# Guide", "1. first", "手順1: second", "- third", "## Nested", "2. fourth"})
	if len(markdownMulti.Ranges) != 4 {
		t.Fatalf("expected markdown multi-range response to cap at four ranges, got %#v", markdownMulti.Ranges)
	}
	if markdownMulti.Ranges[0].StartLine != 20 || markdownMulti.Ranges[3].StartLine != 23 {
		t.Fatalf("unexpected markdown multi-range anchors: %#v", markdownMulti.Ranges)
	}

	if !isInterestingCodeLine("if (ready) {") {
		t.Fatalf("expected if statement to be interesting")
	}
	if !isInterestingCodeLine("await service.Run()") {
		t.Fatalf("expected await expression to be interesting")
	}
	if !isInterestingCodeLine("const service = new Service()") {
		t.Fatalf("expected inline new expression to be interesting")
	}
	if !isInterestingCodeLine(`app.MapPost("/x", handler)`) {
		t.Fatalf("expected minimal API route declaration to be interesting")
	}
	if !isInterestingCodeLine(`router.post("/x", handler)`) {
		t.Fatalf("expected express route declaration to be interesting")
	}
	if !isInterestingCodeLine("performWork(value)") {
		t.Fatalf("expected call-expression line to be interesting")
	}

	cfg := config.Runtime{
		SampleRoots:    []string{t.TempDir()},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}
	if _, err := resolveCode(cfg, normalizedRequest{
		RefID:        "code:src/missing.ts@L1",
		Mode:         "snippet",
		SnippetLines: 3,
		MaxLines:     20,
	}, ref.ParsedRef{Kind: "code", Path: "src/missing.ts", Line: 1}); err == nil {
		t.Fatalf("expected resolveCode to propagate missing-file read error")
	}
	if _, err := resolveFile(cfg, normalizedRequest{
		RefID:    "file:src/missing.ts",
		MaxLines: 20,
	}, ref.ParsedRef{Kind: "file", Path: "src/missing.ts"}); err == nil {
		t.Fatalf("expected resolveFile to propagate missing-file read error")
	}

	interestingCap := interestingCodeLines([]string{
		"await first()",
		"return second",
		"throw third",
		"const fourth = new Fourth()",
		"if (ready) {",
		"router.post('/x', handler)",
	}, 3, 10)
	if len(interestingCap) != 4 {
		t.Fatalf("expected interestingCodeLines to cap results at four, got %#v", interestingCap)
	}

	duplicateCandidate := codeMultiRangeResponse(normalizedRequest{
		RefID:    "code:src/app.ts@L1",
		MaxLines: 20,
	}, "src/app.ts", 1, []string{
		"export function run() {",
		"  return value",
		"}",
	})
	if len(duplicateCandidate.Ranges) != 1 || duplicateCandidate.Ranges[0] != (Range{StartLine: 1, EndLine: 3}) {
		t.Fatalf("expected duplicate target/signature candidates to collapse into one range, got %#v", duplicateCandidate.Ranges)
	}

	invalidCandidate := codeMultiRangeResponse(normalizedRequest{
		RefID:    "code:src/app.ts@L0",
		MaxLines: 20,
	}, "src/app.ts", 0, []string{"plain", "body"})
	if len(invalidCandidate.Ranges) != 0 || invalidCandidate.Content != "" {
		t.Fatalf("expected invalid candidate lines to be skipped, got %#v", invalidCandidate)
	}
}

func TestRangeAndRenderHelpers(t *testing.T) {
	merged := mergeRanges([]Range{
		{StartLine: 1, EndLine: 2},
		{StartLine: 3, EndLine: 5},
		{StartLine: 8, EndLine: 9},
	})
	if !slices.Equal(merged, []Range{{StartLine: 1, EndLine: 5}, {StartLine: 8, EndLine: 9}}) {
		t.Fatalf("unexpected merged ranges: %#v", merged)
	}
	if mergeRanges(nil) != nil {
		t.Fatalf("expected nil merge result for empty input")
	}

	rendered := renderMarkedRanges([]string{"one", "two", "three", "four"}, 1, []Range{
		{StartLine: 1, EndLine: 2},
		{StartLine: 5, EndLine: 6},
		{StartLine: 3, EndLine: 2},
	})
	if rendered != "@@ L1-L2 @@\none\ntwo" {
		t.Fatalf("unexpected rendered ranges: %q", rendered)
	}

	if got := clamp(0, 1, 4, 3); got != 3 {
		t.Fatalf("unexpected clamp default: %d", got)
	}
	if got := clamp(-1, 1, 4, 3); got != 1 {
		t.Fatalf("unexpected clamp lower bound: %d", got)
	}
	if got := clamp(10, 1, 4, 3); got != 4 {
		t.Fatalf("unexpected clamp upper bound: %d", got)
	}
	if got := *reasonPtr("why"); got != "why" {
		t.Fatalf("unexpected reason pointer value: %q", got)
	}
	if min(2, 3) != 2 || max(2, 3) != 3 || abs(-4) != 4 {
		t.Fatalf("unexpected helper min/max/abs behavior")
	}
}

func mustWriteDocsIndex(t *testing.T, path string, body string) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, path), body)
	index, err := docsindex.Build(root, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	indexPath := filepath.Join(t.TempDir(), "docs.sqlite")
	if err := docsindex.Write(indexPath, index); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	return indexPath
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	return repo
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(output))
	}
}
