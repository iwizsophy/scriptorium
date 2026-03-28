package docsindex

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/filesafe"
)

func TestBuildAndLoadArtifact(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n## Detail\nmore\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if artifact.Meta.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected schema version: %#v", artifact.Meta)
	}
	if artifact.Meta.FileCount != 1 || artifact.Meta.BlockCount != 2 {
		t.Fatalf("unexpected meta counts: %#v", artifact.Meta)
	}

	outPath := filepath.Join(t.TempDir(), "scriptorium-index.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Meta.SourceFingerprint == "" {
		t.Fatalf("expected source fingerprint, got %#v", loaded.Meta)
	}
	if _, ok := loaded.FindBlock("md:guide.md#guide"); !ok {
		t.Fatal("expected block lookup to succeed")
	}

	db, err := sql.Open("sqlite", outPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM md_blocks`).Scan(&count); err != nil {
		t.Fatalf("QueryRow returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 md_blocks rows, got %d", count)
	}
}

func TestWriteAndLoadEmptyArtifact(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "empty-docs.sqlite")
	artifact := Artifact{
		Meta: Meta{
			SchemaVersion:     SchemaVersion,
			GeneratedAt:       "2025-01-01T00:00:00Z",
			DocsRoot:          t.TempDir(),
			FileCount:         0,
			BlockCount:        0,
			SourceFingerprint: "empty",
			SourceFileCount:   0,
			SourceMaxMtimeMs:  0,
		},
	}

	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Meta.FileCount != 0 || loaded.Meta.BlockCount != 0 || loaded.Meta.SourceFingerprint != "empty" {
		t.Fatalf("unexpected empty artifact meta: %#v", loaded.Meta)
	}
}

func TestWriteReplacesExistingArtifact(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	firstArtifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build first artifact returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "scriptorium-index.sqlite")
	if err := Write(outPath, firstArtifact); err != nil {
		t.Fatalf("Write first artifact returned error: %v", err)
	}

	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n## Detail\nmore\n")
	secondArtifact, err := Build(docsRoot, nil, false)
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
	if loaded.Meta.BlockCount != secondArtifact.Meta.BlockCount || loaded.Meta.SourceFingerprint != secondArtifact.Meta.SourceFingerprint {
		t.Fatalf("expected replacement artifact metadata, got %#v want %#v", loaded.Meta, secondArtifact.Meta)
	}
}

func TestWriteRejectsInvalidOutputPath(t *testing.T) {
	err := Write(string([]byte{'b', 'a', 'd', 0x00, '.', 's', 'q', 'l', 'i', 't', 'e'}), Artifact{})
	if err == nil {
		t.Fatal("expected Write to reject invalid output path")
	}
}

func TestBuildRejectsMissingDocsRoot(t *testing.T) {
	if _, err := Build(filepath.Join(t.TempDir(), "missing"), nil, false); err == nil {
		t.Fatal("expected Build to reject missing docs root")
	}

	docsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, err := Build(docsRoot, nil, false); err == nil {
		t.Fatal("expected Build to reject undecodable markdown content")
	}
}

func TestBuildAllowsEscapingSymlinkWhenEnabled(t *testing.T) {
	docsRoot := t.TempDir()
	outsideRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(outsideRoot, "guide.md"), "# Outside\nbody\n")

	linkPath := filepath.Join(docsRoot, "external")
	if err := os.Symlink(outsideRoot, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	artifact, err := Build(docsRoot, nil, true)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if artifact.Meta.FileCount != 1 || artifact.files[0].Path != "external/guide.md" {
		t.Fatalf("expected symlinked markdown file to be indexed, got %#v %#v", artifact.Meta, artifact.files)
	}
}

func TestOpenRuntimeVerifiesAndMarksStaleIndex(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	outPath := filepath.Join(docsRoot, "scriptorium-index.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cfg := config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}
	state := OpenRuntime(cfg)
	if !state.Enabled || state.Path != outPath {
		t.Fatalf("expected enabled runtime index, got %#v", state)
	}

	time.Sleep(5 * time.Millisecond)
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nchanged\n")
	state = OpenRuntime(cfg)
	if state.Enabled {
		t.Fatalf("expected stale index to be disabled, got %#v", state)
	}
	if state.Warning == "" {
		t.Fatalf("expected stale warning, got %#v", state)
	}

	cfg.DocsIndexVerify = "off"
	state = OpenRuntime(cfg)
	if !state.Enabled {
		t.Fatalf("expected off verify mode to accept index, got %#v", state)
	}

	missingState := OpenRuntime(config.Runtime{DocsRoot: t.TempDir()})
	if missingState.Enabled || !strings.Contains(missingState.Warning, "docs index not found") {
		t.Fatalf("expected missing-runtime warning, got %#v", missingState)
	}
}

func TestWriteLoadAndVerifyHelpers(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if err := Write("", artifact); err == nil {
		t.Fatal("expected Write to reject empty output path")
	}
	parentFile := filepath.Join(t.TempDir(), "blocked-parent")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := Write(filepath.Join(parentFile, "child.sqlite"), artifact); err == nil {
		t.Fatal("expected Write to reject file parent path")
	}
	blockingDir := filepath.Join(t.TempDir(), "occupied")
	if err := os.MkdirAll(filepath.Join(blockingDir, "child"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := Write(blockingDir, artifact); err == nil {
		t.Fatal("expected Write to reject non-removable directory target")
	}
	duplicateFiles := artifact
	duplicateFiles.files = []FileRecord{
		{Path: "guide.md", LineCount: 1, Content: "# Guide"},
		{Path: "guide.md", LineCount: 2, Content: "# Guide\nbody"},
	}
	if err := Write(filepath.Join(t.TempDir(), "duplicate-files.sqlite"), duplicateFiles); err == nil {
		t.Fatal("expected Write to reject duplicate file rows")
	}
	duplicateBlocks := artifact
	duplicateBlocks.blocks = []BlockRecord{
		{RefID: "md:guide.md#guide", Path: "guide.md", HeadingSlug: "guide", Heading: "Guide", Level: 1, StartLine: 1, EndLine: 1, Content: "# Guide"},
		{RefID: "md:guide.md#guide", Path: "guide.md", HeadingSlug: "guide", Heading: "Guide", Level: 1, StartLine: 2, EndLine: 2, Content: "body"},
	}
	if err := Write(filepath.Join(t.TempDir(), "duplicate-blocks.sqlite"), duplicateBlocks); err == nil {
		t.Fatal("expected Write to reject duplicate block rows")
	}

	outPath := filepath.Join(t.TempDir(), "docs.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cfg := config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexPaths:  []string{outPath},
		DocsIndexVerify: "mtime",
		MaxFileBytes:    1_000_000,
	}
	state := OpenRuntimeSet(cfg)
	if !state.Enabled || len(state.Indexes) != 1 {
		t.Fatalf("expected runtime set to load, got %#v", state)
	}

	ok, warning := verify(config.Runtime{DocsRoot: filepath.Join(t.TempDir(), "other"), DocsIndexVerify: "full", MaxFileBytes: 1_000_000}, artifact)
	if ok || warning == "" {
		t.Fatalf("expected docs root mismatch verification failure, got ok=%v warning=%q", ok, warning)
	}

	ok, warning = verify(config.Runtime{DocsRoot: docsRoot, DocsIndexVerify: "other", MaxFileBytes: 1_000_000}, artifact)
	if !ok || warning != "" {
		t.Fatalf("expected unknown verify mode to pass, got ok=%v warning=%q", ok, warning)
	}

	time.Sleep(5 * time.Millisecond)
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nchanged\n")
	ok, warning = verify(cfg, artifact)
	if ok || !strings.Contains(warning, "mtime verification mismatch") {
		t.Fatalf("expected mtime verification mismatch, got ok=%v warning=%q", ok, warning)
	}
}

func TestSearchMarkdownAndArtifactLookups(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "search.md"), "# Search Guide\nsearch guide body\n")
	mustWriteFile(t, filepath.Join(docsRoot, "notes.md"), "# Notes\nsearch guide body\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "docs.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	results, err := loaded.SearchMarkdown("search guide", 1, 1)
	if err != nil {
		t.Fatalf("SearchMarkdown returned error: %v", err)
	}
	if len(results) != 1 || results[0].Path != "search.md" || results[0].Snippet != "# Search Guide" {
		t.Fatalf("unexpected markdown search results: %#v", results)
	}

	results, err = loaded.SearchMarkdown("", 2, 2)
	if err != nil {
		t.Fatalf("SearchMarkdown empty query returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for empty query, got %#v", results)
	}
	results, err = loaded.SearchMarkdown("search guide", 1, 0)
	if err != nil {
		t.Fatalf("SearchMarkdown zero-topK returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results when topK=0, got %#v", results)
	}
	broken := Artifact{dbPath: filepath.Join(t.TempDir(), "missing.sqlite")}
	if _, err := broken.SearchMarkdown("search", 1, 1); err == nil {
		t.Fatal("expected SearchMarkdown to fail without schema")
	}

	if _, ok := loaded.FindBlock("md:missing.md#guide"); ok {
		t.Fatal("expected missing block lookup to fail")
	}
	file, ok := loaded.FileByPath("./search.md")
	if !ok || file.Path != "search.md" {
		t.Fatalf("expected normalized file lookup, got %#v ok=%v", file, ok)
	}
	if _, ok := loaded.FileByPath("missing.md"); ok {
		t.Fatal("expected missing file lookup to fail")
	}
}

func TestSchemaAndHelperFunctions(t *testing.T) {
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

	if _, err := db.Exec(`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if err := validateSchema(db); err != nil {
		t.Fatalf("expected schema validation success, got %v", err)
	}

	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', '4')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := loadMeta(db); err == nil {
		t.Fatal("expected unsupported schema version error")
	}
	if _, err := db.Exec(`DELETE FROM meta`); err != nil {
		t.Fatalf("DELETE returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', '5')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if meta, err := loadMeta(db); err != nil || meta.SchemaVersion != SchemaVersion {
		t.Fatalf("expected loadMeta success with sparse values, got meta=%#v err=%v", meta, err)
	}

	sortInput := []rankedSearchResult{
		{SearchResult: SearchResult{Path: "z.md", StartLine: 3, Score: 4}, rank: 2},
		{SearchResult: SearchResult{Path: "a.md", StartLine: 2, Score: 4}, rank: 1},
		{SearchResult: SearchResult{Path: "b.md", StartLine: 1, Score: 5}, rank: 9},
		{SearchResult: SearchResult{Path: "a.md", StartLine: 1, Score: 4}, rank: 1},
	}
	sortRanked(sortInput)
	if got := []string{sortInput[0].Path, sortInput[1].Path, sortInput[2].Path, sortInput[3].Path}; !slices.Equal(got, []string{"b.md", "a.md", "a.md", "z.md"}) {
		t.Fatalf("unexpected ranked sort order: %#v", got)
	}
	if sortInput[1].StartLine != 1 || sortInput[2].StartLine != 2 {
		t.Fatalf("expected start-line tiebreak ordering, got %#v", sortInput)
	}

	pathTie := []rankedSearchResult{
		{SearchResult: SearchResult{Path: "z.md", StartLine: 1, Score: 4}, rank: 1},
		{SearchResult: SearchResult{Path: "a.md", StartLine: 1, Score: 4}, rank: 1},
	}
	sortRanked(pathTie)
	if got := []string{pathTie[0].Path, pathTie[1].Path}; !slices.Equal(got, []string{"a.md", "z.md"}) {
		t.Fatalf("expected path tiebreak ordering, got %#v", got)
	}

	if got := uniquePaths([]string{" a ", "a", "", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("unexpected uniquePaths result: %#v", got)
	}
	if got := topSnippet("a\nb\nc", 2); got != "a\nb" {
		t.Fatalf("unexpected topSnippet: %q", got)
	}
	if min(1, 2) != 1 || max(1, 2) != 2 || max(3, 2) != 3 || parseInt("7") != 7 || parseInt64("8") != 8 {
		t.Fatalf("unexpected helper results")
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
}

func TestWriteArtifactHandlesSchemaAndBeginFailures(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if err := writeArtifact(db, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected writeArtifact to fail when schema objects already exist")
	}

	beginErr := errors.New("begin failed")
	previousBegin := beginSQLiteTx
	beginSQLiteTx = func(db *sql.DB) (*sql.Tx, error) {
		return nil, beginErr
	}
	t.Cleanup(func() { beginSQLiteTx = previousBegin })
	beginDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = beginDB.Close() })
	if err := writeArtifact(beginDB, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); !errors.Is(err, beginErr) {
		t.Fatalf("expected writeArtifact to fail when Begin returns an error, got %v", err)
	}
}

