package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/guide"
)

func TestExecuteGuideImplementationWrapsToolResult(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	sampleRoot := filepath.Join(t.TempDir(), "samples")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.MkdirAll(sampleRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\n1. Configure service.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sampleRoot, "Program.cs"), []byte("builder.Services.AddAuthentication();\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.Runtime{
		ServerName:      "scriptorium",
		DocsRoot:        docsRoot,
		SampleRoots:     []string{sampleRoot},
		CodeExtensions:  []string{".cs"},
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}

	result := ExecuteGuideImplementation(cfg, guide.Request{Topic: "authentication", Language: "csharp"}, io.Discard)
	if result.IsError {
		t.Fatalf("expected success result, got %#v", result)
	}
	if result.StructuredContent == nil {
		t.Fatal("expected structured content to be present")
	}
}
