package gitsnapshot

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenRuntimeSetHandlesMissingPathsAndDuplicateRootIDs(t *testing.T) {
	empty := OpenRuntimeSet(nil)
	if empty.Enabled || len(empty.Paths) != 0 || len(empty.Indexes) != 0 || empty.Warning != "" {
		t.Fatalf("expected empty runtime set for nil paths, got %#v", empty)
	}

	missing := OpenRuntimeSet([]string{filepath.Join(t.TempDir(), "missing.sqlite")})
	if missing.Enabled || !strings.Contains(missing.Warning, "file not found") {
		t.Fatalf("expected missing snapshot warning, got %#v", missing)
	}

	firstRepo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(firstRepo, "src", "a.ts"), "export const alpha = 1\n")
	git(t, firstRepo, "add", ".")
	git(t, firstRepo, "commit", "-m", "initial")
	firstArtifact, err := Build(BuildOptions{
		RepoPath:       firstRepo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build first snapshot returned error: %v", err)
	}
	firstPath := filepath.Join(t.TempDir(), "snapshot-a.sqlite")
	if err := Write(firstPath, firstArtifact); err != nil {
		t.Fatalf("Write first snapshot returned error: %v", err)
	}

	secondRepo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(secondRepo, "src", "b.ts"), "export const beta = 2\n")
	git(t, secondRepo, "add", ".")
	git(t, secondRepo, "commit", "-m", "initial")
	secondArtifact, err := Build(BuildOptions{
		RepoPath:       secondRepo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build second snapshot returned error: %v", err)
	}
	secondPath := filepath.Join(t.TempDir(), "snapshot-b.sqlite")
	if err := Write(secondPath, secondArtifact); err != nil {
		t.Fatalf("Write second snapshot returned error: %v", err)
	}

	duplicate := OpenRuntimeSet([]string{firstPath, secondPath})
	if duplicate.Enabled || !strings.Contains(duplicate.Warning, "duplicate root id") {
		t.Fatalf("expected duplicate root-id rejection, got %#v", duplicate)
	}
}

func TestWriteAndLoadEmptyArtifact(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "empty-snapshot.sqlite")
	artifact := Artifact{
		Meta: Meta{
			SchemaVersion: SchemaVersion,
			GeneratedAt:   "2025-01-01T00:00:00Z",
			RootCount:     0,
			FileCount:     0,
			RepoPath:      t.TempDir(),
			Fingerprint:   "empty",
		},
	}

	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Meta.RootCount != 0 || loaded.Meta.FileCount != 0 || loaded.Meta.Fingerprint != "empty" || len(loaded.Roots) != 0 {
		t.Fatalf("unexpected empty artifact payload: %#v", loaded)
	}
}

func TestWriteReplacesExistingArtifact(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const alpha = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	firstArtifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build first artifact returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := Write(outPath, firstArtifact); err != nil {
		t.Fatalf("Write first artifact returned error: %v", err)
	}

	mustWriteFile(t, filepath.Join(repo, "src", "route.ts"), "export const beta = 2\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "second")

	secondArtifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build second artifact returned error: %v", err)
	}
	if err := Write(outPath, secondArtifact); err != nil {
		t.Fatalf("Write second artifact returned error: %v", err)
	}

	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Meta.FileCount != secondArtifact.Meta.FileCount || loaded.Meta.Fingerprint != secondArtifact.Meta.Fingerprint {
		t.Fatalf("expected replacement artifact metadata, got %#v want %#v", loaded.Meta, secondArtifact.Meta)
	}
	root := loaded.Roots[0]
	if entries := loaded.EntriesForRoot(root.ID, []string{".ts"}); len(entries) != 2 {
		t.Fatalf("expected replacement artifact entries, got %#v", entries)
	}
}

