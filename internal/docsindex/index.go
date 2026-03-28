package docsindex

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/docs"
	"github.com/iwizsophy/scriptorium/internal/filesafe"
	"github.com/iwizsophy/scriptorium/internal/ref"
	"github.com/iwizsophy/scriptorium/internal/sqliteutil"
	"github.com/iwizsophy/scriptorium/internal/textsearch"
	"github.com/iwizsophy/scriptorium/internal/textutil"
)

const (
	SchemaVersion       = 5
	builderMaxFileBytes = 50_000_000
)

var (
	openSQLite = func(path string) (*sql.DB, error) {
		return sql.Open("sqlite", path)
	}
	beginSQLiteTx = func(db *sql.DB) (*sql.Tx, error) {
		return db.Begin()
	}
	statPath = os.Stat
)

type Artifact struct {
	Meta   Meta
	dbPath string
	files  []FileRecord
	blocks []BlockRecord
}

type Meta struct {
	SchemaVersion     int    `json:"schemaVersion"`
	GeneratedAt       string `json:"generatedAt"`
	DocsRoot          string `json:"docsRoot"`
	FileCount         int    `json:"fileCount"`
	BlockCount        int    `json:"blockCount"`
	SourceFingerprint string `json:"sourceFingerprint"`
	SourceFileCount   int    `json:"sourceFileCount"`
	SourceMaxMtimeMs  int64  `json:"sourceMaxMtimeMs"`
}

type FileRecord struct {
	Path      string `json:"path"`
	LineCount int    `json:"lineCount"`
	Content   string `json:"content"`
}

