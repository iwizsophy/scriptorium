package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLoadRuntimeResolvesConfiguredPathsAndDefaults(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("SCRIPTORIUM_HOME", baseDir)
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", "markdown-docs")
	t.Setenv("SCRIPTORIUM_CODE_ROOTS", strings.Join([]string{"src", "sample roots"}, string(os.PathListSeparator))+"\nalt")
	t.Setenv("SCRIPTORIUM_SNAPSHOT_FILE", "artifacts/scriptorium-snapshot.sqlite")
	t.Setenv("SCRIPTORIUM_INDEX_FILE", "artifacts/scriptorium-index.sqlite")
	t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "mtime")
	t.Setenv("SCRIPTORIUM_CODE_EXTENSIONS", ".go,ts,.go")
	t.Setenv("SCRIPTORIUM_TEXT_ENCODING_FALLBACK", "shift_jis")
	t.Setenv("SCRIPTORIUM_ALLOW_SYMLINKS", "true")
	t.Setenv("SCRIPTORIUM_MAX_FILE_BYTES", "2048")
	t.Setenv("SCRIPTORIUM_SERVER_NAME", "custom-server")
	t.Setenv("SCRIPTORIUM_MCP_PROFILE", "azure")
	t.Setenv("SCRIPTORIUM_MCP_TOOL_PREFIX", "azure")
	t.Setenv("SCRIPTORIUM_MCP_DOMAIN_DESCRIPTION", "Azure architecture and IaC guidance")
	t.Setenv("SCRIPTORIUM_MCP_CORPUS_SUMMARY", "Azure docs, Bicep samples, and repo snapshots")
	t.Setenv("SCRIPTORIUM_MCP_EXAMPLE_QUERIES", "How do I deploy ACA?\nHow do I grant Key Vault access?")

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime returned error: %v", err)
	}

	if cfg.ServerName != "custom-server" || cfg.MCPBaseDir != baseDir {
		t.Fatalf("unexpected base runtime config: %#v", cfg)
	}
	if cfg.Profile.ID != "azure" || cfg.Profile.ToolPrefix != "azure" {
		t.Fatalf("unexpected MCP profile identity: %#v", cfg.Profile)
	}
	if cfg.Profile.DomainDescription != "Azure architecture and IaC guidance" || cfg.Profile.CorpusSummary != "Azure docs, Bicep samples, and repo snapshots" {
		t.Fatalf("unexpected MCP profile description: %#v", cfg.Profile)
	}
	if !slices.Equal(cfg.Profile.ExampleQueries, []string{"How do I deploy ACA?", "How do I grant Key Vault access?"}) {
		t.Fatalf("unexpected MCP profile example queries: %#v", cfg.Profile.ExampleQueries)
	}
	if cfg.DocsRoot != filepath.Join(baseDir, "markdown-docs") || cfg.DocsRootEnvVar != "SCRIPTORIUM_MARKDOWN_DIR" {
		t.Fatalf("unexpected docs root: %q", cfg.DocsRoot)
	}
	if !slices.Equal(cfg.SampleRoots, []string{
		filepath.Join(baseDir, "src"),
		filepath.Join(baseDir, "sample roots"),
		filepath.Join(baseDir, "alt"),
	}) {
		t.Fatalf("unexpected sample roots: %#v", cfg.SampleRoots)
	}
	if !slices.Equal(cfg.GitSnapshotPaths, []string{filepath.Join(baseDir, "artifacts", "scriptorium-snapshot.sqlite")}) {
		t.Fatalf("unexpected snapshot paths: %#v", cfg.GitSnapshotPaths)
	}
	if !slices.Equal(cfg.DocsIndexPaths, []string{filepath.Join(baseDir, "artifacts", "scriptorium-index.sqlite")}) {
		t.Fatalf("unexpected docs index paths: %#v", cfg.DocsIndexPaths)
	}
	if cfg.DocsIndexVerify != "mtime" || !cfg.AllowSymlinks || cfg.MaxFileBytes != 2048 {
		t.Fatalf("unexpected runtime flags: %#v", cfg)
	}
	if !slices.Equal(cfg.CodeExtensions, []string{".go", ".ts"}) {
		t.Fatalf("unexpected code extensions: %#v", cfg.CodeExtensions)
	}
	if runtime.GOOS == "windows" {
		if !slices.Contains(cfg.TextEncodingFallbacks, "shift_jis") {
			t.Fatalf("expected shift_jis fallback on windows, got %#v", cfg.TextEncodingFallbacks)
		}
	} else if !slices.Equal(cfg.TextEncodingFallbacks, []string{"shift_jis"}) {
		t.Fatalf("unexpected fallback encodings: %#v", cfg.TextEncodingFallbacks)
	}
}

