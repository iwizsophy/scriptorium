package guide

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/content"
	"github.com/iwizsophy/scriptorium/internal/flow"
	"github.com/iwizsophy/scriptorium/internal/related"
	"github.com/iwizsophy/scriptorium/internal/search"
)

func TestExecuteBuildsGroundedImplementationGuide(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	sampleRoot := filepath.Join(t.TempDir(), "samples")
	mustWriteFile(t, filepath.Join(docsRoot, "aspnet-auth.md"), strings.Join([]string{
		"# ASP.NET Core Authentication",
		"Use ASP.NET Core authentication in C#.",
		"1. Add authentication services.",
		"2. Map a protected endpoint.",
		"",
		"## Validate Token",
		"Describe token validation.",
	}, "\n"))
	mustWriteFile(t, filepath.Join(sampleRoot, "Program.cs"), strings.Join([]string{
		"var builder = WebApplication.CreateBuilder(args);",
		"builder.Services.AddAuthentication();",
		"var app = builder.Build();",
		`app.MapGet("/secure", async () => {`,
		"    await authService.ValidateAsync();",
		"    return Results.Ok();",
		"});",
	}, "\n"))

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".cs"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Topic:      "authentication",
		Framework:  "ASP.NET Core",
		Language:   "csharp",
		Preference: "balanced",
		MaxRefs:    4,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Docs) == 0 {
		t.Fatalf("expected docs refs, got %#v", response)
	}
	if len(response.SampleCode) == 0 {
		t.Fatalf("expected sample code refs, got %#v", response)
	}
	if response.Confidence == "low" {
		t.Fatalf("expected grounded confidence, got %#v", response)
	}
	if !strings.Contains(response.Guide, "Add authentication services") {
		t.Fatalf("expected guide to include doc-derived step, got %q", response.Guide)
	}
	if response.SampleCode[0].Path == "" || !strings.Contains(response.SampleCode[0].Path, "Program.cs") {
		t.Fatalf("expected sample code support from Program.cs, got %#v", response.SampleCode)
	}
	if strings.TrimSpace(response.Guide) == "" {
		t.Fatalf("expected non-empty guide, got %#v", response)
	}
}

func TestExecuteReturnsAssumptionWhenSampleCodeIsMissing(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\n1. Configure docs.\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".go"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Topic:     "configuration",
		Framework: "generic",
		MaxRefs:   4,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.SampleCode) != 0 {
		t.Fatalf("expected no sample code refs, got %#v", response.SampleCode)
	}
	found := false
	for _, assumption := range response.Assumptions {
		if strings.Contains(assumption, "No supporting sample code refs") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing sample code assumption, got %#v", response.Assumptions)
	}
}

func TestExecuteFallsBackWhenNoSupportingRefsExist(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Generic\nbackground only\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".go"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Topic:     "nonexistent branch mechanics",
		Framework: "unknown",
		MaxRefs:   4,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(response.Guide, "nonexistent branch mechanics") {
		t.Fatalf("expected topic-based fallback guidance, got %q", response.Guide)
	}
	if response.Confidence != "low" {
		t.Fatalf("expected low confidence without supporting refs, got %#v", response)
	}
	expectedAssumptions := []string{
		"No supporting docs refs matched the current corpus for this question.",
		"No supporting sample code refs matched the current corpus for this question.",
		"The guide falls back to the requested topic because no supporting refs were found.",
	}
	for _, expected := range expectedAssumptions {
		if !slices.Contains(response.Assumptions, expected) {
			t.Fatalf("expected assumption %q, got %#v", expected, response.Assumptions)
		}
	}
}

func TestExecuteReturnsTopicValidationError(t *testing.T) {
	if _, err := Execute(config.Runtime{}, Request{}); err == nil || !strings.Contains(err.Error(), "topic is required") {
		t.Fatalf("expected topic validation error, got %v", err)
	}
}