func TestBuildWriteAndSearchErrorBranches(t *testing.T) {
	if _, err := Build(BuildOptions{RepoPath: filepath.Join(t.TempDir(), "missing"), Samples: "HEAD"}); err == nil {
		t.Fatal("expected Build to reject missing repo")
	}
	if _, err := Build(BuildOptions{RepoPath: string([]byte{'b', 'a', 'd', 0x00}), Samples: "HEAD"}); err == nil {
		t.Fatal("expected Build to reject invalid repo path")
	}

	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "bad.ts"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "binary")
	if _, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	}); err == nil {
		t.Fatal("expected Build to reject undecodable code content")
	}

	blockingDir := filepath.Join(t.TempDir(), "occupied")
	if err := os.MkdirAll(filepath.Join(blockingDir, "child"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := Write(blockingDir, Artifact{}); err == nil {
		t.Fatal("expected Write to reject non-removable directory target")
	}
	parentFile := filepath.Join(t.TempDir(), "blocked-parent")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := Write(filepath.Join(parentFile, "child.sqlite"), Artifact{}); err == nil {
		t.Fatal("expected Write to reject file parent path")
	}
	if err := Write(filepath.Join(t.TempDir(), "duplicate-roots.sqlite"), Artifact{
		Meta: Meta{SchemaVersion: SchemaVersion},
		Roots: []Root{
			{ID: "root", SourceKind: "git_ref", RepoPath: "repo", Description: "a"},
			{ID: "root", SourceKind: "git_ref", RepoPath: "repo", Description: "b"},
		},
	}); err == nil {
		t.Fatal("expected Write to reject duplicate root rows")
	}
	if err := Write(filepath.Join(t.TempDir(), "duplicate-entries.sqlite"), Artifact{
		Meta: Meta{SchemaVersion: SchemaVersion},
		Entries: []CodeEntry{
			{LogicalPath: "@root/src/app.ts", RootID: "root", RelativePath: "src/app.ts", Ext: ".ts", LineCount: 1, Content: "a"},
			{LogicalPath: "@root/src/app.ts", RootID: "root", RelativePath: "src/app.ts", Ext: ".ts", LineCount: 1, Content: "b"},
		},
	}); err == nil {
		t.Fatal("expected Write to reject duplicate code entry rows")
	}

	broken := Artifact{dbPath: filepath.Join(t.TempDir(), "missing.sqlite")}
	if results, err := broken.Search(Root{ID: "root"}, "", []string{".ts"}, 5, 2); err != nil || len(results) != 0 {
		t.Fatalf("expected empty-query search short-circuit, got results=%#v err=%v", results, err)
	}
	if _, err := broken.Search(Root{ID: "root"}, "token", []string{".ts"}, 5, 2); err == nil {
		t.Fatal("expected Search to fail without schema")
	}
	if entries := broken.EntriesForRoot("root", []string{".ts"}); entries != nil {
		t.Fatalf("expected EntriesForRoot to fail on invalid artifact, got %#v", entries)
	}
	if entry, ok := broken.FindEntry("root", "src/app.ts"); ok || entry.LogicalPath != "" {
		t.Fatalf("expected FindEntry to fail on invalid artifact, got %#v ok=%v", entry, ok)
	}
}

func TestBuildRejectsEmptySamplesAndFetchFailures(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const alpha = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	if _, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "",
		CodeExtensions: []string{".ts"},
	}); err == nil || !strings.Contains(err.Error(), "samples are required") {
		t.Fatalf("expected empty samples error, got %v", err)
	}

	if _, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
		FetchEnabled:   true,
		FetchOnStart:   true,
		FetchRemote:    "missing-remote",
	}); err == nil || !strings.Contains(err.Error(), "git fetch missing-remote failed:") {
		t.Fatalf("expected fetch failure, got %v", err)
	}
}

func TestOpenRuntimeAndArtifactQueryHelpers(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export function runFeature() {\n  const specialToken = 1\n  return specialToken\n}\n")
	mustWriteFile(t, filepath.Join(repo, "src", "route.ts"), "export function mapRoutes() {\n  app.MapGet(\"/secure\", () => Results.Ok())\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/runtime")

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/runtime",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	state := OpenRuntime(outPath)
	if !state.Enabled || state.Index == nil || len(state.Index.Roots) != 1 {
		t.Fatalf("expected runtime snapshot to load, got %#v", state)
	}
	root := state.Index.Roots[0]

	entries := state.Index.EntriesForRoot(root.ID, []string{".ts"})
	if len(entries) != 2 {
		t.Fatalf("expected entries for root, got %#v", entries)
	}
	entry, ok := state.Index.FindEntry(root.ID, "src/app.ts")
	if !ok || !strings.Contains(entry.Content, "specialToken") {
		t.Fatalf("expected FindEntry hit, got %#v ok=%v", entry, ok)
	}
	if _, ok := state.Index.FindEntry(root.ID, "missing.ts"); ok {
		t.Fatal("expected missing entry lookup to fail")
	}

	contentResults, err := state.Index.Search(root, "specialToken", []string{".ts"}, 10, 3)
	if err != nil {
		t.Fatalf("Search content returned error: %v", err)
	}
	if len(contentResults) == 0 || contentResults[0].RefID != "code:@feature-runtime/src/app.ts@L2" {
		t.Fatalf("expected content hit with deterministic refId, got %#v", contentResults)
	}

	pathResults, err := state.Index.Search(root, "route", []string{".ts"}, 10, 3)
	if err != nil {
		t.Fatalf("Search path fallback returned error: %v", err)
	}
	if len(pathResults) == 0 || !strings.Contains(pathResults[0].LogicalPath, "route.ts") {
		t.Fatalf("expected path-boost fallback, got %#v", pathResults)
	}

	labelResults, err := state.Index.Search(root, "feature/runtime", []string{".ts"}, 10, 3)
	if err != nil {
		t.Fatalf("Search label fallback returned error: %v", err)
	}
	if len(labelResults) == 0 || labelResults[0].RefID != "code:@feature-runtime/src/app.ts@L1" {
		t.Fatalf("expected label fallback, got %#v", labelResults)
	}
}

