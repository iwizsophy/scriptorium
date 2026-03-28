package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/docsindex"
	"github.com/iwizsophy/scriptorium/internal/gitsnapshot"
)

func TestDocsCoverageUsesIndexAndFilesystemFallback(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteDiagFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	artifact, err := docsindex.Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	files, blocks := docsCoverage(config.Runtime{}, docsindex.RuntimeSet{
		Enabled: true,
		Indexes: []docsindex.Artifact{artifact},
	})
	if files != 1 || blocks != 1 {
		t.Fatalf("unexpected index coverage counts: files=%d blocks=%d", files, blocks)
	}

	cfg := config.Runtime{
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}
	files, blocks = docsCoverage(cfg, docsindex.RuntimeSet{})
	if files != 1 || blocks != 0 {
		t.Fatalf("unexpected filesystem fallback counts: files=%d blocks=%d", files, blocks)
	}

	files, blocks = docsCoverage(config.Runtime{DocsRoot: filepath.Join(docsRoot, "missing")}, docsindex.RuntimeSet{})
	if files != 0 || blocks != 0 {
		t.Fatalf("expected zero counts for invalid docs root, got files=%d blocks=%d", files, blocks)
	}

	brokenDocsRoot := t.TempDir()
	if err := os.Symlink(filepath.Join(brokenDocsRoot, "missing-target"), filepath.Join(brokenDocsRoot, "broken")); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}
	files, blocks = docsCoverage(config.Runtime{
		DocsRoot:       brokenDocsRoot,
		AllowSymlinks:  true,
		MaxFileBytes:   1_000_000,
		CodeExtensions: []string{".md"},
	}, docsindex.RuntimeSet{})
	if files != 0 || blocks != 0 {
		t.Fatalf("expected zero counts when docs listing fails, got files=%d blocks=%d", files, blocks)
	}
}

func TestSampleCoverageCountsFilesystemAndSnapshotSources(t *testing.T) {
	sampleRoot := t.TempDir()
	mustWriteDiagFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "export function run() { return 1 }\n")
	mustWriteDiagFile(t, filepath.Join(sampleRoot, "src", "ignore.md"), "# not code\n")

	repo := initDiagGitRepo(t)
	mustWriteDiagFile(t, filepath.Join(repo, "src", "branch.ts"), "export function branchOnly() { return 1 }\n")
	gitDiag(t, repo, "add", ".")
	gitDiag(t, repo, "commit", "-m", "initial")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build snapshot returned error: %v", err)
	}

	total := sampleCoverage(config.Runtime{
		SampleRoots:    []string{sampleRoot, filepath.Join(sampleRoot, "missing")},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}, gitsnapshot.RuntimeSet{
		Enabled: true,
		Indexes: []gitsnapshot.Artifact{snapshot},
	})
	if total != 2 {
		t.Fatalf("expected one filesystem file plus one snapshot file, got %d", total)
	}

	brokenSampleRoot := t.TempDir()
	if err := os.Symlink(filepath.Join(brokenSampleRoot, "missing-target"), filepath.Join(brokenSampleRoot, "broken")); err != nil {
		t.Skipf("os.Symlink is unavailable in this environment: %v", err)
	}
	total = sampleCoverage(config.Runtime{
		SampleRoots:    []string{brokenSampleRoot},
		CodeExtensions: []string{".ts"},
		AllowSymlinks:  true,
		MaxFileBytes:   1_000_000,
	}, gitsnapshot.RuntimeSet{})
	if total != 0 {
		t.Fatalf("expected broken sample listing to be ignored, got %d", total)
	}
}

func TestRetrievalWarningsAndMustGetwd(t *testing.T) {
	cfg := config.Runtime{SampleRoots: []string{"samples"}}
	warnings := retrievalWarnings(cfg, "docs warning", "snapshot warning", 0, 0, 0)
	expected := []string{
		"No markdown docs are currently available for retrieval.",
		"No sample code is currently available for retrieval.",
		"Docs retrieval is running with a fallback or warning state: docs warning",
		"Snapshot retrieval is running with a fallback or warning state: snapshot warning",
		"Only filesystem-backed sample roots are available; snapshot-backed samples are not configured.",
		"Implementation guidance quality is likely limited because docs and sample code are not both available.",
	}
	if !slices.Equal(warnings, expected) {
		t.Fatalf("unexpected retrieval warnings: got=%#v want=%#v", warnings, expected)
	}

	if wd := mustGetwd(); wd == "" {
		t.Fatal("expected mustGetwd to return current working directory")
	}
}

