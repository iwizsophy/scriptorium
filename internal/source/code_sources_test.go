package source

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/gitsnapshot"
)

func TestBuildCodeSourcesUsesRuntimeCacheStats(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	sampleRoot := t.TempDir()
	mustWriteSourceFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "export const value = 1\n")

	cfg := config.Runtime{
		SampleRoots:  []string{sampleRoot},
		MaxFileBytes: 1_000_000,
	}

	firstSources, firstMultiple, err := BuildCodeSources(cfg)
	if err != nil {
		t.Fatalf("first BuildCodeSources returned error: %v", err)
	}
	secondSources, secondMultiple, err := BuildCodeSources(cfg)
	if err != nil {
		t.Fatalf("second BuildCodeSources returned error: %v", err)
	}
	if len(firstSources) != 1 || len(secondSources) != 1 {
		t.Fatalf("unexpected sources: first=%#v second=%#v", firstSources, secondSources)
	}
	if firstMultiple || secondMultiple {
		t.Fatalf("expected single-root configuration, got first=%v second=%v", firstMultiple, secondMultiple)
	}

	stats := GetCodeSourceCacheStats()
	if stats.Entries != 1 || stats.Hits != 1 || stats.Misses != 1 || stats.MaxEntries != 128 || stats.Evictions != 0 || stats.FilesystemRoots != 1 || stats.SnapshotRoots != 0 {
		t.Fatalf("unexpected code source cache stats: %#v", stats)
	}
}

func TestBuildCodeSourcesStatsIncludeSnapshotRootsAndEvictions(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	mustWriteSourceFile(t, filepath.Join(repo, "src", "app.ts"), "export const value = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := gitsnapshot.Write(snapshotPath, snapshot); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	for idx := 0; idx <= maxCodeSourceCacheEntries; idx++ {
		sampleRoot := filepath.Join(t.TempDir(), fmt.Sprintf("sample-root-%03d", idx))
		mustMkdirAll(t, sampleRoot)
		cfg := config.Runtime{
			SampleRoots:      []string{sampleRoot},
			GitSnapshotPaths: []string{snapshotPath},
			MaxFileBytes:     1_000_000,
		}
		if _, _, err := BuildCodeSources(cfg); err != nil {
			t.Fatalf("BuildCodeSources returned error: %v", err)
		}
	}

	stats := GetCodeSourceCacheStats()
	if stats.Entries != maxCodeSourceCacheEntries || stats.Evictions < 1 || stats.SnapshotRoots < 1 {
		t.Fatalf("expected snapshot-aware cache stats, got %#v", stats)
	}
}

func TestBuildCodeSourcesIgnoresInvalidSnapshotSetAndRejectsDuplicateRootIDs(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	repoA := t.TempDir()
	git(t, repoA, "init")
	git(t, repoA, "config", "user.email", "test@example.com")
	git(t, repoA, "config", "user.name", "Test User")
	mustWriteSourceFile(t, filepath.Join(repoA, "src", "a.ts"), "export const a = 1\n")
	git(t, repoA, "add", ".")
	git(t, repoA, "commit", "-m", "initial")

	repoB := t.TempDir()
	git(t, repoB, "init")
	git(t, repoB, "config", "user.email", "test@example.com")
	git(t, repoB, "config", "user.name", "Test User")
	mustWriteSourceFile(t, filepath.Join(repoB, "src", "b.ts"), "export const b = 1\n")
	git(t, repoB, "add", ".")
	git(t, repoB, "commit", "-m", "initial")

	snapshotA, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repoA,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	snapshotAPath := filepath.Join(t.TempDir(), "snapshot-a.sqlite")
	if err := gitsnapshot.Write(snapshotAPath, snapshotA); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	snapshotB, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repoB,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	snapshotBPath := filepath.Join(t.TempDir(), "snapshot-b.sqlite")
	if err := gitsnapshot.Write(snapshotBPath, snapshotB); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cfg := config.Runtime{
		GitSnapshotPaths: []string{snapshotAPath, snapshotBPath},
		MaxFileBytes:     1_000_000,
	}

	sources, multipleRoots, err := BuildCodeSources(cfg)
	if err != nil {
		t.Fatalf("BuildCodeSources returned error: %v", err)
	}
	if len(sources) != 0 || multipleRoots {
		t.Fatalf("expected duplicate snapshot root ids to disable snapshot sources, got sources=%#v multiple=%v", sources, multipleRoots)
	}
}