func TestLoadRuntimeResolvesMultipleArtifactPaths(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("SCRIPTORIUM_HOME", baseDir)
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", "docs")
	t.Setenv(
		"SCRIPTORIUM_SNAPSHOT_FILE",
		strings.Join([]string{"artifacts/repo a.sqlite", "artifacts/repo b.sqlite"}, string(os.PathListSeparator)),
	)
	t.Setenv("SCRIPTORIUM_INDEX_FILE", "artifacts/docs a.sqlite\nartifacts/docs b.sqlite")

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime returned error: %v", err)
	}

	if !slices.Equal(cfg.GitSnapshotPaths, []string{
		filepath.Join(baseDir, "artifacts", "repo a.sqlite"),
		filepath.Join(baseDir, "artifacts", "repo b.sqlite"),
	}) {
		t.Fatalf("unexpected snapshot paths: %#v", cfg.GitSnapshotPaths)
	}
	if !slices.Equal(cfg.DocsIndexPaths, []string{
		filepath.Join(baseDir, "artifacts", "docs a.sqlite"),
		filepath.Join(baseDir, "artifacts", "docs b.sqlite"),
	}) {
		t.Fatalf("unexpected docs index paths: %#v", cfg.DocsIndexPaths)
	}
	if cfg.DocsRootEnvVar != "SCRIPTORIUM_MARKDOWN_DIR" {
		t.Fatalf("expected markdown dir env var source, got %q", cfg.DocsRootEnvVar)
	}
}

func TestLoadRuntimeRejectsInvalidRequiredInputs(t *testing.T) {
	t.Run("missing docs dir", func(t *testing.T) {
		t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "full")
		if _, err := LoadRuntime(); err == nil {
			t.Fatal("expected missing docs dir error")
		}
	})

	t.Run("invalid verify mode", func(t *testing.T) {
		t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", t.TempDir())
		t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "invalid")
		if _, err := LoadRuntime(); err == nil {
			t.Fatal("expected invalid verify mode error")
		}
	})

	t.Run("invalid max file bytes", func(t *testing.T) {
		t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", t.TempDir())
		t.Setenv("SCRIPTORIUM_MAX_FILE_BYTES", "0")
		if _, err := LoadRuntime(); err == nil {
			t.Fatal("expected invalid max file bytes error")
		}
	})

	t.Run("invalid mcp tool prefix", func(t *testing.T) {
		t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", t.TempDir())
		t.Setenv("SCRIPTORIUM_MCP_TOOL_PREFIX", "azure docs")
		if _, err := LoadRuntime(); err == nil {
			t.Fatal("expected invalid MCP tool prefix error")
		}
	})
}