func TestGuideHelperBranches(t *testing.T) {
	normalized, err := normalizeRequest(Request{
		Topic:      "auth",
		Framework:  "ASP.NET Core",
		Language:   "TypeScript",
		Preference: "documentation",
		MaxRefs:    99,
	}, []string{".go", ".ts"})
	if err != nil {
		t.Fatalf("normalizeRequest returned error: %v", err)
	}
	if normalized.Query != "auth ASP.NET Core TypeScript" {
		t.Fatalf("unexpected normalized query: %#v", normalized)
	}
	if normalized.Preference != "docs" {
		t.Fatalf("expected docs preference, got %#v", normalized)
	}
	if !slices.Equal(normalized.CodeExtensions, []string{".ts", ".tsx"}) {
		t.Fatalf("expected language-specific extensions, got %#v", normalized.CodeExtensions)
	}
	if normalized.MaxRefs != 12 {
		t.Fatalf("expected clamped max refs, got %#v", normalized)
	}

	if _, err := normalizeRequest(Request{}, []string{".go"}); err == nil {
		t.Fatalf("expected topic validation error")
	}
	if normalizePreference(" sample_code ") != "samples" || normalizePreference("other") != "balanced" {
		t.Fatalf("unexpected preference normalization")
	}
	if !slices.Equal(extensionsForLanguage("golang"), []string{".go"}) {
		t.Fatalf("expected Go extension mapping")
	}
	if extensionsForLanguage("ruby") != nil {
		t.Fatalf("expected nil extension mapping for unsupported language")
	}

	if got := buildQuestion(normalized); got != "auth in ASP.NET Core using TypeScript" {
		t.Fatalf("unexpected built question: %q", got)
	}
	if docsWanted, codeWanted := supportingBudgets("docs", 6); docsWanted != 4 || codeWanted != 2 {
		t.Fatalf("unexpected docs-preferred budget: docs=%d code=%d", docsWanted, codeWanted)
	}
	if docsWanted, codeWanted := supportingBudgets("samples", 6); docsWanted != 2 || codeWanted != 4 {
		t.Fatalf("unexpected samples-preferred budget: docs=%d code=%d", docsWanted, codeWanted)
	}
	if docsWanted, codeWanted := supportingBudgets("balanced", 5); docsWanted != 2 || codeWanted != 3 {
		t.Fatalf("unexpected balanced budget: docs=%d code=%d", docsWanted, codeWanted)
	}

	searchRefs := []search.Result{
		{RefID: "md:guide.md#auth", Kind: "md_heading_block", Path: "guide.md", Range: search.Range{StartLine: 1, EndLine: 4}, Snippet: "guide"},
		{RefID: "code:src/app.ts@L2", Kind: "code_range", Path: "src/app.ts", Range: search.Range{StartLine: 2, EndLine: 4}, Snippet: "code"},
		{RefID: "md:other.md#skip", Kind: "md_heading_block", Path: "other.md", Range: search.Range{StartLine: 1, EndLine: 2}, Snippet: "other"},
	}
	docsRefs := takeSearchRefs(searchRefs, "md_heading_block", 1, "search")
	if len(docsRefs) != 1 || docsRefs[0].Range.StartLine != 1 || docsRefs[0].Origin != "search" {
		t.Fatalf("unexpected taken docs refs: %#v", docsRefs)
	}
	codeRefs := takeSearchRefs(searchRefs, "code_range", 2, "search")
	if len(codeRefs) != 1 || codeRefs[0].Path != "src/app.ts" {
		t.Fatalf("unexpected taken code refs: %#v", codeRefs)
	}

	seeds := initialSeeds(
		[]SupportingRef{{RefID: "d1"}, {RefID: "d2"}},
		[]SupportingRef{{RefID: "c1"}, {RefID: "c2"}, {RefID: "c2"}},
	)
	if !slices.Equal(seeds, []string{"d1", "c1", "d2", "c2"}) {
		t.Fatalf("unexpected initial seeds: %#v", seeds)
	}

	filtered := filterSupportingRefs([]SupportingRef{
		{RefID: "d1", Kind: "md_heading_block"},
		{RefID: "c1", Kind: "code_range"},
	}, "code_range")
	if !slices.Equal(filtered, []SupportingRef{{RefID: "c1", Kind: "code_range"}}) {
		t.Fatalf("unexpected filtered supporting refs: %#v", filtered)
	}

	appended := appendSupportingRefs([]SupportingRef{{RefID: "d1"}}, []SupportingRef{{RefID: "d1"}, {RefID: "d2"}, {RefID: "d3"}}, 2)
	if !slices.Equal(appended, []SupportingRef{{RefID: "d1"}, {RefID: "d2"}}) {
		t.Fatalf("unexpected appended supporting refs: %#v", appended)
	}

	if got := supportingRefIDs([]SupportingRef{{RefID: "d1"}, {RefID: "d1"}}, []SupportingRef{{RefID: "c1"}}); !slices.Equal(got, []string{"d1", "c1"}) {
		t.Fatalf("unexpected supporting ref ids: %#v", got)
	}
}