func TestSnapshotBackedCodeSourceMethodsAndResolveLogicalPath(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	mustWriteSourceFile(t, filepath.Join(repo, "src", "app.ts"), "export function run() {\n  return specialToken\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/runtime")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/runtime",
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
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}
	sources, multipleRoots, err := BuildCodeSources(cfg)
	if err != nil {
		t.Fatalf("BuildCodeSources returned error: %v", err)
	}
	if len(sources) != 1 || multipleRoots {
		t.Fatalf("expected single snapshot source with rooted logical paths, got sources=%#v multiple=%v", sources, multipleRoots)
	}

	results, ok, err := sources[0].Search("specialToken", []string{".ts"}, 10, 3)
	if err != nil || !ok || len(results) == 0 {
		t.Fatalf("expected snapshot search hit, got ok=%v err=%v results=%#v", ok, err, results)
	}

	files, err := sources[0].ListFiles([]string{".ts"})
	if err != nil || len(files) != 1 || files[0] != "src/app.ts" {
		t.Fatalf("expected snapshot file listing, got files=%#v err=%v", files, err)
	}

	textFile, err := sources[0].ReadText("src/app.ts", nil)
	if err != nil || !strings.Contains(textFile.Text, "specialToken") {
		t.Fatalf("expected snapshot text read, got %#v err=%v", textFile, err)
	}

	logicalPath := sources[0].BuildLogicalPath("src/app.ts", multipleRoots)
	if logicalPath != "@feature-runtime/src/app.ts" {
		t.Fatalf("unexpected logical path: %q", logicalPath)
	}

	resolvedSource, relativePath, resolvedMultiple, err := ResolveLogicalPath(cfg, logicalPath)
	if err != nil || relativePath != "src/app.ts" || !resolvedMultiple || resolvedSource.ID != sources[0].ID {
		t.Fatalf("expected snapshot logical path resolution, got source=%#v path=%q multiple=%v err=%v", resolvedSource, relativePath, resolvedMultiple, err)
	}
	if _, _, _, err := ResolveLogicalPath(cfg, "src/app.ts"); err == nil {
		t.Fatal("expected single snapshot source to require rooted logical paths")
	}
	if _, err := sources[0].ReadText("missing.ts", nil); err == nil {
		t.Fatal("expected snapshot ReadText to fail for missing entry")
	}
}