func TestHelperFunctionsAndSchemaValidation(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "a.ts"), "export const alpha = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/alpha")
	git(t, repo, "checkout", "-b", "feature/beta")
	git(t, repo, "checkout", "master")

	specs, err := parseSamples(repo, "custom=feature/alpha; ALL ; custom=feature/alpha ; WORKTREE")
	if err != nil {
		t.Fatalf("parseSamples returned error: %v", err)
	}
	if len(specs) < 4 {
		t.Fatalf("expected expanded sample specs, got %#v", specs)
	}
	if specs[0].ID != "custom" || specs[len(specs)-1].Kind != "worktree" {
		t.Fatalf("unexpected parsed specs: %#v", specs)
	}
	if _, err := parseSamples(repo, ""); err == nil {
		t.Fatal("expected parseSamples to reject empty input")
	}
	specs, err = parseSamples(repo, " custom=head ; ; WORKTREE ")
	if err != nil {
		t.Fatalf("parseSamples with empty segment returned error: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected empty segment to be skipped, got %#v", specs)
	}

	if err := gitFetch(repo, "missing-remote"); err == nil {
		t.Fatal("expected gitFetch to fail for missing remote")
	}
	branches, err := listBranches(repo)
	if err != nil || !slices.Contains(branches, "feature/alpha") {
		t.Fatalf("expected listBranches to include feature branch, got %#v err=%v", branches, err)
	}
	if _, err := gitOutput(repo, "no-such-command"); err == nil {
		t.Fatal("expected gitOutput to surface git failure")
	}
	if _, err := gitOutputBuffer(repo, "cat-file", "-p", "missing:path"); err == nil {
		t.Fatal("expected gitOutputBuffer to surface git failure")
	}

	if got := normalizeExtensions([]string{"ts", ".TS", "", "go"}); !slices.Equal(got, []string{".ts", ".go"}) {
		t.Fatalf("unexpected normalizeExtensions result: %#v", got)
	}
	if got := normalizeExtensions(nil); !slices.Equal(got, []string{".cs", ".ts"}) {
		t.Fatalf("unexpected default normalizeExtensions result: %#v", got)
	}
	if !containsString([]string{".ts", ".go"}, ".go") || containsString([]string{".ts"}, ".cs") {
		t.Fatalf("unexpected containsString behavior")
	}
	if !isWorktreeRef("@WORKTREE") || isWorktreeRef("feature/x") {
		t.Fatalf("unexpected isWorktreeRef behavior")
	}
	if got := dedupeStrings([]string{" a ", "", "a", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("unexpected dedupeStrings result: %#v", got)
	}
	if lineCount("") != 0 || lineCount("a\nb") != 2 {
		t.Fatalf("unexpected lineCount behavior")
	}
	if normalizedMaxFileBytes(0) != 1_000_000 || normalizedMaxFileBytes(9) != 9 {
		t.Fatalf("unexpected normalizedMaxFileBytes behavior")
	}
	if fetchEnabled, fetchOnStart := normalizeFetchOptions(false, true); fetchEnabled || fetchOnStart {
		t.Fatalf("expected disabled fetch to suppress fetchOnStart")
	}
	if buildLogicalPath("head", "src\\a.ts") != "@head/src/a.ts" || normalizeRootID(" Feature/Alpha ") != "feature-alpha" {
		t.Fatalf("unexpected logical/root normalization")
	}
	if normalizeRootID("///") != "samples" {
		t.Fatalf("expected fallback root id")
	}
	if got := splitLabels("a\n\nb\n"); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("unexpected splitLabels result: %#v", got)
	}
	if nullableString(" ") != nil || nullableString("x") != "x" {
		t.Fatalf("unexpected nullableString behavior")
	}
	if placeholders(3) != "?, ?, ?" || placeholders(0) != "" {
		t.Fatalf("unexpected placeholders behavior")
	}
	args := stringSliceArgs([]string{"a", "b"})
	if len(args) != 2 || args[0] != "a" || args[1] != "b" {
		t.Fatalf("unexpected stringSliceArgs result: %#v", args)
	}
	if q := firstFileQuery("root", 2); !strings.Contains(q, "ext IN (?, ?)") {
		t.Fatalf("unexpected firstFileQuery: %q", q)
	}
	matches := findLineMatches([]string{"plain", "return alpha", "alpha beta"}, "alpha beta", []string{"alpha", "beta"})
	if len(matches) != 2 || matches[1].score != 4 {
		t.Fatalf("unexpected line matches: %#v", matches)
	}

	sortInput := []rankedSnapshotResult{
		{SearchResult: SearchResult{LogicalPath: "@x/b.ts", StartLine: 2, EndLine: 3, Score: 4}, rank: 2},
		{SearchResult: SearchResult{LogicalPath: "@x/a.ts", StartLine: 5, EndLine: 6, Score: 4}, rank: 1},
		{SearchResult: SearchResult{LogicalPath: "@x/c.ts", StartLine: 1, EndLine: 2, Score: 5}, rank: 9},
		{SearchResult: SearchResult{LogicalPath: "@x/a.ts", StartLine: 3, EndLine: 4, Score: 4}, rank: 1},
		{SearchResult: SearchResult{LogicalPath: "@x/d.ts", StartLine: 1, EndLine: 2, Score: 4}, rank: 1},
	}
	sortSnapshotResults(sortInput)
	if got := []string{sortInput[0].LogicalPath, sortInput[1].LogicalPath, sortInput[2].LogicalPath, sortInput[3].LogicalPath, sortInput[4].LogicalPath}; !slices.Equal(got, []string{"@x/c.ts", "@x/a.ts", "@x/a.ts", "@x/d.ts", "@x/b.ts"}) {
		t.Fatalf("unexpected sortSnapshotResults order: %#v", got)
	}
	if sortInput[1].StartLine != 3 || sortInput[2].StartLine != 5 {
		t.Fatalf("expected start-line tiebreak ordering, got %#v", sortInput)
	}

	if got := uniqueRuntimePaths([]string{" a ", "a", "", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("unexpected uniqueRuntimePaths result: %#v", got)
	}
	if min(1, 2) != 1 || max(1, 2) != 2 || max(3, 2) != 3 || parseInt("7") != 7 || strconvItoa(8) != "8" {
		t.Fatalf("unexpected numeric helper behavior")
	}
}

