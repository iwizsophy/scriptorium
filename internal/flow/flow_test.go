package flow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/content"
)

func TestExecuteBuildsMarkdownNumberedSteps(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Flow\n1. 認証する\n2. 結果を返す\n")

	cfg := config.Runtime{
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds: []string{"md:guide.md#flow"},
		Style: "簡潔",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 2 {
		t.Fatalf("expected 2 flow steps, got %#v", response.Flow)
	}
	if response.Flow[0].Title != "認証する" || response.Flow[1].Title != "結果を返す" {
		t.Fatalf("unexpected markdown steps: %#v", response.Flow)
	}
	if response.Confidence != "medium" {
		t.Fatalf("expected medium confidence, got %q", response.Confidence)
	}
	if len(response.Assumptions) != 0 {
		t.Fatalf("expected no assumptions, got %#v", response.Assumptions)
	}
}

func TestExecuteUsesHeadingFallbackWhenNoExplicitSteps(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Flow\nintro\n## Prepare\nsetup\n## Execute\nrun\n")

	cfg := config.Runtime{
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds: []string{"md:guide.md#flow"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 2 {
		t.Fatalf("expected 2 heading-derived steps, got %#v", response.Flow)
	}
	if response.Assumptions[0] != "文書に明示的な番号付き手順がないため、見出し構造から流れを抽出しました。" {
		t.Fatalf("unexpected assumptions: %#v", response.Assumptions)
	}
}

func TestExecuteMergesMarkdownAndCodeForHighConfidence(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "auth.md"), "# Auth Flow\n1. 入力を確認する\n2. トークンを返す\n")
	mustWriteFile(
		t,
		filepath.Join(sampleRoot, "src", "auth.ts"),
		"export async function handleAuth() {\n  const token = await login()\n  return buildResponse(token)\n}\n",
	)

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds:   []string{"md:auth.md#auth-flow"},
		Sources: []string{"code:src/auth.ts@L2"},
		Style:   "detailed",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 3 {
		t.Fatalf("expected merged 3 steps, got %#v", response.Flow)
	}
	if response.Confidence != "high" {
		t.Fatalf("expected high confidence, got %q", response.Confidence)
	}
	if response.Flow[2].Title != "handleAuth" {
		t.Fatalf("unexpected code title: %#v", response.Flow[2])
	}
	if !strings.Contains(response.Flow[2].Description, "await login()") {
		t.Fatalf("unexpected code description: %#v", response.Flow[2])
	}
	if len(response.Assumptions) == 0 || response.Assumptions[0] != "コード参照はシグネチャと近傍の呼び出しから決定的に要約しています。" {
		t.Fatalf("unexpected assumptions: %#v", response.Assumptions)
	}
}

func TestExecuteReturnsTopicOnlyFallback(t *testing.T) {
	cfg := config.Runtime{}

	response, err := Execute(cfg, Request{
		Topic: "Authentication",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 1 || response.Flow[0].Title != "Authentication" {
		t.Fatalf("unexpected topic fallback: %#v", response.Flow)
	}
	if response.Assumptions[0] != "参照が与えられていないため、topic のみを返しました。" {
		t.Fatalf("unexpected assumptions: %#v", response.Assumptions)
	}
}

func TestExtractMarkdownStepsUsesBulletFallback(t *testing.T) {
	steps, assumptions := extractMarkdownSteps(content.Response{
		RefID:   "md:guide.md#flow",
		Path:    "guide.md",
		Content: "# Flow\n- 認証する\n- 結果を返す\n",
	}, 2)

	if len(assumptions) != 0 {
		t.Fatalf("expected no assumptions for bullet fallback, got %#v", assumptions)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 bullet steps, got %#v", steps)
	}
	if steps[0].Title != "認証する" || steps[1].Title != "結果を返す" {
		t.Fatalf("unexpected bullet steps: %#v", steps)
	}
}

func TestExtractMarkdownStepsUsesSingleBlockFallback(t *testing.T) {
	steps, assumptions := extractMarkdownSteps(content.Response{
		RefID:   "md:guide.md#flow",
		Path:    "guide.md",
		Content: "# Flow\n\n最初の段落です。\n続きの説明です。\n",
	}, 1)

	if len(steps) != 1 {
		t.Fatalf("expected single fallback step, got %#v", steps)
	}
	if steps[0].Title != "Flow" {
		t.Fatalf("unexpected fallback title: %#v", steps[0])
	}
	if !strings.Contains(steps[0].Description, "最初の段落です。 続きの説明です。") {
		t.Fatalf("unexpected fallback description: %#v", steps[0])
	}
	if len(assumptions) != 1 || assumptions[0] != "文書に明示的な手順がないため、見出しと本文から要点を抽出しました。" {
		t.Fatalf("unexpected assumptions: %#v", assumptions)
	}
}

func TestFirstParagraphSkipsHeadingsAndMergesParagraphs(t *testing.T) {
	got := firstParagraph("# Flow\n\n最初の段落\n続き\n\n## Detail\n\n二つ目\n", 2)
	want := "最初の段落 続き 二つ目"
	if got != want {
		t.Fatalf("unexpected paragraph extraction: got=%q want=%q", got, want)
	}
}

func TestCodeTitlePrefersRouteDescriptionOverSignature(t *testing.T) {
	lines := []string{
		"app.MapPost(\"/login\", handler);",
		"export async function handler() {",
		"  return ok();",
		"}",
	}

	got := codeTitle(lines, 2, "src/auth.ts")
	if got != "Handle POST /login route" {
		t.Fatalf("unexpected route title: %q", got)
	}
}

func TestSignatureTitleCoversArrowClassAndCSharpPatterns(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name: "arrow",
			lines: []string{
				"const buildToken = async () => {",
				"  return token;",
				"}",
			},
			want: "buildToken",
		},
		{
			name: "class",
			lines: []string{
				"export class AuthController {",
				"}",
			},
			want: "AuthController",
		},
		{
			name: "csharp",
			lines: []string{
				"public static Task HandleLogin(string input) {",
				"}",
			},
			want: "HandleLogin",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := signatureTitle(tc.lines, len(tc.lines)); got != tc.want {
				t.Fatalf("unexpected signature title: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestFirstNonEmptyReturnsFirstTrimmedLine(t *testing.T) {
	got := firstNonEmpty([]string{"", "   ", "\treturn token"})
	if got != "return token" {
		t.Fatalf("unexpected first non-empty line: %q", got)
	}
}

func TestMergeStepsDeduplicatesAndPrefersRicherData(t *testing.T) {
	merged := mergeSteps([]extractedStep{
		{
			Order:       10,
			Title:       "Authenticate user",
			Description: "short",
			Refs:        []string{"code:src/auth.ts@L10"},
			Explicit:    true,
			SourceKind:  "code",
		},
		{
			Order:       30,
			Title:       "Authenticate user",
			Description: "short",
			Refs:        []string{"md:guide.md#auth"},
			Explicit:    false,
			SourceKind:  "md",
		},
		{
			Order:       40,
			Title:       "Authenticate user",
			Description: "a much longer description",
			Refs:        []string{"md:guide.md#auth"},
			Explicit:    false,
			SourceKind:  "md",
		},
	})

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged steps, got %#v", merged)
	}
	if merged[0].Order != 10 || merged[0].Title != "Authenticate user" {
		t.Fatalf("unexpected primary merged step: %#v", merged[0])
	}
	if merged[0].Description != "short" {
		t.Fatalf("expected description to stay tied to the identical-key merge, got %#v", merged[0])
	}
	if !merged[0].Explicit {
		t.Fatalf("expected explicit flag to survive merge: %#v", merged[0])
	}
	if !slices.Contains(merged[0].Refs, "code:src/auth.ts@L10") || !slices.Contains(merged[0].Refs, "md:guide.md#auth") {
		t.Fatalf("expected merged refs from both sources, got %#v", merged[0].Refs)
	}
	if _, ok := merged[0].SourceKinds["code"]; !ok {
		t.Fatalf("expected code source kind in %#v", merged[0].SourceKinds)
	}
	if _, ok := merged[0].SourceKinds["md"]; !ok {
		t.Fatalf("expected md source kind in %#v", merged[0].SourceKinds)
	}
}

func TestSortMergedStepsUsesNormalizedTitleTiebreak(t *testing.T) {
	steps := []mergedStep{
		{Order: 200, Title: "Later"},
		{Order: 100, Title: "Ｂeta"},
		{Order: 100, Title: " alpha "},
	}

	sortMergedSteps(steps)

	if got := []string{steps[0].Title, steps[1].Title, steps[2].Title}; !slices.Equal(got, []string{" alpha ", "Ｂeta", "Later"}) {
		t.Fatalf("unexpected sorted merged steps: %#v", got)
	}
}

func TestNormalizeRequestAndStyleHelpers(t *testing.T) {
	normalized := normalizeRequest(Request{
		Topic:    " topic ",
		Seeds:    []string{" md:guide.md#flow ", "md:guide.md#flow", "code:src/app.ts@L2"},
		Sources:  []string{"code:src/app.ts@L2", "code:src/helper.ts@L3"},
		Style:    "詳細",
		MaxSteps: 99,
	})
	if normalized.Topic != "topic" {
		t.Fatalf("unexpected normalized topic: %#v", normalized)
	}
	if !slices.Equal(normalized.Refs, []string{"md:guide.md#flow", "code:src/app.ts@L2", "code:src/helper.ts@L3"}) {
		t.Fatalf("unexpected normalized refs: %#v", normalized.Refs)
	}
	if normalized.Style != "detailed" || normalized.MaxSteps != 50 {
		t.Fatalf("unexpected normalized request values: %#v", normalized)
	}

	if normalizeStyle("簡潔") != "brief" || normalizeStyle("other") != "default" {
		t.Fatalf("unexpected style normalization")
	}
	if clamp(0, 1, 4, 3) != 3 || clamp(-1, 1, 4, 3) != 1 || clamp(9, 1, 4, 3) != 4 {
		t.Fatalf("unexpected clamp behavior")
	}
}

func TestMarkdownAndCodeHelperBranches(t *testing.T) {
	labeled := numberedOrLabeledSteps([]string{"手順1: 認証する", "step"}, "md:guide.md#flow", 2)
	if len(labeled) != 1 || labeled[0].Title != "認証する" || !labeled[0].Explicit {
		t.Fatalf("unexpected labeled markdown steps: %#v", labeled)
	}
	if got := bulletSteps([]string{"plain", "- one", "* two"}, "md:guide.md#flow", 1); len(got) != 2 {
		t.Fatalf("unexpected bullet steps: %#v", got)
	}

	steps, assumptions := extractCodeSteps("code:src/app.ts@L2", 2, content.Response{
		RefID:     "code:src/app.ts@L2",
		Path:      "src/app.ts",
		Content:   "const token = buildToken()\nreturn token\n",
		Truncated: true,
	}, "brief", 1)
	if len(steps) != 1 || len(assumptions) != 2 {
		t.Fatalf("unexpected extracted code steps: %#v assumptions=%#v", steps, assumptions)
	}
	if !strings.Contains(steps[0].Description, "buildToken()") {
		t.Fatalf("unexpected code step description: %#v", steps[0])
	}

	if got := codeTitle([]string{"", ""}, 1, "src/app.ts"); got != "src/app.ts" {
		t.Fatalf("expected path fallback title, got %q", got)
	}
	if got := routeDescription([]string{"app.MapGet(\"/x\", h)", ""}, 5); got != "Handle GET /x route" {
		t.Fatalf("unexpected routeDescription: %q", got)
	}
	if got := signatureTitle([]string{"plain", "value"}, 2); got != "" {
		t.Fatalf("expected empty signatureTitle, got %q", got)
	}

	detailedOps := codeOperations([]string{
		"plain",
		"await login()",
		"if (ready) {",
		"return token",
		"new AuthService()",
	}, 3, "detailed")
	if !slices.Equal(detailedOps, []string{"await login()", "if (ready) {", "return token"}) {
		t.Fatalf("unexpected detailed operations: %#v", detailedOps)
	}
	if describeOperation("value = buildToken()") != "value = buildToken()" {
		t.Fatalf("expected assignment call operation")
	}
	if describeOperation("") != "" {
		t.Fatalf("expected empty describeOperation result")
	}
}

func TestMergeConfidenceAndTextHelpers(t *testing.T) {
	merged := mergeSteps([]extractedStep{
		{Order: 5, Title: "", Description: "", SourceKind: "md"},
		{Order: 2, Title: "Reference setup", Description: "desc", Refs: []string{"r1"}, SourceKind: "md"},
		{Order: 1, Title: "Setup", Description: "desc", Refs: []string{"r2"}, SourceKind: "code"},
	})
	if len(merged) != 2 {
		t.Fatalf("unexpected merged step count: %#v", merged)
	}
	if merged[0].Order != 2 || merged[0].Title != "Reference setup" {
		t.Fatalf("unexpected first merged step ordering: %#v", merged[0])
	}
	if merged[1].Order != 1 || merged[1].Title != "Setup" {
		t.Fatalf("unexpected second merged step ordering: %#v", merged[1])
	}

	high := confidenceFor([]mergedStep{
		{Explicit: true, SourceKinds: map[string]struct{}{"md": {}}},
		{Explicit: true, SourceKinds: map[string]struct{}{"code": {}}},
		{Explicit: true, SourceKinds: map[string]struct{}{"md": {}, "code": {}}},
	})
	if high != "high" {
		t.Fatalf("expected high confidence, got %q", high)
	}
	medium := confidenceFor([]mergedStep{
		{Explicit: false, SourceKinds: map[string]struct{}{"md": {}}},
		{Explicit: false, SourceKinds: map[string]struct{}{"md": {}}},
	})
	if medium != "medium" {
		t.Fatalf("expected medium confidence, got %q", medium)
	}
	if low := confidenceFor([]mergedStep{{Explicit: false, SourceKinds: map[string]struct{}{"code": {}}}}); low != "low" {
		t.Fatalf("expected low confidence, got %q", low)
	}

	if got := normalizeText(" Ａuth\tFlow "); got != "auth flow" {
		t.Fatalf("unexpected normalizeText: %q", got)
	}
	if got := dedupeStrings([]string{"a", "a", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("unexpected dedupeStrings: %#v", got)
	}
	if min(1, 2) != 1 || max(1, 2) != 2 {
		t.Fatalf("unexpected min/max behavior")
	}
}

func TestExecutePropagatesContentErrorsAndCodeFallbacks(t *testing.T) {
	cfg := config.Runtime{
		DocsRoot:     filepath.Join(t.TempDir(), "missing"),
		MaxFileBytes: 1_000_000,
	}
	if _, err := Execute(cfg, Request{Seeds: []string{"md:guide.md#flow"}}); err == nil {
		t.Fatal("expected markdown content error to propagate")
	}

	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "plain.ts"), "line one\nline two\n")
	cfg = config.Runtime{
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}
	response, err := Execute(cfg, Request{Seeds: []string{"code:src/plain.ts@L1"}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 1 || response.Flow[0].Title != "line one" || response.Flow[0].Description != "line one" {
		t.Fatalf("expected code fallback title/description, got %#v", response.Flow)
	}
}

func TestFlowHelperFallbackBranches(t *testing.T) {
	if steps, assumptions := extractMarkdownSteps(content.Response{
		RefID:   "md:empty.md#flow",
		Path:    "empty.md",
		Content: "",
	}, 1); steps != nil || len(assumptions) != 1 {
		t.Fatalf("expected empty markdown fallback assumptions, got steps=%#v assumptions=%#v", steps, assumptions)
	}

	if got := firstParagraph("## Heading\n\n", 1); got != "" {
		t.Fatalf("expected empty firstParagraph, got %q", got)
	}

	if got := routeDescription([]string{"plain", "value"}, 2); got != "" {
		t.Fatalf("expected empty routeDescription, got %q", got)
	}

	if got := codeOperations([]string{"plain", "value"}, 1, "brief"); len(got) != 0 {
		t.Fatalf("expected no code operations, got %#v", got)
	}

	if got := describeOperation("service.Call()"); got != "service.Call()" {
		t.Fatalf("expected generic call operation, got %q", got)
	}

	merged := mergeSteps([]extractedStep{
		{Order: 30, Title: "Reference login", Description: "short", Refs: []string{"r1"}, Explicit: false, SourceKind: "md"},
		{Order: 10, Title: "Reference  login", Description: " short ", Refs: []string{"r2"}, Explicit: true, SourceKind: "code"},
	})
	if len(merged) != 1 || merged[0].Description != " short " || merged[0].Order != 10 || !merged[0].Explicit {
		t.Fatalf("expected normalized-key merge update, got %#v", merged)
	}

	if medium := confidenceFor([]mergedStep{{Explicit: true, SourceKinds: map[string]struct{}{"code": {}}}, {Explicit: false, SourceKinds: map[string]struct{}{"code": {}}}}); medium != "medium" {
		t.Fatalf("expected medium confidence from explicit steps, got %q", medium)
	}
}

func TestFlowAdditionalHelperBranches(t *testing.T) {
	normalized := normalizeRequest(Request{
		Seeds:    []string{" md:guide.md#flow ", "code:src/app.ts@L2"},
		Sources:  []string{"code:src/helper.ts@L3"},
		MaxSteps: 1,
	})
	if !slices.Equal(normalized.Refs, []string{"md:guide.md#flow"}) || normalized.MaxSteps != 1 {
		t.Fatalf("expected normalizeRequest to trim and truncate refs, got %#v", normalized)
	}

	if got := routeDescription([]string{`router.post("/login", handler)`}, 1); got != "Handle POST /login route" {
		t.Fatalf("unexpected express routeDescription: %q", got)
	}
	if got := codeTitle([]string{"plain", "throw err"}, 2, "src/app.ts"); got != "throw err" {
		t.Fatalf("expected codeTitle to fall back to operation summary, got %q", got)
	}

	if got := describeOperation(`app.MapDelete("/users/{id}", handler)`); got != "route handling DELETE /users/{id}" {
		t.Fatalf("unexpected minimal-api operation: %q", got)
	}
	if got := describeOperation(`router.patch("/users/{id}", handler)`); got != "route handling PATCH /users/{id}" {
		t.Fatalf("unexpected express operation: %q", got)
	}
	if got := describeOperation("throw err"); got != "throw err" {
		t.Fatalf("unexpected throw operation: %q", got)
	}
	if got := describeOperation("const svc = new AuthService()"); got != "const svc = new AuthService()" {
		t.Fatalf("unexpected new-expression operation: %q", got)
	}
	if got := describeOperation("} else if (ready) {"); got != "} else if (ready) {" {
		t.Fatalf("unexpected inline if operation: %q", got)
	}
}

func TestExecuteAdditionalBranches(t *testing.T) {
	if _, err := Execute(config.Runtime{}, Request{Seeds: []string{"bad-ref"}}); err == nil {
		t.Fatal("expected Execute to reject invalid ref ids")
	}

	response, err := Execute(config.Runtime{}, Request{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 0 || len(response.Assumptions) != 0 || response.Confidence != "low" {
		t.Fatalf("expected empty low-confidence response without refs or topic, got %#v", response)
	}
}

func TestExecuteWithFileSeedFallsBackToTopic(t *testing.T) {
	response, err := Execute(config.Runtime{}, Request{
		Topic:   "File topic",
		Sources: []string{"file:docs/readme.md"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 1 || response.Flow[0].Title != "File topic" || response.Confidence != "low" {
		t.Fatalf("expected topic fallback for non-md/code ref, got %#v", response)
	}
}

func TestExecutePropagatesCodeContentErrorsAndTrimsToMaxSteps(t *testing.T) {
	cfg := config.Runtime{
		SampleRoots:    []string{filepath.Join(t.TempDir(), "missing")},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}
	if _, err := Execute(cfg, Request{Seeds: []string{"code:src/app.ts@L1"}}); err == nil {
		t.Fatal("expected code content error to propagate")
	}

	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\n1. First\n2. Second\n")
	response, err := Execute(config.Runtime{
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}, Request{
		Seeds:    []string{"md:guide.md#guide"},
		Sources:  []string{"", "  ", "md:guide.md#guide"},
		MaxSteps: 1,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Flow) != 1 || response.Flow[0].Title != "First" {
		t.Fatalf("expected Execute to trim merged flow steps, got %#v", response)
	}
}

func TestFlowHelperBranches(t *testing.T) {
	if got := signatureTitle([]string{"const handler = async () => {}", "class Startup {}", "public async Task Run() {"}, 3); got != "Run" {
		t.Fatalf("expected csharp signature match nearest to focus line, got %q", got)
	}
	if got := signatureTitle([]string{"const handler = async () => {}", "class Startup {}"}, 1); got != "handler" {
		t.Fatalf("expected arrow signature title, got %q", got)
	}
	if got := signatureTitle([]string{"class Startup {}"}, 1); got != "Startup" {
		t.Fatalf("expected class signature title, got %q", got)
	}
	if got := codeTitle([]string{""}, 1, "src/app.ts"); got != "src/app.ts" {
		t.Fatalf("expected codeTitle to fall back to path, got %q", got)
	}

	ops := codeOperations([]string{
		"",
		"await auth.Login()",
		"if (ready) {",
		"service.Run()",
		"throw err",
	}, 3, "detailed")
	if len(ops) != 3 || ops[0] != "await auth.Login()" || ops[1] != "if (ready) {" || ops[2] != "service.Run()" {
		t.Fatalf("unexpected detailed code operations: %#v", ops)
	}

	if got := confidenceFor([]mergedStep{
		{Explicit: true, SourceKinds: map[string]struct{}{"md": {}}},
		{Explicit: true, SourceKinds: map[string]struct{}{"code": {}}},
		{Explicit: false, SourceKinds: map[string]struct{}{"md": {}, "code": {}}},
	}); got != "high" {
		t.Fatalf("expected high confidence, got %q", got)
	}
	if got := confidenceFor([]mergedStep{
		{Explicit: true, SourceKinds: map[string]struct{}{"code": {}}},
		{Explicit: false, SourceKinds: map[string]struct{}{"code": {}}},
	}); got != "medium" {
		t.Fatalf("expected medium confidence from explicit code step, got %q", got)
	}

	if steps := numberedOrLabeledSteps([]string{"手順 1: ログインする", "plain"}, "md:guide.md#flow", 2); len(steps) != 1 || steps[0].Title != "ログインする" {
		t.Fatalf("expected labeled japanese markdown step, got %#v", steps)
	}

	codeSteps, assumptions := extractCodeSteps("code:src/blank.ts@L1", 1, content.Response{
		RefID:   "code:src/blank.ts@L1",
		Path:    "src/blank.ts",
		Content: "",
	}, "default", 1)
	if len(codeSteps) != 1 || codeSteps[0].Description != "src/blank.ts" || len(assumptions) != 1 {
		t.Fatalf("expected empty code fallback to path, got steps=%#v assumptions=%#v", codeSteps, assumptions)
	}

	if normalized := normalizeRequest(Request{
		Seeds:    []string{" ", "md:guide.md#flow"},
		Sources:  []string{"", "code:src/app.ts@L2"},
		MaxSteps: 5,
	}); !slices.Equal(normalized.Refs, []string{"md:guide.md#flow", "code:src/app.ts@L2"}) {
		t.Fatalf("expected normalizeRequest to skip empty refs, got %#v", normalized)
	}

	if got := firstParagraph("one\n\nsecond\n\nthird", 1); got != "one" {
		t.Fatalf("expected firstParagraph to stop after requested paragraph count, got %q", got)
	}

	multiBlockSteps, multiAssumptions := extractMarkdownSteps(content.Response{
		RefID: "md:guide.md#guide",
		Path:  "guide.md",
		Content: strings.Join([]string{
			"# Guide",
			"",
			"intro",
			"",
			"## Configure",
			"",
			"",
		}, "\n"),
	}, 2)
	if len(multiBlockSteps) != 1 || multiBlockSteps[0].Description != "Configure" || len(multiAssumptions) != 1 {
		t.Fatalf("expected heading fallback description for empty subsection, got steps=%#v assumptions=%#v", multiBlockSteps, multiAssumptions)
	}

	singleBlockSteps, singleAssumptions := extractMarkdownSteps(content.Response{
		RefID:   "md:solo.md#solo",
		Path:    "solo.md",
		Content: "# Solo\n\n",
	}, 3)
	if len(singleBlockSteps) != 1 || singleBlockSteps[0].Description != "Solo" || len(singleAssumptions) != 1 {
		t.Fatalf("expected single-block heading fallback, got steps=%#v assumptions=%#v", singleBlockSteps, singleAssumptions)
	}

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