func TestResolveLogicalPathHandlesSingleRootMultiRootAndErrors(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	singleRoot := t.TempDir()
	mustWriteSourceFile(t, filepath.Join(singleRoot, "src", "app.ts"), "export const value = 1\n")

	cfg := config.Runtime{
		SampleRoots:  []string{singleRoot},
		MaxFileBytes: 1_000_000,
	}
	_, relativePath, multipleRoots, err := ResolveLogicalPath(cfg, "src/app.ts")
	if err != nil || relativePath != "src/app.ts" || multipleRoots {
		t.Fatalf("expected single-root resolution, got path=%q multiple=%v err=%v", relativePath, multipleRoots, err)
	}

	secondRoot := t.TempDir()
	mustWriteSourceFile(t, filepath.Join(secondRoot, "src", "other.ts"), "export const other = 1\n")
	multiCfg := config.Runtime{
		SampleRoots:  []string{singleRoot, secondRoot},
		MaxFileBytes: 1_000_000,
	}
	if _, _, _, err := ResolveLogicalPath(multiCfg, "src/app.ts"); err == nil {
		t.Fatal("expected missing root-id error for multi-root resolution")
	}
	if _, _, _, err := ResolveLogicalPath(multiCfg, "@missing/src/app.ts"); err == nil {
		t.Fatal("expected unknown root-id error")
	}
	if _, _, _, err := ResolveLogicalPath(multiCfg, "@"+NormalizeRootID(singleRoot)); err == nil {
		t.Fatal("expected malformed logical path error")
	}

	resolvedSource, relativePath, multipleRoots, err := ResolveLogicalPath(multiCfg, "@"+NormalizeRootID(secondRoot)+"/src/other.ts")
	if err != nil || !multipleRoots || relativePath != "src/other.ts" || resolvedSource.ID != NormalizeRootID(secondRoot) {
		t.Fatalf("expected multi-root resolution success, got source=%#v path=%q multiple=%v err=%v", resolvedSource, relativePath, multipleRoots, err)
	}
}

func TestFilesystemBackedCodeSourceMethods(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	sampleRoot := t.TempDir()
	mustWriteSourceFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "export const value = 1\n")

	cfg := config.Runtime{
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
		AllowSymlinks:  false,
	}
	sources, multipleRoots, err := BuildCodeSources(cfg)
	if err != nil {
		t.Fatalf("BuildCodeSources returned error: %v", err)
	}
	if len(sources) != 1 || multipleRoots {
		t.Fatalf("expected single filesystem source, got sources=%#v multiple=%v", sources, multipleRoots)
	}
	if results, ok, err := sources[0].Search("value", []string{".ts"}, 10, 3); ok || err != nil || results != nil {
		t.Fatalf("expected filesystem Search to return ok=false without error, got results=%#v ok=%v err=%v", results, ok, err)
	}

	files, err := sources[0].ListFiles([]string{".ts"})
	if err != nil || len(files) != 1 || files[0] != "src/app.ts" {
		t.Fatalf("expected filesystem file listing, got files=%#v err=%v", files, err)
	}

	textFile, err := sources[0].ReadText("src/app.ts", nil)
	if err != nil || !strings.Contains(textFile.Text, "value") || textFile.Path != "src/app.ts" {
		t.Fatalf("expected filesystem text read, got %#v err=%v", textFile, err)
	}
	if _, err := sources[0].ReadText("missing.ts", nil); err == nil {
		t.Fatal("expected filesystem ReadText to fail for missing file")
	}
}

func TestCodeSourceAndCacheHelpers(t *testing.T) {
	var empty CodeSource
	if _, ok, err := empty.Search("q", []string{".ts"}, 5, 3); ok || err != nil {
		t.Fatalf("expected unconfigured search to return ok=false without error, got ok=%v err=%v", ok, err)
	}
	if _, err := empty.ListFiles([]string{".ts"}); err == nil {
		t.Fatal("expected ListFiles error for unconfigured source")
	}
	if _, err := empty.ReadText("src/app.ts", nil); err == nil {
		t.Fatal("expected ReadText error for unconfigured source")
	}

	fsSource := CodeSource{ID: "root", Kind: "fs"}
	if fsSource.BuildLogicalPath("src\\app.ts", false) != "src/app.ts" {
		t.Fatalf("expected single-root filesystem logical path")
	}
	if fsSource.BuildLogicalPath("src\\app.ts", true) != "@root/src/app.ts" {
		t.Fatalf("expected multi-root filesystem logical path")
	}
	if BuildLogicalPath("root", "src\\app.ts", false) != "src/app.ts" || BuildLogicalPath("root", "src\\app.ts", true) != "@root/src/app.ts" {
		t.Fatalf("unexpected package BuildLogicalPath behavior")
	}

	if NormalizeRootID(" C:/Repo/My Samples ") != "c-repo-my-samples" || NormalizeRootID("///") != "samples" {
		t.Fatalf("unexpected NormalizeRootID behavior")
	}

	ResetCodeSourceCacheForTesting()
	keyA := buildCodeSourceCacheKey(config.Runtime{SampleRoots: []string{"a"}, MaxFileBytes: 10})
	keyB := buildCodeSourceCacheKey(config.Runtime{SampleRoots: []string{"a"}, MaxFileBytes: 11})
	if keyA == keyB {
		t.Fatalf("expected cache key to depend on max file bytes")
	}

	entry := codeSourceCacheEntry{
		sources:       []CodeSource{{ID: "a", Kind: "fs"}},
		multipleRoots: true,
	}
	storeCodeSourceCache("k", entry)
	loaded, ok := lookupCodeSourceCache("k")
	if !ok || !loaded.multipleRoots || len(loaded.sources) != 1 || loaded.sources[0].ID != "a" {
		t.Fatalf("unexpected cache lookup result: %#v ok=%v", loaded, ok)
	}
	loaded.sources[0].ID = "changed"
	again, ok := lookupCodeSourceCache("k")
	if !ok || again.sources[0].ID != "a" {
		t.Fatalf("expected cached sources copy isolation, got %#v ok=%v", again, ok)
	}
	if _, ok := lookupCodeSourceCache("missing"); ok {
		t.Fatal("expected cache miss")
	}
}