func TestCreateSchemaAndInsertArtifactTxFailures(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := createSchema(db); err != nil {
		t.Fatalf("createSchema returned error: %v", err)
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
		files: []FileRecord{
			{Path: "guide.md", LineCount: 1, Content: "# Guide"},
		},
		blocks: []BlockRecord{
			{RefID: "md:guide.md#guide", Path: "guide.md", HeadingSlug: "guide", Heading: "Guide", Level: 1, StartLine: 1, EndLine: 1, Content: "# Guide"},
		},
	}); err != nil {
		t.Fatalf("insertArtifactTx returned error: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback returned error: %v", err)
	}

	metaFailDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = metaFailDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`,
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
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`,
		`CREATE TRIGGER fts_fail BEFORE INSERT ON md_blocks_fts BEGIN SELECT RAISE(FAIL, 'fts fail'); END`,
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
		blocks: []BlockRecord{
			{RefID: "md:guide.md#guide", Path: "guide.md", HeadingSlug: "guide", Heading: "Guide", Level: 1, StartLine: 1, EndLine: 1, Content: "# Guide"},
		},
	}); err == nil || !strings.Contains(err.Error(), "fts fail") {
		t.Fatalf("expected fts insert failure, got %v", err)
	}
	_ = ftsFailTx.Rollback()

	filePrepareDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = filePrepareDB.Close() })
	if _, err := filePrepareDB.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	filePrepareTx, err := filePrepareDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(filePrepareTx, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected insertArtifactTx to fail when md_files table is absent")
	}
	_ = filePrepareTx.Rollback()

	blockPrepareDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = blockPrepareDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
	} {
		if _, err := blockPrepareDB.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}
	blockPrepareTx, err := blockPrepareDB.Begin()
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if err := insertArtifactTx(blockPrepareTx, Artifact{Meta: Meta{SchemaVersion: SchemaVersion}}); err == nil {
		t.Fatal("expected insertArtifactTx to fail when md_blocks table is absent")
	}
	_ = blockPrepareTx.Rollback()

	ftsPrepareDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = ftsPrepareDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
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
		t.Fatal("expected insertArtifactTx to fail when md_blocks_fts table is absent")
	}
	_ = ftsPrepareTx.Rollback()

	closedTxDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = closedTxDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`,
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

func TestLoadMetaHandlesScanErrorsAndInvalidNumericValues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "meta-edge.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', '5'), ('file_count', 'not-a-number'), ('source_max_mtime_ms', 'bad')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	meta, err := loadMeta(db)
	if err != nil {
		t.Fatalf("expected invalid numeric fields to parse as zero, got %v", err)
	}
	if meta.FileCount != 0 || meta.SourceMaxMtimeMs != 0 {
		t.Fatalf("expected invalid numeric fields to fall back to zero, got %#v", meta)
	}

	if _, err := db.Exec(`DELETE FROM meta`); err != nil {
		t.Fatalf("DELETE returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := loadMeta(db); err == nil {
		t.Fatal("expected loadMeta scan error for NULL meta value")
	}
}

func TestValidateSchemaAndLoadMetaReturnQueryErrorsForMissingMetaTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`,
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