func TestSearchLabelOnlyFallbackUsesFirstFileQuery(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const value = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/alpha")

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "custom=feature/alpha",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	results, err := loaded.Search(loaded.Roots[0], "alpha", nil, 5, 2)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].RefID != "code:@custom/src/app.ts@L1" {
		t.Fatalf("expected first-file label fallback result, got %#v", results)
	}
}

func TestSearchDedupesDuplicateFileFallbackRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "duplicate-fallback.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE VIRTUAL TABLE code_entries_fts USING fts5(
			logical_path UNINDEXED,
			root_id UNINDEXED,
			ext UNINDEXED,
			path_search,
			content_search,
			tokenize = 'unicode61 remove_diacritics 0'
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("CREATE TABLE returned error: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO code_entries(logical_path, root_id, relative_path, ext, line_count, content) VALUES ('@root/src/route.ts', 'root', 'src/route.ts', '.ts', 1, 'const plain = 1')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	for range 2 {
		if _, err := db.Exec(`INSERT INTO code_entries_fts(logical_path, root_id, ext, path_search, content_search) VALUES ('@root/src/route.ts', 'root', '.ts', 'route ts', 'plain')`); err != nil {
			t.Fatalf("INSERT returned error: %v", err)
		}
	}

	artifact := Artifact{dbPath: dbPath}
	results, err := artifact.Search(Root{ID: "root", Labels: []string{"root"}}, "route", []string{".ts"}, 5, 2)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].RefID != "code:@root/src/route.ts@L1" {
		t.Fatalf("expected duplicate file fallback rows to collapse to one result, got %#v", results)
	}
}

func TestLoadAndQueriesRejectInvalidDBPath(t *testing.T) {
	invalidPath := string([]byte{'b', 'a', 'd', 0x00, '.', 's', 'q', 'l', 'i', 't', 'e'})
	if _, err := Load(invalidPath); err == nil {
		t.Fatal("expected Load to fail for invalid db path")
	}

	artifact := Artifact{dbPath: invalidPath}
	if entries := artifact.EntriesForRoot("root", nil); entries != nil {
		t.Fatalf("expected EntriesForRoot to fail on invalid db path, got %#v", entries)
	}
	if _, ok := artifact.FindEntry("root", "src/app.ts"); ok {
		t.Fatal("expected FindEntry to fail on invalid db path")
	}
	if _, err := artifact.Search(Root{ID: "root", Labels: []string{"root"}}, "root", nil, 5, 2); err == nil {
		t.Fatal("expected Search to fail on invalid db path")
	}
}

func TestFormatGitCommandErrorWithoutOutput(t *testing.T) {
	err := formatGitCommandError([]string{"status"}, sql.ErrConnDone, nil)
	if got := err.Error(); got != "git status failed: sql: connection is already closed" {
		t.Fatalf("expected compact git command error without trailing output details, got %v", err)
	}
}

func TestLoadAndSchemaFailures(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "broken.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if err := validateSchema(db); err == nil {
		t.Fatal("expected schema validation failure for missing tables")
	}

	if _, err := db.Exec(`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, ext TEXT, path_search TEXT, content_search TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if err := validateSchema(db); err != nil {
		t.Fatalf("expected valid schema, got %v", err)
	}

	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', '5')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := loadMeta(db); err == nil {
		t.Fatal("expected unsupported schema version error")
	}
	if _, err := db.Exec(`DELETE FROM meta`); err != nil {
		t.Fatalf("DELETE returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', '6')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if meta, err := loadMeta(db); err != nil || meta.SchemaVersion != SchemaVersion {
		t.Fatalf("expected loadMeta success with sparse values, got meta=%#v err=%v", meta, err)
	}
	if _, err := db.Exec(`INSERT INTO git_roots(root_id, source_kind, repo_path, ref, description, labels) VALUES ('root', 'git_ref', 'repo', NULL, 'desc', '')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	roots, err := loadRoots(db)
	if err != nil {
		t.Fatalf("loadRoots returned error: %v", err)
	}
	if len(roots) != 1 || roots[0].Ref != "" || len(roots[0].Labels) != 0 {
		t.Fatalf("expected nullable ref and empty labels to round-trip, got %#v", roots)
	}
	loaded, err := Load(dbPath)
	if err != nil {
		t.Fatalf("expected Load success for sparse meta/root fixture, got %v", err)
	}
	if loaded.Meta.SchemaVersion != SchemaVersion || len(loaded.Roots) != 1 {
		t.Fatalf("expected sparse fixture to load, got %#v", loaded)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.sqlite")); err == nil {
		t.Fatal("expected Load to fail for missing sqlite file")
	}

	state := OpenRuntime(filepath.Join(t.TempDir(), "missing.sqlite"))
	if state.Enabled || state.Warning == "" {
		t.Fatalf("expected OpenRuntime missing path warning, got %#v", state)
	}

	brokenRuntimePath := filepath.Join(t.TempDir(), "runtime-broken.sqlite")
	brokenRuntime, err := sql.Open("sqlite", brokenRuntimePath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	if _, err := brokenRuntime.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if err := brokenRuntime.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	stateSet := OpenRuntimeSet([]string{brokenRuntimePath})
	if stateSet.Enabled || !strings.Contains(stateSet.Warning, "git snapshot load failed") {
		t.Fatalf("expected invalid-schema warning, got %#v", stateSet)
	}
}

func TestClosedDBHelperErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "closed.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if err := validateSchema(db); err == nil {
		t.Fatal("expected validateSchema to fail on closed db")
	}
	if _, err := loadMeta(db); err == nil {
		t.Fatal("expected loadMeta to fail on closed db")
	}
	if _, err := loadRoots(db); err == nil {
		t.Fatal("expected loadRoots to fail on closed db")
	}
}

