package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/content"
	"github.com/silvekt/scriptorium/internal/flow"
	"github.com/silvekt/scriptorium/internal/guide"
	"github.com/silvekt/scriptorium/internal/related"
	"github.com/silvekt/scriptorium/internal/search"
)

func TestExecuteSearchAndRelatedWrappers(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteWrapperFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")

	cfg := config.Runtime{
		ServerName:   " wrapper ",
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}
	var stderr bytes.Buffer

	searchResult := ExecuteSearch(cfg, search.Request{}, &stderr)
	if searchResult.IsError {
		t.Fatalf("expected empty-query search wrapper to succeed: %#v", searchResult)
	}
	if !strings.Contains(searchResult.Content[0].Text, "Found 0 result(s).") {
		t.Fatalf("unexpected search wrapper content: %#v", searchResult)
	}

	relatedResult := ExecuteExpandRelated(cfg, related.Request{Seeds: []string{"md:guide.md#guide"}}, &stderr)
	if relatedResult.IsError {
		t.Fatalf("expected related wrapper to succeed: %#v", relatedResult)
	}
	if !strings.Contains(relatedResult.Content[0].Text, "Expanded 0 related reference(s).") {
		t.Fatalf("unexpected related wrapper content: %#v", relatedResult)
	}

	searchErr := ExecuteSearch(config.Runtime{ServerName: "wrapper", DocsRoot: filepath.Join(docsRoot, "missing"), MaxFileBytes: 1_000_000}, search.Request{Query: "guide"}, &stderr)
	if !searchErr.IsError || !strings.Contains(searchErr.Content[0].Text, "search failed") {
		t.Fatalf("expected search error payload, got %#v", searchErr)
	}

	relatedErr := ExecuteExpandRelated(cfg, related.Request{}, &stderr)
	if !relatedErr.IsError || !strings.Contains(relatedErr.Content[0].Text, "expand_related failed") {
		t.Fatalf("expected related error payload, got %#v", relatedErr)
	}

	logText := stderr.String()
	if !strings.Contains(logText, "[wrapper] tool=search") || !strings.Contains(logText, "[wrapper] tool=expand_related") {
		t.Fatalf("expected wrapper logs, got %q", logText)
	}
}

func TestExecuteContentAndFlowWrappers(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteWrapperFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\n1. First\n2. Second\n")

	cfg := config.Runtime{
		ServerName:   "wrapper",
		DocsRoot:     docsRoot,
		MaxFileBytes: 1_000_000,
	}
	var stderr bytes.Buffer

	okResult := ExecuteGetContent(cfg, content.Request{RefID: "md:guide.md#guide", Mode: "full"}, &stderr)
	if okResult.IsError {
		t.Fatalf("expected get_content success, got %#v", okResult)
	}
	if !strings.Contains(okResult.Content[0].Text, "Loaded md_heading_block content.") {
		t.Fatalf("unexpected get_content success payload: %#v", okResult)
	}

	errResult := ExecuteGetContent(cfg, content.Request{RefID: "bad-ref"}, &stderr)
	if !errResult.IsError || !strings.Contains(errResult.Content[0].Text, "get_content failed") {
		t.Fatalf("expected get_content error payload, got %#v", errResult)
	}

	flowResult := ExecuteSummarizeFlow(cfg, flow.Request{Seeds: []string{"md:guide.md#guide"}, Style: "brief"}, &stderr)
	if flowResult.IsError {
		t.Fatalf("expected summarize_flow success, got %#v", flowResult)
	}
	if !strings.Contains(flowResult.Content[0].Text, "Built 2 flow step(s).") {
		t.Fatalf("unexpected summarize_flow success payload: %#v", flowResult)
	}

	flowErr := ExecuteSummarizeFlow(cfg, flow.Request{Seeds: []string{"bad-ref"}}, &stderr)
	if !flowErr.IsError || !strings.Contains(flowErr.Content[0].Text, "summarize_flow failed") {
		t.Fatalf("expected summarize_flow error payload, got %#v", flowErr)
	}
}

func TestExecuteGuideWrapper(t *testing.T) {
	docsRoot := t.TempDir()
	codeRoot := t.TempDir()
	mustWriteWrapperFile(t, filepath.Join(docsRoot, "guide.md"), "# Dependency Injection\n1. Register service\n2. Resolve dependency\n")
	mustWriteWrapperFile(t, filepath.Join(codeRoot, "program.cs"), "builder.Services.AddScoped<IMyService, MyService>();\n")

	cfg := config.Runtime{
		ServerName:     "wrapper",
		DocsRoot:       docsRoot,
		SampleRoots:    []string{codeRoot},
		CodeExtensions: []string{".cs"},
		MaxFileBytes:   1_000_000,
	}
	var stderr bytes.Buffer

	okResult := ExecuteGuideImplementation(cfg, guide.Request{Topic: "dependency injection", Language: "csharp", MaxRefs: 2}, &stderr)
	if okResult.IsError {
		t.Fatalf("expected guide_implementation success, got %#v", okResult)
	}
	if !strings.Contains(okResult.Content[0].Text, "Built implementation guide with") {
		t.Fatalf("unexpected guide success payload: %#v", okResult)
	}

	errResult := ExecuteGuideImplementation(cfg, guide.Request{}, &stderr)
	if !errResult.IsError || !strings.Contains(errResult.Content[0].Text, "guide_implementation failed") {
		t.Fatalf("expected guide error payload, got %#v", errResult)
	}
}

func mustWriteWrapperFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
