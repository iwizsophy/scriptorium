package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunBuildDocsIndexCommandSuccessAndFailure(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("expected missing-args failure, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected failure stderr output")
	}

	docsRoot := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "scriptorium-index.sqlite")
	stdout.Reset()
	stderr.Reset()
	code := run([]string{"--docs-root", docsRoot, "--out", outPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected success exit code, got %d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output artifact, got %v", err)
	}
}

func TestRunBuildDocsIndexCommandParseError(t *testing.T) {
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

func TestRunBuildDocsIndexCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected help exit code, got %d stderr=%q", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Usage of build-docs-index")) {
		t.Fatalf("expected help text, got %q", stderr.String())
	}
}

func TestMainWrapperReturnsSuccessExitCode(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "scriptorium-index.sqlite")

	cmd := exec.Command(os.Args[0], "-test.run=TestMainHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"GO_HELPER_MAIN=build-docs-index",
		"GO_HELPER_DOCS_ROOT="+docsRoot,
		"GO_HELPER_OUT="+outPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected helper process success, got %v: %q", err, string(output))
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output artifact from main wrapper, got %v", err)
	}
}

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	switch os.Getenv("GO_HELPER_MAIN") {
	case "build-docs-index":
		os.Args = []string{
			"build-docs-index",
			"--docs-root", os.Getenv("GO_HELPER_DOCS_ROOT"),
			"--out", os.Getenv("GO_HELPER_OUT"),
		}
		main()
	}
}