func TestResolveOptionalAndEffectiveArtifactPaths(t *testing.T) {
	baseDir := t.TempDir()

	if got := resolveOptional(baseDir, ""); got != "" {
		t.Fatalf("expected empty optional path, got %q", got)
	}
	if got := resolveOptional(baseDir, "artifacts/index.sqlite"); got != filepath.Join(baseDir, "artifacts", "index.sqlite") {
		t.Fatalf("unexpected resolved optional path: %q", got)
	}

	cfg := Runtime{
		GitSnapshotPaths: []string{filepath.Join(baseDir, "snapshot.sqlite")},
		DocsIndexPaths:   []string{filepath.Join(baseDir, "index.sqlite")},
	}
	if !slices.Equal(cfg.EffectiveGitSnapshotPaths(), cfg.GitSnapshotPaths) {
		t.Fatalf("unexpected effective snapshot paths: %#v", cfg.EffectiveGitSnapshotPaths())
	}
	if !slices.Equal(cfg.EffectiveDocsIndexPaths(), cfg.DocsIndexPaths) {
		t.Fatalf("unexpected effective docs index paths: %#v", cfg.EffectiveDocsIndexPaths())
	}

	cfg.GitSnapshotPaths = []string{filepath.Join(baseDir, "a.sqlite"), filepath.Join(baseDir, "b.sqlite")}
	cfg.DocsIndexPaths = []string{filepath.Join(baseDir, "docs-a.sqlite"), filepath.Join(baseDir, "docs-b.sqlite")}
	effectiveSnapshots := cfg.EffectiveGitSnapshotPaths()
	effectiveDocs := cfg.EffectiveDocsIndexPaths()
	if !slices.Equal(effectiveSnapshots, cfg.GitSnapshotPaths) {
		t.Fatalf("unexpected effective snapshot paths: %#v", effectiveSnapshots)
	}
	if !slices.Equal(effectiveDocs, cfg.DocsIndexPaths) {
		t.Fatalf("unexpected effective docs index paths: %#v", effectiveDocs)
	}

	effectiveSnapshots[0] = "changed"
	effectiveDocs[0] = "changed"
	if cfg.GitSnapshotPaths[0] == "changed" || cfg.DocsIndexPaths[0] == "changed" {
		t.Fatal("effective artifact paths should return defensive copies")
	}

	if got := (Runtime{}).EffectiveGitSnapshotPaths(); got != nil {
		t.Fatalf("expected nil effective snapshot paths, got %#v", got)
	}
	if got := (Runtime{}).EffectiveDocsIndexPaths(); got != nil {
		t.Fatalf("expected nil effective docs index paths, got %#v", got)
	}
}

func TestResolveWithBase(t *testing.T) {
	baseDir := t.TempDir()
	absolute := filepath.Join(baseDir, "already-absolute")
	if got := resolveWithBase(baseDir, absolute); got != absolute {
		t.Fatalf("expected absolute path to remain absolute, got %q", got)
	}
	if got := resolveWithBase(baseDir, filepath.Join("nested", "value")); got != filepath.Join(baseDir, "nested", "value") {
		t.Fatalf("unexpected relative resolution: %q", got)
	}
}

func TestSplitPathListSupportsWindowsStyleSeparators(t *testing.T) {
	got := splitPathList("C:\\Docs Root;D:\\Repo Samples\nE:\\Alt", ';')
	want := []string{"C:\\Docs Root", "D:\\Repo Samples", "E:\\Alt"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected windows-style split result: %#v", got)
	}
}

func TestSplitPathListSupportsPOSIXStyleSeparators(t *testing.T) {
	got := splitPathList("/tmp/docs root:/tmp/repo samples\n/tmp/alt", ':')
	want := []string{"/tmp/docs root", "/tmp/repo samples", "/tmp/alt"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected posix-style split result: %#v", got)
	}
}

func TestSplitTokenListRemainsPermissive(t *testing.T) {
	got := splitTokenList(" ts ; .TS , go\n md\t")
	want := []string{"ts", ".TS", "go", "md"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected token split result: %#v", got)
	}
}

