package search

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/docsindex"
	"github.com/silvekt/scriptorium/internal/gitsnapshot"
	"github.com/silvekt/scriptorium/internal/source"
	_ "modernc.org/sqlite"
)

func TestExecuteReturnsNoResultsForEmptyQuery(t *testing.T) {
	cfg := config.Runtime{
		DocsRoot:       t.TempDir(),
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 0 {
		t.Fatalf("expected no results, got %#v", response.Results)
	}
}

func TestExecuteFindsDocsByJapaneseAndPathQuery(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "auth", "guide.md"), "# 認証フロー\nログインの説明\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	japaneseResult, err := Execute(cfg, Request{Query: "認証", TopK: 5, SnippetLines: 2})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(japaneseResult.Results) != 1 || japaneseResult.Results[0].Kind != "md_heading_block" {
		t.Fatalf("unexpected japanese search result: %#v", japaneseResult.Results)
	}

	pathResult, err := Execute(cfg, Request{Query: "guide", TopK: 5, SnippetLines: 2})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(pathResult.Results) != 1 || pathResult.Results[0].Path != "auth/guide.md" {
		t.Fatalf("unexpected path search result: %#v", pathResult.Results)
	}
}

func TestExecuteRanksDocsHeadingAndPathBoostAheadOfContentOnlyMatch(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Search Guide\nbody\n")
	mustWriteFile(t, filepath.Join(docsRoot, "notes.md"), "# Notes\nsearch guide appears in body\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{Query: "search guide", TopK: 10})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) < 2 {
		t.Fatalf("expected at least two docs results, got %#v", response.Results)
	}
	if response.Results[0].Path != "guide.md" {
		t.Fatalf("expected heading/path match to rank first, got %#v", response.Results)
	}
}

func TestExecuteFindsCodeAndClustersNearbyHits(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	mustWriteFile(
		t,
		filepath.Join(sampleRoot, "src", "app.ts"),
		"const start = 1\nconst specialToken = 1\nconst specialToken = 2\nreturn specialToken\n",
	)

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{Query: "specialToken", TopK: 10, SnippetLines: 5, Extensions: []string{".ts"}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected clustered code result, got %#v", response.Results)
	}
	if response.Results[0].RefID != "code:src/app.ts@L2" {
		t.Fatalf("unexpected code refId: %#v", response.Results[0])
	}
	if response.Results[0].Range.StartLine != 1 || response.Results[0].Range.EndLine != 4 {
		t.Fatalf("unexpected code snippet range: %#v", response.Results[0])
	}
}

func TestExecuteRespectsExtensionFiltering(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "a.ts"), "const token = 1\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "b.cs"), "var token = 1;\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts", ".cs"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{Query: "token", Extensions: []string{".cs"}, TopK: 10})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	paths := make([]string, 0, len(response.Results))
	for _, result := range response.Results {
		paths = append(paths, result.Path)
	}
	if !slices.Equal(paths, []string{"src/b.cs"}) {
		t.Fatalf("unexpected extension-filtered paths: %#v", paths)
	}
}

func TestExecuteUsesCodePathBoostAndCodeRangeFallback(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "src", "pathonly.ts"), "export const value = 1\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{Query: "pathonly", Extensions: []string{".ts"}, TopK: 10, SnippetLines: 5})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected one code path result, got %#v", response.Results)
	}
	if response.Results[0].Kind != "code_range" || response.Results[0].RefID != "code:src/pathonly.ts@L1" {
		t.Fatalf("unexpected code path fallback result: %#v", response.Results[0])
	}
}

func TestExecuteImplementationModeDiversifiesDocsAndCodeResults(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "aspnet-auth.md"), "# ASP.NET Authentication\nUse ASP.NET Core authentication and configure the endpoint.\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "Program.cs"), "var builder = WebApplication.CreateBuilder(args);\nbuilder.Services.AddAuthentication();\napp.MapGet(\"/secure\", () => Results.Ok());\n")
	mustWriteFile(t, filepath.Join(sampleRoot, "Helper.cs"), "public class Helper { public int Run() { return 1; } }\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".cs"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Query:      "ASP.NET Core authentication endpoint startup",
		Extensions: []string{".md", ".cs"},
		TopK:       2,
		Mode:       "implementation",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("expected two diversified results, got %#v", response.Results)
	}
	kinds := []string{response.Results[0].Kind, response.Results[1].Kind}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{"code_range", "md_heading_block"}) {
		t.Fatalf("expected docs and code diversification, got %#v", response.Results)
	}
}

