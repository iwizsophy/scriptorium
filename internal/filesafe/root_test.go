package filesafe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/silvekt/scriptorium/internal/textdecode"
)

func TestNewRootRequiresDirectory(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := NewRoot(filePath, false, 0); err == nil {
		t.Fatal("expected NewRoot to reject a file path")
	}
}

func TestNewRootRequiresPathAndAppliesDefaultMaxFileBytes(t *testing.T) {
	if _, err := NewRoot("", false, 0); err == nil {
		t.Fatal("expected NewRoot to reject empty path")
	}
	if _, err := NewRoot(filepath.Join(t.TempDir(), "missing"), false, 0); err == nil {
		t.Fatal("expected NewRoot to reject missing directory")
	}

	root, err := NewRoot(t.TempDir(), false, -1)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	if root.maxFileBytes != defaultMaxFileBytes {
		t.Fatalf("expected default max file bytes, got %#v", root)
	}
}

func TestResolveRejectsUnsafePaths(t *testing.T) {
	root, err := NewRoot(t.TempDir(), false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	absolutePath := "/absolute/path.txt"
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(root.Path())
		if volume == "" {
			volume = `C:`
		}
		absolutePath = filepath.Join(volume+`\`, "absolute", "path.txt")
	}

	cases := []string{
		"",
		".",
		"..\\secret.txt",
		absolutePath,
		"nested:file.txt",
		"bad\x00path.txt",
	}

	for _, input := range cases {
		if _, err := root.Resolve(input); err == nil {
			t.Fatalf("expected Resolve to reject %q", input)
		}
	}
}

func TestResolveReturnsNormalizedPaths(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "nested", "file.txt"), "hello")

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	resolved, err := root.Resolve("./nested\\file.txt")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolved.RelativePath != "nested/file.txt" {
		t.Fatalf("unexpected relative path: %#v", resolved)
	}
	if resolved.AbsolutePath != filepath.Join(dir, "nested", "file.txt") || resolved.RealPath != filepath.Join(dir, "nested", "file.txt") {
		t.Fatalf("unexpected absolute or real path: %#v", resolved)
	}
}

func TestResolveReturnsErrorForMissingSymlinkTargetWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	if _, err := root.Resolve("missing.txt"); err == nil {
		t.Fatal("expected Resolve to fail for missing target when symlink resolution is enabled")
	}
}

func TestResolveCoversMissingPathAndEnabledSuccessBranches(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "nested", "file.txt"), "hello")

	disabledRoot, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	if _, err := disabledRoot.Resolve("missing.txt"); err == nil {
		t.Fatal("expected Resolve to fail for missing target when symlink checks are enforced")
	}

	enabledRoot, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	resolved, err := enabledRoot.Resolve("nested/file.txt")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolved.RealPath != filepath.Join(dir, "nested", "file.txt") {
		t.Fatalf("unexpected resolved real path: %#v", resolved)
	}
}

func TestResolveAllowsSymlinkEscapingRootWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	targetPath := filepath.Join(outsideDir, "outside.txt")
	mustWriteFile(t, targetPath, "secret")

	linkPath := filepath.Join(dir, "escape.txt")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	resolved, err := root.Resolve("escape.txt")
	if err != nil {
		t.Fatalf("expected symlink escape to be allowed, got %v", err)
	}
	if resolved.RelativePath != "escape.txt" || resolved.RealPath != targetPath {
		t.Fatalf("unexpected resolved escaping symlink path: %#v", resolved)
	}
}

func TestListFilesDeterministicAndIgnoresDirs(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "docs", "b.md"), "# b\n")
	mustWriteFile(t, filepath.Join(dir, "docs", "a.md"), "# a\n")
	mustWriteFile(t, filepath.Join(dir, "docs", "nested", "c.md"), "# c\n")
	mustWriteFile(t, filepath.Join(dir, "docs", "ignore.txt"), "plain\n")
	mustWriteFile(t, filepath.Join(dir, ".git", "ignored.md"), "# ignored\n")
	mustWriteFile(t, filepath.Join(dir, "node_modules", "ignored.md"), "# ignored\n")

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ListFiles([]string{".md"})
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}

	want := []string{"docs/a.md", "docs/b.md", "docs/nested/c.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected file list: got=%v want=%v", got, want)
	}
}

func TestRootExposesConfiguredPaths(t *testing.T) {
	dir := t.TempDir()
	root, err := NewRoot(filepath.Join(dir, "."), false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	want := filepath.Clean(dir)
	if root.Path() != want {
		t.Fatalf("unexpected Path: got=%q want=%q", root.Path(), want)
	}
	if root.RealPath() != want {
		t.Fatalf("unexpected RealPath: got=%q want=%q", root.RealPath(), want)
	}
}

func TestListFilesSkipsVisitedSymlinkDirectories(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "docs", "real.md"), "# real\n")

	firstLink := filepath.Join(dir, "docs-link")
	if err := os.Symlink(filepath.Join(dir, "docs"), firstLink); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}
	secondLink := filepath.Join(dir, "docs-link-2")
	if err := os.Symlink(filepath.Join(dir, "docs"), secondLink); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ListFiles([]string{".md"})
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}

	want := []string{"docs/real.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected file list with symlink directories: got=%v want=%v", got, want)
	}
}

func TestListFilesAllowsEscapingSymlinkDirectoryWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	mustWriteFile(t, filepath.Join(outsideDir, "outside.md"), "# outside\n")

	linkPath := filepath.Join(dir, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ListFiles([]string{".md"})
	if err != nil {
		t.Fatalf("expected escaping symlink directory listing to succeed, got %v", err)
	}
	if !slices.Equal(got, []string{"escape/outside.md"}) {
		t.Fatalf("unexpected escaping symlink directory listing: %#v", got)
	}
}

func TestListFilesIncludesSymlinkedRegularFileWithinRootWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "docs", "target.md")
	mustWriteFile(t, targetPath, "# target\n")

	linkPath := filepath.Join(dir, "docs", "linked.md")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ListFiles([]string{".md"})
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if !slices.Equal(got, []string{"docs/linked.md", "docs/target.md"}) {
		t.Fatalf("unexpected file list with symlinked regular file: %#v", got)
	}
}

func TestListFilesSkipsSymlinkWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "docs", "target.md")
	mustWriteFile(t, targetPath, "# target\n")

	linkPath := filepath.Join(dir, "docs", "linked.md")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ListFiles([]string{".md"})
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if !slices.Equal(got, []string{"docs/target.md"}) {
		t.Fatalf("unexpected file list with symlink disabled: %#v", got)
	}
}

func TestListFilesSkipsIgnoredSymlinkDirsAndNonMatchingSymlinkFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "docs", "guide.md"), "# guide\n")
	mustWriteFile(t, filepath.Join(dir, "docs", "note.txt"), "note")

	ignoredLink := filepath.Join(dir, ".git")
	if err := os.Symlink(filepath.Join(dir, "docs"), ignoredLink); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}
	textLink := filepath.Join(dir, "linked.txt")
	if err := os.Symlink(filepath.Join(dir, "docs", "note.txt"), textLink); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ListFiles([]string{".md"})
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if !slices.Equal(got, []string{"docs/guide.md"}) {
		t.Fatalf("unexpected file list with ignored symlink dir and txt link: %#v", got)
	}
}

func TestListFilesReturnsErrorForBrokenSymlinkWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing.md"), filepath.Join(dir, "broken.md")); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	if _, err := root.ListFiles([]string{".md"}); err == nil {
		t.Fatal("expected broken symlink to surface an error")
	}
}

func TestWalkReturnsUnderlyingReadDirError(t *testing.T) {
	root, err := NewRoot(t.TempDir(), false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	var results []string
	if err := root.walk(filepath.Join(root.Path(), "missing"), "missing", map[string]struct{}{root.RealPath(): {}}, nil, &results); err == nil {
		t.Fatal("expected walk to return ReadDir error")
	}
}

func TestWalkWithNilFilterIncludesRegularFilesAndIgnoresConcreteDirs(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "docs", "guide.md"), "# guide\n")
	mustWriteFile(t, filepath.Join(dir, "notes.txt"), "note\n")
	mustWriteFile(t, filepath.Join(dir, "bin", "ignored.exe"), "binary")
	mustWriteFile(t, filepath.Join(dir, "obj", "ignored.tmp"), "temp")

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	var results []string
	if err := root.walk(root.Path(), "", map[string]struct{}{root.RealPath(): {}}, nil, &results); err != nil {
		t.Fatalf("walk returned error: %v", err)
	}

	if !slices.Equal(results, []string{"docs/guide.md", "notes.txt"}) {
		t.Fatalf("unexpected walk results: %#v", results)
	}
}

func TestPathHelperSuccessBranches(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "nested", "file.txt")
	mustWriteFile(t, targetPath, "hello")

	normalized, err := normalizeRelativePath("./nested/file.txt")
	if err != nil || normalized != "nested/file.txt" {
		t.Fatalf("unexpected normalizeRelativePath result: %q err=%v", normalized, err)
	}

	if err := ensureNoSymlinkPath(dir, targetPath, osFilesystem{}); err != nil {
		t.Fatalf("expected ensureNoSymlinkPath success, got %v", err)
	}
	if err := ensureNoSymlinkPath(dir, dir, osFilesystem{}); err != nil {
		t.Fatalf("expected ensureNoSymlinkPath to allow the root path itself, got %v", err)
	}
	if !isWithinRoot(dir, dir) {
		t.Fatal("expected root to be within itself")
	}
}

func TestEnsureNoSymlinkPathReturnsErrorForSymlinkSegment(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "target.txt"), "hello")

	linkPath := filepath.Join(dir, "nested-link")
	if err := os.Symlink(filepath.Join(dir, "target.txt"), linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	if err := ensureNoSymlinkPath(dir, linkPath, osFilesystem{}); err == nil || !strings.Contains(err.Error(), "symlink access is disabled") {
		t.Fatalf("expected symlink path rejection, got %v", err)
	}
}

func TestPathHelpersCoverErrorBranches(t *testing.T) {
	dir := t.TempDir()
	if err := ensureNoSymlinkPath(dir, filepath.Join(dir, "missing.txt"), osFilesystem{}); err == nil {
		t.Fatal("expected ensureNoSymlinkPath to fail for missing path")
	}
	if isWithinRoot(filepath.Join(dir, "child"), dir) {
		t.Fatal("expected parent path to be outside child root")
	}
}

func TestWindowsVolumeMismatchHelperBranches(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("volume mismatch behavior is Windows-specific")
	}

	if isWithinRoot(`C:\repo`, `D:\repo\file.txt`) {
		t.Fatal("expected cross-volume path to be outside root")
	}
	if err := ensureNoSymlinkPath(`C:\repo`, `D:\repo\file.txt`, osFilesystem{}); err == nil {
		t.Fatal("expected ensureNoSymlinkPath to fail for cross-volume target")
	}
}

func TestReadTextRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "large.txt"), "0123456789")

	root, err := NewRoot(dir, false, 4)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	if _, err := root.ReadText("large.txt", nil); err == nil {
		t.Fatal("expected oversized file to be rejected")
	}
}

func TestReadTextRejectsBinaryContent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "binary.dat")
	if err := os.WriteFile(filePath, []byte{0x00, 0x01, 0x00, 0x02, 0x00}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	_, err = root.ReadText("binary.dat", nil)
	if !errors.Is(err, textdecode.ErrBinary) {
		t.Fatalf("expected ErrBinary, got %v", err)
	}
}

func TestReadTextRejectsSymlinkWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.txt")
	mustWriteFile(t, targetPath, "hello")

	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	if _, err := root.ReadText("link.txt", nil); err == nil {
		t.Fatal("expected symlink access to be rejected")
	}
}

func TestReadTextAllowsSymlinkWithinRootWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.txt")
	mustWriteFile(t, targetPath, "hello")

	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ReadText("link.txt", nil)
	if err != nil {
		t.Fatalf("ReadText returned error: %v", err)
	}
	if got.Text != "hello" {
		t.Fatalf("unexpected file contents: %#v", got)
	}
}

func TestReadTextAllowsSymlinkOutsideRootWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	targetPath := filepath.Join(outsideDir, "target.txt")
	mustWriteFile(t, targetPath, "outside")

	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}

	root, err := NewRoot(dir, true, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	got, err := root.ReadText("link.txt", nil)
	if err != nil {
		t.Fatalf("ReadText returned error: %v", err)
	}
	if got.Path != "link.txt" || got.Text != "outside" {
		t.Fatalf("unexpected file contents for escaping symlink: %#v", got)
	}
}

func TestReadTextRejectsDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	if _, err := root.ReadText("subdir", nil); err == nil || !strings.Contains(err.Error(), "path is not a regular file") {
		t.Fatalf("expected non-regular-file error, got %v", err)
	}
}

func TestReadTextUsesLiveCacheStats(t *testing.T) {
	ResetTextFileCacheForTesting()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "cached.txt")
	mustWriteFile(t, filePath, "alpha")

	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	first, err := root.ReadText("cached.txt", nil)
	if err != nil {
		t.Fatalf("first ReadText returned error: %v", err)
	}
	second, err := root.ReadText("cached.txt", nil)
	if err != nil {
		t.Fatalf("second ReadText returned error: %v", err)
	}
	if first.Text != "alpha" || second.Text != "alpha" {
		t.Fatalf("unexpected cached text values: first=%#v second=%#v", first, second)
	}

	stats := GetTextFileCacheStats()
	if stats.Entries != 1 || stats.Hits != 1 || stats.Misses != 1 || stats.MaxEntries != 512 || stats.Evictions != 0 {
		t.Fatalf("unexpected cache stats after hit: %#v", stats)
	}

	if err := os.WriteFile(filePath, []byte("beta"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	modTime := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filePath, modTime, modTime); err != nil {
		t.Fatalf("Chtimes returned error: %v", err)
	}

	third, err := root.ReadText("cached.txt", nil)
	if err != nil {
		t.Fatalf("third ReadText returned error: %v", err)
	}
	if third.Text != "beta" {
		t.Fatalf("expected cache invalidation after file change, got %#v", third)
	}

	stats = GetTextFileCacheStats()
	if stats.Entries != 1 || stats.Hits != 1 || stats.Misses != 2 || stats.Evictions != 0 {
		t.Fatalf("unexpected cache stats after invalidation: %#v", stats)
	}
}

func TestTextFileCacheStatsTrackEvictions(t *testing.T) {
	ResetTextFileCacheForTesting()
	dir := t.TempDir()
	root, err := NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	for idx := 0; idx <= maxTextFileCacheEntries; idx++ {
		name := filepath.Join(dir, "cache", fmt.Sprintf("file-%03d.txt", idx))
		mustWriteFile(t, name, "value")
		relative, err := filepath.Rel(dir, name)
		if err != nil {
			t.Fatalf("Rel returned error: %v", err)
		}
		if _, err := root.ReadText(relative, nil); err != nil {
			t.Fatalf("ReadText returned error: %v", err)
		}
	}

	stats := GetTextFileCacheStats()
	if stats.Entries != maxTextFileCacheEntries || stats.Evictions < 1 {
		t.Fatalf("expected cache eviction stats, got %#v", stats)
	}
}

func TestHelperPredicatesCoverRemainingBranches(t *testing.T) {
	if !isWithinRoot(`C:\repo`, `C:\repo\child.txt`) {
		t.Fatal("expected child path to remain within root")
	}
	if isWithinRoot(`C:\repo`, `C:\other\child.txt`) {
		t.Fatal("expected unrelated path to be outside root")
	}

	filter := normalizeExtensionFilter([]string{"ts", ".GO", ""})
	if !matchesExtension("app.ts", filter) {
		t.Fatal("expected normalized ts extension to match")
	}
	if !matchesExtension("main.go", filter) {
		t.Fatal("expected normalized go extension to match")
	}
	if matchesExtension("README.md", filter) {
		t.Fatal("did not expect md extension to match")
	}
	if !matchesExtension("any.file", nil) {
		t.Fatal("expected empty filter to match any extension")
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
