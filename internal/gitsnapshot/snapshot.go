package gitsnapshot

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/silvekt/scriptorium/internal/filesafe"
	"github.com/silvekt/scriptorium/internal/ref"
	"github.com/silvekt/scriptorium/internal/sqliteutil"
	"github.com/silvekt/scriptorium/internal/textdecode"
	"github.com/silvekt/scriptorium/internal/textsearch"
	"github.com/silvekt/scriptorium/internal/textutil"
)

const SchemaVersion = 6

var (
	gitLookPath        = exec.LookPath
	gitCombinedOutput  = defaultGitCombinedOutput
	openSQLiteSnapshot = func(path string) (*sql.DB, error) {
		return sql.Open("sqlite", path)
	}
	beginSQLiteSnapshotTx = func(db *sql.DB) (*sql.Tx, error) {
		return db.Begin()
	}
	errGitUnavailable  = errors.New("git command unavailable: install git and ensure it is available in PATH")
	errGitLookupFailed = errors.New("git command unavailable: lookup failed")
)

type BuildOptions struct {
	RepoPath       string
	Samples        string
	CodeExtensions []string
	FetchEnabled   bool
	FetchOnStart   bool
	FetchRemote    string
	AllowSymlinks  bool
	MaxFileBytes   int64
}

type Artifact struct {
	Meta    Meta        `json:"meta"`
	Roots   []Root      `json:"roots"`
	Entries []CodeEntry `json:"entries"`
	dbPath  string
}

type Meta struct {
	SchemaVersion int    `json:"schemaVersion"`
	GeneratedAt   string `json:"generatedAt"`
	RootCount     int    `json:"rootCount"`
	FileCount     int    `json:"fileCount"`
	RepoPath      string `json:"repoPath"`
	Fingerprint   string `json:"fingerprint"`
}