func TestExecuteUsesDocsIndexAndFallsBackWhenStale(t *testing.T) {
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

	response, err := Execute(cfg, Request{Query: "indexed"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected indexed search result, got %#v", response.Results)
	}

	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nfresh body\n")
	response, err = Execute(cfg, Request{Query: "fresh"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Path != "guide.md" {
		t.Fatalf("expected filesystem fallback result, got %#v", response.Results)
	}
}

func TestExecuteFallsBackToFilesystemWhenDocsIndexLoadWarns(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nfilesystem body\n")

	cfg := config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexPaths:  []string{filepath.Join(t.TempDir(), "missing.sqlite")},
		DocsIndexVerify: "full",
		CodeExtensions:  []string{".ts"},
		MaxFileBytes:    1_000_000,
	}

	response, err := Execute(cfg, Request{Query: "filesystem", TopK: 5})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Path != "guide.md" {
		t.Fatalf("expected filesystem fallback result after docs index warning, got %#v", response.Results)
	}
}

func TestExecuteSearchesIndexedMixedJapaneseASCIIContent(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "ai.md"), "# AI Guide\n生成AIが話題になっている\n")

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

	for _, query := range []string{"AI", "生成", "話題"} {
		response, err := Execute(cfg, Request{Query: query, TopK: 5})
		if err != nil {
			t.Fatalf("Execute returned error for %q: %v", query, err)
		}
		if len(response.Results) == 0 || response.Results[0].Path != "ai.md" {
			t.Fatalf("expected indexed mixed-language hit for %q, got %#v", query, response.Results)
		}
	}
}

func TestExecuteSearchesAcrossMultipleDocsIndexes(t *testing.T) {
	docsRoot := t.TempDir()

	firstRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(firstRoot, "a.md"), "# Alpha\nfirst token\n")
	firstIndex, err := docsindex.Build(firstRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	firstIndexPath := filepath.Join(t.TempDir(), "docs-a.sqlite")
	if err := docsindex.Write(firstIndexPath, firstIndex); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	secondRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(secondRoot, "b.md"), "# Beta\nsecond token\n")
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

	response, err := Execute(cfg, Request{Query: "token", TopK: 10})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("expected merged docs index results, got %#v", response.Results)
	}
	paths := []string{response.Results[0].Path, response.Results[1].Path}
	slices.Sort(paths)
	if !slices.Equal(paths, []string{"a.md", "b.md"}) {
		t.Fatalf("unexpected merged docs index paths: %#v", response.Results)
	}
}

func TestExecuteSearchesGitSnapshotAndSupportsLabelFallback(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export function runFeature() {\n  return specialToken\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/search")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/search",
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
		DocsIndexVerify:  "full",
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}

	response, err := Execute(cfg, Request{Query: "specialToken", Extensions: []string{".ts"}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 1 || !strings.HasPrefix(response.Results[0].Path, "@feature-search/") {
		t.Fatalf("unexpected snapshot search results: %#v", response.Results)
	}

	response, err = Execute(cfg, Request{Query: "feature/search", Extensions: []string{".ts"}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) == 0 || response.Results[0].Kind != "code_range" || response.Results[0].RefID != "code:@feature-search/src/app.ts@L1" {
		t.Fatalf("expected label fallback file result, got %#v", response.Results)
	}
}

func TestExecuteSearchesSnapshotMixedJapaneseASCIIContent(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "ai.ts"), "export const text = '生成AIが話題になっている'\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/ai")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/ai",
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
		DocsIndexVerify:  "full",
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}

	for _, query := range []string{"AI", "生成", "話題"} {
		response, err := Execute(cfg, Request{Query: query, Extensions: []string{".ts"}})
		if err != nil {
			t.Fatalf("Execute returned error for %q: %v", query, err)
		}
		if len(response.Results) == 0 || !strings.HasPrefix(response.Results[0].Path, "@feature-ai/") {
			t.Fatalf("expected snapshot mixed-language hit for %q, got %#v", query, response.Results)
		}
	}
}

