package filesafe

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/silvekt/scriptorium/internal/textdecode"
)

const defaultMaxFileBytes int64 = 1_000_000
const maxTextFileCacheEntries = 512

type Root struct {
	path          string
	realPath      string
	allowSymlinks bool
	maxFileBytes  int64
	host          filesystem
}

type ResolvedPath struct {
	RelativePath string
	AbsolutePath string
	RealPath     string
}

type TextFile struct {
	Path     string
	Text     string
	Encoding string
	Size     int64
}

type CacheStats struct {
	MaxEntries int `json:"maxEntries"`
	Entries    int `json:"entries"`
	Hits       int `json:"hits"`
	Misses     int `json:"misses"`
	Evictions  int `json:"evictions"`
}

type textFileCacheEntry struct {
	mtimeUnixNano int64
	size          int64
	textFile      TextFile
}

var textFileCacheState = struct {
	mu        sync.Mutex
	entries   map[string]textFileCacheEntry
	hits      int
	misses    int
	evictions int
}{
	entries: map[string]textFileCacheEntry{},
}

func NewRoot(rootPath string, allowSymlinks bool, maxFileBytes int64) (Root, error) {
	return newRootWithFS(rootPath, allowSymlinks, maxFileBytes, osFilesystem{})
}

func newRootWithFS(rootPath string, allowSymlinks bool, maxFileBytes int64, host filesystem) (Root, error) {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return Root{}, errors.New("root path is required")
	}

	absoluteRoot, err := host.Abs(rootPath)
	if err != nil {
		return Root{}, err
	}

	info, err := host.Stat(absoluteRoot)
	if err != nil {
		return Root{}, err
	}
	if !info.IsDir() {
		return Root{}, fmt.Errorf("root path is not a directory: %s", absoluteRoot)
	}

	realRoot, err := host.EvalSymlinks(absoluteRoot)
	if err != nil {
		return Root{}, err
	}

	if maxFileBytes <= 0 {
		maxFileBytes = defaultMaxFileBytes
	}

	return Root{
		path:          filepath.Clean(absoluteRoot),
		realPath:      filepath.Clean(realRoot),
		allowSymlinks: allowSymlinks,
		maxFileBytes:  maxFileBytes,
		host:          host,
	}, nil
}

func (r Root) hostFS() filesystem {
	if r.host == nil {
		return osFilesystem{}
	}
	return r.host
}

func (r Root) Path() string {
	return r.path
}

func (r Root) RealPath() string {
	return r.realPath
}

func (r Root) Resolve(relativePath string) (ResolvedPath, error) {
	normalizedPath, err := normalizeRelativePath(relativePath)
	if err != nil {
		return ResolvedPath{}, err
	}

	absolutePath := filepath.Clean(filepath.Join(r.path, filepath.FromSlash(normalizedPath)))

	host := r.hostFS()
	if r.allowSymlinks {
		realPath, err := host.EvalSymlinks(absolutePath)
		if err != nil {
			return ResolvedPath{}, err
		}
		return ResolvedPath{
			RelativePath: normalizedPath,
			AbsolutePath: absolutePath,
			RealPath:     filepath.Clean(realPath),
		}, nil
	}

	if err := ensureNoSymlinkPath(r.path, absolutePath, host); err != nil {
		return ResolvedPath{}, err
	}

	return ResolvedPath{
		RelativePath: normalizedPath,
		AbsolutePath: absolutePath,
		RealPath:     absolutePath,
	}, nil
}

func (r Root) ListFiles(extensions []string) ([]string, error) {
	filter := normalizeExtensionFilter(extensions)
	visitedRealDirs := map[string]struct{}{r.realPath: {}}
	results := make([]string, 0, 32)

	if err := r.walk(r.path, "", visitedRealDirs, filter, &results); err != nil {
		return nil, err
	}

	sort.Strings(results)
	return results, nil
}

func (r Root) ReadText(relativePath string, fallbackEncodings []string) (TextFile, error) {
	resolved, err := r.Resolve(relativePath)
	if err != nil {
		return TextFile{}, err
	}

	host := r.hostFS()
	info, err := host.Stat(resolved.RealPath)
	if err != nil {
		return TextFile{}, err
	}
	if !info.Mode().IsRegular() {
		return TextFile{}, fmt.Errorf("path is not a regular file: %s", resolved.RelativePath)
	}
	if info.Size() > r.maxFileBytes {
		return TextFile{}, fmt.Errorf("file exceeds MAX_FILE_BYTES: %s", resolved.RelativePath)
	}

	cacheKey := filepath.Clean(resolved.RealPath)
	if cached, ok := lookupTextFileCache(cacheKey, info); ok {
		return cached, nil
	}

	data, err := host.ReadFile(resolved.RealPath)
	if err != nil {
		return TextFile{}, err
	}

	decoded, err := textdecode.Decode(data, fallbackEncodings)
	if err != nil {
		return TextFile{}, err
	}

	textFile := TextFile{
		Path:     resolved.RelativePath,
		Text:     decoded.Text,
		Encoding: decoded.Encoding,
		Size:     info.Size(),
	}
	storeTextFileCache(cacheKey, info, textFile)
	return textFile, nil
}

