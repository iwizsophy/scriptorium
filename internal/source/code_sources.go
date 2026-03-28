package source

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/filesafe"
	"github.com/iwizsophy/scriptorium/internal/gitsnapshot"
)

const maxCodeSourceCacheEntries = 128

type CodeSource struct {
	ID          string
	Kind        string
	Description string
	Labels      []string

	root        *filesafe.Root
	snapshot    *gitsnapshot.Artifact
	snapshotRef string
}

type CodeTextFile struct {
	Path string
	Text string
	Size int64
}

type CacheStats struct {
	MaxEntries      int `json:"maxEntries"`
	Entries         int `json:"entries"`
	Hits            int `json:"hits"`
	Misses          int `json:"misses"`
	Evictions       int `json:"evictions"`
	FilesystemRoots int `json:"filesystemRoots"`
	SnapshotRoots   int `json:"snapshotRoots"`
}

type codeSourceCacheEntry struct {
	sources       []CodeSource
	multipleRoots bool
}

var codeSourceCacheState = struct {
	mu        sync.Mutex
	entries   map[string]codeSourceCacheEntry
	hits      int
	misses    int
	evictions int
}{
	entries: map[string]codeSourceCacheEntry{},
}

func (s CodeSource) Search(query string, extensions []string, topK int, snippetLines int) ([]gitsnapshot.SearchResult, bool, error) {
	if s.snapshot == nil {
		return nil, false, nil
	}
	for _, root := range s.snapshot.Roots {
		if root.ID == s.snapshotRef {
			results, err := s.snapshot.Search(root, query, extensions, topK, snippetLines)
			return results, true, err
		}
	}
	return nil, true, fmt.Errorf("snapshot root not found: %s", s.snapshotRef)
}

func BuildCodeSources(cfg config.Runtime) ([]CodeSource, bool, error) {
	cacheKey := buildCodeSourceCacheKey(cfg)
	if cached, ok := lookupCodeSourceCache(cacheKey); ok {
		return cached.sources, cached.multipleRoots, nil
	}

	sources := make([]CodeSource, 0, len(cfg.SampleRoots)+1)
	seenIDs := map[string]int{}

	for _, sampleRoot := range cfg.SampleRoots {
		root, err := filesafe.NewRoot(sampleRoot, cfg.AllowSymlinks, cfg.MaxFileBytes)
		if err != nil {
			return nil, false, err
		}

		rootID := NormalizeRootID(sampleRoot)
		seenIDs[rootID]++
		if seenIDs[rootID] > 1 {
			rootID = rootID + "-" + strconv.Itoa(seenIDs[rootID])
		}

		rootCopy := root
		sources = append(sources, CodeSource{
			ID:          rootID,
			Kind:        "fs",
			Description: sampleRoot,
			Labels:      []string{rootID},
			root:        &rootCopy,
		})
	}

	if state := gitsnapshot.OpenRuntimeSet(cfg.EffectiveGitSnapshotPaths()); state.Enabled {
		for indexIdx := range state.Indexes {
			artifact := &state.Indexes[indexIdx]
			for _, snapshotRoot := range artifact.Roots {
				sources = append(sources, CodeSource{
					ID:          snapshotRoot.ID,
					Kind:        "git_snapshot",
					Description: snapshotRoot.Description,
					Labels:      append([]string(nil), snapshotRoot.Labels...),
					snapshot:    artifact,
					snapshotRef: snapshotRoot.ID,
				})
			}
		}
	}

	multipleRoots := len(sources) > 1
	storeCodeSourceCache(cacheKey, codeSourceCacheEntry{
		sources:       append([]CodeSource(nil), sources...),
		multipleRoots: multipleRoots,
	})
	return sources, multipleRoots, nil
}

func (s CodeSource) ListFiles(extensions []string) ([]string, error) {
	if s.root != nil {
		return s.root.ListFiles(extensions)
	}
	if s.snapshot != nil {
		entries := s.snapshot.EntriesForRoot(s.snapshotRef, extensions)
		results := make([]string, 0, len(entries))
		for _, entry := range entries {
			results = append(results, entry.RelativePath)
		}
		return results, nil
	}
	return nil, fmt.Errorf("code source is not configured")
}

func (s CodeSource) ReadText(relativePath string, fallbackEncodings []string) (CodeTextFile, error) {
	if s.root != nil {
		textFile, err := s.root.ReadText(relativePath, fallbackEncodings)
		if err != nil {
			return CodeTextFile{}, err
		}
		return CodeTextFile{
			Path: textFile.Path,
			Text: textFile.Text,
			Size: textFile.Size,
		}, nil
	}
	if s.snapshot != nil {
		entry, ok := s.snapshot.FindEntry(s.snapshotRef, relativePath)
		if !ok {
			return CodeTextFile{}, fmt.Errorf("snapshot entry not found: %s", relativePath)
		}
		return CodeTextFile{
			Path: entry.RelativePath,
			Text: entry.Content,
			Size: int64(len(entry.Content)),
		}, nil
	}
	return CodeTextFile{}, fmt.Errorf("code source is not configured")
}