func TestExecuteSearchesAcrossMultipleSnapshots(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	firstRepo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(firstRepo, "src", "a.ts"), "export const alphaToken = 1\n")
	git(t, firstRepo, "add", ".")
	git(t, firstRepo, "commit", "-m", "initial")
	firstSnapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       firstRepo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	firstSnapshotPath := filepath.Join(t.TempDir(), "snapshot-a.sqlite")
	if err := gitsnapshot.Write(firstSnapshotPath, firstSnapshot); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	secondRepo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(secondRepo, "src", "b.ts"), "export const betaToken = 2\n")
	git(t, secondRepo, "add", ".")
	git(t, secondRepo, "commit", "-m", "initial")
	git(t, secondRepo, "checkout", "-b", "feature/beta")
	secondSnapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       secondRepo,
		Samples:        "feature/beta",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	secondSnapshotPath := filepath.Join(t.TempDir(), "snapshot-b.sqlite")
	if err := gitsnapshot.Write(secondSnapshotPath, secondSnapshot); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cfg := config.Runtime{
		DocsRoot:         docsRoot,
		GitSnapshotPaths: []string{firstSnapshotPath, secondSnapshotPath},
		DocsIndexVerify:  "full",
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}

	response, err := Execute(cfg, Request{Query: "export", Extensions: []string{".ts"}, TopK: 10})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("expected merged snapshot results, got %#v", response.Results)
	}
	paths := []string{response.Results[0].Path, response.Results[1].Path}
	slices.Sort(paths)
	if !slices.Equal(paths, []string{"@feature-beta/src/b.ts", "@head/src/a.ts"}) {
		t.Fatalf("unexpected merged snapshot paths: %#v", response.Results)
	}
}

func TestNormalizeRequestAndModeHelpers(t *testing.T) {
	normalized := normalizeRequest(Request{
		Query:        "  startup dependency injection  ",
		Extensions:   []string{"ts", ".md", ".TS", ".cs", ".md"},
		TopK:         -1,
		SnippetLines: 999,
		Mode:         " IMPLEMENTATION ",
	}, []string{".go"})

	if normalized.Query != "startup dependency injection" {
		t.Fatalf("unexpected trimmed query: %#v", normalized)
	}
	if normalized.Mode != "implementation" {
		t.Fatalf("expected implementation mode, got %#v", normalized)
	}
	if normalized.TopK != 1 || normalized.SnippetLines != 200 {
		t.Fatalf("expected clamped values, got %#v", normalized)
	}
	if !normalized.SearchDocs {
		t.Fatalf("expected markdown search enabled, got %#v", normalized)
	}
	if !slices.Equal(normalized.CodeExtensions, []string{".ts", ".cs"}) {
		t.Fatalf("unexpected normalized code extensions: %#v", normalized.CodeExtensions)
	}

	fallback := normalizeRequest(Request{
		Query:      "topic",
		Extensions: []string{"Go", ".GO"},
		Mode:       "unknown",
	}, []string{".ts"})
	if fallback.Mode != "default" {
		t.Fatalf("expected default mode fallback, got %#v", fallback)
	}
	if fallback.SearchDocs {
		t.Fatalf("did not expect docs search for code-only extensions: %#v", fallback)
	}
	if !slices.Equal(fallback.CodeExtensions, []string{".go"}) {
		t.Fatalf("unexpected deduped fallback extensions: %#v", fallback.CodeExtensions)
	}
}

func TestFrameworkBoostHelpers(t *testing.T) {
	request := normalizedRequest{
		Phrase: "startup dependency injection route configuration attribute",
		Mode:   "implementation",
	}

	docsScore := frameworkBoostForDocs(
		"Program-overview.md",
		"Configuration Route Guide",
		`[Authorize]
builder.Services.AddAuthentication()
app.MapGet("/secure", () => "ok")
options["auth.token"] = true`,
		request,
	)
	if docsScore < 10 {
		t.Fatalf("expected cumulative docs framework boost, got %d", docsScore)
	}

	codeScore := frameworkBoostForCode(
		"src/bootstrap/Program.cs",
		`builder.Services.AddScoped<AuthService>();
app.MapGet("/secure", () => Results.Ok());
[Authorize]
var configuration = "auth.token";`,
		request,
	)
	if codeScore < 12 {
		t.Fatalf("expected cumulative code framework boost, got %d", codeScore)
	}

	fallbackScore := frameworkBoostForDocs("notes.md", "Notes", "builder.Services.AddSingleton();", normalizedRequest{
		Phrase: "implementation notes",
		Mode:   "implementation",
	})
	if fallbackScore != 1 {
		t.Fatalf("expected generic implementation fallback boost, got %d", fallbackScore)
	}

	plainRequest := normalizedRequest{Phrase: request.Phrase, Mode: "default"}
	if frameworkBoostForDocs("guide.md", "Route", "MapGet", plainRequest) != 0 {
		t.Fatalf("expected no docs boost outside implementation mode")
	}
	if frameworkBoostForCode("Program.cs", "MapGet", plainRequest) != 0 {
		t.Fatalf("expected no code boost outside implementation mode")
	}

	if !wantsSetupFile(request) || !wantsStartup(request) || !wantsDI(request) || !wantsRoute(request) || !wantsConfig(request) || !wantsAttributes(request) {
		t.Fatalf("expected implementation helper predicates to match request: %#v", request)
	}
	if containsAny("plain text", "missing") {
		t.Fatalf("containsAny should not match absent candidates")
	}
}

