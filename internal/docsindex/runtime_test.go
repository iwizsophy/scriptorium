package docsindex

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
)

func TestArtifactSearchMarkdownAndFileByPath(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(docsRoot, "api", "guide.md"), "# Authentication Setup\nRegister authentication services.\nKeep route wiring in the same guide.\n")
	mustWriteFile(t, filepath.Join(docsRoot, "notes.md"), "# Notes\nAuthentication Setup appears here too.\n")

	artifact, err := Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "scriptorium-index.sqlite")
	if err := Write(outPath, artifact); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	loaded, err := Load(outPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	results, err := loaded.SearchMarkdown("authentication setup", 2, 10)
	if err != nil {
		t.Fatalf("SearchMarkdown returned error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected multiple markdown hits, got %#v", results)
	}
	if results[0].Path != "api/guide.md" {
		t.Fatalf("expected heading/path boosted file first, got %#v", results)
	}
	if strings.Count(results[0].Snippet, "\n") > 1 {
		t.Fatalf("expected two-line snippet, got %q", results[0].Snippet)
	}

	file, ok := loaded.FileByPath("api/guide.md")
	if !ok || file.Path != "api/guide.md" || !strings.Contains(file.Content, "Register authentication services.") {
		t.Fatalf("expected FileByPath hit, got %#v ok=%v", file, ok)
	}
	if _, ok := loaded.FileByPath("missing.md"); ok {
		t.Fatal("expected missing FileByPath lookup to fail")
	}

	empty, err := loaded.SearchMarkdown("", 5, 10)
	if err != nil {
		t.Fatalf("SearchMarkdown empty query returned error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty-query search to return no hits, got %#v", empty)
	}
}

func TestOpenRuntimeSetSupportsExplicitMultiArtifactPathsAndDedupes(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(firstRoot, "a.md"), "# Alpha\nfirst token\n")
	mustWriteFile(t, filepath.Join(secondRoot, "b.md"), "# Beta\nsecond token\n")

	firstArtifact, err := Build(firstRoot, nil, false)
	if err != nil {
		t.Fatalf("Build first artifact returned error: %v", err)
	}
	secondArtifact, err := Build(secondRoot, nil, false)
	if err != nil {
		t.Fatalf("Build second artifact returned error: %v", err)
	}

	firstPath := filepath.Join(t.TempDir(), "docs-a.sqlite")
	secondPath := filepath.Join(t.TempDir(), "docs-b.sqlite")
	if err := Write(firstPath, firstArtifact); err != nil {
		t.Fatalf("Write first artifact returned error: %v", err)
	}
	if err := Write(secondPath, secondArtifact); err != nil {
		t.Fatalf("Write second artifact returned error: %v", err)
	}

	state := OpenRuntimeSet(config.Runtime{
		DocsRoot:        firstRoot,
		DocsIndexPaths:  []string{firstPath, secondPath, firstPath},
		DocsIndexVerify: "off",
		MaxFileBytes:    1_000_000,
	})
	if !state.Enabled || len(state.Indexes) != 2 {
		t.Fatalf("expected two explicit indexes to load, got %#v", state)
	}
	paths := append([]string(nil), state.Paths...)
	slices.Sort(paths)
	if !slices.Equal(paths, []string{firstPath, secondPath}) {
		t.Fatalf("expected deduped explicit paths, got %#v", state.Paths)
	}

	missing := OpenRuntimeSet(config.Runtime{
		DocsRoot:        firstRoot,
		DocsIndexPaths:  []string{filepath.Join(t.TempDir(), "missing.sqlite")},
		DocsIndexVerify: "off",
		MaxFileBytes:    1_000_000,
	})
	if missing.Enabled || !strings.Contains(missing.Warning, "not found") {
		t.Fatalf("expected missing explicit path warning, got %#v", missing)
	}
}