func (s CodeSource) BuildLogicalPath(relativePath string, multipleRoots bool) string {
	relativePath = normalizeSlashPath(relativePath)
	if s.Kind == "git_snapshot" || multipleRoots {
		return "@" + s.ID + "/" + relativePath
	}
	return relativePath
}

func BuildLogicalPath(sourceID, relativePath string, multipleRoots bool) string {
	relativePath = normalizeSlashPath(relativePath)
	if !multipleRoots {
		return relativePath
	}
	return "@" + sourceID + "/" + relativePath
}

func ResolveLogicalPath(cfg config.Runtime, logicalPath string) (CodeSource, string, bool, error) {
	sources, multipleRoots, err := BuildCodeSources(cfg)
	if err != nil {
		return CodeSource{}, "", false, err
	}
	if len(sources) == 0 {
		return CodeSource{}, "", false, fmt.Errorf("code sources are not configured")
	}

	if len(sources) == 1 && sources[0].Kind == "git_snapshot" {
		multipleRoots = true
	}

	if !multipleRoots {
		return sources[0], normalizeSlashPath(logicalPath), false, nil
	}

	if !strings.HasPrefix(logicalPath, "@") {
		return CodeSource{}, "", true, fmt.Errorf("logical path must start with @<rootId>/ when multiple code sources are configured")
	}

	parts := strings.SplitN(strings.TrimPrefix(logicalPath, "@"), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return CodeSource{}, "", true, fmt.Errorf("logical path must use @<rootId>/<relative/path> form")
	}

	for _, source := range sources {
		if source.ID == parts[0] {
			return source, normalizeSlashPath(parts[1]), true, nil
		}
	}

	return CodeSource{}, "", true, fmt.Errorf("unknown code source id: %s", parts[0])
}

func NormalizeRootID(path string) string {
	normalized := strings.ToLower(normalizeSlashPath(path))
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

func normalizeSlashPath(path string) string {
	return strings.ReplaceAll(filepath.ToSlash(path), `\`, `/`)
}

func GetCodeSourceCacheStats() CacheStats {
	codeSourceCacheState.mu.Lock()
	defer codeSourceCacheState.mu.Unlock()
	filesystemRoots := 0
	snapshotRoots := 0
	for _, entry := range codeSourceCacheState.entries {
		for _, source := range entry.sources {
			switch source.Kind {
			case "fs":
				filesystemRoots++
			case "git_snapshot":
				snapshotRoots++
			}
		}
	}
	return CacheStats{
		MaxEntries:      maxCodeSourceCacheEntries,
		Entries:         len(codeSourceCacheState.entries),
		Hits:            codeSourceCacheState.hits,
		Misses:          codeSourceCacheState.misses,
		Evictions:       codeSourceCacheState.evictions,
		FilesystemRoots: filesystemRoots,
		SnapshotRoots:   snapshotRoots,
	}
}

func ResetCodeSourceCacheForTesting() {
	codeSourceCacheState.mu.Lock()
	defer codeSourceCacheState.mu.Unlock()
	codeSourceCacheState.entries = map[string]codeSourceCacheEntry{}
	codeSourceCacheState.hits = 0
	codeSourceCacheState.misses = 0
	codeSourceCacheState.evictions = 0
}

func buildCodeSourceCacheKey(cfg config.Runtime) string {
	parts := make([]string, 0, len(cfg.SampleRoots)+4)
	parts = append(parts, strconv.FormatBool(cfg.AllowSymlinks), strconv.FormatInt(cfg.MaxFileBytes, 10))
	parts = append(parts, cfg.EffectiveGitSnapshotPaths()...)
	parts = append(parts, cfg.SampleRoots...)
	return strings.Join(parts, "::")
}

func lookupCodeSourceCache(cacheKey string) (codeSourceCacheEntry, bool) {
	codeSourceCacheState.mu.Lock()
	defer codeSourceCacheState.mu.Unlock()

	cached, ok := codeSourceCacheState.entries[cacheKey]
	if !ok {
		codeSourceCacheState.misses++
		return codeSourceCacheEntry{}, false
	}
	delete(codeSourceCacheState.entries, cacheKey)
	codeSourceCacheState.entries[cacheKey] = cached
	codeSourceCacheState.hits++
	return codeSourceCacheEntry{
		sources:       append([]CodeSource(nil), cached.sources...),
		multipleRoots: cached.multipleRoots,
	}, true
}

func storeCodeSourceCache(cacheKey string, entry codeSourceCacheEntry) {
	codeSourceCacheState.mu.Lock()
	defer codeSourceCacheState.mu.Unlock()

	delete(codeSourceCacheState.entries, cacheKey)
	codeSourceCacheState.entries[cacheKey] = codeSourceCacheEntry{
		sources:       append([]CodeSource(nil), entry.sources...),
		multipleRoots: entry.multipleRoots,
	}
	if len(codeSourceCacheState.entries) <= maxCodeSourceCacheEntries {
		return
	}
	for oldestKey := range codeSourceCacheState.entries {
		delete(codeSourceCacheState.entries, oldestKey)
		codeSourceCacheState.evictions++
		break
	}
}