func TestResultHelpersClusterSortAndDiversify(t *testing.T) {
	raw := []Result{
		{RefID: "dup", Kind: "code_range", Path: "src/a.ts", Range: Range{StartLine: 1, EndLine: 2}, Score: 3},
		{RefID: "dup", Kind: "code_range", Path: "src/a.ts", Range: Range{StartLine: 1, EndLine: 2}, Score: 3},
		{RefID: "doc", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 5, EndLine: 8}, Score: 4},
	}
	deduped := dedupeResults(raw)
	if len(deduped) != 2 {
		t.Fatalf("expected duplicate search results to collapse, got %#v", deduped)
	}

	clustered := clusterCodeResults([]Result{
		{RefID: "doc", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 8},
		{RefID: "code:src/a.ts@L9", Kind: "code_range", Path: "src/a.ts", Range: Range{StartLine: 9, EndLine: 10}, Score: 4},
		{RefID: "code:src/a.ts@L11", Kind: "code_range", Path: "src/a.ts", Range: Range{StartLine: 11, EndLine: 12}, Score: 6},
		{RefID: "code:src/b.ts@L3", Kind: "code_range", Path: "src/b.ts", Range: Range{StartLine: 3, EndLine: 4}, Score: 2},
	}, 10)
	if len(clustered) != 3 {
		t.Fatalf("expected nearby code hits to cluster, got %#v", clustered)
	}
	if clustered[1].Path != "src/a.ts" || clustered[1].Score != 7 || clustered[1].Range.StartLine != 11 {
		t.Fatalf("expected best clustered code hit to remain, got %#v", clustered)
	}

	unsorted := []Result{
		{RefID: "b", Kind: "code_range", Path: "src/b.ts", Range: Range{StartLine: 3, EndLine: 4}, Score: 2},
		{RefID: "a", Kind: "code_range", Path: "src/a.ts", Range: Range{StartLine: 5, EndLine: 5}, Score: 9},
		{RefID: "c", Kind: "code_range", Path: "src/a.ts", Range: Range{StartLine: 1, EndLine: 1}, Score: 9},
	}
	sortResults(unsorted)
	if got := []string{unsorted[0].RefID, unsorted[1].RefID, unsorted[2].RefID}; !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("unexpected sorted result order: %#v", got)
	}

	diversified := diversifyResults([]Result{
		{RefID: "doc-1", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 10},
		{RefID: "doc-2", Kind: "md_heading_block", Path: "z-guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 9},
		{RefID: "code-1", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 5, EndLine: 8}, Score: 8},
	}, 2)
	if diversified[0].Kind != "md_heading_block" || diversified[1].Kind != "code_range" {
		t.Fatalf("expected top two diversified results to include docs and code, got %#v", diversified)
	}

	unchanged := diversifyResults([]Result{
		{RefID: "doc-1", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 10},
		{RefID: "doc-2", Kind: "md_heading_block", Path: "other.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 9},
	}, 3)
	if len(unchanged) != 2 || unchanged[1].RefID != "doc-2" {
		t.Fatalf("expected diversifyResults to preserve single-kind results, got %#v", unchanged)
	}

	if got := topSnippet("a\nb\nc", 2); got != "a\nb" {
		t.Fatalf("unexpected top snippet: %q", got)
	}

	forcedKinds := diversifyResults([]Result{
		{RefID: "doc-seed", Kind: "md_heading_block", Path: "z-guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 1},
		{RefID: "code-seed", Kind: "code_range", Path: "src/z.ts", Range: Range{StartLine: 1, EndLine: 2}, Score: 1},
		{RefID: "doc-top", Kind: "md_heading_block", Path: "a-guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 10},
		{RefID: "doc-next", Kind: "md_heading_block", Path: "b-guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 9},
	}, 2)
	if len(forcedKinds) < 2 || forcedKinds[0].RefID != "doc-seed" || forcedKinds[1].RefID != "code-seed" {
		t.Fatalf("expected diversifyResults to prepend the seeded doc/code pair when sorting reorders the top kinds, got %#v", forcedKinds)
	}

	endLineTie := []Result{
		{RefID: "later", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 3, EndLine: 7}, Score: 4},
		{RefID: "earlier", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 3, EndLine: 5}, Score: 4},
	}
	sortResults(endLineTie)
	if got := []string{endLineTie[0].RefID, endLineTie[1].RefID}; !slices.Equal(got, []string{"earlier", "later"}) {
		t.Fatalf("expected end-line tiebreak ordering, got %#v", got)
	}
}

func TestDirectSearchHelpersReturnErrorsAndStructuralFallbacks(t *testing.T) {
	t.Run("searchDocs invalid root", func(t *testing.T) {
		cfg := config.Runtime{
			DocsRoot:     filepath.Join(t.TempDir(), "missing"),
			MaxFileBytes: 1_000_000,
		}
		request := normalizeRequest(Request{Query: "guide"}, []string{".ts"})

		if _, err := searchDocs(cfg, request); err == nil {
			t.Fatal("expected searchDocs to fail for a missing docs root")
		}
	})

	t.Run("searchDocs filesystem decode error", func(t *testing.T) {
		docsRoot := t.TempDir()
		if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}

		cfg := config.Runtime{
			DocsRoot:     docsRoot,
			MaxFileBytes: 1_000_000,
		}
		request := normalizeRequest(Request{Query: "guide", Extensions: []string{".md"}}, []string{".ts"})
		if _, err := searchDocs(cfg, request); err == nil {
			t.Fatal("expected searchDocs to fail when markdown decoding fails")
		}
	})

	t.Run("searchCode read error", func(t *testing.T) {
		sampleRoot := t.TempDir()
		mustWriteFile(t, filepath.Join(sampleRoot, "src", "large.ts"), "0123456789")

		cfg := config.Runtime{
			SampleRoots:    []string{sampleRoot},
			CodeExtensions: []string{".ts"},
			MaxFileBytes:   4,
		}
		request := normalizeRequest(Request{Query: "large", Extensions: []string{".ts"}, TopK: 5}, cfg.CodeExtensions)

		_, err := searchCode(cfg, request)
		if err == nil || !strings.Contains(err.Error(), "MAX_FILE_BYTES") {
			t.Fatalf("expected oversize read error, got %v", err)
		}
	})

	t.Run("searchCode structural fallback", func(t *testing.T) {
		sampleRoot := t.TempDir()
		mustWriteFile(t, filepath.Join(sampleRoot, "Program.cs"), "builder.Services.AddScoped<AuthService>();\n")

		cfg := config.Runtime{
			SampleRoots:    []string{sampleRoot},
			CodeExtensions: []string{".cs"},
			MaxFileBytes:   1_000_000,
		}
		request := normalizeRequest(Request{
			Query:        "startup dependency injection",
			Extensions:   []string{".cs"},
			TopK:         5,
			SnippetLines: 5,
			Mode:         "implementation",
		}, cfg.CodeExtensions)

		results, err := searchCode(cfg, request)
		if err != nil {
			t.Fatalf("searchCode returned error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected one structural fallback result, got %#v", results)
		}
		if results[0].Path != "Program.cs" || results[0].RefID != "code:Program.cs@L1" {
			t.Fatalf("unexpected structural fallback result: %#v", results[0])
		}
	})

	t.Run("searchCode filesystem label fallback", func(t *testing.T) {
		parent := t.TempDir()
		sampleRoot := filepath.Join(parent, "feature-search")
		mustWriteFile(t, filepath.Join(sampleRoot, "src", "plain.ts"), "const value = 1\n")

		cfg := config.Runtime{
			SampleRoots:    []string{sampleRoot},
			CodeExtensions: []string{".ts"},
			MaxFileBytes:   1_000_000,
		}
		request := normalizeRequest(Request{
			Query:        "feature-search",
			Extensions:   []string{".ts"},
			TopK:         5,
			SnippetLines: 5,
		}, cfg.CodeExtensions)

		results, err := searchCode(cfg, request)
		if err != nil {
			t.Fatalf("searchCode returned error: %v", err)
		}
		if len(results) != 1 || results[0].RefID != "code:src/plain.ts@L1" {
			t.Fatalf("expected filesystem label fallback result, got %#v", results)
		}
	})

	t.Run("searchCode build sources error", func(t *testing.T) {
		cfg := config.Runtime{
			SampleRoots:    []string{filepath.Join(t.TempDir(), "missing")},
			CodeExtensions: []string{".ts"},
			MaxFileBytes:   1_000_000,
		}
		request := normalizeRequest(Request{Query: "guide", Extensions: []string{".ts"}, TopK: 5}, cfg.CodeExtensions)

		if _, err := searchCode(cfg, request); err == nil {
			t.Fatal("expected searchCode to propagate code-source build errors")
		}
	})

	t.Run("searchCode empty result without line, path, or structural hits", func(t *testing.T) {
		sampleRoot := t.TempDir()
		mustWriteFile(t, filepath.Join(sampleRoot, "src", "plain.ts"), "const value = 1\n")

		cfg := config.Runtime{
			SampleRoots:    []string{sampleRoot},
			CodeExtensions: []string{".ts"},
			MaxFileBytes:   1_000_000,
		}
		request := normalizeRequest(Request{
			Query:        "missing token",
			Extensions:   []string{".ts"},
			TopK:         5,
			SnippetLines: 5,
		}, cfg.CodeExtensions)

		results, err := searchCode(cfg, request)
		if err != nil {
			t.Fatalf("searchCode returned error: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected no results when nothing matches, got %#v", results)
		}
	})

	t.Run("searchDocsIndexes empty index set", func(t *testing.T) {
		request := normalizeRequest(Request{Query: "guide", TopK: 5}, []string{".ts"})
		results, err := searchDocsIndexes(nil, request)
		if err != nil {
			t.Fatalf("searchDocsIndexes returned error: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected no results for empty index set, got %#v", results)
		}
	})

	t.Run("searchDocsIndexes malformed indexed query", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "non-fts.sqlite")
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("sql.Open returned error: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		for _, stmt := range []string{
			`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
			`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
			`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
			`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`,
			`INSERT INTO meta(key, value) VALUES ('schema_version', '5')`,
			`INSERT INTO md_files(path, line_count, content) VALUES ('guide.md', 1, '# Guide')`,
			`INSERT INTO md_blocks(ref_id, path, heading_slug, heading, level, start_line, end_line, content) VALUES ('md:guide.md#guide', 'guide.md', 'guide', 'Guide', 1, 1, 1, 'body')`,
			`INSERT INTO md_blocks_fts(ref_id, path, path_search, heading_search, content_search) VALUES ('md:guide.md#guide', 'guide.md', 'guide', 'guide', 'body')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("Exec returned error: %v", err)
			}
		}

		index, err := docsindex.Load(dbPath)
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		request := normalizeRequest(Request{Query: "guide", TopK: 5}, []string{".ts"})
		if _, err := searchDocsIndexes([]docsindex.Artifact{index}, request); err == nil {
			t.Fatal("expected searchDocsIndexes to propagate malformed indexed query errors")
		}
	})
}

