package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunBuildGitSnapshotCommandSuccessAndFailure(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("expected missing-args failure, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected failure stderr output")
	}

	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "app.go"), []byte("package src\nfunc Run() int { return 1 }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	stdout.Reset()
	stderr.Reset()
	code := run([]string{"--repo", repo, "--out", outPath, "--samples", "HEAD", "--code-extensions", ".go"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected success exit code, got %d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output artifact, got %v", err)
	}
}

func TestRunBuildGitSnapshotCommandParseError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--unknown"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected parse error exit code, got %d stderr=%q", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("flag provided but not defined")) {
		t.Fatalf("expected parse error text, got %q", stderr.String())
	}
}

func TestRunBuildGitSnapshotCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected help exit code 0, got %d stderr=%q", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Usage of build-git-snapshot")) {
		t.Fatalf("expected help text, got %q", stderr.String())
	}
}

func TestMainWrapperReturnsSuccessExitCode(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "app.go"), []byte("package src\nfunc Run() int { return 1 }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	cmd := exec.Command(os.Args[0], "-test.run=TestMainHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"GO_HELPER_MAIN=build-git-snapshot",
		"GO_HELPER_REPO="+repo,
		"GO_HELPER_OUT="+outPath,
		"GO_HELPER_SAMPLES=HEAD",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected helper process success, got %v: %q", err, string(output))
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output artifact from main wrapper, got %v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	return repo
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(output))
	}
}

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	switch os.Getenv("GO_HELPER_MAIN") {
	case "build-git-snapshot":
		os.Args = []string{
			"build-git-snapshot",
			"--repo", os.Getenv("GO_HELPER_REPO"),
			"--out", os.Getenv("GO_HELPER_OUT"),
			"--samples", os.Getenv("GO_HELPER_SAMPLES"),
			"--code-extensions", ".go",
		}
		main()
	}
}