func TestRenderGuideConfidenceAndDedupeHelpers(t *testing.T) {
	if got := renderGuide("auth", nil); !strings.Contains(got, `No grounded implementation steps were found for "auth".`) {
		t.Fatalf("unexpected empty render guide message: %q", got)
	}
	guide := renderGuide("auth", []flow.Step{
		{Step: 1, Title: "", Description: "bootstrap"},
		{Step: 2, Title: "Register", Description: "Register"},
		{Step: 3, Title: "Map", Description: "Map endpoints"},
	})
	if guide != "1. bootstrap\n2. Register\n3. Map: Map endpoints" {
		t.Fatalf("unexpected rendered guide: %q", guide)
	}

	if adjustedConfidence("high", true, false) != "medium" {
		t.Fatalf("expected degraded high confidence without both support types")
	}
	if adjustedConfidence("medium", true, false) != "medium" {
		t.Fatalf("expected medium confidence to remain medium with one support type")
	}
	if adjustedConfidence("medium", false, false) != "low" || adjustedConfidence("low", true, true) != "low" {
		t.Fatalf("unexpected adjusted confidence behavior")
	}

	if got := dedupeStrings([]string{" docs ", "", "docs", "code"}); !slices.Equal(got, []string{"docs", "code"}) {
		t.Fatalf("unexpected dedupeStrings result: %#v", got)
	}
	if clamp(0, 2, 4, 3) != 3 || clamp(-1, 2, 4, 3) != 2 || clamp(9, 2, 4, 3) != 4 {
		t.Fatalf("unexpected clamp behavior")
	}
	if min(1, 2) != 1 || max(1, 2) != 2 {
		t.Fatalf("unexpected min/max behavior")
	}
}

func TestGuideAdditionalHelperBranches(t *testing.T) {
	if !slices.Equal(extensionsForLanguage("js"), []string{".js", ".jsx"}) {
		t.Fatalf("expected JavaScript extension mapping")
	}
	if !slices.Equal(extensionsForLanguage("python"), []string{".py"}) {
		t.Fatalf("expected Python extension mapping")
	}
	if !slices.Equal(extensionsForLanguage("java"), []string{".java"}) {
		t.Fatalf("expected Java extension mapping")
	}
	if !slices.Equal(extensionsForLanguage("c#"), []string{".cs"}) {
		t.Fatalf("expected C# extension mapping")
	}
	if docsWanted, codeWanted := supportingBudgets("balanced", 1); docsWanted != 1 || codeWanted != 1 {
		t.Fatalf("unexpected balanced budget for tiny max refs: docs=%d code=%d", docsWanted, codeWanted)
	}
	limited := appendSupportingRefs(
		[]SupportingRef{{RefID: "d1"}, {RefID: "d2"}},
		[]SupportingRef{{RefID: "d3"}},
		1,
	)
	if !slices.Equal(limited, []SupportingRef{{RefID: "d1"}}) {
		t.Fatalf("expected appendSupportingRefs to honor existing limit, got %#v", limited)
	}
	if max(3, 1) != 3 {
		t.Fatalf("expected max to keep larger left operand")
	}
}