func TestConfigNormalizationHelperBranches(t *testing.T) {
	if normalizeServerName(" \t ", "") != "scriptorium" {
		t.Fatalf("expected blank server name to fall back to default")
	}
	if normalizeServerName(" \t ", "azure") != "scriptorium-azure" {
		t.Fatalf("expected blank server name to fall back to profile-derived name")
	}
	if normalizeServerName(" custom ", "azure") != "custom" {
		t.Fatalf("expected server name trimming")
	}
	if normalizeVerifyMode(" off ") != "off" {
		t.Fatalf("expected off verify mode normalization")
	}
	if normalizeVerifyMode(" FULL ") != "full" {
		t.Fatalf("expected full verify mode normalization")
	}
	if normalizeVerifyMode("invalid") != "" {
		t.Fatalf("expected invalid verify mode to normalize to empty string")
	}

	if got := normalizeExtensions(".TS,go,.ts"); !slices.Equal(got, []string{".ts", ".go"}) {
		t.Fatalf("unexpected normalized extensions: %#v", got)
	}
	if got := normalizeExtensions(" , ; \n\t "); !slices.Equal(got, []string{".cs", ".ts"}) {
		t.Fatalf("expected empty token list to fall back to defaults, got %#v", got)
	}

	if got := splitPathList(" ; \n C:\\Docs ", ';'); !slices.Equal(got, []string{"C:\\Docs"}) {
		t.Fatalf("unexpected splitPathList filtering result: %#v", got)
	}
	if got := splitTokenList(" , ; \n\t "); len(got) != 0 {
		t.Fatalf("expected splitTokenList to drop empty fields, got %#v", got)
	}
	if got := splitLineList("one\n \r\ntwo\r\n"); !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("unexpected splitLineList result: %#v", got)
	}
	if !parseBoolEnv("YES") || !parseBoolEnv(" on ") || parseBoolEnv("false") {
		t.Fatalf("unexpected parseBoolEnv behavior")
	}
	if got := normalizeDomainDescription(" "); got != "the configured documentation and code corpus" {
		t.Fatalf("unexpected default domain description: %q", got)
	}
	if got := normalizeDomainDescription(" Azure docs "); got != "Azure docs" {
		t.Fatalf("unexpected trimmed domain description: %q", got)
	}
	if got, err := normalizeToolPrefix(""); err != nil || got != "" {
		t.Fatalf("expected empty tool prefix to stay empty, got value=%q err=%v", got, err)
	}
	if got, err := normalizeToolPrefix("Azure_Prod"); err != nil || got != "azure_prod" {
		t.Fatalf("expected tool prefix normalization, got value=%q err=%v", got, err)
	}
	if _, err := normalizeToolPrefix("azure docs"); err == nil {
		t.Fatal("expected invalid tool prefix to fail validation")
	}

	if got, err := parseMaxFileBytes(""); err != nil || got != 1_000_000 {
		t.Fatalf("expected default max file bytes, got value=%d err=%v", got, err)
	}
	if _, err := parseMaxFileBytes("not-a-number"); err == nil {
		t.Fatalf("expected parseMaxFileBytes to reject invalid syntax")
	}

	pathValue := strings.Join([]string{"alpha", "beta"}, string(os.PathListSeparator))
	if got := splitPathListEnv(pathValue); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected splitPathListEnv result: %#v", got)
	}

	if got := resolveAll(t.TempDir(), nil); len(got) != 0 {
		t.Fatalf("expected resolveAll(nil) to stay empty, got %#v", got)
	}
}

func TestResolveWithBaseSwallowsAbsErrors(t *testing.T) {
	previousAbsPath := absPath
	t.Cleanup(func() {
		absPath = previousAbsPath
	})
	absPath = func(string) (string, error) { return "", errors.New("abs failed") }

	if got := resolveWithBase("base", "relative/path"); got != "" {
		t.Fatalf("expected empty path when absPath fails for relative value, got %q", got)
	}
	if got := resolveWithBase("base", filepath.Join("C:", "absolute")); got != "" {
		t.Fatalf("expected empty path when absPath fails for absolute value, got %q", got)
	}
}

func TestLoadRuntimePropagatesGetwdAndAbsErrors(t *testing.T) {
	previousGetwd := getwd
	previousAbsPath := absPath
	t.Cleanup(func() {
		getwd = previousGetwd
		absPath = previousAbsPath
	})

	t.Run("getwd error", func(t *testing.T) {
		getwd = func() (string, error) { return "", errors.New("getwd failed") }
		absPath = previousAbsPath
		t.Setenv("SCRIPTORIUM_HOME", "")
		t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", "docs")

		if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), "getwd failed") {
			t.Fatalf("expected getwd failure, got %v", err)
		}
	})

	t.Run("abs error", func(t *testing.T) {
		getwd = previousGetwd
		absPath = func(string) (string, error) { return "", errors.New("abs failed") }
		t.Setenv("SCRIPTORIUM_HOME", "relative-home")
		t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", "docs")

		if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), "abs failed") {
			t.Fatalf("expected abs failure, got %v", err)
		}
	})
}
