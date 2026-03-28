package related

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/gitsnapshot"
	"github.com/iwizsophy/scriptorium/internal/source"
)

func TestExecuteScoresDocsAndCodeFromSeeds(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(docsRoot, "guides", "auth.md"), "# Auth Flow\n1. Import service\n2. Return token\n")
	mustWriteFile(t, filepath.Join(docsRoot, "guides", "session.md"), "# Session Flow\n1. Import service\n2. Return session token\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "service.ts"), "import { login } from './auth'\nexport async function runFlow() {\n  const token = await login()\n  return token\n}\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "helper.ts"), "export function noop() {\n  return 1\n}\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds:        []string{"md:guides/auth.md#auth-flow", "code:src/service.ts@L2"},
		Budget:       10,
		SnippetLines: 4,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Related) == 0 {
		t.Fatal("expected related results")
	}

	first := response.Related[0]
	if first.Path != "guides/session.md" {
		t.Fatalf("expected session doc to rank first, got %#v", first)
	}
	if len(first.Reasons) == 0 {
		t.Fatalf("expected reasons on top result, got %#v", first)
	}

	var foundCode bool
	for _, result := range response.Related {
		if result.Path == "src/service.ts" {
			t.Fatalf("seed code ref should not be returned: %#v", result)
		}
		if result.Path == "src/helper.ts" {
			foundCode = true
			if result.Kind != "code_range" {
				t.Fatalf("expected code_range result, got %#v", result)
			}
		}
	}
	if !foundCode {
		t.Fatalf("expected helper code result in %#v", response.Related)
	}
}

func TestExecuteHonorsSignalFilteringAndCodeExtensionFiltering(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "api.ts"), "import { helper } from './helper'\nexport function callApi() {\n  return helper()\n}\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "api.cs"), "using Demo.Api;\npublic class ApiCall {\n    public int Run() {\n        return 1;\n    }\n}\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts", ".cs"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds:      []string{"code:src/api.ts@L2"},
		Extensions: []string{".cs"},
		Signals:    []string{"same_file_hits"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Related) != 0 {
		t.Fatalf("expected no results with same_file_hits-only and .cs filter, got %#v", response.Related)
	}
}