func TestBuildDiagnosticsPayloadCapturesArtifactMetadataAndFallback(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteDiagFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	indexArtifact, err := docsindex.Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build docs index returned error: %v", err)
	}
	indexPath := filepath.Join(t.TempDir(), "docs.sqlite")
	if err := docsindex.Write(indexPath, indexArtifact); err != nil {
		t.Fatalf("Write docs index returned error: %v", err)
	}

	repo := initDiagGitRepo(t)
	mustWriteDiagFile(t, filepath.Join(repo, "src", "app.ts"), "export function run() { return 1 }\n")
	gitDiag(t, repo, "add", ".")
	gitDiag(t, repo, "commit", "-m", "initial")
	snapshotArtifact, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build snapshot returned error: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := gitsnapshot.Write(snapshotPath, snapshotArtifact); err != nil {
		t.Fatalf("Write snapshot returned error: %v", err)
	}

	payload := buildDiagnosticsPayload(config.Runtime{
		ServerName: "scriptorium-azure",
		Profile: config.MCPProfile{
			ID:                "azure",
			ToolPrefix:        "azure",
			DomainDescription: "Azure architecture guidance",
			CorpusSummary:     "Azure docs and Bicep samples",
			ExampleQueries:    []string{"How do I deploy ACA?"},
		},
		DocsRoot:         docsRoot,
		DocsIndexPaths:   []string{indexPath},
		DocsIndexVerify:  "off",
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	})
	if !payload.Docs.Index.Enabled || payload.Docs.Index.Path != indexPath {
		t.Fatalf("expected docs index metadata, got %#v", payload.Docs.Index)
	}
	if !payload.Code.GitSnapshot.Enabled || payload.Code.GitSnapshot.Path != snapshotPath {
		t.Fatalf("expected snapshot metadata, got %#v", payload.Code.GitSnapshot)
	}
	if payload.Profile.ID != "azure" || payload.Profile.ToolPrefix != "azure" || payload.Profile.DomainDescription != "Azure architecture guidance" {
		t.Fatalf("expected profile metadata, got %#v", payload.Profile)
	}
	if len(payload.Code.SampleSources) != 1 || payload.Code.SampleSources[0].Kind != "git_snapshot" {
		t.Fatalf("expected snapshot sample source, got %#v", payload.Code.SampleSources)
	}
	if !payload.Retrieval.Corpus.DocsAvailable || !payload.Retrieval.Corpus.SampleCodeAvailable || !payload.Retrieval.Corpus.CrossSourceRelations {
		t.Fatalf("expected docs and sample code to be available, got %#v", payload.Retrieval.Corpus)
	}
	if payload.Retrieval.Fallback.DocsIndexToFilesystem || payload.Retrieval.Fallback.SnippetOnlyLikely || len(payload.Retrieval.Warnings) != 0 {
		t.Fatalf("expected no fallback warnings with healthy artifacts, got fallback=%#v warnings=%#v", payload.Retrieval.Fallback, payload.Retrieval.Warnings)
	}

	fallbackPayload := buildDiagnosticsPayload(config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	})
	if !fallbackPayload.Docs.DocsOnlyMode || fallbackPayload.Code.Enabled {
		t.Fatalf("expected docs-only fallback payload, got docs=%#v code=%#v", fallbackPayload.Docs, fallbackPayload.Code)
	}
	if !fallbackPayload.Retrieval.Fallback.DocsIndexToFilesystem || !fallbackPayload.Retrieval.Fallback.SnippetOnlyLikely {
		t.Fatalf("expected docs filesystem fallback with snippet-only risk, got %#v", fallbackPayload.Retrieval.Fallback)
	}
	if len(fallbackPayload.Retrieval.Warnings) == 0 {
		t.Fatal("expected fallback warnings when sample code is unavailable")
	}
}