type BlockRecord struct {
	RefID       string `json:"refId"`
	Path        string `json:"path"`
	HeadingSlug string `json:"headingSlug"`
	Heading     string `json:"heading"`
	Level       int    `json:"level"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	Content     string `json:"content"`
}

type SearchResult struct {
	RefID     string
	Path      string
	StartLine int
	EndLine   int
	Score     int
	Snippet   string
}

type rankedSearchResult struct {
	SearchResult
	rank float64
}

type RuntimeState struct {
	Enabled bool
	Path    string
	Meta    Meta
	Index   *Artifact
	Warning string
}

type RuntimeSet struct {
	Enabled bool
	Paths   []string
	Indexes []Artifact
	Warning string
}

func Build(docsRoot string, fallbackEncodings []string, allowSymlinks bool) (Artifact, error) {
	root, err := filesafe.NewRoot(docsRoot, allowSymlinks, builderMaxFileBytes)
	if err != nil {
		return Artifact{}, err
	}

	files, stats, err := scanDocs(root, fallbackEncodings)
	if err != nil {
		return Artifact{}, err
	}

	fileRecords := make([]FileRecord, 0, len(files))
	blockRecords := make([]BlockRecord, 0, len(files)*2)
	for _, file := range files {
		fileRecords = append(fileRecords, FileRecord{
			Path:      file.Path,
			LineCount: file.LineCount,
			Content:   file.Content,
		})
		for _, block := range file.Blocks {
			blockRecords = append(blockRecords, BlockRecord{
				RefID:       block.RefID,
				Path:        block.Path,
				HeadingSlug: block.HeadingSlug,
				Heading:     block.Heading,
				Level:       block.Level,
				StartLine:   block.StartLine,
				EndLine:     block.EndLine,
				Content:     block.Content,
			})
		}
	}

	return Artifact{
		Meta: Meta{
			SchemaVersion:     SchemaVersion,
			GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
			DocsRoot:          root.RealPath(),
			FileCount:         len(fileRecords),
			BlockCount:        len(blockRecords),
			SourceFingerprint: stats.Fingerprint,
			SourceFileCount:   stats.FileCount,
			SourceMaxMtimeMs:  stats.MaxMtimeMs,
		},
		files:  fileRecords,
		blocks: blockRecords,
	}, nil
}

func Write(outPath string, artifact Artifact) error {
	return sqliteutil.WriteAtomically(outPath, func(db *sql.DB) error {
		return writeArtifact(db, artifact)
	})
}

func writeArtifact(db *sql.DB, artifact Artifact) error {
	if err := createSchema(db); err != nil {
		return err
	}

	tx, err := beginSQLiteTx(db)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertArtifactTx(tx, artifact); err != nil {
		return err
	}

	return tx.Commit()
}

func createSchema(db *sql.DB) error {
	schema := []string{
		`CREATE TABLE meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE md_files (
			path TEXT PRIMARY KEY,
			line_count INTEGER NOT NULL,
			content TEXT NOT NULL
		)`,
		`CREATE TABLE md_blocks (
			ref_id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			heading_slug TEXT NOT NULL,
			heading TEXT NOT NULL,
			level INTEGER NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			content TEXT NOT NULL
		)`,
		`CREATE VIRTUAL TABLE md_blocks_fts USING fts5(
			ref_id UNINDEXED,
			path UNINDEXED,
			path_search,
			heading_search,
			content_search,
			tokenize = 'unicode61 remove_diacritics 0'
		)`,
		`CREATE INDEX idx_md_blocks_path ON md_blocks(path)`,
		`CREATE INDEX idx_md_blocks_slug ON md_blocks(path, heading_slug)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func insertArtifactTx(tx *sql.Tx, artifact Artifact) error {
	metaStmt, err := tx.Prepare(`INSERT INTO meta(key, value) VALUES(?, ?)`)
	if err != nil {
		return err
	}
	defer metaStmt.Close()
	fileStmt, err := tx.Prepare(`INSERT INTO md_files(path, line_count, content) VALUES(?, ?, ?)`)
	if err != nil {
		return err
	}
	defer fileStmt.Close()
	blockStmt, err := tx.Prepare(`INSERT INTO md_blocks(ref_id, path, heading_slug, heading, level, start_line, end_line, content) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer blockStmt.Close()
	ftsStmt, err := tx.Prepare(`INSERT INTO md_blocks_fts(ref_id, path, path_search, heading_search, content_search) VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ftsStmt.Close()

	metaValues := map[string]string{
		"schema_version":      fmt.Sprintf("%d", artifact.Meta.SchemaVersion),
		"generated_at":        artifact.Meta.GeneratedAt,
		"docs_root":           artifact.Meta.DocsRoot,
		"file_count":          fmt.Sprintf("%d", artifact.Meta.FileCount),
		"block_count":         fmt.Sprintf("%d", artifact.Meta.BlockCount),
		"source_fingerprint":  artifact.Meta.SourceFingerprint,
		"source_file_count":   fmt.Sprintf("%d", artifact.Meta.SourceFileCount),
		"source_max_mtime_ms": fmt.Sprintf("%d", artifact.Meta.SourceMaxMtimeMs),
	}
	for key, value := range metaValues {
		if _, err := metaStmt.Exec(key, value); err != nil {
			return err
		}
	}
	for _, file := range artifact.files {
		if _, err := fileStmt.Exec(file.Path, file.LineCount, file.Content); err != nil {
			return err
		}
	}
	for _, block := range artifact.blocks {
		if _, err := blockStmt.Exec(block.RefID, block.Path, block.HeadingSlug, block.Heading, block.Level, block.StartLine, block.EndLine, block.Content); err != nil {
			return err
		}
		if _, err := ftsStmt.Exec(
			block.RefID,
			block.Path,
			textsearch.BuildFullTextSearchContent(block.Path),
			textsearch.BuildFullTextSearchContent(block.Heading),
			textsearch.BuildFullTextSearchContent(block.Content),
		); err != nil {
			return err
		}
	}

	return nil
}

func Load(path string) (Artifact, error) {
	db, err := openSQLite(path)
	if err != nil {
		return Artifact{}, err
	}
	defer db.Close()

	if err := validateSchema(db); err != nil {
		return Artifact{}, err
	}
	meta, err := loadMeta(db)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{
		Meta:   meta,
		dbPath: path,
	}, nil
}

func OpenRuntime(cfg config.Runtime) RuntimeState {
	set := OpenRuntimeSet(cfg)
	if !set.Enabled || len(set.Indexes) == 0 {
		return RuntimeState{Warning: set.Warning}
	}
	artifact := set.Indexes[0]
	return RuntimeState{
		Enabled: true,
		Path:    set.Paths[0],
		Meta:    artifact.Meta,
		Index:   &artifact,
	}
}

func OpenRuntimeSet(cfg config.Runtime) RuntimeSet {
	discoveredPaths, found, warning := discoverPaths(cfg)
	if !found {
		return RuntimeSet{Warning: warning}
	}

	indexes := make([]Artifact, 0, len(discoveredPaths))
	for _, discoveredPath := range discoveredPaths {
		artifact, err := Load(discoveredPath)
		if err != nil {
			return RuntimeSet{Warning: fmt.Sprintf("docs index load failed: %v", err)}
		}
		ok, verifyWarning := verify(cfg, artifact)
		if !ok {
			return RuntimeSet{Warning: verifyWarning}
		}
		indexes = append(indexes, artifact)
	}

	return RuntimeSet{
		Enabled: true,
		Paths:   append([]string(nil), discoveredPaths...),
		Indexes: indexes,
	}
}

func (a Artifact) SearchMarkdown(query string, snippetLines int, topK int) ([]SearchResult, error) {
	matchQuery := textsearch.BuildFullTextSearchQuery(query)
	if matchQuery == "" {
		return []SearchResult{}, nil
	}

	db, err := openSQLite(a.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	normalizedQuery := textsearch.Normalize(strings.TrimSpace(query))
	tokens := textsearch.TokenizeFullTextSearch(query)
	limit := max(topK*10, 100)
	rows, err := db.Query(`
		SELECT b.ref_id, b.path, b.heading, b.start_line, b.end_line, b.content,
		       bm25(md_blocks_fts, 0.0, 0.0, 3.0, 5.0, 1.0) AS rank
		FROM md_blocks_fts
		JOIN md_blocks b ON b.ref_id = md_blocks_fts.ref_id
		WHERE md_blocks_fts MATCH ?
		ORDER BY bm25(md_blocks_fts, 0.0, 0.0, 3.0, 5.0, 1.0)
		LIMIT ?`,
		matchQuery,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]rankedSearchResult, 0, limit)
	for rows.Next() {
		var refID, path, heading, content string
		var startLine, endLine int
		var rank float64
		if err := rows.Scan(&refID, &path, &heading, &startLine, &endLine, &content, &rank); err != nil {
			return nil, err
		}
		score := textsearch.ScoreTokenMatches(content, tokens, normalizedQuery, 1, 2) +
			textsearch.ScoreTokenMatches(heading, tokens, normalizedQuery, 2, 4) +
			textsearch.ScorePathBoost(path, tokens, normalizedQuery)
		if score <= 0 {
			continue
		}
		results = append(results, rankedSearchResult{
			SearchResult: SearchResult{
				RefID:     refID,
				Path:      path,
				StartLine: startLine,
				EndLine:   endLine,
				Score:     score,
				Snippet:   topSnippet(content, snippetLines),
			},
			rank: rank,
		})
	}
	// COVERAGE_EXCEPTION: modernc sqlite reports these query failures during
	// Query/Scan for this runtime path; a post-iteration rows.Err here requires
	// driver-level fault injection that is not stably reproducible in local/CI.
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sortRanked(results)
	output := make([]SearchResult, 0, min(topK, len(results)))
	for idx, result := range results {
		if idx == topK {
			break
		}
		output = append(output, result.SearchResult)
	}
	return output, nil
}

func (a Artifact) FindBlock(refID string) (BlockRecord, bool) {
	db, err := openSQLite(a.dbPath)
	if err != nil {
		return BlockRecord{}, false
	}
	defer db.Close()

	row := db.QueryRow(`
		SELECT ref_id, path, heading_slug, heading, level, start_line, end_line, content
		FROM md_blocks
		WHERE ref_id = ?
		LIMIT 1`,
		refID,
	)
	var block BlockRecord
	if err := row.Scan(&block.RefID, &block.Path, &block.HeadingSlug, &block.Heading, &block.Level, &block.StartLine, &block.EndLine, &block.Content); err != nil {
		return BlockRecord{}, false
	}
	return block, true
}

func (a Artifact) FileByPath(path string) (FileRecord, bool) {
	db, err := openSQLite(a.dbPath)
	if err != nil {
		return FileRecord{}, false
	}
	defer db.Close()

	row := db.QueryRow(`SELECT path, line_count, content FROM md_files WHERE path = ? LIMIT 1`, ref.NormalizePath(path))
	var file FileRecord
	if err := row.Scan(&file.Path, &file.LineCount, &file.Content); err != nil {
		return FileRecord{}, false
	}
	return file, true
}

func discoverPaths(cfg config.Runtime) ([]string, bool, string) {
	if len(cfg.EffectiveDocsIndexPaths()) > 0 {
		paths := uniquePaths(cfg.EffectiveDocsIndexPaths())
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				continue
			}
			return nil, false, fmt.Sprintf("docs index not found at explicit path: %s", path)
		}
		return paths, true, ""
	}

	candidates := []string{
		filepath.Join(cfg.DocsRoot, "scriptorium-index.sqlite"),
		filepath.Join(cfg.DocsRoot, "..", "scriptorium-index.sqlite"),
		filepath.Join(cfg.DocsRoot, "..", "..", "scriptorium-index.sqlite"),
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, err := os.Stat(candidate); err == nil {
			return []string{candidate}, true, ""
		}
	}
	return nil, false, "docs index not found; falling back to filesystem scan"
}