func TestExecuteImplementationModeSurfacesFrameworkLinksAcrossFiles(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(docsRoot, "aspnet-auth.md"), "# Auth\nConfigure authentication and routing.\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "Controllers", "SecureController.cs"), "[Authorize]\npublic class SecureController {\n    [HttpGet(\"/secure\")]\n    public string Get() => \"ok\";\n}\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "Program.cs"), "var builder = WebApplication.CreateBuilder(args);\nbuilder.Services.AddAuthentication();\napp.MapControllers();\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".cs"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds:      []string{"code:Controllers/SecureController.cs@L2", "md:aspnet-auth.md#auth"},
		Budget:     10,
		Mode:       "implementation",
		Extensions: []string{".md", ".cs"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	foundProgram := false
	for _, result := range response.Related {
		if strings.EqualFold(result.Path, "Program.cs") {
			foundProgram = true
			hasFrameworkReason := false
			for _, reason := range result.Reasons {
				if reason.Type == "framework_links" {
					hasFrameworkReason = true
					break
				}
			}
			if !hasFrameworkReason {
				t.Fatalf("expected framework_links reason on %#v", result)
			}
		}
	}
	if !foundProgram {
		t.Fatalf("expected Program.cs to be related in %#v", response.Related)
	}
}

func TestRelatedHelperBranches(t *testing.T) {
	normalized, err := normalizeRequest(Request{
		Seeds:        []string{" md:guide.md#auth ", "md:guide.md#auth", "code:src/app.ts@L2"},
		Extensions:   []string{" ts ", ".md", ".TS"},
		Budget:       999,
		SnippetLines: -1,
		Signals:      []string{"imports", "invalid", "framework_links"},
		Mode:         "IMPLEMENTATION",
	}, []string{".go"})
	if err != nil {
		t.Fatalf("normalizeRequest returned error: %v", err)
	}
	if !slices.Equal(normalized.Seeds, []string{"md:guide.md#auth", "code:src/app.ts@L2"}) {
		t.Fatalf("unexpected normalized seeds: %#v", normalized.Seeds)
	}
	if normalized.Budget != 200 || normalized.SnippetLines != 1 || normalized.Mode != "implementation" {
		t.Fatalf("unexpected normalized request values: %#v", normalized)
	}
	if !normalized.SearchDocs || !slices.Equal(normalized.CodeExtensions, []string{".ts"}) {
		t.Fatalf("unexpected docs/code normalization: %#v", normalized)
	}
	if !normalized.Signals["imports"] || !normalized.Signals["framework_links"] || normalized.Signals["invalid"] {
		t.Fatalf("unexpected signal normalization: %#v", normalized.Signals)
	}

	defaultSignals := normalizeSignals(nil)
	if len(defaultSignals) != 0 {
		t.Fatalf("expected empty explicit signal set, got %#v", defaultSignals)
	}
	if normalizeMode("other") != "default" {
		t.Fatalf("expected default mode normalization")
	}
	if _, err := normalizeRequest(Request{}, []string{".go"}); err == nil {
		t.Fatal("expected missing seeds error")
	}

	lines := []string{"plain", "return authToken", "await login()", "final"}
	rangeHit := codeCandidateRange(lines, []seedContext{{OverlapTokens: []string{"authtoken"}}}, 2)
	if rangeHit.StartLine != 1 || rangeHit.EndLine != 2 {
		t.Fatalf("unexpected code candidate range hit: %#v", rangeHit)
	}
	rangeFallback := codeCandidateRange(lines, []seedContext{{OverlapTokens: []string{"missing"}}}, 3)
	if rangeFallback.StartLine != 1 || rangeFallback.EndLine != 3 {
		t.Fatalf("unexpected code candidate fallback: %#v", rangeFallback)
	}

	item := candidate{
		RefID:   "code:src/program.cs@L1",
		Kind:    "code_range",
		Path:    "src/Program.cs",
		Range:   Range{StartLine: 1, EndLine: 2},
		Tokens:  []string{"auth", "token", "secure"},
		Imports: map[string]struct{}{"service": {}},
		Tags:    map[string]struct{}{"setup_file": {}, "di_registration": {}},
	}
	seeds := []seedContext{{
		RefID:         "md:guide.md#auth",
		Kind:          "md_heading_block",
		Path:          "docs/guide.md",
		OverlapTokens: []string{"auth", "token"},
		ImportTokens:  map[string]struct{}{"service": {}},
		FrameworkTags: map[string]struct{}{"route_decl": {}, "attribute": {}},
	}}
	scored := scoreCandidate(item, seeds, map[string]bool{
		"imports":         true,
		"token_overlap":   true,
		"path_proximity":  true,
		"same_file_hits":  true,
		"framework_links": true,
	})
	if scored == nil || scored.Score <= 0 || len(scored.Reasons) < 3 {
		t.Fatalf("expected scored candidate with multiple reasons, got %#v", scored)
	}
	if scored.Reasons[0].Type > scored.Reasons[len(scored.Reasons)-1].Type {
		t.Fatalf("expected sorted reasons, got %#v", scored.Reasons)
	}
	if scoreCandidate(item, seeds, map[string]bool{}) != nil {
		t.Fatalf("expected nil score when all signals disabled")
	}

	if got := overlapTokens("Auth auth token secure route"); len(got) == 0 {
		t.Fatalf("expected overlap tokens, got %#v", got)
	}
	imports := extractImportTokens("import auth from './auth'\nconst x = 1\nusing Demo.Auth;\nrequire('fs')\n")
	if _, ok := imports["auth"]; !ok {
		t.Fatalf("expected import token extraction, got %#v", imports)
	}
	tags := extractFrameworkTags("Program.cs", "builder.Services.AddAuthentication(); app.MapGet(\"/secure\", () => {}); [Authorize] configuration[\"auth.token\"] = true")
	for _, key := range []string{"setup_file", "di_registration", "route_decl", "attribute", "config"} {
		if _, ok := tags[key]; !ok {
			t.Fatalf("expected framework tag %q in %#v", key, tags)
		}
	}
	if !complementaryFrameworkRelation(map[string]struct{}{"setup_file": {}}, map[string]struct{}{"route_decl": {}}) {
		t.Fatalf("expected complementary framework relation")
	}
	if hasTag(tags, "missing") {
		t.Fatalf("did not expect missing tag")
	}
	if !isImportLine("using Demo.Auth;") || !isImportLine("const x = require('fs')") || isImportLine("return value") {
		t.Fatalf("unexpected import line detection")
	}
	if countOverlap(map[string]struct{}{"a": {}, "b": {}}, map[string]struct{}{"b": {}, "c": {}}) != 1 {
		t.Fatalf("unexpected countOverlap")
	}
	if countSliceOverlap([]string{"a", "b", "b"}, []string{"b", "c"}) != 2 {
		t.Fatalf("unexpected countSliceOverlap")
	}
	if proximity := pathProximity("src/api/auth.ts", "src/api/session.ts"); proximity <= 0 {
		t.Fatalf("expected positive path proximity, got %f", proximity)
	}
	if far := pathProximity("a/b/c/d/e/f", "x/y/z/u/v/w"); far != 0 {
		t.Fatalf("expected zero path proximity for sufficiently distant paths, got %f", far)
	}
	if got := splitPathSegments("@feature/main/src/app.ts"); !slices.Equal(got, []string{"feature", "main", "src", "app.ts"}) {
		t.Fatalf("unexpected splitPathSegments: %#v", got)
	}
	if normalizeExtensions(nil) != nil {
		t.Fatalf("expected nil normalized extensions for empty input")
	}
	if got := normalizeExtensions([]string{"ts", ".TS", "", ".md"}); !slices.Equal(got, []string{".ts", ".md"}) {
		t.Fatalf("unexpected normalizeExtensions result: %#v", got)
	}
	if !containsString([]string{".ts"}, ".ts") || containsString([]string{".ts"}, ".go") {
		t.Fatalf("unexpected containsString behavior")
	}
	if !containsAny("builder services", "services") || containsAny("plain", "route") {
		t.Fatalf("unexpected containsAny behavior")
	}
	if clamp(0, 1, 3, 2) != 2 || clamp(-1, 1, 3, 2) != 1 || clamp(9, 1, 3, 2) != 3 {
		t.Fatalf("unexpected clamp behavior")
	}
	if min(1, 2) != 1 || max(1, 2) != 2 || minFloat(0.5, 0.2) != 0.2 {
		t.Fatalf("unexpected numeric helper behavior")
	}
	if got := splitPathSegments(""); got != nil {
		t.Fatalf("expected nil splitPathSegments for empty input, got %#v", got)
	}
	if complementaryFrameworkRelation(map[string]struct{}{"route_decl": {}}, map[string]struct{}{"attribute": {}}) {
		t.Fatalf("did not expect complementary relation for runtime-only tags")
	}
	if !hasTag(tags, "config") {
		t.Fatalf("expected hasTag positive branch")
	}
	adjusted := scoreCandidate(candidate{
		RefID:   "code:src/program.cs@L1",
		Kind:    "code_range",
		Path:    "src/Program.cs",
		Range:   Range{StartLine: 1, EndLine: 2},
		Tokens:  []string{"route"},
		Imports: map[string]struct{}{},
		Tags:    map[string]struct{}{"route_decl": {}},
	}, []seedContext{{
		RefID:         "md:guide.md#setup",
		Kind:          "md_heading_block",
		Path:          "docs/guide.md",
		OverlapTokens: []string{"setup"},
		ImportTokens:  map[string]struct{}{},
		FrameworkTags: map[string]struct{}{"setup_file": {}},
	}}, map[string]bool{"framework_links": true})
	if adjusted == nil || adjusted.Score < 0.2 {
		t.Fatalf("expected cross-kind framework adjustment, got %#v", adjusted)
	}
}

func TestExecutePropagatesSeedResolutionErrors(t *testing.T) {
	cfg := config.Runtime{
		MaxFileBytes: 1_000_000,
	}
	if _, err := Execute(cfg, Request{
		Seeds: []string{"bad-ref"},
	}); err == nil {
		t.Fatal("expected Execute to propagate invalid seed resolution error")
	}
}

func TestExecuteRejectsMissingSeeds(t *testing.T) {
	if _, err := Execute(config.Runtime{}, Request{}); err == nil {
		t.Fatal("expected Execute to reject missing seeds")
	}
}

func TestExecuteTruncatesSortedResultsToBudget(t *testing.T) {
	docsRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(docsRoot, "auth.md"), "# Auth\nshared import token\n")
	mustWriteFile(t, filepath.Join(docsRoot, "session.md"), "# Session\nshared import token extra\n")
	mustWriteFile(t, filepath.Join(docsRoot, "token.md"), "# Token\nshared import token extra extra\n")

	cfg := config.Runtime{
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds:  []string{"md:auth.md#auth"},
		Budget: 1,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Related) != 1 {
		t.Fatalf("expected budget truncation to one result, got %#v", response.Related)
	}
}

func TestExecutePropagatesDocsAndCodeRuntimeErrors(t *testing.T) {
	t.Run("docs scan error", func(t *testing.T) {
		docsRoot := t.TempDir()
		sampleRoot := t.TempDir()
		mustWriteFile(t, filepath.Join(docsRoot, "seed.md"), "# Seed\nauth token\n")
		if err := os.WriteFile(filepath.Join(docsRoot, "broken.md"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
		mustWriteFile(t, filepath.Join(sampleRoot, "src", "seed.ts"), "export function seed() { return 'ok' }\n")

		_, err := Execute(config.Runtime{
			DocsRoot:       docsRoot,
			SampleRoots:    []string{sampleRoot},
			CodeExtensions: []string{".ts"},
			MaxFileBytes:   1_000_000,
		}, Request{
			Seeds: []string{"code:src/seed.ts@L1"},
		})
		if err == nil {
			t.Fatal("expected Execute to propagate docs scan error")
		}
	})

	t.Run("code read error", func(t *testing.T) {
		docsRoot := t.TempDir()
		sampleRoot := t.TempDir()
		mustWriteFile(t, filepath.Join(docsRoot, "seed.md"), "# Seed\nauth token\n")
		if err := os.WriteFile(filepath.Join(sampleRoot, "broken.ts"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}

		_, err := Execute(config.Runtime{
			DocsRoot:       docsRoot,
			SampleRoots:    []string{sampleRoot},
			CodeExtensions: []string{".ts"},
			MaxFileBytes:   1_000_000,
		}, Request{
			Seeds:      []string{"md:seed.md#seed"},
			Extensions: []string{".ts"},
		})
		if err == nil {
			t.Fatal("expected Execute to propagate code read error")
		}
	})
}

func TestRelatedDocsAndCodePropagateSourceErrors(t *testing.T) {
	signals := map[string]bool{
		"token_overlap": true,
	}

	if _, err := relatedDocs(config.Runtime{
		DocsRoot:     filepath.Join(t.TempDir(), "missing"),
		MaxFileBytes: 1_000_000,
	}, normalizedRequest{
		SearchDocs: true,
		Signals:    signals,
	}, []seedContext{{RefID: "md:guide.md#auth", Path: "guide.md"}}, map[string]struct{}{}); err == nil {
		t.Fatal("expected relatedDocs to propagate docs root error")
	}

	if _, err := relatedCode(config.Runtime{
		SampleRoots:    []string{filepath.Join(t.TempDir(), "missing")},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, normalizedRequest{
		CodeExtensions: []string{".ts"},
		Signals:        signals,
		SnippetLines:   3,
	}, []seedContext{{RefID: "code:src/app.ts@L1", Path: "src/app.ts"}}, map[string]struct{}{}, map[string]struct{}{}); err == nil {
		t.Fatal("expected relatedCode to propagate sample root error")
	}
}

func TestAdditionalHelperBranches(t *testing.T) {
	sameFile := scoreCandidate(candidate{
		RefID: "code:src/app.ts@L1",
		Kind:  "code_range",
		Path:  "src/app.ts",
		Range: Range{StartLine: 1, EndLine: 2},
	}, []seedContext{{
		RefID: "code:src/app.ts@L9",
		Kind:  "code_range",
		Path:  "src/app.ts",
	}}, map[string]bool{"same_file_hits": true})
	if sameFile == nil || sameFile.Score != 0.05 || len(sameFile.Reasons) != 1 || sameFile.Reasons[0].Type != "same_file_hits" {
		t.Fatalf("expected same-file-only score, got %#v", sameFile)
	}

	if got := splitPathSegments("  @root//src/app.ts "); !slices.Equal(got, []string{"root", "src", "app.ts"}) {
		t.Fatalf("unexpected splitPathSegments normalization: %#v", got)
	}
	if min(3, 2) != 2 || max(1, 3) != 3 || minFloat(0.2, 0.5) != 0.2 {
		t.Fatalf("unexpected numeric helper branches")
	}

	normalized, err := normalizeRequest(Request{
		Seeds:      []string{"", " md:guide.md#auth "},
		Extensions: []string{".ts"},
	}, []string{".go"})
	if err != nil {
		t.Fatalf("normalizeRequest returned error: %v", err)
	}
	if !slices.Equal(normalized.Seeds, []string{"md:guide.md#auth"}) {
		t.Fatalf("expected empty seed to be skipped, got %#v", normalized.Seeds)
	}

	pathOnly := scoreCandidate(candidate{
		RefID: "code:src/other.ts@L1",
		Kind:  "code_range",
		Path:  "alpha/beta/gamma/delta/epsilon/zeta.ts",
	}, []seedContext{{
		RefID: "code:elsewhere/file.ts@L1",
		Kind:  "code_range",
		Path:  "one/two/three/four/five/six/seven/eight.md",
	}}, map[string]bool{"path_proximity": true})
	if pathOnly != nil {
		t.Fatalf("expected far path proximity to produce no score, got %#v", pathOnly)
	}

	crossKindFloor := scoreCandidate(candidate{
		RefID: "code:src/program.cs@L1",
		Kind:  "code_range",
		Path:  "src/Program.cs",
		Tags:  map[string]struct{}{"route_decl": {}},
	}, []seedContext{{
		RefID:         "md:guide.md#route",
		Kind:          "md_heading_block",
		Path:          "docs/guide.md",
		FrameworkTags: map[string]struct{}{"route_decl": {}},
	}}, map[string]bool{"framework_links": true})
	if crossKindFloor == nil || crossKindFloor.Score != 0.2 {
		t.Fatalf("expected cross-kind framework floor of 0.2, got %#v", crossKindFloor)
	}
}

func TestExecuteReturnsEmptyWhenNoSearchSurfaceIsEnabled(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nauth seed\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Seeds:      []string{"md:guide.md#guide"},
		Extensions: []string{".go"},
		Budget:     5,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Related) != 0 {
		t.Fatalf("expected no related results without docs or code search surfaces, got %#v", response.Related)
	}
}

func TestRelatedDocsAndCodeSkipBranches(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nauth shared token\n")
	mustWriteFile(t, filepath.Join(docsRoot, "other.md"), "# Other\nplain unrelated body\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "seed.ts"), "export function seed() {\n  return authToken\n}\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "other.ts"), "export function other() {\n  return noop\n}\n")

	signals := map[string]bool{
		"token_overlap": true,
	}

	docsResults, err := relatedDocs(config.Runtime{
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}, normalizedRequest{
		SearchDocs: true,
		Signals:    signals,
	}, []seedContext{{
		RefID:         "md:guide.md#guide",
		Kind:          "md_heading_block",
		Path:          "guide.md",
		OverlapTokens: []string{"auth", "shared"},
		ImportTokens:  map[string]struct{}{},
		FrameworkTags: map[string]struct{}{},
	}}, map[string]struct{}{"md:guide.md#guide": {}})
	if err != nil {
		t.Fatalf("relatedDocs returned error: %v", err)
	}
	if len(docsResults) != 0 {
		t.Fatalf("expected docs skip/nil-score branches to yield no results, got %#v", docsResults)
	}

	codeResults, err := relatedCode(config.Runtime{
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, normalizedRequest{
		CodeExtensions: []string{".ts"},
		Signals:        signals,
		SnippetLines:   3,
	}, []seedContext{{
		RefID:         "file:src/seed.ts",
		Kind:          "file",
		Path:          "src/seed.ts",
		OverlapTokens: []string{"auth", "token"},
		ImportTokens:  map[string]struct{}{},
		FrameworkTags: map[string]struct{}{},
	}}, map[string]struct{}{"file:src/seed.ts": {}}, map[string]struct{}{})
	if err != nil {
		t.Fatalf("relatedCode returned error: %v", err)
	}
	if len(codeResults) != 0 {
		t.Fatalf("expected code skip/nil-score branches to yield no results, got %#v", codeResults)
	}
}

func TestRelatedImportAndFrameworkHelperBranches(t *testing.T) {
	imports := extractImportTokens("import './polyfill'\nusing Demo.Auth;\nconst x = require('fs')\n")
	for _, token := range []string{"import", "./polyfill", "demo.auth", "require"} {
		if _, ok := imports[token]; !ok {
			t.Fatalf("expected import token %q in %#v", token, imports)
		}
	}
	if !isImportLine("import './polyfill'") {
		t.Fatalf("expected bare import line to be detected")
	}
	if !isImportLine("import { auth } from './auth'") {
		t.Fatalf("expected from-import line to be detected")
	}

	tags := extractFrameworkTags("Startup.cs", "services.GetRequiredService<Auth>();")
	if !hasTag(tags, "setup_file") || !hasTag(tags, "di_resolution") {
		t.Fatalf("expected setup_file and di_resolution tags, got %#v", tags)
	}
	if max(3, 1) != 3 {
		t.Fatalf("expected max to keep larger left operand")
	}

	numberFiltered := extractImportTokens("import 123 from './123'\n")
	if _, ok := numberFiltered["123"]; ok {
		t.Fatalf("expected purely numeric import token to be filtered, got %#v", numberFiltered)
	}
}

func TestRelatedNormalizeAndScoringHelperBranches(t *testing.T) {
	normalized, err := normalizeRequest(Request{
		Seeds:      []string{" md:guide.md#guide ", "md:guide.md#guide"},
		Extensions: []string{"ts", ".ts", ".md"},
		Signals:    []string{"same_file_hits", "unknown"},
		Budget:     0,
	}, []string{".ts"})
	if err != nil {
		t.Fatalf("normalizeRequest returned error: %v", err)
	}
	if len(normalized.Seeds) != 1 || normalized.Seeds[0] != "md:guide.md#guide" {
		t.Fatalf("unexpected normalized seeds: %#v", normalized)
	}
	if normalized.SearchDocs != true || len(normalized.CodeExtensions) != 1 || normalized.CodeExtensions[0] != ".ts" {
		t.Fatalf("unexpected normalized extensions/searchDocs: %#v", normalized)
	}
	if !normalized.Signals["same_file_hits"] || len(normalized.Signals) != 1 {
		t.Fatalf("unexpected normalized signals: %#v", normalized.Signals)
	}

	scored := scoreCandidate(candidate{
		RefID: "code:src/app.ts@L1",
		Kind:  "code_range",
		Path:  "src/app.ts",
		Range: Range{StartLine: 1, EndLine: 3},
	}, []seedContext{{
		RefID: "code:src/app.ts@L8",
		Kind:  "code_range",
		Path:  "src/app.ts",
	}}, map[string]bool{"same_file_hits": true})
	if scored == nil || scored.Score != 0.05 || len(scored.Reasons) != 1 || scored.Reasons[0].Type != "same_file_hits" {
		t.Fatalf("expected same-file-only score, got %#v", scored)
	}

	if got := pathProximity("a/b/c/d/e/f", "x/y/z"); got < 0.099 || got > 0.101 {
		t.Fatalf("expected deterministic low proximity score, got %v", got)
	}
	if got := splitPathSegments("@root//src///app.ts"); !slices.Equal(got, []string{"root", "src", "app.ts"}) {
		t.Fatalf("expected splitPathSegments to skip empty path parts, got %#v", got)
	}
	if got := splitPathSegments("/root/src/app.ts"); !slices.Equal(got, []string{"root", "src", "app.ts"}) {
		t.Fatalf("expected splitPathSegments to drop leading empty path parts, got %#v", got)
	}

	results := []Result{
		{Path: "b.md", Score: 2.0, Range: Range{StartLine: 1, EndLine: 1}},
		{Path: "a.md", Score: 3.0, Range: Range{StartLine: 9, EndLine: 9}},
		{Path: "a.md", Score: 2.0, Range: Range{StartLine: 3, EndLine: 5}},
		{Path: "a.md", Score: 2.0, Range: Range{StartLine: 3, EndLine: 4}},
		{Path: "a.md", Score: 2.0, Range: Range{StartLine: 1, EndLine: 2}},
	}
	sortResults(results)
	if got := []Range{results[0].Range, results[1].Range, results[2].Range, results[3].Range, results[4].Range}; !slices.Equal(got, []Range{
		{StartLine: 9, EndLine: 9},
		{StartLine: 1, EndLine: 2},
		{StartLine: 3, EndLine: 4},
		{StartLine: 3, EndLine: 5},
		{StartLine: 1, EndLine: 1},
	}) {
		t.Fatalf("unexpected sortResults order: %#v", results)
	}
}

func TestRelatedCodeSupportsSnapshotSources(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := gitsnapshot.Write(snapshotPath, gitsnapshot.Artifact{
		Meta: gitsnapshot.Meta{SchemaVersion: gitsnapshot.SchemaVersion},
		Roots: []gitsnapshot.Root{{
			ID:          "head",
			SourceKind:  "git_ref",
			RepoPath:    "repo",
			Ref:         "HEAD",
			Description: "head",
			Labels:      []string{"head"},
		}},
		Entries: []gitsnapshot.CodeEntry{
			{LogicalPath: "@head/src/seed.ts", RootID: "head", RelativePath: "src/seed.ts", Ext: ".ts", LineCount: 2, Content: "export const seed = sharedtoken\n"},
			{LogicalPath: "@head/src/peer.ts", RootID: "head", RelativePath: "src/peer.ts", Ext: ".ts", LineCount: 3, Content: "import { seed } from './seed'\nexport const peer = sharedtoken\n"},
		},
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	results, err := relatedCode(config.Runtime{
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}, normalizedRequest{
		CodeExtensions: []string{".ts"},
		Signals:        map[string]bool{"imports": true, "token_overlap": true},
		SnippetLines:   3,
	}, []seedContext{{
		RefID:         "code:@head/src/seed.ts@L1",
		Kind:          "code_range",
		Path:          "@head/src/seed.ts",
		OverlapTokens: []string{"sharedtoken"},
		ImportTokens:  map[string]struct{}{"sharedtoken": {}},
		FrameworkTags: map[string]struct{}{},
	}}, map[string]struct{}{"code:@head/src/seed.ts@L1": {}}, map[string]struct{}{"@head/src/seed.ts": {}})
	if err != nil {
		t.Fatalf("relatedCode returned error: %v", err)
	}
	if len(results) != 1 || results[0].Path != "@head/src/peer.ts" {
		t.Fatalf("expected snapshot-backed related code result, got %#v", results)
	}
}

func TestRelatedCodePropagatesCachedListFilesError(t *testing.T) {
	source.ResetCodeSourceCacheForTesting()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "export const token = 1\n")
	cfg := config.Runtime{
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}
	if _, _, err := source.BuildCodeSources(cfg); err != nil {
		t.Fatalf("BuildCodeSources returned error: %v", err)
	}
	if err := os.RemoveAll(sampleRoot); err != nil {
		t.Fatalf("RemoveAll returned error: %v", err)
	}

	if _, err := relatedCode(cfg, normalizedRequest{
		CodeExtensions: []string{".ts"},
		Signals:        map[string]bool{"token_overlap": true},
		SnippetLines:   3,
	}, []seedContext{{
		RefID:         "code:src/app.ts@L1",
		Kind:          "code_range",
		Path:          "src/app.ts",
		OverlapTokens: []string{"token"},
		ImportTokens:  map[string]struct{}{},
		FrameworkTags: map[string]struct{}{},
	}}, map[string]struct{}{}, map[string]struct{}{}); err == nil {
		t.Fatal("expected relatedCode to surface cached ListFiles failure")
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