func TestBuildSupportsFetchAndWorktreeSamples(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, "", "init", "--bare", remote)

	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const value = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "remote", "add", "origin", remote)
	git(t, repo, "push", "-u", "origin", "master")

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "WORKTREE",
		CodeExtensions: []string{".ts"},
		FetchEnabled:   true,
		FetchOnStart:   true,
		FetchRemote:    "origin",
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if artifact.Meta.RootCount != 1 || artifact.Roots[0].SourceKind != "worktree" || artifact.Meta.FileCount != 1 {
		t.Fatalf("unexpected worktree build artifact: %#v", artifact)
	}
}

func TestBuildWorktreeEntriesAndLoadHelpersErrorBranches(t *testing.T) {
	if _, err := buildWorktreeEntries(filepath.Join(t.TempDir(), "missing"), sampleSpec{ID: "worktree", Ref: "WORKTREE", Kind: "worktree"}, []string{".ts"}, BuildOptions{MaxFileBytes: 1_000_000}); err == nil {
		t.Fatal("expected buildWorktreeEntries to reject missing repo root")
	}

	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "large.ts"), "export const large = 1234567890\n")
	entries, err := buildWorktreeEntries(repo, sampleSpec{ID: "worktree", Ref: "WORKTREE", Kind: "worktree"}, []string{".go"}, BuildOptions{MaxFileBytes: 1_000_000})
	if err != nil {
		t.Fatalf("buildWorktreeEntries returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected extension filter to skip worktree entries, got %#v", entries)
	}
	if _, err := buildWorktreeEntries(repo, sampleSpec{ID: "worktree", Ref: "WORKTREE", Kind: "worktree"}, []string{".ts"}, BuildOptions{MaxFileBytes: 4}); err == nil || !strings.Contains(err.Error(), "MAX_FILE_BYTES") {
		t.Fatalf("expected oversized worktree entry error, got %v", err)
	}

	decodeRepo := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(decodeRepo, "bad.ts"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, err := buildWorktreeEntries(decodeRepo, sampleSpec{ID: "worktree", Ref: "WORKTREE", Kind: "worktree"}, []string{".ts"}, BuildOptions{MaxFileBytes: 1_000_000}); err == nil {
		t.Fatal("expected buildWorktreeEntries to surface decode error")
	}

	dbPath := filepath.Join(t.TempDir(), "load-roots-scan.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO git_roots(root_id, source_kind, repo_path, ref, description, labels) VALUES ('root', 'git_ref', 'repo', 'HEAD', 'desc', NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := loadRoots(db); err == nil {
		t.Fatal("expected loadRoots scan error for NULL labels")
	}
}

func TestBuildRefEntriesSkipsBinaryAndRejectsOversizedFiles(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "good.ts"), "export const good = 1\n")
	if err := os.WriteFile(filepath.Join(repo, "src", "binary.ts"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "fixtures")

	entries, err := buildRefEntries(repo, sampleSpec{ID: "head", Ref: "HEAD", Kind: "git_ref"}, []string{".ts"}, BuildOptions{
		MaxFileBytes: 1_000_000,
	})
	if err != nil {
		t.Fatalf("buildRefEntries returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].RelativePath != "src/good.ts" {
		t.Fatalf("expected binary entry to be skipped, got %#v", entries)
	}

	oversizedRepo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(oversizedRepo, "src", "large.ts"), "export const large = 1234567890\n")
	git(t, oversizedRepo, "add", ".")
	git(t, oversizedRepo, "commit", "-m", "large")

	if _, err := buildRefEntries(oversizedRepo, sampleSpec{ID: "head", Ref: "HEAD", Kind: "git_ref"}, []string{".ts"}, BuildOptions{
		MaxFileBytes: 4,
	}); err == nil || !strings.Contains(err.Error(), "MAX_FILE_BYTES") {
		t.Fatalf("expected oversized ref entry error, got %v", err)
	}
}