func TestLoadRelatedRefsSuccessAndErrorBranches(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	sampleRoot := filepath.Join(t.TempDir(), "samples")
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), strings.Join([]string{
		"# Guide",
		"Configure authentication services.",
	}, "\n"))
	mustWriteFile(t, filepath.Join(docsRoot, "followup.md"), strings.Join([]string{
		"# Follow Up",
		"Authentication services also map secure endpoints.",
	}, "\n"))
	mustWriteFile(t, filepath.Join(sampleRoot, "Program.cs"), strings.Join([]string{
		"var builder = WebApplication.CreateBuilder(args);",
		"builder.Services.AddAuthentication();",
		"app.MapGet(\"/secure\", () => Results.Ok());",
	}, "\n"))

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".cs"},
		MaxFileBytes:   1_000_000,
	}

	refs, err := loadRelatedRefs(cfg, normalizedRequest{
		Topic:          "authentication",
		CodeExtensions: []string{".cs"},
		MaxRefs:        4,
	}, []string{"md:guide.md#guide"})
	if err != nil {
		t.Fatalf("loadRelatedRefs returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatalf("expected related supporting refs, got %#v", refs)
	}
	if refs[0].Origin != "related" {
		t.Fatalf("expected related origin, got %#v", refs[0])
	}

	if _, err := loadRelatedRefs(cfg, normalizedRequest{
		Topic:          "authentication",
		CodeExtensions: []string{".cs"},
		MaxRefs:        4,
	}, []string{"bad-ref"}); err == nil {
		t.Fatalf("expected loadRelatedRefs to propagate invalid seed errors")
	}
}

func TestExecuteAndLoadRelatedRefsErrorBranchesViaSeams(t *testing.T) {
	stubGuideDeps(t)

	searchCalls := 0
	searchExecute = func(cfg config.Runtime, request search.Request) (search.Response, error) {
		searchCalls++
		if searchCalls == 1 {
			return search.Response{}, errors.New("docs search failed")
		}
		return search.Response{}, nil
	}
	if _, err := Execute(config.Runtime{}, Request{Topic: "auth"}); err == nil || !strings.Contains(err.Error(), "docs search failed") {
		t.Fatalf("expected docs search error, got %v", err)
	}

	searchCalls = 0
	searchExecute = func(cfg config.Runtime, request search.Request) (search.Response, error) {
		searchCalls++
		if searchCalls == 2 {
			return search.Response{}, errors.New("code search failed")
		}
		return search.Response{Results: []search.Result{{RefID: "md:guide.md#auth", Kind: "md_heading_block", Path: "guide.md"}}}, nil
	}
	if _, err := Execute(config.Runtime{}, Request{Topic: "auth"}); err == nil || !strings.Contains(err.Error(), "code search failed") {
		t.Fatalf("expected code search error, got %v", err)
	}

	searchExecute = func(cfg config.Runtime, request search.Request) (search.Response, error) {
		switch {
		case slices.Equal(request.Extensions, []string{".md"}):
			return search.Response{Results: []search.Result{{RefID: "md:guide.md#auth", Kind: "md_heading_block", Path: "guide.md", Range: search.Range{StartLine: 1, EndLine: 2}, Snippet: "doc"}}}, nil
		default:
			return search.Response{Results: []search.Result{{RefID: "code:src/app.ts@L2", Kind: "code_range", Path: "src/app.ts", Range: search.Range{StartLine: 2, EndLine: 4}, Snippet: "code"}}}, nil
		}
	}
	relatedExecute = func(cfg config.Runtime, request related.Request) (related.Response, error) {
		return related.Response{}, errors.New("related failed")
	}
	if _, err := Execute(config.Runtime{}, Request{Topic: "auth"}); err == nil || !strings.Contains(err.Error(), "related failed") {
		t.Fatalf("expected related error, got %v", err)
	}

	relatedExecute = func(cfg config.Runtime, request related.Request) (related.Response, error) {
		return related.Response{}, nil
	}
	flowExecute = func(cfg config.Runtime, request flow.Request) (flow.Response, error) {
		return flow.Response{}, errors.New("flow failed")
	}
	if _, err := Execute(config.Runtime{}, Request{Topic: "auth"}); err == nil || !strings.Contains(err.Error(), "flow failed") {
		t.Fatalf("expected flow error, got %v", err)
	}

	relatedExecute = func(cfg config.Runtime, request related.Request) (related.Response, error) {
		return related.Response{
			Related: []related.Result{{RefID: "md:guide.md#auth", Kind: "md_heading_block", Path: "guide.md"}},
		}, nil
	}
	contentExecute = func(cfg config.Runtime, request content.Request) (content.Response, error) {
		return content.Response{}, errors.New("content failed")
	}
	if _, err := loadRelatedRefs(config.Runtime{}, normalizedRequest{CodeExtensions: []string{".go"}, MaxRefs: 4}, []string{"md:guide.md#auth"}); err == nil || !strings.Contains(err.Error(), "content failed") {
		t.Fatalf("expected content error, got %v", err)
	}
}

