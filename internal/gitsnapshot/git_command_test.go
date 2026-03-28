package gitsnapshot

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestGitOutputNormalizesMissingExecutable(t *testing.T) {
	stubGitCommand(t, func(string) (string, error) {
		return "", exec.ErrNotFound
	}, nil)

	if _, err := gitOutput("repo", "status"); !errors.Is(err, errGitUnavailable) {
		t.Fatalf("expected normalized missing-git error, got %v", err)
	}
	if _, err := gitOutputBuffer("repo", "status"); !errors.Is(err, errGitUnavailable) {
		t.Fatalf("expected normalized missing-git buffer error, got %v", err)
	}
}

func TestGitOutputNormalizesLookupFailure(t *testing.T) {
	stubGitCommand(t, func(string) (string, error) {
		return "", errors.New("unexpected lookup failure")
	}, nil)

	if _, err := gitOutput("repo", "status"); !errors.Is(err, errGitLookupFailed) {
		t.Fatalf("expected normalized lookup failure, got %v", err)
	}
}

func TestGitOutputKeepsCommandContextForGitFailures(t *testing.T) {
	var invokedName string
	var invokedArgs []string
	stubGitCommand(t,
		func(string) (string, error) { return "/fake/git", nil },
		func(name string, args ...string) ([]byte, error) {
			invokedName = name
			invokedArgs = append([]string(nil), args...)
			return []byte("fatal: bad revision"), errors.New("exit status 128")
		},
	)

	if _, err := gitOutput("repo", "rev-parse", "missing"); err == nil {
		t.Fatal("expected gitOutput failure")
	} else if !strings.Contains(err.Error(), "git rev-parse missing failed:") || !strings.Contains(err.Error(), "fatal: bad revision") {
		t.Fatalf("expected gitOutput to keep command context, got %v", err)
	}

	expectedArgs := []string{"-C", "repo", "rev-parse", "missing"}
	if invokedName != "/fake/git" || !slices.Equal(invokedArgs, expectedArgs) {
		t.Fatalf("unexpected git invocation: name=%q args=%#v", invokedName, invokedArgs)
	}
}

func TestGitOutputBufferReturnsSuccessPayload(t *testing.T) {
	stubGitCommand(t,
		func(string) (string, error) { return "/fake/git", nil },
		func(name string, args ...string) ([]byte, error) {
			return []byte("ok\n"), nil
		},
	)

	buffer, err := gitOutputBuffer("repo", "status", "--short")
	if err != nil {
		t.Fatalf("expected gitOutputBuffer success, got %v", err)
	}
	if string(buffer) != "ok\n" {
		t.Fatalf("unexpected buffer payload: %q", string(buffer))
	}
}

func stubGitCommand(
	t *testing.T,
	lookPath func(string) (string, error),
	combinedOutput func(string, ...string) ([]byte, error),
) {
	t.Helper()
	previousLookPath := gitLookPath
	previousCombinedOutput := gitCombinedOutput
	gitLookPath = lookPath
	if combinedOutput != nil {
		gitCombinedOutput = combinedOutput
	}
	t.Cleanup(func() {
		gitLookPath = previousLookPath
		gitCombinedOutput = previousCombinedOutput
	})
}