func TestSearchAndRefEntryHelpersCoverMalformedRows(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const alpha = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	entries, err := listRefEntries(repo, "HEAD")
	if err != nil {
		t.Fatalf("listRefEntries returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].RelativePath != "src/app.ts" || entries[0].Size <= 0 {
		t.Fatalf("unexpected ref entries: %#v", entries)
	}

	dbPath := filepath.Join(t.TempDir(), "malformed-search.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content)`,
		`CREATE VIRTUAL TABLE code_entries_fts USING fts5(
			logical_path UNINDEXED,
			root_id UNINDEXED,
			ext UNINDEXED,
			path_search,
			content_search,
			tokenize = 'unicode61 remove_diacritics 0'
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("CREATE TABLE returned error: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO code_entries(logical_path, root_id, relative_path, ext, line_count, content) VALUES ('@root/src/app.ts', 'root', 'src/app.ts', '.ts', 1, NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO code_entries_fts(logical_path, root_id, ext, path_search, content_search) VALUES ('@root/src/app.ts', 'root', '.ts', 'app ts', 'alpha')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}

	artifact := Artifact{dbPath: dbPath}
	if entries := artifact.EntriesForRoot("root", nil); entries != nil {
		t.Fatalf("expected EntriesForRoot to fail on malformed scan row, got %#v", entries)
	}
	if _, ok := artifact.FindEntry("root", "src/app.ts"); ok {
		t.Fatal("expected FindEntry to fail on malformed scan row")
	}
	if _, err := artifact.Search(Root{ID: "root"}, "alpha", nil, 5, 2); err == nil {
		t.Fatal("expected Search to fail on malformed scan row")
	}

	sortInput := []rankedSnapshotResult{
		{SearchResult: SearchResult{LogicalPath: "@x/a.ts", StartLine: 3, EndLine: 5, Score: 4}, rank: 1},
		{SearchResult: SearchResult{LogicalPath: "@x/a.ts", StartLine: 3, EndLine: 4, Score: 4}, rank: 1},
	}
	sortSnapshotResults(sortInput)
	if sortInput[0].EndLine != 4 || sortInput[1].EndLine != 5 {
		t.Fatalf("expected end-line tiebreak ordering, got %#v", sortInput)
	}
}

func TestListRefEntriesSkipsMalformedLsTreeLines(t *testing.T) {
	stubGitCommand(t,
		func(string) (string, error) { return "/fake/git", nil },
		func(name string, args ...string) ([]byte, error) {
			return []byte(strings.Join([]string{
				"bad line without tab",
				"100644 blob deadbeef not-a-size\tsrc/zero.ts",
				"",
				"100644 blob deadbeef 12\tsrc/app.ts",
			}, "\n")), nil
		},
	)

	entries, err := listRefEntries("repo", "HEAD")
	if err != nil {
		t.Fatalf("listRefEntries returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected only the valid ls-tree row to survive, got %#v", entries)
	}
	if entries[0].RelativePath != "src/zero.ts" || entries[0].Size != 0 || entries[1].RelativePath != "src/app.ts" || entries[1].Size != 12 || entries[1].Mode != "100644" {
		t.Fatalf("unexpected parsed ref entries: %#v", entries)
	}
}

func TestListRefEntriesParsesWindowsStylePathsAndSkipsShortHeaders(t *testing.T) {
	stubGitCommand(t,
		func(string) (string, error) { return "/fake/git", nil },
		func(name string, args ...string) ([]byte, error) {
			return []byte(strings.Join([]string{
				"\tmissing.ts",
				"100644 blob\tsrc/too-short.ts",
				"100644 blob deadbeef 9\tsrc\\win.ts",
			}, "\n")), nil
		},
	)

	entries, err := listRefEntries("repo", "HEAD")
	if err != nil {
		t.Fatalf("listRefEntries returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].RelativePath != "src/win.ts" || entries[0].Size != 9 {
		t.Fatalf("unexpected parsed ref entries: %#v", entries)
	}
}

func TestParseSamplesAndListRefEntriesPropagateGitErrors(t *testing.T) {
	stubGitCommand(t,
		func(string) (string, error) { return "/fake/git", nil },
		func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("git failed")
		},
	)

	if _, err := parseSamples("repo", "*"); err == nil {
		t.Fatal("expected parseSamples to surface listBranches failure for wildcard samples")
	}
	if _, err := listRefEntries("repo", "HEAD"); err == nil {
		t.Fatal("expected listRefEntries to surface gitOutput failure")
	}
}

func TestBuildRefEntriesReturnsCatFileErrorsAndSkipsSymlinkModes(t *testing.T) {
	stubGitCommand(t,
		func(string) (string, error) { return "/fake/git", nil },
		func(name string, args ...string) ([]byte, error) {
			switch {
			case len(args) >= 5 && args[2] == "ls-tree":
				return []byte(strings.Join([]string{
					"120000 blob deadbeef 8\tsrc/link.ts",
					"100644 blob cafe1234 12\tsrc/app.ts",
				}, "\n")), nil
			case len(args) >= 5 && args[2] == "cat-file":
				return nil, os.ErrPermission
			default:
				return nil, nil
			}
		},
	)

	if _, err := buildRefEntries("repo", sampleSpec{ID: "head", Ref: "HEAD", Kind: "git_ref"}, []string{".ts"}, BuildOptions{
		MaxFileBytes: 1_000_000,
	}); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected cat-file failure to be returned, got %v", err)
	}
}

func TestBuildRefEntriesSurfacesListFailures(t *testing.T) {
	if _, err := buildRefEntries(filepath.Join(t.TempDir(), "missing"), sampleSpec{ID: "head", Ref: "HEAD", Kind: "git_ref"}, []string{".ts"}, BuildOptions{
		MaxFileBytes: 1_000_000,
	}); err == nil {
		t.Fatal("expected buildRefEntries to surface listRefEntries failure")
	}
}

func TestListBranchesReturnsGitErrors(t *testing.T) {
	stubGitCommand(t,
		func(string) (string, error) { return "/fake/git", nil },
		func(name string, args ...string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	)

	if _, err := listBranches("repo"); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected listBranches to surface git error, got %v", err)
	}
}

func TestLoadSearchAndBuildRefEntriesAdditionalBranches(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const alpha = 1\n")
	mustWriteFile(t, filepath.Join(repo, "docs", "guide.md"), "# guide\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	refEntries, err := buildRefEntries(repo, sampleSpec{ID: "head", Ref: "HEAD", Kind: "git_ref"}, []string{".ts"}, BuildOptions{
		MaxFileBytes: 1_000_000,
	})
	if err != nil {
		t.Fatalf("buildRefEntries returned error: %v", err)
	}
	if len(refEntries) != 1 || refEntries[0].RelativePath != "src/app.ts" {
		t.Fatalf("expected non-matching extension to be skipped, got %#v", refEntries)
	}

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	root := loaded.Roots[0]
	if entries := loaded.EntriesForRoot(root.ID, nil); len(entries) != 1 {
		t.Fatalf("expected nil extension filter to return all entries, got %#v", entries)
	}
	if _, ok := loaded.FindEntry(root.ID, "./src/app.ts"); !ok {
		t.Fatal("expected normalized FindEntry lookup to succeed")
	}
	if _, ok := loaded.FindEntry(root.ID, `src\app.ts`); !ok {
		t.Fatal("expected FindEntry to normalize Windows separators")
	}
	if results, err := loaded.Search(root, "missing-token", nil, 5, 2); err != nil || len(results) != 0 {
		t.Fatalf("expected no results for unmatched query, got results=%#v err=%v", results, err)
	}

	metaErrorPath := filepath.Join(t.TempDir(), "meta-null.sqlite")
	db, err := sql.Open("sqlite", metaErrorPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, ext TEXT, path_search TEXT, content_search TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("CREATE TABLE returned error: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := Load(metaErrorPath); err == nil {
		t.Fatal("expected Load to fail on meta scan error")
	}
	if state := OpenRuntimeSet([]string{metaErrorPath}); state.Enabled || !strings.Contains(state.Warning, "git snapshot load failed") {
		t.Fatalf("expected runtime warning for meta scan error, got %#v", state)
	}
}

func TestLoadReturnsRootScanErrorsForMalformedPresentSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "root-scan.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, ext TEXT, path_search TEXT, content_search TEXT)`,
		`INSERT INTO meta(key, value) VALUES ('schema_version', '6')`,
		`INSERT INTO git_roots(root_id, source_kind, repo_path, ref, description, labels) VALUES ('root', 'git_ref', 'repo', 'HEAD', 'desc', NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}

	if _, err := Load(dbPath); err == nil {
		t.Fatal("expected Load to fail when git_roots scan encounters NULL labels")
	}
	if state := OpenRuntimeSet([]string{dbPath}); state.Enabled || !strings.Contains(state.Warning, "git snapshot load failed") {
		t.Fatalf("expected runtime warning for root scan error, got %#v", state)
	}
}

func TestSQLiteHelpersHandleOpenAndInsertFailures(t *testing.T) {
	stubSnapshotSQLite(t)
	openSQLiteSnapshot = func(path string) (*sql.DB, error) {
		return nil, errors.New("open failed")
	}
	if _, err := Load("snapshot.sqlite"); err == nil || !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("expected Load to surface open seam failure, got %v", err)
	}
	artifact := Artifact{dbPath: "snapshot.sqlite"}
	if entries := artifact.EntriesForRoot("root", nil); entries != nil {
		t.Fatalf("expected EntriesForRoot to fail on open error, got %#v", entries)
	}
	if _, ok := artifact.FindEntry("root", "src/app.ts"); ok {
		t.Fatal("expected FindEntry to fail on open error")
	}
	if _, err := artifact.Search(Root{ID: "root"}, "alpha", nil, 5, 2); err == nil || !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("expected Search to surface open seam failure, got %v", err)
	}

	beginDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = beginDB.Close() })
	beginErr := errors.New("begin failed")
	previousBegin := beginSQLiteSnapshotTx
	beginSQLiteSnapshotTx = func(db *sql.DB) (*sql.Tx, error) {
		return nil, beginErr
	}
	if err := writeArtifact(beginDB, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); !errors.Is(err, beginErr) {
		t.Fatalf("expected writeArtifact to surface begin failure, got %v", err)
	}
	beginSQLiteSnapshotTx = previousBegin
	t.Cleanup(func() { beginSQLiteSnapshotTx = previousBegin })
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := createSchema(db); err != nil {
		t.Fatalf("createSchema returned error: %v", err)
	}
	if err := writeArtifact(db, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected writeArtifact to fail when schema objects already exist")
	}
	if err := createSchema(db); err == nil {
		t.Fatal("expected createSchema to fail when schema already exists")
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(tx, Artifact{
		Meta: Meta{SchemaVersion: SchemaVersion},
		Roots: []Root{
			{ID: "root", SourceKind: "git_ref", RepoPath: "repo", Ref: "HEAD", Description: "head", Labels: []string{"root"}},
		},
		Entries: []CodeEntry{
			{LogicalPath: "@root/src/app.ts", RootID: "root", RelativePath: "src/app.ts", Ext: ".ts", LineCount: 1, Content: "alpha"},
		},
	}); err != nil {
		t.Fatalf("insertArtifactTx returned error: %v", err)
	}
	_ = tx.Rollback()

	metaFailDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = metaFailDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, ext TEXT, path_search TEXT, content_search TEXT)`,
		`CREATE TRIGGER meta_fail BEFORE INSERT ON meta BEGIN SELECT RAISE(FAIL, 'meta fail'); END`,
	} {
		if _, err := metaFailDB.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}
	metaFailTx, err := metaFailDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(metaFailTx, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil || !strings.Contains(err.Error(), "meta fail") {
		t.Fatalf("expected meta insert failure, got %v", err)
	}
	_ = metaFailTx.Rollback()

	ftsFailDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = ftsFailDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, ext TEXT, path_search TEXT, content_search TEXT)`,
		`CREATE TRIGGER fts_fail BEFORE INSERT ON code_entries_fts BEGIN SELECT RAISE(FAIL, 'fts fail'); END`,
	} {
		if _, err := ftsFailDB.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}
	ftsFailTx, err := ftsFailDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(ftsFailTx, Artifact{
		Meta: Meta{SchemaVersion: SchemaVersion},
		Entries: []CodeEntry{
			{LogicalPath: "@root/src/app.ts", RootID: "root", RelativePath: "src/app.ts", Ext: ".ts", LineCount: 1, Content: "alpha"},
		},
	}); err == nil || !strings.Contains(err.Error(), "fts fail") {
		t.Fatalf("expected fts insert failure, got %v", err)
	}
	_ = ftsFailTx.Rollback()

	rootPrepareDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = rootPrepareDB.Close() })
	if _, err := rootPrepareDB.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	rootPrepareTx, err := rootPrepareDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(rootPrepareTx, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected insertArtifactTx to fail when git_roots table is absent")
	}
	_ = rootPrepareTx.Rollback()

	entryPrepareDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = entryPrepareDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`,
	} {
		if _, err := entryPrepareDB.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}
	entryPrepareTx, err := entryPrepareDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(entryPrepareTx, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected insertArtifactTx to fail when code_entries table is absent")
	}
	_ = entryPrepareTx.Rollback()

	ftsPrepareDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = ftsPrepareDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
	} {
		if _, err := ftsPrepareDB.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}
	ftsPrepareTx, err := ftsPrepareDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(ftsPrepareTx, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected insertArtifactTx to fail when code_entries_fts table is absent")
	}
	_ = ftsPrepareTx.Rollback()

	closedTxDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = closedTxDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, ext TEXT, path_search TEXT, content_search TEXT)`,
	} {
		if _, err := closedTxDB.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}
	closedTx, err := closedTxDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := closedTx.Rollback(); err != nil {
		t.Fatalf("Rollback returned error: %v", err)
	}
	if err := insertArtifactTx(closedTx, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected insertArtifactTx to fail when Prepare runs on a closed transaction")
	}
}

func TestSearchAndEntriesAdditionalEdgeBranches(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const alpha = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	root := loaded.Roots[0]

	if results, err := loaded.Search(root, "head", []string{".go"}, 5, 2); err != nil || len(results) != 0 {
		t.Fatalf("expected label fallback to respect extension filter, got results=%#v err=%v", results, err)
	}
	if results, err := loaded.Search(root, "head", nil, 1, 0); err != nil || len(results) != 1 || results[0].StartLine != 1 || results[0].EndLine != 1 {
		t.Fatalf("expected label fallback to keep at least one snippet line, got results=%#v err=%v", results, err)
	}
	if results, err := loaded.Search(root, "alpha", nil, 0, 0); err != nil || len(results) != 0 {
		t.Fatalf("expected topK=0 to return no results, got results=%#v err=%v", results, err)
	}
}

func TestValidateSchemaAndLoadMetaReturnQueryErrorsForMissingMetaTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, ref TEXT, description TEXT NOT NULL, labels TEXT NOT NULL)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, ext TEXT NOT NULL, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, ext TEXT, path_search TEXT, content_search TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}

	if err := validateSchema(db); err == nil || !strings.Contains(err.Error(), "missing meta") {
		t.Fatalf("expected validateSchema to report missing meta table, got %v", err)
	}
	if _, err := loadMeta(db); err == nil {
		t.Fatal("expected loadMeta to fail when meta table is absent")
	}
}

func stubSnapshotSQLite(t *testing.T) {
	t.Helper()
	prevOpen := openSQLiteSnapshot
	t.Cleanup(func() {
		openSQLiteSnapshot = prevOpen
	})
}

func TestLoadAndQueriesHandleMalformedPresentSchemaQueryFailures(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "query-failure.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE git_roots (root_id TEXT PRIMARY KEY, source_kind TEXT NOT NULL, repo_path TEXT NOT NULL, description TEXT NOT NULL)`,
		`CREATE TABLE code_entries (logical_path TEXT PRIMARY KEY, root_id TEXT NOT NULL, relative_path TEXT NOT NULL, line_count INTEGER NOT NULL)`,
		`CREATE TABLE code_entries_fts (logical_path TEXT, root_id TEXT, path_search TEXT, content_search TEXT)`,
		`INSERT INTO meta(key, value) VALUES ('schema_version', '6')`,
		`INSERT INTO git_roots(root_id, source_kind, repo_path, description) VALUES ('root', 'git_ref', 'repo', 'desc')`,
		`INSERT INTO code_entries(logical_path, root_id, relative_path, line_count) VALUES ('@root/src/app.ts', 'root', 'src/app.ts', 1)`,
		`INSERT INTO code_entries_fts(logical_path, root_id, path_search, content_search) VALUES ('@root/src/app.ts', 'root', 'src app ts', 'alpha')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}

	if _, err := Load(dbPath); err == nil {
		t.Fatal("expected Load to fail when git_roots query cannot project the required columns")
	}

	artifact := Artifact{
		Meta:   Meta{SchemaVersion: SchemaVersion},
		dbPath: dbPath,
	}
	if entries := artifact.EntriesForRoot("root", nil); entries != nil {
		t.Fatalf("expected EntriesForRoot query failure, got %#v", entries)
	}
	if _, ok := artifact.FindEntry("root", "src/app.ts"); ok {
		t.Fatal("expected FindEntry to fail when code_entries is missing required columns")
	}
	if _, err := artifact.Search(Root{ID: "root", Labels: []string{"root"}}, "alpha", nil, 5, 2); err == nil {
		t.Fatal("expected Search to fail when search tables are malformed")
	}
}