func TestExecuteDocsOnlyAndSamplesOnlyPreferences(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	sampleRoot := filepath.Join(t.TempDir(), "samples")
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), strings.Join([]string{
		"# Auth Guide",
		"1. Configure docs pipeline.",
		"2. Validate request headers.",
	}, "\n"))
	mustWriteFile(t, filepath.Join(docsRoot, "deep-dive.md"), strings.Join([]string{
		"# Auth Deep Dive",
		"1. Configure auth middleware.",
		"2. Validate secure routes.",
	}, "\n"))
	mustWriteFile(t, filepath.Join(sampleRoot, "main.go"), strings.Join([]string{
		"package main",
		"func configureAuth() {}",
	}, "\n"))
	mustWriteFile(t, filepath.Join(sampleRoot, "routes.go"), strings.Join([]string{
		"package main",
		"func configureAuthRoutes() {}",
	}, "\n"))

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".go"},
		MaxFileBytes:   1_000_000,
	}

	docsOnly, err := Execute(cfg, Request{Topic: "configure auth", Preference: "docs", MaxRefs: 3})
	if err != nil {
		t.Fatalf("Execute docs preference returned error: %v", err)
	}
	if len(docsOnly.Docs) <= len(docsOnly.SampleCode) {
		t.Fatalf("expected docs-preferred response to prioritize docs refs, got docs=%d code=%d", len(docsOnly.Docs), len(docsOnly.SampleCode))
	}

	samplesOnly, err := Execute(cfg, Request{Topic: "configureAuth", Preference: "samples", Language: "go", MaxRefs: 3})
	if err != nil {
		t.Fatalf("Execute samples preference returned error: %v", err)
	}
	if len(samplesOnly.SampleCode) <= len(samplesOnly.Docs) {
		t.Fatalf("expected samples-preferred response to prioritize code refs, got docs=%d code=%d", len(samplesOnly.Docs), len(samplesOnly.SampleCode))
	}
}

func stubGuideDeps(t *testing.T) {
	t.Helper()
	prevSearch := searchExecute
	prevRelated := relatedExecute
	prevFlow := flowExecute
	prevContent := contentExecute
	t.Cleanup(func() {
		searchExecute = prevSearch
		relatedExecute = prevRelated
		flowExecute = prevFlow
		contentExecute = prevContent
	})
}

func mustWriteFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