func TestScoreHelpers(t *testing.T) {
	request := normalizedRequest{
		Phrase: "search guide",
		Tokens: []string{"search", "guide"},
	}

	if score := matchScore("search guide content", request.Phrase, request.Tokens); score != 4 {
		t.Fatalf("unexpected line match score: %d", score)
	}
	if score := matchScore("unrelated", request.Phrase, request.Tokens); score != 0 {
		t.Fatalf("unexpected empty match score: %d", score)
	}

	score := scoreDocsBlock("docs/search-guide.md", "Search Guide", "details", request)
	if score <= 0 {
		t.Fatalf("expected docs block score, got %d", score)
	}

	extensions := normalizeExtensions([]string{"ts", ".ts", " cs ", ".MD", ""})
	if got := strings.Join(extensions, ","); got != ".ts,.cs,.md" {
		t.Fatalf("unexpected normalized extensions: %q", got)
	}

	if got := normalizeExtensions(nil); got != nil {
		t.Fatalf("expected nil extension list, got %#v", got)
	}

	keys := make([]string, 0, len(dedupeResults([]Result{
		{RefID: "r1", Kind: "code_range", Path: "a", Range: Range{StartLine: 1, EndLine: 1}},
	})))
	for _, result := range dedupeResults([]Result{
		{RefID: "r1", Kind: "code_range", Path: "a", Range: Range{StartLine: 1, EndLine: 1}},
	}) {
		keys = append(keys, result.RefID+"|"+strconv.Itoa(result.Range.StartLine))
	}
	if !slices.Equal(keys, []string{"r1|1"}) {
		t.Fatalf("unexpected dedupe key rendering: %#v", keys)
	}
}

