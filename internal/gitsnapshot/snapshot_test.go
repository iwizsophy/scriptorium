package gitsnapshot

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
	_ "modernc.org/sqlite"
)

func TestBuildAndLoadSnapshot(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export function run() {\n  return 1\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/test")

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/test",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if artifact.Meta.RootCount != 1 || artifact.Meta.FileCount != 1 {
		t.Fatalf("unexpected artifact meta: %#v", artifact.Meta)
	}
	if !strings.HasPrefix(artifact.Entries[0].LogicalPath, "@feature-test/") {
		t.Fatalf("unexpected logical path: %#v", artifact.Entries[0])
	}

	outPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Meta.Fingerprint == "" {
		t.Fatalf("expected fingerprint, got %#v", loaded.Meta)
	}

	db, err := sql.Open("sqlite", outPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM code_entries`).Scan(&count); err != nil {
		t.Fatalf("QueryRow returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 code_entries row, got %d", count)
	}
}

func TestParseSamplesSupportsExplicitAndAllBranchSelectors(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "app.ts"), "export const value = 1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "sample-auth")
	git(t, repo, "checkout", "-b", "feature/extra")

	specs, err := parseSamples(repo, "auth=sample-auth;wip=WORKTREE;auth=feature/extra")
	if err != nil {
		t.Fatalf("parseSamples returned error: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("expected 3 explicit specs, got %#v", specs)
	}
	if specs[0].ID != "auth" || specs[0].Ref != "sample-auth" || specs[1].Kind != "worktree" || specs[2].ID != "auth-2" {
		t.Fatalf("unexpected explicit specs: %#v", specs)
	}

	allBranches, err := parseSamples(repo, "ALL")
	if err != nil {
		t.Fatalf("parseSamples ALL returned error: %v", err)
	}
	if !containsRoot(allBranches, "sample-auth") || !containsRoot(allBranches, "feature-extra") {
		t.Fatalf("expected ALL selector to include branch roots, got %#v", allBranches)
	}
}

func TestNormalizeFetchOptions(t *testing.T) {
	tests := []struct {
		name         string
		fetchEnabled bool
		fetchOnStart bool
		wantEnabled  bool
		wantStart    bool
	}{
		{
			name:         "disabled turns off fetch",
			fetchEnabled: false,
			fetchOnStart: true,
			wantEnabled:  false,
			wantStart:    false,
		},
		{
			name:         "enabled keeps startup fetch",
			fetchEnabled: true,
			fetchOnStart: true,
			wantEnabled:  true,
			wantStart:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEnabled, gotStart := normalizeFetchOptions(tt.fetchEnabled, tt.fetchOnStart)
			if gotEnabled != tt.wantEnabled || gotStart != tt.wantStart {
				t.Fatalf("unexpected fetch options: got (%v,%v) want (%v,%v)", gotEnabled, gotStart, tt.wantEnabled, tt.wantStart)
			}
		})
	}
}

func TestBuildRejectsOversizedWorktreeFile(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "large.ts"), "0123456789")

	_, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "WORKTREE",
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   4,
	})
	if err == nil || !strings.Contains(err.Error(), "MAX_FILE_BYTES") {
		t.Fatalf("expected MAX_FILE_BYTES error, got %v", err)
	}
}

func TestBuildRejectsOversizedGitRefFile(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "large.ts"), "0123456789")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/large")

	_, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/large",
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   4,
	})
	if err == nil || !strings.Contains(err.Error(), "MAX_FILE_BYTES") {
		t.Fatalf("expected MAX_FILE_BYTES error, got %v", err)
	}
}

func TestBuildDecodesShiftJISForWorktreeAndGitRef(t *testing.T) {
	repo := initGitRepo(t)
	encoded, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte("生成AI"))
	if err != nil {
		t.Fatalf("shift_jis encode returned error: %v", err)
	}
	mustWriteBinaryFile(t, filepath.Join(repo, "src", "sjis.ts"), encoded)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/sjis")

	t.Setenv("SCRIPTORIUM_TEXT_ENCODING_FALLBACK", "shift_jis")

	worktree, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "WORKTREE",
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	})
	if err != nil {
		t.Fatalf("Build worktree returned error: %v", err)
	}
	if len(worktree.Entries) != 1 || !strings.Contains(worktree.Entries[0].Content, "生成AI") {
		t.Fatalf("expected decoded worktree content, got %#v", worktree.Entries)
	}

	refSnapshot, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/sjis",
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	})
	if err != nil {
		t.Fatalf("Build git ref returned error: %v", err)
	}
	if len(refSnapshot.Entries) != 1 || !strings.Contains(refSnapshot.Entries[0].Content, "生成AI") {
		t.Fatalf("expected decoded git ref content, got %#v", refSnapshot.Entries)
	}
}

func TestBuildSkipsWorktreeSymlinkWhenDisabled(t *testing.T) {
	repo := initGitRepo(t)
	mustWriteFile(t, filepath.Join(repo, "src", "real.ts"), "export const value = 1\n")
	target := filepath.Join(repo, "src", "real.ts")
	link := filepath.Join(repo, "src", "link.ts")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unavailable: %v", err)
	}

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "WORKTREE",
		CodeExtensions: []string{".ts"},
		AllowSymlinks:  false,
		MaxFileBytes:   1_000_000,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(artifact.Entries) != 1 || artifact.Entries[0].RelativePath != "src/real.ts" {
		t.Fatalf("expected symlink to be skipped, got %#v", artifact.Entries)
	}
}

func TestBuildAllowsWorktreeSymlinkOutsideRootWhenEnabled(t *testing.T) {
	repo := initGitRepo(t)
	outsideDir := t.TempDir()
	mustWriteFile(t, filepath.Join(outsideDir, "outside.ts"), "export const outside = 1\n")

	link := filepath.Join(repo, "src", "link.ts")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "outside.ts"), link); err != nil {
		t.Skipf("os.Symlink unavailable: %v", err)
	}

	artifact, err := Build(BuildOptions{
		RepoPath:       repo,
		Samples:        "WORKTREE",
		CodeExtensions: []string{".ts"},
		AllowSymlinks:  true,
		MaxFileBytes:   1_000_000,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(artifact.Entries) != 1 || artifact.Entries[0].RelativePath != "src/link.ts" || !strings.Contains(artifact.Entries[0].Content, "outside") {
		t.Fatalf("expected escaping symlink entry to be materialized, got %#v", artifact.Entries)
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

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func mustWriteBinaryFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func containsRoot(specs []sampleSpec, id string) bool {
	for _, spec := range specs {
		if spec.ID == id {
			return true
		}
	}
	return false
}