func verify(cfg config.Runtime, artifact Artifact) (bool, string) {
	if cfg.DocsIndexVerify == "off" {
		return true, ""
	}

	root, err := filesafe.NewRoot(cfg.DocsRoot, cfg.AllowSymlinks, cfg.MaxFileBytes)
	if err != nil {
		return false, fmt.Sprintf("docs index verify failed: %v", err)
	}
	if artifact.Meta.DocsRoot != root.RealPath() {
		return false, "docs index is stale: docs_root mismatch"
	}

	stats, err := sourceStats(root, cfg.TextEncodingFallbacks)
	if err != nil {
		return false, fmt.Sprintf("docs index verify failed: %v", err)
	}

	switch cfg.DocsIndexVerify {
	case "mtime":
		if artifact.Meta.SourceFileCount != stats.FileCount || artifact.Meta.SourceMaxMtimeMs != stats.MaxMtimeMs {
			return false, "docs index is stale: mtime verification mismatch"
		}
		return true, ""
	case "full":
		if artifact.Meta.SourceFingerprint != stats.Fingerprint {
			return false, "docs index is stale: source fingerprint mismatch"
		}
		return true, ""
	default:
		return true, ""
	}
}

func uniquePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

type stats struct {
	FileCount   int
	MaxMtimeMs  int64
	Fingerprint string
}