func TestFrameworkBoostAndDiversifyGuardBranches(t *testing.T) {
	impl := normalizedRequest{Phrase: "unrelated", Mode: "implementation"}
	if got := frameworkBoostForDocs("guide.md", "Guide", "builder.Services.AddScoped<Auth>();", impl); got != 1 {
		t.Fatalf("expected fallback docs framework boost, got %d", got)
	}
	if got := frameworkBoostForCode("src/bootstrap.ts", "app.MapGet(\"/health\", handler)\n", normalizedRequest{Phrase: "startup endpoint", Mode: "implementation"}); got < 5 {
		t.Fatalf("expected startup+route code boost, got %d", got)
	}
	if got := frameworkBoostForDocs("guide.md", "Guide", "builder.Services.AddScoped<Auth>();", normalizedRequest{Phrase: "service", Mode: "default"}); got != 0 {
		t.Fatalf("expected no framework boost outside implementation mode, got %d", got)
	}

	short := []Result{
		{RefID: "doc", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 10},
		{RefID: "code", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 1, EndLine: 2}, Score: 9},
	}
	if got := diversifyResults(short, 1); len(got) != 2 || got[0].RefID != "doc" || got[1].RefID != "code" {
		t.Fatalf("expected diversifyResults to keep short result sets unchanged, got %#v", got)
	}
	onlyCode := []Result{
		{RefID: "code-1", Kind: "code_range", Path: "src/a.ts", Range: Range{StartLine: 1, EndLine: 2}, Score: 10},
		{RefID: "code-2", Kind: "code_range", Path: "src/b.ts", Range: Range{StartLine: 1, EndLine: 2}, Score: 9},
		{RefID: "code-3", Kind: "code_range", Path: "src/c.ts", Range: Range{StartLine: 1, EndLine: 2}, Score: 8},
	}
	if got := diversifyResults(onlyCode, 3); len(got) != 3 || got[0].RefID != "code-1" {
		t.Fatalf("expected diversifyResults to leave single-kind results unchanged, got %#v", got)
	}
}