type Root struct {
	ID          string   `json:"id"`
	SourceKind  string   `json:"sourceKind"`
	RepoPath    string   `json:"repoPath"`
	Ref         string   `json:"ref"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
}

type CodeEntry struct {
	LogicalPath  string `json:"logicalPath"`
	RootID       string `json:"rootId"`
	RelativePath string `json:"relativePath"`
	Ext          string `json:"ext"`
	LineCount    int    `json:"lineCount"`
	Content      string `json:"content"`
}

type SearchResult struct {
	LogicalPath string
	StartLine   int
	EndLine     int
	Score       int
	Snippet     string
	RefID       string
}

type rankedSnapshotResult struct {
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

func Build(options BuildOptions) (Artifact, error) {
	options.MaxFileBytes = normalizedMaxFileBytes(options.MaxFileBytes)
	repoPath, err := filepath.Abs(strings.TrimSpace(options.RepoPath))
	if err != nil {
		return Artifact{}, err
	}
	if err := ensureGitRepo(repoPath); err != nil {
		return Artifact{}, err
	}
	fetchEnabled, fetchOnStart := normalizeFetchOptions(options.FetchEnabled, options.FetchOnStart)
	if fetchEnabled && fetchOnStart {
		if err := gitFetch(repoPath, strings.TrimSpace(options.FetchRemote)); err != nil {
			return Artifact{}, err
		}
	}

	specs, err := parseSamples(repoPath, options.Samples)
	if err != nil {
		return Artifact{}, err
	}

	entries := make([]CodeEntry, 0, 64)
	roots := make([]Root, 0, len(specs))
	extensions := normalizeExtensions(options.CodeExtensions)
	for _, spec := range specs {
		root := Root{
			ID:          spec.ID,
			SourceKind:  spec.Kind,
			RepoPath:    repoPath,
			Ref:         spec.Ref,
			Description: spec.Ref,
			Labels:      spec.Labels,
		}
		roots = append(roots, root)

		rootEntries, err := buildEntries(repoPath, spec, extensions, options)
		if err != nil {
			return Artifact{}, err
		}
		entries = append(entries, rootEntries...)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LogicalPath < entries[j].LogicalPath
	})

	return Artifact{
		Meta: Meta{
			SchemaVersion: SchemaVersion,
			GeneratedAt:   nowUTC(),
			RootCount:     len(roots),
			FileCount:     len(entries),
			RepoPath:      repoPath,
			Fingerprint:   fingerprint(repoPath, roots, entries),
		},
		Roots:   roots,
		Entries: entries,
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

	tx, err := beginSQLiteSnapshotTx(db)
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
		`CREATE TABLE git_roots (
			root_id TEXT PRIMARY KEY,
			source_kind TEXT NOT NULL,
			repo_path TEXT NOT NULL,
			ref TEXT,
			description TEXT NOT NULL,
			labels TEXT NOT NULL
		)`,
		`CREATE TABLE code_entries (
			logical_path TEXT PRIMARY KEY,
			root_id TEXT NOT NULL,
			relative_path TEXT NOT NULL,
			ext TEXT NOT NULL,
			line_count INTEGER NOT NULL,
			content TEXT NOT NULL
		)`,
		`CREATE VIRTUAL TABLE code_entries_fts USING fts5(
			logical_path UNINDEXED,
			root_id UNINDEXED,
			ext UNINDEXED,
			path_search,
			content_search,
			tokenize = 'unicode61 remove_diacritics 0'
		)`,
		`CREATE INDEX idx_code_entries_root ON code_entries(root_id)`,
		`CREATE INDEX idx_code_entries_ext ON code_entries(ext)`,
		`CREATE INDEX idx_code_entries_root_relative ON code_entries(root_id, relative_path)`,
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
	rootStmt, err := tx.Prepare(`INSERT INTO git_roots(root_id, source_kind, repo_path, ref, description, labels) VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer rootStmt.Close()
	entryStmt, err := tx.Prepare(`INSERT INTO code_entries(logical_path, root_id, relative_path, ext, line_count, content) VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer entryStmt.Close()
	ftsStmt, err := tx.Prepare(`INSERT INTO code_entries_fts(logical_path, root_id, ext, path_search, content_search) VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ftsStmt.Close()

	metaValues := map[string]string{
		"schema_version": fmt.Sprintf("%d", artifact.Meta.SchemaVersion),
		"generated_at":   artifact.Meta.GeneratedAt,
		"root_count":     fmt.Sprintf("%d", artifact.Meta.RootCount),
		"file_count":     fmt.Sprintf("%d", artifact.Meta.FileCount),
		"repo_path":      artifact.Meta.RepoPath,
		"fingerprint":    artifact.Meta.Fingerprint,
	}
	for key, value := range metaValues {
		if _, err := metaStmt.Exec(key, value); err != nil {
			return err
		}
	}
	for _, root := range artifact.Roots {
		if _, err := rootStmt.Exec(root.ID, root.SourceKind, root.RepoPath, nullableString(root.Ref), root.Description, strings.Join(root.Labels, "\n")); err != nil {
			return err
		}
	}
	for _, entry := range artifact.Entries {
		if _, err := entryStmt.Exec(entry.LogicalPath, entry.RootID, entry.RelativePath, entry.Ext, entry.LineCount, entry.Content); err != nil {
			return err
		}
		if _, err := ftsStmt.Exec(
			entry.LogicalPath,
			entry.RootID,
			entry.Ext,
			textsearch.BuildFullTextSearchContent(entry.LogicalPath),
			textsearch.BuildFullTextSearchContent(entry.Content),
		); err != nil {
			return err
		}
	}

	return nil
}

func Load(path string) (Artifact, error) {
	db, err := openSQLiteSnapshot(path)
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
	roots, err := loadRoots(db)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{
		Meta:   meta,
		Roots:  roots,
		dbPath: path,
	}, nil
}

func OpenRuntime(path string) RuntimeState {
	set := OpenRuntimeSet([]string{path})
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

func OpenRuntimeSet(paths []string) RuntimeSet {
	paths = uniqueRuntimePaths(paths)
	if len(paths) == 0 {
		return RuntimeSet{}
	}

	indexes := make([]Artifact, 0, len(paths))
	seenRootIDs := map[string]struct{}{}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return RuntimeSet{Warning: fmt.Sprintf("git snapshot load failed: file not found: %s", path)}
		}
		artifact, err := Load(path)
		if err != nil {
			return RuntimeSet{Warning: fmt.Sprintf("git snapshot load failed: %v", err)}
		}
		for _, root := range artifact.Roots {
			if _, ok := seenRootIDs[root.ID]; ok {
				return RuntimeSet{Warning: fmt.Sprintf("git snapshot load failed: duplicate root id: %s", root.ID)}
			}
			seenRootIDs[root.ID] = struct{}{}
		}
		indexes = append(indexes, artifact)
	}

	return RuntimeSet{
		Enabled: true,
		Paths:   append([]string(nil), paths...),
		Indexes: indexes,
	}
}