func TestResolveLogicalPathWithoutSourcesAndSnapshotRootError(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	if _, _, _, err := ResolveLogicalPath(config.Runtime{}, "src/app.ts"); err == nil {
		t.Fatal("expected error when no code sources are configured")
	}

	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	mustWriteSourceFile(t, filepath.Join(repo, "src", "app.ts"), "export const value = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
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
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}
	sources, _, err := BuildCodeSources(cfg)
	if err != nil {
		t.Fatalf("BuildCodeSources returned error: %v", err)
	}
	snapshotSource := sources[0]
	snapshotSource.snapshotRef = "missing"
	if _, ok, err := snapshotSource.Search("value", []string{".ts"}, 10, 3); !ok || err == nil {
		t.Fatalf("expected snapshot root lookup failure, got ok=%v err=%v", ok, err)
	}

	stats := GetCodeSourceCacheStats()
	if stats.MaxEntries != maxCodeSourceCacheEntries || stats.Entries == 0 {
		t.Fatalf("unexpected cache stats after snapshot build: %#v", stats)
	}
}

func TestBuildCodeSourcesRejectsInvalidSampleRoot(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	cfg := config.Runtime{
		SampleRoots:  []string{filepath.Join(t.TempDir(), "missing")},
		MaxFileBytes: 1_000_000,
	}
	if _, _, err := BuildCodeSources(cfg); err == nil {
		t.Fatal("expected BuildCodeSources to reject invalid sample root")
	}
}

func TestBuildCodeSourcesDisambiguatesDuplicateSampleRootIDs(t *testing.T) {
	ResetCodeSourceCacheForTesting()
	sampleRoot := t.TempDir()
	mustWriteSourceFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "export const value = 1\n")

	cfg := config.Runtime{
		SampleRoots:  []string{sampleRoot, sampleRoot},
		MaxFileBytes: 1_000_000,
	}
	sources, multipleRoots, err := BuildCodeSources(cfg)
	if err != nil {
		t.Fatalf("BuildCodeSources returned error: %v", err)
	}
	if !multipleRoots || len(sources) != 2 {
		t.Fatalf("expected duplicate sample roots to produce two sources, got sources=%#v multiple=%v", sources, multipleRoots)
	}
	if sources[0].ID != NormalizeRootID(sampleRoot) || sources[1].ID != NormalizeRootID(sampleRoot)+"-2" {
		t.Fatalf("expected duplicate source ids to be disambiguated, got %#v", sources)
	}
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func mustWriteSourceFile(t *testing.T, path string, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
}