func TestExecuteReturnsEmptyWhenSearchSurfacesAreDisabled(t *testing.T) {
	cfg := config.Runtime{
		DocsRoot:       t.TempDir(),
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	response, err := Execute(cfg, Request{
		Query:      "guide",
		Extensions: []string{".go"},
		TopK:       5,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(response.Results) != 0 {
		t.Fatalf("expected no results when docs/code surfaces are disabled, got %#v", response.Results)
	}
}

func TestExecutePropagatesDocsAndCodeErrorsAndTrimsTopK(t *testing.T) {
	t.Run("docs error", func(t *testing.T) {
		cfg := config.Runtime{
			DocsRoot:     filepath.Join(t.TempDir(), "missing"),
			MaxFileBytes: 1_000_000,
		}
		if _, err := Execute(cfg, Request{Query: "guide", Extensions: []string{".md"}}); err == nil {
			t.Fatal("expected Execute to propagate docs search error")
		}
	})

	t.Run("code error", func(t *testing.T) {
		cfg := config.Runtime{
			DocsRoot:       t.TempDir(),
			SampleRoots:    []string{filepath.Join(t.TempDir(), "missing")},
			CodeExtensions: []string{".ts"},
			MaxFileBytes:   1_000_000,
		}
		if _, err := Execute(cfg, Request{Query: "token", Extensions: []string{".ts"}}); err == nil {
			t.Fatal("expected Execute to propagate code search error")
		}
	})

	t.Run("topK trim", func(t *testing.T) {
		docsRoot := t.TempDir()
		mustWriteFile(t, filepath.Join(docsRoot, "a.md"), "# Alpha\nalpha token\n")
		mustWriteFile(t, filepath.Join(docsRoot, "b.md"), "# Beta\nbeta token\n")

		cfg := config.Runtime{
			DocsRoot:     docsRoot,
			MaxFileBytes: 1_000_000,
		}
		response, err := Execute(cfg, Request{Query: "token", Extensions: []string{".md"}, TopK: 1})
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if len(response.Results) != 1 {
			t.Fatalf("expected Execute to trim to topK=1, got %#v", response.Results)
		}
	})
}

func TestDirectSearchHelpersHandleEmptySurfaces(t *testing.T) {
	docsResults, err := searchDocs(config.Runtime{
		DocsRoot:     t.TempDir(),
		MaxFileBytes: 1_000_000,
	}, normalizedRequest{
		Query:        "guide",
		Phrase:       "guide",
		Tokens:       []string{"guide"},
		TopK:         5,
		SnippetLines: 2,
	})
	if err != nil {
		t.Fatalf("searchDocs returned error: %v", err)
	}
	if len(docsResults) != 0 {
		t.Fatalf("expected no docs results for empty docs root, got %#v", docsResults)
	}

	codeResults, err := searchCode(config.Runtime{
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, normalizedRequest{
		Query:          "token",
		Phrase:         "token",
		Tokens:         []string{"token"},
		CodeExtensions: []string{".ts"},
		TopK:           5,
		SnippetLines:   2,
	})
	if err != nil {
		t.Fatalf("searchCode returned error: %v", err)
	}
	if len(codeResults) != 0 {
		t.Fatalf("expected no code results without code sources, got %#v", codeResults)
	}

	onlyDocs := []Result{{RefID: "md:guide.md#guide", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 5, Snippet: "body"}}
	if got := clusterCodeResults(onlyDocs, 3); len(got) != 1 || got[0].RefID != "md:guide.md#guide" {
		t.Fatalf("expected clusterCodeResults to preserve non-code results, got %#v", got)
	}
}

func TestClusterCodeResultsMergesAdjacentHitsAndPrefersEarlierTie(t *testing.T) {
	clustered := clusterCodeResults([]Result{
		{RefID: "doc", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 3},
		{RefID: "code:src/app.ts@L4", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 4, EndLine: 5}, Score: 5},
		{RefID: "code:src/app.ts@L2", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 2, EndLine: 3}, Score: 5},
	}, 10)
	if len(clustered) != 2 {
		t.Fatalf("expected clustered code result plus doc, got %#v", clustered)
	}
	if clustered[0].RefID != "doc" {
		t.Fatalf("expected docs result to remain first in append order, got %#v", clustered)
	}
	if clustered[1].RefID != "code:src/app.ts@L2" || clustered[1].Score != 6 {
		t.Fatalf("expected earlier tied code hit to absorb cluster bonus, got %#v", clustered[1])
	}
}

func TestSearchCodePropagatesCachedListFilesError(t *testing.T) {
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

	if _, err := searchCode(cfg, normalizedRequest{
		Query:          "token",
		Phrase:         "token",
		Tokens:         []string{"token"},
		CodeExtensions: []string{".ts"},
		TopK:           5,
		SnippetLines:   3,
	}); err == nil {
		t.Fatal("expected searchCode to surface cached ListFiles failure")
	}
}

func TestDiversifyResultsReturnsMixedKindsAndClusterSortsByEndLine(t *testing.T) {
	diversified := diversifyResults([]Result{
		{RefID: "doc-1", Kind: "md_heading_block", Path: "guide.md", Range: Range{StartLine: 1, EndLine: 2}, Score: 10},
		{RefID: "code-1", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 2, EndLine: 4}, Score: 9},
		{RefID: "doc-2", Kind: "md_heading_block", Path: "other.md", Range: Range{StartLine: 1, EndLine: 1}, Score: 8},
	}, 3)
	if len(diversified) != 3 || diversified[0].Kind == diversified[1].Kind {
		t.Fatalf("expected diversified mixed leading kinds, got %#v", diversified)
	}

	clustered := clusterCodeResults([]Result{
		{RefID: "code:src/app.ts@L2", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 2, EndLine: 5}, Score: 5},
		{RefID: "code:src/app.ts@L2b", Kind: "code_range", Path: "src/app.ts", Range: Range{StartLine: 2, EndLine: 4}, Score: 5},
	}, 2)
	if len(clustered) != 1 || clustered[0].RefID != "code:src/app.ts@L2b" {
		t.Fatalf("expected cluster sort to prefer shorter end-line for identical starts, got %#v", clustered)
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