func (a Artifact) EntriesForRoot(rootID string, extensions []string) []CodeEntry {
	db, err := openSQLiteSnapshot(a.dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()

	filter := normalizeExtensions(extensions)
	query := `SELECT logical_path, root_id, relative_path, ext, line_count, content FROM code_entries WHERE root_id = ?`
	args := []any{rootID}
	if len(filter) > 0 {
		query += ` AND ext IN (` + placeholders(len(filter)) + `)`
		for _, ext := range filter {
			args = append(args, ext)
		}
	}
	query += ` ORDER BY logical_path`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	results := make([]CodeEntry, 0, 32)
	for rows.Next() {
		var entry CodeEntry
		if err := rows.Scan(&entry.LogicalPath, &entry.RootID, &entry.RelativePath, &entry.Ext, &entry.LineCount, &entry.Content); err != nil {
			return nil
		}
		results = append(results, entry)
	}
	return results
}

func (a Artifact) FindEntry(rootID, relativePath string) (CodeEntry, bool) {
	db, err := openSQLiteSnapshot(a.dbPath)
	if err != nil {
		return CodeEntry{}, false
	}
	defer db.Close()

	row := db.QueryRow(`
		SELECT logical_path, root_id, relative_path, ext, line_count, content
		FROM code_entries
		WHERE root_id = ? AND relative_path = ?
		LIMIT 1`,
		rootID,
		ref.NormalizePath(relativePath),
	)
	var entry CodeEntry
	if err := row.Scan(&entry.LogicalPath, &entry.RootID, &entry.RelativePath, &entry.Ext, &entry.LineCount, &entry.Content); err != nil {
		return CodeEntry{}, false
	}
	return entry, true
}

func (a Artifact) Search(root Root, query string, extensions []string, topK int, snippetLines int) ([]SearchResult, error) {
	matchQuery := textsearch.BuildFullTextSearchQuery(query)
	if matchQuery == "" {
		return []SearchResult{}, nil
	}
	db, err := openSQLiteSnapshot(a.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	normalizedExtensions := normalizeExtensions(extensions)
	tokens := textsearch.TokenizeFullTextSearch(query)
	phrase := textsearch.Normalize(strings.TrimSpace(query))
	labelScore := textsearch.ScoreLabelBoost(root.Labels, phrase, tokens)
	limit := max(topK*20, 200)
	args := []any{matchQuery, root.ID}
	querySQL := `
		SELECT e.logical_path, e.content,
		       bm25(code_entries_fts, 0.0, 0.0, 0.0, 3.0, 1.0) AS rank
		FROM code_entries_fts
		JOIN code_entries e ON e.logical_path = code_entries_fts.logical_path
		WHERE code_entries_fts MATCH ?
		  AND e.root_id = ?`
	if len(normalizedExtensions) > 0 {
		querySQL += ` AND e.ext IN (` + placeholders(len(normalizedExtensions)) + `)`
		for _, ext := range normalizedExtensions {
			args = append(args, ext)
		}
	}
	querySQL += ` ORDER BY bm25(code_entries_fts, 0.0, 0.0, 0.0, 3.0, 1.0) LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]rankedSnapshotResult, 0, limit)
	fileFallbackAdded := map[string]struct{}{}
	rootMatched := false
	for rows.Next() {
		var logicalPath, content string
		var rank float64
		if err := rows.Scan(&logicalPath, &content, &rank); err != nil {
			return nil, err
		}
		lines := textutil.SplitLines(content)
		matchedLines := findLineMatches(lines, phrase, tokens)
		pathScore := textsearch.ScorePathBoost(logicalPath, tokens, phrase)
		if len(matchedLines) == 0 && labelScore+pathScore > 0 {
			// Duplicate fallback rows can only happen with malformed or duplicated
			// FTS entries; keep the first deterministic fallback for the file.
			if _, ok := fileFallbackAdded[logicalPath]; ok {
				continue
			}
			fileFallbackAdded[logicalPath] = struct{}{}
			rootMatched = rootMatched || labelScore > 0
			endLine := max(1, min(len(lines), snippetLines))
			results = append(results, rankedSnapshotResult{
				SearchResult: SearchResult{
					LogicalPath: logicalPath,
					StartLine:   1,
					EndLine:     endLine,
					Score:       labelScore + pathScore,
					Snippet:     strings.Join(lines[:endLine], "\n"),
					RefID:       ref.BuildCodeRefID(logicalPath, 1),
				},
				rank: rank,
			})
			continue
		}
		for _, matched := range matchedLines {
			startLine, endLine, snippet := textutil.CenteredSnippet(lines, matched.line, snippetLines)
			results = append(results, rankedSnapshotResult{
				SearchResult: SearchResult{
					LogicalPath: logicalPath,
					StartLine:   startLine,
					EndLine:     endLine,
					Score:       matched.score + labelScore + pathScore,
					Snippet:     snippet,
					RefID:       ref.BuildCodeRefID(logicalPath, matched.line),
				},
				rank: rank,
			})
		}
		if len(matchedLines) > 0 {
			rootMatched = true
		}
	}
	// COVERAGE_EXCEPTION: modernc sqlite reports these runtime query failures
	// during Query/Scan for normal fixtures; reaching rows.Err only after a
	// completed iteration requires driver-level fault injection that is not
	// stable in local/CI.
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if !rootMatched && labelScore > 0 {
		row := db.QueryRow(firstFileQuery(root.ID, len(normalizedExtensions)), append([]any{root.ID}, stringSliceArgs(normalizedExtensions)...)...)
		var logicalPath, content string
		if err := row.Scan(&logicalPath, &content); err == nil {
			lines := textutil.SplitLines(content)
			endLine := max(1, min(len(lines), snippetLines))
			results = append(results, rankedSnapshotResult{
				SearchResult: SearchResult{
					LogicalPath: logicalPath,
					StartLine:   1,
					EndLine:     endLine,
					Score:       labelScore,
					Snippet:     strings.Join(lines[:endLine], "\n"),
					RefID:       ref.BuildCodeRefID(logicalPath, 1),
				},
				rank: 1e12,
			})
		}
	}

	sortSnapshotResults(results)
	output := make([]SearchResult, 0, min(topK, len(results)))
	for idx, result := range results {
		if idx == topK {
			break
		}
		output = append(output, result.SearchResult)
	}
	return output, nil
}

type sampleSpec struct {
	ID     string
	Ref    string
	Kind   string
	Labels []string
}

func parseSamples(repoPath, samples string) ([]sampleSpec, error) {
	parts := strings.FieldsFunc(samples, func(r rune) bool {
		return r == ';' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("samples are required")
	}

	specs := make([]sampleSpec, 0, len(parts))
	seen := map[string]int{}
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw == "*" || strings.EqualFold(raw, "ALL") {
			branches, err := listBranches(repoPath)
			if err != nil {
				return nil, err
			}
			for _, branch := range branches {
				specs = append(specs, uniqueSpec(seen, newSpec("", branch)))
			}
			continue
		}
		id := ""
		refName := raw
		if strings.Contains(raw, "=") {
			parts := strings.SplitN(raw, "=", 2)
			id = strings.TrimSpace(parts[0])
			refName = strings.TrimSpace(parts[1])
		}
		specs = append(specs, uniqueSpec(seen, newSpec(id, refName)))
	}
	return specs, nil
}

func newSpec(id, refName string) sampleSpec {
	normalizedRef := strings.TrimSpace(refName)
	kind := "git_ref"
	if isWorktreeRef(normalizedRef) {
		kind = "worktree"
	}
	if strings.TrimSpace(id) == "" {
		id = normalizeRootID(normalizedRef)
	}
	id = normalizeRootID(id)
	labels := []string{id}
	if normalizedRef != "" {
		labels = append(labels, normalizedRef)
		if idx := strings.LastIndex(normalizedRef, "/"); idx >= 0 && idx+1 < len(normalizedRef) {
			labels = append(labels, normalizedRef[idx+1:])
		}
	}
	if kind == "worktree" {
		labels = append(labels, "worktree")
	}
	return sampleSpec{
		ID:     id,
		Ref:    normalizedRef,
		Kind:   kind,
		Labels: dedupeStrings(labels),
	}
}

func uniqueSpec(seen map[string]int, spec sampleSpec) sampleSpec {
	seen[spec.ID]++
	if seen[spec.ID] > 1 {
		spec.ID = fmt.Sprintf("%s-%d", spec.ID, seen[spec.ID])
	}
	return spec
}

func buildEntries(repoPath string, spec sampleSpec, extensions []string, options BuildOptions) ([]CodeEntry, error) {
	if spec.Kind == "worktree" {
		return buildWorktreeEntries(repoPath, spec, extensions, options)
	}
	return buildRefEntries(repoPath, spec, extensions, options)
}

func buildWorktreeEntries(repoPath string, spec sampleSpec, extensions []string, options BuildOptions) ([]CodeEntry, error) {
	root, err := filesafe.NewRoot(repoPath, options.AllowSymlinks, options.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	paths, err := root.ListFiles(extensions)
	if err != nil {
		// COVERAGE_EXCEPTION: the remaining ListFiles failure path depends on
		// environment-specific symlink or directory read failures inside the OS
		// filesystem adapter, which are not stably reproducible in local/CI here.
		return nil, err
	}
	entries := make([]CodeEntry, 0, len(paths))
	for _, rel := range paths {
		textFile, err := root.ReadText(rel, snapshotFallbackEncodings())
		if err != nil {
			return nil, err
		}
		entries = append(entries, CodeEntry{
			LogicalPath:  buildLogicalPath(spec.ID, rel),
			RootID:       spec.ID,
			RelativePath: rel,
			Ext:          strings.ToLower(filepath.Ext(rel)),
			LineCount:    lineCount(textFile.Text),
			Content:      textutil.NormalizeLineEndings(textFile.Text),
		})
	}
	return entries, nil
}

func buildRefEntries(repoPath string, spec sampleSpec, extensions []string, options BuildOptions) ([]CodeEntry, error) {
	entriesMeta, err := listRefEntries(repoPath, spec.Ref)
	if err != nil {
		return nil, err
	}
	entries := make([]CodeEntry, 0, 32)
	for _, meta := range entriesMeta {
		ext := strings.ToLower(filepath.Ext(meta.RelativePath))
		if !containsString(extensions, ext) {
			continue
		}
		if meta.Mode == "120000" {
			// Assumption: historical git symlink blobs are skipped during snapshot
			// builds because the target cannot be resolved through the
			// display-path-based runtime policy without materializing the tree.
			continue
		}
		if meta.Size > options.MaxFileBytes {
			return nil, fmt.Errorf("file exceeds MAX_FILE_BYTES: %s", meta.RelativePath)
		}
		buffer, err := gitOutputBuffer(repoPath, "cat-file", "-p", fmt.Sprintf("%s:%s", spec.Ref, meta.RelativePath))
		if err != nil {
			return nil, err
		}
		decoded, err := textdecode.Decode(buffer, snapshotFallbackEncodings())
		if err != nil {
			if err == textdecode.ErrBinary || err == textdecode.ErrUnsupportedEncoding {
				continue
			}
			// COVERAGE_EXCEPTION: textdecode.Decode currently returns only nil,
			// ErrBinary, or ErrUnsupportedEncoding for this call shape, so hitting
			// this fallback would require changing that contract first.
			return nil, err
		}
		content := textutil.NormalizeLineEndings(decoded.Text)
		entries = append(entries, CodeEntry{
			LogicalPath:  buildLogicalPath(spec.ID, meta.RelativePath),
			RootID:       spec.ID,
			RelativePath: meta.RelativePath,
			Ext:          ext,
			LineCount:    lineCount(content),
			Content:      content,
		})
	}
	return entries, nil
}

func ensureGitRepo(repoPath string) error {
	_, err := gitOutput(repoPath, "rev-parse", "--is-inside-work-tree")
	return err
}

func gitFetch(repoPath, remote string) error {
	args := []string{"fetch"}
	if remote != "" {
		args = append(args, remote)
	}
	_, err := gitOutput(repoPath, args...)
	return err
}

func listBranches(repoPath string) ([]string, error) {
	output, err := gitOutput(repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	results := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			results = append(results, line)
		}
	}
	return results, nil
}

func gitOutput(repoPath string, args ...string) (string, error) {
	output, err := runGitCommand(repoPath, args...)
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func gitOutputBuffer(repoPath string, args ...string) ([]byte, error) {
	output, err := runGitCommand(repoPath, args...)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func runGitCommand(repoPath string, args ...string) ([]byte, error) {
	gitPath, err := gitExecutablePath()
	if err != nil {
		return nil, err
	}

	output, err := gitCombinedOutput(gitPath, append([]string{"-C", repoPath}, args...)...)
	if err != nil {
		return nil, formatGitCommandError(args, err, output)
	}
	return output, nil
}

func gitExecutablePath() (string, error) {
	path, err := gitLookPath("git")
	if err == nil {
		return path, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", errGitUnavailable
	}
	return "", errGitLookupFailed
}

func formatGitCommandError(args []string, err error, output []byte) error {
	command := strings.Join(args, " ")
	details := strings.TrimSpace(string(output))
	if details == "" {
		return fmt.Errorf("git %s failed: %w", command, err)
	}
	return fmt.Errorf("git %s failed: %w: %s", command, err, details)
}

func defaultGitCombinedOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

func fingerprint(repoPath string, roots []Root, entries []CodeEntry) string {
	parts := make([]string, 0, len(roots)+len(entries))
	for _, root := range roots {
		parts = append(parts, strings.Join([]string{root.ID, root.SourceKind, repoPath, root.Ref}, "|"))
	}
	for _, entry := range entries {
		parts = append(parts, strings.Join([]string{
			entry.LogicalPath,
			entry.RootID,
			strconvItoa(len([]byte(entry.Content))),
		}, "|"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func normalizeExtensions(extensions []string) []string {
	if len(extensions) == 0 {
		return []string{".cs", ".ts"}
	}
	normalized := make([]string, 0, len(extensions))
	seen := map[string]struct{}{}
	for _, ext := range extensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if _, ok := seen[ext]; ok {
			continue
		}
		seen[ext] = struct{}{}
		normalized = append(normalized, ext)
	}
	return normalized
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isWorktreeRef(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "WORKTREE", "@WORKTREE", "HEAD":
		return true
	default:
		return false
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	return len(textutil.SplitLines(content))
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func normalizeFetchOptions(fetchEnabled, fetchOnStart bool) (bool, bool) {
	if !fetchEnabled {
		return false, false
	}
	return true, fetchOnStart
}

func normalizedMaxFileBytes(value int64) int64 {
	if value <= 0 {
		return 1_000_000
	}
	return value
}

func strconvItoa(value int) string {
	return fmt.Sprintf("%d", value)
}

func buildLogicalPath(rootID, relativePath string) string {
	return "@" + rootID + "/" + filepath.ToSlash(relativePath)
}

func normalizeRootID(path string) string {
	normalized := strings.ToLower(filepath.ToSlash(path))
	var builder strings.Builder
	lastDash := false
	for _, r := range normalized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if lastDash {
			continue
		}
		builder.WriteRune('-')
		lastDash = true
	}

	rootID := strings.Trim(builder.String(), "-")
	if rootID == "" {
		return "samples"
	}
	return rootID
}

func validateSchema(db *sql.DB) error {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name IN ('meta', 'git_roots', 'code_entries', 'code_entries_fts')`)
	if err != nil {
		return err
	}
	defer rows.Close()

	found := map[string]struct{}{}
	for rows.Next() {
		var name string
		// COVERAGE_EXCEPTION: sqlite_master.name is textual for normal SQLite
		// fixtures; a Scan failure here would require corrupt driver metadata and
		// is not a stable local/CI scenario.
		if err := rows.Scan(&name); err != nil {
			return err
		}
		found[name] = struct{}{}
	}
	for _, required := range []string{"meta", "git_roots", "code_entries", "code_entries_fts"} {
		if _, ok := found[required]; !ok {
			return fmt.Errorf("invalid git snapshot schema: missing %s", required)
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
	// COVERAGE_EXCEPTION: modernc sqlite surfaces meta query failures at
	// Query/Scan time for these fixtures; a deferred rows.Err after iteration is
	// not stably triggerable without custom driver fault injection.
	if err := rows.Err(); err != nil {
		return Meta{}, err
	}
	schemaVersion := parseInt(values["schema_version"])
	if schemaVersion != SchemaVersion {
		return Meta{}, fmt.Errorf("unsupported git snapshot schema_version: %q", values["schema_version"])
	}
	return Meta{
		SchemaVersion: schemaVersion,
		GeneratedAt:   values["generated_at"],
		RootCount:     parseInt(values["root_count"]),
		FileCount:     parseInt(values["file_count"]),
		RepoPath:      values["repo_path"],
		Fingerprint:   values["fingerprint"],
	}, nil
}

func loadRoots(db *sql.DB) ([]Root, error) {
	rows, err := db.Query(`SELECT root_id, source_kind, repo_path, ref, description, labels FROM git_roots ORDER BY root_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roots := make([]Root, 0, 8)
	for rows.Next() {
		var root Root
		var refValue sql.NullString
		var labels string
		if err := rows.Scan(&root.ID, &root.SourceKind, &root.RepoPath, &refValue, &root.Description, &labels); err != nil {
			return nil, err
		}
		if refValue.Valid {
			root.Ref = refValue.String
		}
		root.Labels = splitLabels(labels)
		roots = append(roots, root)
	}
	return roots, rows.Err()
}

func splitLabels(labels string) []string {
	parts := strings.Split(labels, "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	values := make([]string, count)
	for idx := range values {
		values[idx] = "?"
	}
	return strings.Join(values, ", ")
}

func stringSliceArgs(values []string) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func firstFileQuery(rootID string, extCount int) string {
	query := `SELECT logical_path, content FROM code_entries WHERE root_id = ?`
	if extCount > 0 {
		query += ` AND ext IN (` + placeholders(extCount) + `)`
	}
	query += ` ORDER BY logical_path LIMIT 1`
	return query
}

type lineMatch struct {
	line  int
	score int
}

func findLineMatches(lines []string, phrase string, tokens []string) []lineMatch {
	matches := make([]lineMatch, 0, 8)
	for idx, line := range lines {
		normalized := textsearch.Normalize(line)
		score := 0
		for _, token := range tokens {
			if strings.Contains(normalized, token) {
				score++
			}
		}
		if phrase != "" && strings.Contains(normalized, phrase) {
			score += 2
		}
		if score > 0 {
			matches = append(matches, lineMatch{line: idx + 1, score: score})
		}
	}
	return matches
}

func sortSnapshotResults(results []rankedSnapshotResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].rank != results[j].rank {
			return results[i].rank < results[j].rank
		}
		if results[i].LogicalPath != results[j].LogicalPath {
			return results[i].LogicalPath < results[j].LogicalPath
		}
		if results[i].StartLine != results[j].StartLine {
			return results[i].StartLine < results[j].StartLine
		}
		return results[i].EndLine < results[j].EndLine
	})
}

func parseInt(value string) int {
	var parsed int
	fmt.Sscanf(value, "%d", &parsed)
	return parsed
}

type refEntryMeta struct {
	Mode         string
	RelativePath string
	Size         int64
}

func listRefEntries(repoPath string, refName string) ([]refEntryMeta, error) {
	output, err := gitOutput(repoPath, "ls-tree", "-r", "-l", refName)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	results := make([]refEntryMeta, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tab := strings.IndexRune(line, '\t')
		if tab <= 0 || tab+1 >= len(line) {
			continue
		}
		header := strings.Fields(line[:tab])
		if len(header) < 4 {
			continue
		}
		size := int64(0)
		fmt.Sscanf(header[3], "%d", &size)
		results = append(results, refEntryMeta{
			Mode:         header[0],
			RelativePath: filepath.ToSlash(line[tab+1:]),
			Size:         size,
		})
	}
	return results, nil
}

func snapshotFallbackEncodings() []string {
	return textdecode.ParseFallbackEncodings(os.Getenv("SCRIPTORIUM_TEXT_ENCODING_FALLBACK"), runtime.GOOS)
}

func uniqueRuntimePaths(paths []string) []string {
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
