package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/silvekt/scriptorium/internal/filesafe"
)

func TestScanFilesystemReturnsEmptySliceForEmptyRoot(t *testing.T) {
	root, err := filesafe.NewRoot(t.TempDir(), false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	files, err := ScanFilesystem(root, nil)
	if err != nil {
		t.Fatalf("ScanFilesystem returned error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected empty file list, got %#v", files)
	}
}

func TestScanFilesystemPropagatesListFilesErrors(t *testing.T) {
	dir := t.TempDir()
	root, err := filesafe.NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll returned error: %v", err)
	}

	if _, err := ScanFilesystem(root, nil); err == nil {
		t.Fatal("expected ScanFilesystem to propagate ListFiles error")
	}
}

func TestScanFilesystemPreservesDeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	writeScanFile(t, filepath.Join(dir, "z.md"), "# Z\n")
	writeScanFile(t, filepath.Join(dir, "a.md"), "# A\n")

	root, err := filesafe.NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	files, err := ScanFilesystem(root, nil)
	if err != nil {
		t.Fatalf("ScanFilesystem returned error: %v", err)
	}
	if len(files) != 2 || files[0].Path != "a.md" || files[1].Path != "z.md" {
		t.Fatalf("expected deterministic sort order, got %#v", files)
	}
}

func writeScanFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