func TestScanDocsEmptyRootAndMetaScanErrors(t *testing.T) {
	emptyRootPath := t.TempDir()
	root, err := filesafe.NewRoot(emptyRootPath, false, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	files, stats, err := scanDocs(root, nil)
	if err != nil {
		t.Fatalf("scanDocs returned error: %v", err)
	}
	if len(files) != 0 || stats.FileCount != 0 || stats.MaxMtimeMs != 0 || stats.Fingerprint == "" {
		t.Fatalf("unexpected empty scan result: files=%#v stats=%#v", files, stats)
	}

	dbPath := filepath.Join(t.TempDir(), "meta-scan.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := loadMeta(db); err == nil {
		t.Fatal("expected loadMeta scan error for NULL value")
	}
}

func TestDiscoverAndScanHelperBranches(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	if _, ok, warning := discoverPaths(config.Runtime{
		DocsRoot:       docsRoot,
		DocsIndexPaths: []string{filepath.Join(docsRoot, "missing.sqlite")},
	}); ok || !strings.Contains(warning, "explicit path") {
		t.Fatalf("expected explicit-path warning, got ok=%v warning=%q", ok, warning)
	}

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	autoPath := filepath.Join(docsRoot, "scriptorium-index.sqlite")
	if err := Write(autoPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if paths, ok, warning := discoverPaths(config.Runtime{DocsRoot: docsRoot}); !ok || warning != "" || len(paths) != 1 || paths[0] != autoPath {
		t.Fatalf("unexpected auto-discovered paths: %#v ok=%v warning=%q", paths, ok, warning)
	}
	if paths, ok, warning := discoverPaths(config.Runtime{
		DocsRoot:       docsRoot,
		DocsIndexPaths: []string{" " + autoPath + " ", autoPath},
	}); !ok || warning != "" || !slices.Equal(paths, []string{autoPath}) {
		t.Fatalf("expected explicit paths to be trimmed and deduped, got %#v ok=%v warning=%q", paths, ok, warning)
	}
	nestedDocsRoot := filepath.Join(docsRoot, "nested", "docs")
	if err := os.MkdirAll(nestedDocsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	parentIndexPath := filepath.Join(docsRoot, "nested", "scriptorium-index.sqlite")
	if err := Write(parentIndexPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if paths, ok, warning := discoverPaths(config.Runtime{DocsRoot: nestedDocsRoot}); !ok || warning != "" || !slices.Equal(paths, []string{parentIndexPath}) {
		t.Fatalf("expected parent candidate discovery, got %#v ok=%v warning=%q", paths, ok, warning)
	}

	root, err := filesafe.NewRoot(docsRoot, false, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	files, stats, err := scanDocs(root, nil)
	if err != nil {
		t.Fatalf("scanDocs returned error: %v", err)
	}
	if len(files) != 1 || stats.FileCount != 1 || stats.Fingerprint == "" {
		t.Fatalf("unexpected scanDocs result: files=%#v stats=%#v", files, stats)
	}
	if sourceStats, err := sourceStats(root, nil); err != nil || sourceStats.FileCount != 1 {
		t.Fatalf("unexpected sourceStats result: %#v err=%v", sourceStats, err)
	}
	if ok, warning := verify(config.Runtime{
		DocsRoot:        filepath.Join(t.TempDir(), "missing"),
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}, artifact); ok || !strings.Contains(warning, "verify failed") {
		t.Fatalf("expected invalid docs root verification warning, got ok=%v warning=%q", ok, warning)
	}
}

func TestScanDocsAndVerifyReturnBrokenSymlinkErrors(t *testing.T) {
	docsRoot := t.TempDir()
	if err := os.Symlink(filepath.Join(docsRoot, "missing-target"), filepath.Join(docsRoot, "broken")); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := filesafe.NewRoot(docsRoot, true, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	if _, _, err := scanDocs(root, nil); err == nil {
		t.Fatal("expected scanDocs to fail when a broken symlink is listed")
	}

	ok, warning := verify(config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexVerify: "full",
		AllowSymlinks:   true,
		MaxFileBytes:    1_000_000,
	}, Artifact{
		Meta: Meta{
			SchemaVersion: SchemaVersion,
			DocsRoot:      root.RealPath(),
		},
	})
	if ok || !strings.Contains(warning, "verify failed") {
		t.Fatalf("expected verify to surface broken symlink scan failure, got ok=%v warning=%q", ok, warning)
	}
}

func TestScanDocsAndVerifyReturnStatErrorsViaSeam(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	root, err := filesafe.NewRoot(docsRoot, false, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	stubDocsIndexDeps(t, nil, func(path string) (os.FileInfo, error) {
		return nil, errors.New("stat failed")
	})

	if _, _, err := scanDocs(root, nil); err == nil || !strings.Contains(err.Error(), "stat failed") {
		t.Fatalf("expected scanDocs to surface stat failure, got %v", err)
	}

	ok, warning := verify(config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}, Artifact{
		Meta: Meta{
			SchemaVersion: SchemaVersion,
			DocsRoot:      root.RealPath(),
		},
	})
	if ok || !strings.Contains(warning, "verify failed") {
		t.Fatalf("expected verify to surface stat failure, got ok=%v warning=%q", ok, warning)
	}
}

func TestLoadAndOpenRuntimeSetReturnWarningsForInvalidArtifacts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "broken.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if _, err := Load(dbPath); err == nil || !strings.Contains(err.Error(), "invalid docs index schema") {
		t.Fatalf("expected invalid schema error from Load, got %v", err)
	}
	if block, ok := (Artifact{dbPath: dbPath}).FindBlock("md:guide.md#guide"); ok || block.RefID != "" {
		t.Fatalf("expected FindBlock to fail on invalid artifact, got %#v ok=%v", block, ok)
	}
	if file, ok := (Artifact{dbPath: dbPath}).FileByPath("guide.md"); ok || file.Path != "" {
		t.Fatalf("expected FileByPath to fail on invalid artifact, got %#v ok=%v", file, ok)
	}

	state := OpenRuntimeSet(config.Runtime{
		DocsRoot:       t.TempDir(),
		DocsIndexPaths: []string{dbPath},
	})
	if state.Enabled || !strings.Contains(state.Warning, "docs index load failed") {
		t.Fatalf("expected runtime load warning, got %#v", state)
	}

	state = OpenRuntimeSet(config.Runtime{})
	if state.Enabled || !strings.Contains(state.Warning, "docs index not found") {
		t.Fatalf("expected empty-config warning, got %#v", state)
	}

	if _, err := Load(filepath.Join(t.TempDir(), "missing.sqlite")); err == nil {
		t.Fatal("expected Load to fail for missing sqlite file")
	}
}

func TestLoadAndRuntimeSetHandleMetaScanErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "meta-null.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value)`,
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("CREATE TABLE returned error: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}

	if _, err := Load(dbPath); err == nil {
		t.Fatal("expected Load to fail on meta scan error")
	}

	state := OpenRuntimeSet(config.Runtime{
		DocsRoot:       t.TempDir(),
		DocsIndexPaths: []string{dbPath},
	})
	if state.Enabled || !strings.Contains(state.Warning, "docs index load failed") {
		t.Fatalf("expected runtime load warning for meta scan error, got %#v", state)
	}
}

func TestSearchMarkdownHandlesNoRowsAndVerifySuccess(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ok, warning := verify(config.Runtime{
		DocsRoot:        docsRoot,
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}, artifact); !ok || warning != "" {
		t.Fatalf("expected full verify success, got ok=%v warning=%q", ok, warning)
	}

	outPath := filepath.Join(t.TempDir(), "nested", "scriptorium-index.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	results, err := loaded.SearchMarkdown("missing-token", 3, 5)
	if err != nil {
		t.Fatalf("SearchMarkdown returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for unmatched query, got %#v", results)
	}
}

func TestWriteAndSearchAdditionalHelperBranches(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nalpha\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	indexPath := filepath.Join(t.TempDir(), "docs.sqlite")
	if err := Write(indexPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(indexPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	results, err := loaded.SearchMarkdown("guide", 0, 5)
	if err != nil {
		t.Fatalf("SearchMarkdown returned error: %v", err)
	}
	if len(results) == 0 || results[0].Snippet != "" {
		t.Fatalf("expected zero-line snippet branch, got %#v", results)
	}

	if got := topSnippet("a\nb", 0); got != "" {
		t.Fatalf("expected empty topSnippet for zero lines, got %q", got)
	}
}

func TestArtifactQueriesHandleMalformedRowsAndOpenErrors(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nalpha\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	indexPath := filepath.Join(t.TempDir(), "docs.sqlite")
	if err := Write(indexPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	db, err := sql.Open("sqlite", indexPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`UPDATE md_blocks SET start_line = 'oops' WHERE ref_id = 'md:guide.md#guide'`); err != nil {
		t.Fatalf("UPDATE returned error: %v", err)
	}

	loaded, err := Load(indexPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if _, err := loaded.SearchMarkdown("guide", 1, 5); err == nil {
		t.Fatal("expected SearchMarkdown to fail when a row cannot be scanned")
	}

	invalidPath := t.TempDir()
	if block, ok := (Artifact{dbPath: invalidPath}).FindBlock("md:guide.md#guide"); ok || block.RefID != "" {
		t.Fatalf("expected FindBlock to fail on invalid db path, got %#v ok=%v", block, ok)
	}
	if file, ok := (Artifact{dbPath: invalidPath}).FileByPath("guide.md"); ok || file.Path != "" {
		t.Fatalf("expected FileByPath to fail on invalid db path, got %#v ok=%v", file, ok)
	}
}

func TestArtifactQueriesHandleMalformedButPresentSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "malformed.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER, content INTEGER)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT, heading_slug TEXT, heading INTEGER, level INTEGER, start_line INTEGER, end_line INTEGER, content INTEGER)`,
		`CREATE TABLE md_blocks_fts (ref_id TEXT, path TEXT, path_search TEXT, heading_search TEXT, content_search TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("CREATE TABLE returned error: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', '5')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO md_files(path, line_count, content) VALUES ('guide.md', 1, NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO md_blocks(ref_id, path, heading_slug, heading, level, start_line, end_line, content) VALUES ('md:guide.md#guide', 'guide.md', 'guide', NULL, 1, 1, 1, NULL)`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO md_blocks_fts(ref_id, path, path_search, heading_search, content_search) VALUES ('md:guide.md#guide', 'guide.md', 'guide', 'guide', 'guide')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}

	artifact := Artifact{
		Meta:   Meta{SchemaVersion: SchemaVersion},
		dbPath: dbPath,
	}
	if _, err := artifact.SearchMarkdown("guide", 1, 1); err == nil {
		t.Fatal("expected SearchMarkdown scan error for malformed schema row")
	}
	if _, ok := artifact.FindBlock("md:guide.md#guide"); ok {
		t.Fatal("expected FindBlock to fail when row scanning fails")
	}
	if _, ok := artifact.FileByPath("guide.md"); ok {
		t.Fatal("expected FileByPath to fail when row scanning fails")
	}
}

func TestArtifactQueriesHandleOpenFailures(t *testing.T) {
	dirPath := t.TempDir()
	if _, err := Load(dirPath); err == nil {
		t.Fatal("expected Load to fail when dbPath points to a directory")
	}

	artifact := Artifact{dbPath: dirPath}
	if _, err := artifact.SearchMarkdown("guide", 1, 1); err == nil {
		t.Fatal("expected SearchMarkdown to fail when dbPath points to a directory")
	}
	if _, ok := artifact.FindBlock("md:guide.md#guide"); ok {
		t.Fatal("expected FindBlock to fail when dbPath points to a directory")
	}
	if _, ok := artifact.FileByPath("guide.md"); ok {
		t.Fatal("expected FileByPath to fail when dbPath points to a directory")
	}
}

func TestLoadAndArtifactQueriesHandleOpenFailureViaSeam(t *testing.T) {
	stubDocsIndexDeps(t, func(path string) (*sql.DB, error) {
		return nil, errors.New("open failed")
	}, nil)

	if _, err := Load("docs.sqlite"); err == nil || !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("expected Load to surface seam open failure, got %v", err)
	}

	artifact := Artifact{dbPath: "docs.sqlite"}
	if _, err := artifact.SearchMarkdown("guide", 1, 1); err == nil || !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("expected SearchMarkdown to surface seam open failure, got %v", err)
	}
	if _, ok := artifact.FindBlock("md:guide.md#guide"); ok {
		t.Fatal("expected FindBlock to fail when openSQLite returns error")
	}
	if _, ok := artifact.FileByPath("guide.md"); ok {
		t.Fatal("expected FileByPath to fail when openSQLite returns error")
	}
}

func TestArtifactQueriesHandleInvalidOpenPath(t *testing.T) {
	invalidPath := string([]byte{'b', 'a', 'd', 0x00, '.', 's', 'q', 'l', 'i', 't', 'e'})
	if _, err := Load(invalidPath); err == nil {
		t.Fatal("expected Load to fail when dbPath is invalid")
	}

	artifact := Artifact{dbPath: invalidPath}
	if _, err := artifact.SearchMarkdown("guide", 1, 1); err == nil {
		t.Fatal("expected SearchMarkdown to fail when dbPath is invalid")
	}
	if _, ok := artifact.FindBlock("md:guide.md#guide"); ok {
		t.Fatal("expected FindBlock to fail when dbPath is invalid")
	}
	if _, ok := artifact.FileByPath("guide.md"); ok {
		t.Fatal("expected FileByPath to fail when dbPath is invalid")
	}
}

func TestVerifyDetectsExistingDocsRootMismatch(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	otherDocsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(otherDocsRoot, "guide.md"), "# Guide\nbody\n")
	ok, warning := verify(config.Runtime{
		DocsRoot:        otherDocsRoot,
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}, artifact)
	if ok || !strings.Contains(warning, "docs_root mismatch") {
		t.Fatalf("expected docs_root mismatch warning, got ok=%v warning=%q", ok, warning)
	}
}

func TestSearchMarkdownSkipsZeroScoreRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zero-score.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE md_files (path TEXT PRIMARY KEY, line_count INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE md_blocks (ref_id TEXT PRIMARY KEY, path TEXT NOT NULL, heading_slug TEXT NOT NULL, heading TEXT NOT NULL, level INTEGER NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, content TEXT NOT NULL)`,
		`CREATE VIRTUAL TABLE md_blocks_fts USING fts5(
			ref_id UNINDEXED,
			path UNINDEXED,
			path_search,
			heading_search,
			content_search,
			tokenize = 'unicode61 remove_diacritics 0'
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("CREATE TABLE returned error: %v", err)
		}
	}
	for _, stmt := range []string{
		`INSERT INTO meta(key, value) VALUES ('schema_version', '5')`,
		`INSERT INTO md_files(path, line_count, content) VALUES ('guide.md', 1, '# Guide')`,
		`INSERT INTO md_blocks(ref_id, path, heading_slug, heading, level, start_line, end_line, content) VALUES ('md:guide.md#guide', 'guide.md', 'guide', 'Guide', 1, 1, 1, 'body')`,
		`INSERT INTO md_blocks_fts(ref_id, path, path_search, heading_search, content_search) VALUES ('md:guide.md#guide', 'guide.md', '', '', 'ghost')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("INSERT returned error: %v", err)
		}
	}

	artifact := Artifact{
		Meta:   Meta{SchemaVersion: SchemaVersion},
		dbPath: dbPath,
	}
	results, err := artifact.SearchMarkdown("ghost", 2, 5)
	if err != nil {
		t.Fatalf("SearchMarkdown returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero-score row to be skipped, got %#v", results)
	}
}

func TestSearchMarkdownReturnsQueryErrorForNonFTSTable(t *testing.T) {
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
		`INSERT INTO md_blocks_fts(ref_id, path, path_search, heading_search, content_search) VALUES ('md:guide.md#guide', 'guide.md', 'guide', 'guide', 'guide body')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	}

	artifact := Artifact{
		Meta:   Meta{SchemaVersion: SchemaVersion},
		dbPath: dbPath,
	}
	if _, err := artifact.SearchMarkdown("guide", 1, 1); err == nil {
		t.Fatal("expected SearchMarkdown to fail when md_blocks_fts is not a virtual FTS table")
	}
}

func TestScanDocsReturnsDecodeErrors(t *testing.T) {
	docsRoot := t.TempDir()
	badPath := filepath.Join(docsRoot, "bad.md")
	if err := os.WriteFile(badPath, []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	root, err := filesafe.NewRoot(docsRoot, false, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	if _, _, err := scanDocs(root, nil); err == nil {
		t.Fatal("expected scanDocs to surface markdown decode failure")
	}
	if _, err := sourceStats(root, nil); err == nil {
		t.Fatal("expected sourceStats to surface markdown decode failure")
	}
}

func TestScanDocsReturnsStatErrors(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	root, err := filesafe.NewRoot(docsRoot, false, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	stubDocsIndexDeps(t, nil, func(string) (os.FileInfo, error) {
		return nil, errors.New("stat failed")
	})

	if _, _, err := scanDocs(root, nil); err == nil || !strings.Contains(err.Error(), "stat failed") {
		t.Fatalf("expected scanDocs stat failure, got %v", err)
	}
}

func TestScanDocsReturnsListFilesErrors(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	root, err := filesafe.NewRoot(docsRoot, false, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	if err := os.RemoveAll(docsRoot); err != nil {
		t.Fatalf("RemoveAll returned error: %v", err)
	}

	if _, _, err := scanDocs(root, nil); err == nil {
		t.Fatal("expected scanDocs to surface ListFiles error after root removal")
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

func stubDocsIndexDeps(
	t *testing.T,
	open func(string) (*sql.DB, error),
	stat func(string) (os.FileInfo, error),
) {
	t.Helper()
	previousOpen := openSQLite
	previousStat := statPath
	if open != nil {
		openSQLite = open
	}
	if stat != nil {
		statPath = stat
	}
	t.Cleanup(func() {
		openSQLite = previousOpen
		statPath = previousStat
	})
}
