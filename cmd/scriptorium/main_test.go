package main

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
)

func TestRunReturnsErrorCodeWhenServerFailsWithoutEnv(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected non-zero exit code, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected error output on stderr")
	}
}

func TestMainWrapperReturnsNonZeroExitCode(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestMainHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"GO_HELPER_MAIN=scriptorium",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected helper process to exit non-zero")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected exit error, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("unexpected exit code: %d output=%q", exitErr.ExitCode(), string(output))
	}
}

func TestMainWrapperReturnsZeroExitCodeWithValidConfig(t *testing.T) {
	docsRoot := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestMainHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"GO_HELPER_MAIN=scriptorium-success",
		"SCRIPTORIUM_MARKDOWN_DIR="+docsRoot,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected helper process success, got %v: %q", err, string(output))
	}
}

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	switch os.Getenv("GO_HELPER_MAIN") {
	case "scriptorium":
		os.Args = []string{"scriptorium"}
		main()
	case "scriptorium-success":
		os.Args = []string{"scriptorium"}
		main()
	}
}