func (r Root) walk(displayDir, relativeDir string, visitedRealDirs map[string]struct{}, filter map[string]struct{}, results *[]string) error {
	host := r.hostFS()
	entries, err := host.ReadDir(displayDir)
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		nextRelative := joinRelativePath(relativeDir, name)
		displayPath := filepath.Join(displayDir, name)

		if entry.Type()&iofs.ModeSymlink != 0 {
			if !r.allowSymlinks {
				continue
			}

			realPath, err := host.EvalSymlinks(displayPath)
			if err != nil {
				return err
			}

			info, err := host.Stat(displayPath)
			if err != nil {
				return err
			}
			if info.IsDir() {
				if shouldIgnoreDir(name) {
					continue
				}
				if _, seen := visitedRealDirs[realPath]; seen {
					continue
				}
				visitedRealDirs[realPath] = struct{}{}
				if err := r.walk(displayPath, nextRelative, visitedRealDirs, filter, results); err != nil {
					return err
				}
				continue
			}
			if info.Mode().IsRegular() && matchesExtension(name, filter) {
				*results = append(*results, filepath.ToSlash(nextRelative))
			}
			continue
		}

		if entry.IsDir() {
			if shouldIgnoreDir(name) {
				continue
			}
			if err := r.walk(displayPath, nextRelative, visitedRealDirs, filter, results); err != nil {
				return err
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && matchesExtension(name, filter) {
			*results = append(*results, filepath.ToSlash(nextRelative))
		}
	}

	return nil
}

func normalizeRelativePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	if strings.ContainsRune(path, rune(0)) {
		return "", errors.New("path contains NUL")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("absolute paths are not allowed")
	}
	if strings.ContainsRune(path, ':') {
		return "", errors.New("colon is not allowed in relative paths")
	}

	normalized := strings.ReplaceAll(path, `\`, `/`)
	segments := strings.Split(normalized, "/")
	cleaned := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" || segment == "." {
			continue
		}
		if segment == ".." {
			return "", errors.New("path traversal is not allowed")
		}
		cleaned = append(cleaned, segment)
	}
	if len(cleaned) == 0 {
		return "", errors.New("path is required")
	}
	return strings.Join(cleaned, "/"), nil
}

func ensureNoSymlinkPath(rootPath, targetPath string, host filesystem) error {
	relativePath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return err
	}

	current := rootPath
	for _, segment := range strings.Split(relativePath, string(filepath.Separator)) {
		if segment == "." || segment == "" {
			continue
		}
		current = filepath.Join(current, segment)
		info, err := host.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&iofs.ModeSymlink != 0 {
			return fmt.Errorf("symlink access is disabled: %s", current)
		}
	}

	return nil
}

func isWithinRoot(rootPath, targetPath string) bool {
	relativePath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return false
	}
	if relativePath == ".." {
		return false
	}
	return !strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
}

func normalizeExtensionFilter(extensions []string) map[string]struct{} {
	filter := make(map[string]struct{}, len(extensions))
	for _, ext := range extensions {
		normalized := strings.ToLower(strings.TrimSpace(ext))
		if normalized == "" {
			continue
		}
		if !strings.HasPrefix(normalized, ".") {
			normalized = "." + normalized
		}
		filter[normalized] = struct{}{}
	}
	return filter
}

func matchesExtension(name string, filter map[string]struct{}) bool {
	if len(filter) == 0 {
		return true
	}
	_, ok := filter[strings.ToLower(filepath.Ext(name))]
	return ok
}

func shouldIgnoreDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "bin", "obj":
		return true
	default:
		return false
	}
}

func joinRelativePath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func GetTextFileCacheStats() CacheStats {
	textFileCacheState.mu.Lock()
	defer textFileCacheState.mu.Unlock()
	return CacheStats{
		MaxEntries: maxTextFileCacheEntries,
		Entries:    len(textFileCacheState.entries),
		Hits:       textFileCacheState.hits,
		Misses:     textFileCacheState.misses,
		Evictions:  textFileCacheState.evictions,
	}
}

func ResetTextFileCacheForTesting() {
	textFileCacheState.mu.Lock()
	defer textFileCacheState.mu.Unlock()
	textFileCacheState.entries = map[string]textFileCacheEntry{}
	textFileCacheState.hits = 0
	textFileCacheState.misses = 0
	textFileCacheState.evictions = 0
}

func lookupTextFileCache(cacheKey string, info iofs.FileInfo) (TextFile, bool) {
	textFileCacheState.mu.Lock()
	defer textFileCacheState.mu.Unlock()

	cached, ok := textFileCacheState.entries[cacheKey]
	if !ok || cached.size != info.Size() || cached.mtimeUnixNano != info.ModTime().UnixNano() {
		textFileCacheState.misses++
		return TextFile{}, false
	}

	delete(textFileCacheState.entries, cacheKey)
	textFileCacheState.entries[cacheKey] = cached
	textFileCacheState.hits++
	return cached.textFile, true
}

func storeTextFileCache(cacheKey string, info iofs.FileInfo, textFile TextFile) {
	textFileCacheState.mu.Lock()
	defer textFileCacheState.mu.Unlock()

	delete(textFileCacheState.entries, cacheKey)
	textFileCacheState.entries[cacheKey] = textFileCacheEntry{
		mtimeUnixNano: info.ModTime().UnixNano(),
		size:          info.Size(),
		textFile:      textFile,
	}
	if len(textFileCacheState.entries) <= maxTextFileCacheEntries {
		return
	}
	for oldestKey := range textFileCacheState.entries {
		delete(textFileCacheState.entries, oldestKey)
		textFileCacheState.evictions++
		break
	}
}
