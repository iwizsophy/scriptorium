package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBuildDocsIndexWritesArtifact(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "scriptorium-index.sqlite")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunBuildDocsIndex(docsRoot, outPath, false, &stdout, &stderr); err != nil {
		t.Fatalf("RunBuildDocsIndex returned error: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected artifact to exist: %v", err)
	}
	if stdout.Len() == 0 || stderr.Len() == 0 {
		t.Fatalf("expected builder output, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunBuildGitSnapshotWritesArtifact(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "app.ts"), []byte("export function run() {\n  return 1\n}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunBuildGitSnapshot(BuildGitSnapshotOptions{
		RepoPath:       repo,
		OutPath:        outPath,
		Samples:        "HEAD",
		CodeExtensions: ".ts",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("RunBuildGitSnapshot returned error: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected artifact to exist: %v", err)
	}
}

func TestServerHelperBranches(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := RunBuildDocsIndex("", "out.sqlite", false, &stdout, &stderr); err == nil {
		t.Fatal("expected docs-root validation error")
	}
	if err := RunBuildDocsIndex("docs", "", false, &stdout, &stderr); err == nil {
		t.Fatal("expected out validation error")
	}
	if err := RunBuildDocsIndex(filepath.Join(t.TempDir(), "missing"), "out.sqlite", false, &stdout, &stderr); err == nil {
		t.Fatal("expected docs index build failure for missing docs root")
	}
	if err := RunBuildGitSnapshot(BuildGitSnapshotOptions{}, &stdout, &stderr); err == nil {
		t.Fatal("expected repo validation error")
	}
	if err := RunBuildGitSnapshot(BuildGitSnapshotOptions{RepoPath: "repo"}, &stdout, &stderr); err == nil {
		t.Fatal("expected out validation error")
	}
	if err := RunBuildGitSnapshot(BuildGitSnapshotOptions{RepoPath: "repo", OutPath: "out"}, &stdout, &stderr); err == nil {
		t.Fatal("expected samples validation error")
	}
	if err := RunBuildGitSnapshot(BuildGitSnapshotOptions{
		RepoPath: filepath.Join(t.TempDir(), "missing"),
		OutPath:  filepath.Join(t.TempDir(), "snapshot.sqlite"),
		Samples:  "HEAD",
	}, &stdout, &stderr); err == nil {
		t.Fatal("expected git snapshot build failure for missing repo")
	}

	docsRoot := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	blockingDocsOut := filepath.Join(t.TempDir(), "occupied-docs")
	if err := os.MkdirAll(filepath.Join(blockingDocsOut, "child"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := RunBuildDocsIndex(docsRoot, blockingDocsOut, false, &stdout, &stderr); err == nil {
		t.Fatal("expected docs index write failure for occupied output path")
	}

	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "app.ts"), []byte("export function run() {\n  return 1\n}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	blockingSnapshotOut := filepath.Join(t.TempDir(), "occupied-snapshot")
	if err := os.MkdirAll(filepath.Join(blockingSnapshotOut, "child"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := RunBuildGitSnapshot(BuildGitSnapshotOptions{
		RepoPath:       repo,
		OutPath:        blockingSnapshotOut,
		Samples:        "HEAD",
		CodeExtensions: ".ts",
	}, &stdout, &stderr); err == nil {
		t.Fatal("expected git snapshot write failure for occupied output path")
	}

	log := newLogger("", &stderr)
	log.Printf("ready %s", "ok")
	if !strings.Contains(stderr.String(), "["+DefaultName+"] ready ok") {
		t.Fatalf("unexpected logger output: %q", stderr.String())
	}
	if custom := newLogger(" custom ", &stderr); !strings.Contains(custom.prefix, "[custom]") {
		t.Fatalf("unexpected custom logger prefix: %#v", custom)
	}

	if got := configCodeExtensions(""); len(got) != 2 || got[0] != ".cs" || got[1] != ".ts" {
		t.Fatalf("unexpected default code extensions: %#v", got)
	}
	if got := configCodeExtensions(" ts ; .TS , go "); strings.Join(got, ",") != ".ts,.go" {
		t.Fatalf("unexpected normalized code extensions: %#v", got)
	}
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(output))
	}
}