func TestLogStartupPrintsEnabledArtifactsAndDocsOnlyMode(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteDiagFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	indexArtifact, err := docsindex.Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build docs index returned error: %v", err)
	}
	indexPath := filepath.Join(t.TempDir(), "docs.sqlite")
	if err := docsindex.Write(indexPath, indexArtifact); err != nil {
		t.Fatalf("Write docs index returned error: %v", err)
	}

	repo := initDiagGitRepo(t)
	mustWriteDiagFile(t, filepath.Join(repo, "src", "app.ts"), "export function run() { return 1 }\n")
	gitDiag(t, repo, "add", ".")
	gitDiag(t, repo, "commit", "-m", "initial")
	snapshotArtifact, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build snapshot returned error: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := gitsnapshot.Write(snapshotPath, snapshotArtifact); err != nil {
		t.Fatalf("Write snapshot returned error: %v", err)
	}

	var stderr bytes.Buffer
	logStartup(config.Runtime{
		ServerName:       "scriptorium-test",
		Profile:          config.MCPProfile{ID: "azure", ToolPrefix: "azure", CorpusSummary: "Azure docs and Bicep samples"},
		DocsRoot:         docsRoot,
		DocsRootEnvVar:   "SCRIPTORIUM_MARKDOWN_DIR",
		DocsIndexPaths:   []string{indexPath},
		DocsIndexVerify:  "off",
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}, &stderr)
	logged := stderr.String()
	for _, needle := range []string{
		"SCRIPTORIUM_MARKDOWN_DIR=" + docsRoot,
		"SCRIPTORIUM_MCP_PROFILE=azure",
		"SCRIPTORIUM_MCP_TOOL_PREFIX=azure",
		"SCRIPTORIUM_MCP_CORPUS_SUMMARY=Azure docs and Bicep samples",
		"SCRIPTORIUM_SNAPSHOT_FILE[0]=" + snapshotPath,
		"DOCS_INDEX[0]=" + indexPath,
		"GIT_SNAPSHOT[0]=" + snapshotPath,
		"SCRIPTORIUM_INDEX_VERIFY=off",
		"SCRIPTORIUM_CODE_EXTENSIONS=.ts",
		"MCP server ready",
	} {
		if !strings.Contains(logged, needle) {
			t.Fatalf("expected startup log to contain %q, got %q", needle, logged)
		}
	}

	stderr.Reset()
	logStartup(config.Runtime{
		ServerName:      "scriptorium-test",
		DocsRoot:        docsRoot,
		DocsRootEnvVar:  "SCRIPTORIUM_MARKDOWN_DIR",
		DocsIndexVerify: "off",
		CodeExtensions:  []string{".ts"},
		MaxFileBytes:    1_000_000,
	}, &stderr)
	logged = stderr.String()
	if !strings.Contains(logged, "SCRIPTORIUM_MARKDOWN_DIR="+docsRoot) {
		t.Fatalf("expected canonical markdown dir log line, got %q", logged)
	}
	if !strings.Contains(logged, "SAMPLE_SOURCE=(none) docs-only mode") {
		t.Fatalf("expected docs-only startup log, got %q", logged)
	}
	if strings.Contains(logged, "GIT_SNAPSHOT[0]=") {
		t.Fatalf("did not expect enabled snapshot log in docs-only mode, got %q", logged)
	}
}

func TestLogStartupPrintsDisabledAndWarningBranches(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteDiagFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	var stderr bytes.Buffer
	logStartup(config.Runtime{
		ServerName:      "scriptorium-test",
		DocsRoot:        docsRoot,
		DocsIndexVerify: "off",
		DocsIndexPaths:  []string{filepath.Join(t.TempDir(), "missing-docs.sqlite")},
		CodeExtensions:  []string{".ts"},
		MaxFileBytes:    1_000_000,
	}, &stderr)
	logged := stderr.String()
	if !strings.Contains(logged, "DOCS_INDEX=disabled warning=") {
		t.Fatalf("expected docs warning branch, got %q", logged)
	}
	if strings.Contains(logged, "GIT_SNAPSHOT=disabled") {
		t.Fatalf("did not expect snapshot warning/disabled line without configured snapshot paths, got %q", logged)
	}

	stderr.Reset()
	logStartup(config.Runtime{
		ServerName:       "scriptorium-test",
		DocsRoot:         docsRoot,
		DocsIndexVerify:  "off",
		GitSnapshotPaths: []string{filepath.Join(t.TempDir(), "missing-snapshot.sqlite")},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}, &stderr)
	logged = stderr.String()
	if !strings.Contains(logged, "DOCS_INDEX=disabled warning=") {
		t.Fatalf("expected docs warning branch, got %q", logged)
	}
	if !strings.Contains(logged, "GIT_SNAPSHOT=disabled warning=") {
		t.Fatalf("expected snapshot warning branch, got %q", logged)
	}
}

func mustWriteDiagFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func initDiagGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitDiag(t, repo, "init")
	gitDiag(t, repo, "config", "user.email", "test@example.com")
	gitDiag(t, repo, "config", "user.name", "Test User")
	return repo
}

func gitDiag(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(output))
	}
}
