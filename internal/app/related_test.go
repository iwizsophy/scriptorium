package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/related"
)

func TestExecuteExpandRelatedWrapsToolResult(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\nbody\n## Detail\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.Runtime{
		ServerName:   "scriptorium",
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}

	result := ExecuteExpandRelated(cfg, related.Request{Seeds: []string{"md:guide.md#guide"}}, io.Discard)
	if result.IsError {
		t.Fatalf("expected success result, got %#v", result)
	}
	if result.StructuredContent == nil {
		t.Fatal("expected structured content to be present")
	}
}
