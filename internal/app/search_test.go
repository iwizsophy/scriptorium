package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/search"
)

func TestExecuteSearchWrapsToolResult(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\nsearch body\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.Runtime{
		ServerName:            "scriptorium",
		DocsRoot:              docsRoot,
		DocsIndexVerify:       "full",
		CodeExtensions:        []string{".ts"},
		TextEncodingFallbacks: nil,
		MaxFileBytes:          1_000_000,
	}

	result := ExecuteSearch(cfg, search.Request{Query: "search"}, io.Discard)
	if result.IsError {
		t.Fatalf("expected success result, got error: %#v", result)
	}
	if result.StructuredContent == nil {
		t.Fatal("expected structured content to be present")
	}
}