func scanDocs(root filesafe.Root, fallbackEncodings []string) ([]docs.File, stats, error) {
	paths, err := root.ListFiles([]string{".md"})
	if err != nil {
		return nil, stats{}, err
	}

	files := make([]docs.File, 0, len(paths))
	parts := make([]string, 0, len(paths))
	var maxMtime int64
	for _, path := range paths {
		textFile, err := root.ReadText(path, fallbackEncodings)
		if err != nil {
			return nil, stats{}, err
		}
		files = append(files, docs.ParseMarkdownFile(textFile.Path, textFile.Text))

		// COVERAGE_EXCEPTION: scanDocs consumes paths returned by Root.ListFiles
		// and resolves them immediately. Forcing Resolve to fail after ReadText
		// succeeds would require an unstable filesystem race in local/CI.
		resolved, err := root.Resolve(path)
		if err != nil {
			return nil, stats{}, err
		}
		info, err := statPath(resolved.AbsolutePath)
		if err != nil {
			return nil, stats{}, err
		}
		mtimeMs := info.ModTime().UTC().UnixMilli()
		if mtimeMs > maxMtime {
			maxMtime = mtimeMs
		}
		parts = append(parts, fmt.Sprintf("%s|%d|%d", ref.NormalizePath(path), info.Size(), mtimeMs))
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return files, stats{
		FileCount:   len(paths),
		MaxMtimeMs:  maxMtime,
		Fingerprint: hex.EncodeToString(sum[:]),
	}, nil
}

func sourceStats(root filesafe.Root, fallbackEncodings []string) (stats, error) {
	_, stats, err := scanDocs(root, fallbackEncodings)
	return stats, err
}

func validateSchema(db *sql.DB) error {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name IN ('meta', 'md_files', 'md_blocks', 'md_blocks_fts')`)
	if err != nil {
		return err
	}
	defer rows.Close()

	found := map[string]struct{}{}
	for rows.Next() {
		var name string
		// COVERAGE_EXCEPTION: sqlite_master.name is always returned as textual
		// metadata for normal SQLite fixtures; provoking a Scan type failure here
		// would require corrupt driver metadata rather than a stable local/CI case.
		if err := rows.Scan(&name); err != nil {
			return err
		}
		found[name] = struct{}{}
	}
	for _, required := range []string{"meta", "md_files", "md_blocks", "md_blocks_fts"} {
		if _, ok := found[required]; !ok {
			return fmt.Errorf("invalid docs index schema: missing %s", required)
		}
	}
	return rows.Err()
}

func loadMeta(db *sql.DB) (Meta, error) {
	rows, err := db.Query(`SELECT key, value FROM meta`)
	if err != nil {
		return Meta{}, err
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Meta{}, err
		}
		values[key] = value
	}
	// COVERAGE_EXCEPTION: modernc sqlite surfaces meta query faults at Query/Scan
	// time for these fixtures; a deferred rows.Err after successful iteration is
	// not stably triggerable without custom driver fault injection.
	if err := rows.Err(); err != nil {
		return Meta{}, err
	}
	schemaVersion := parseInt(values["schema_version"])
	if schemaVersion != SchemaVersion {
		return Meta{}, fmt.Errorf("unsupported docs index schema_version: %q", values["schema_version"])
	}
	return Meta{
		SchemaVersion:     schemaVersion,
		GeneratedAt:       values["generated_at"],
		DocsRoot:          values["docs_root"],
		FileCount:         parseInt(values["file_count"]),
		BlockCount:        parseInt(values["block_count"]),
		SourceFingerprint: values["source_fingerprint"],
		SourceFileCount:   parseInt(values["source_file_count"]),
		SourceMaxMtimeMs:  parseInt64(values["source_max_mtime_ms"]),
	}, nil
}

func topSnippet(text string, snippetLines int) string {
	lines := textutil.SplitLines(text)
	if len(lines) > snippetLines {
		lines = lines[:snippetLines]
	}
	return strings.Join(lines, "\n")
}

func sortRanked(results []rankedSearchResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].rank != results[j].rank {
			return results[i].rank < results[j].rank
		}
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].StartLine < results[j].StartLine
	})
}

func parseInt(value string) int {
	var parsed int
	fmt.Sscanf(value, "%d", &parsed)
	return parsed
}

func parseInt64(value string) int64 {
	var parsed int64
	fmt.Sscanf(value, "%d", &parsed)
	return parsed
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
