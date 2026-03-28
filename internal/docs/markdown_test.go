package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/silvekt/scriptorium/internal/filesafe"
)

func TestParseMarkdownFileBuildsHeadingBlocks(t *testing.T) {
	file := ParseMarkdownFile("docs/guide.md", "# Root\nintro\n## Child\nchild body\n### Leaf\nleaf body\n## Next\nnext body\n")

	if file.LineCount != 8 {
		t.Fatalf("unexpected line count: %d", file.LineCount)
	}
	if len(file.Blocks) != 4 {
		t.Fatalf("unexpected block count: %d", len(file.Blocks))
	}

	rootBlock := file.Blocks[0]
	if rootBlock.RefID != "md:docs/guide.md#root" || rootBlock.StartLine != 1 || rootBlock.EndLine != 8 {
		t.Fatalf("unexpected root block: %#v", rootBlock)
	}

	childBlock := file.Blocks[1]
	if childBlock.RefID != "md:docs/guide.md#child" || childBlock.StartLine != 3 || childBlock.EndLine != 6 {
		t.Fatalf("unexpected child block: %#v", childBlock)
	}

	leafBlock := file.Blocks[2]
	if leafBlock.RefID != "md:docs/guide.md#leaf" || leafBlock.StartLine != 5 || leafBlock.EndLine != 6 {
		t.Fatalf("unexpected leaf block: %#v", leafBlock)
	}
}

func TestParseMarkdownFileDedupesJapaneseHeadingSlugs(t *testing.T) {
	file := ParseMarkdownFile("docs/flow.md", "## 認証フロー\nbody\n## 認証フロー\nbody2\n")

	if len(file.Blocks) != 2 {
		t.Fatalf("unexpected block count: %d", len(file.Blocks))
	}
	if file.Blocks[0].HeadingSlug != "認証フロー" {
		t.Fatalf("unexpected first slug: %#v", file.Blocks[0])
	}
	if file.Blocks[1].HeadingSlug != "認証フロー-2" {
		t.Fatalf("unexpected second slug: %#v", file.Blocks[1])
	}
}

func TestScanFilesystemParsesMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "docs", "a.md"), "# A\nbody\n")
	mustWriteFile(t, filepath.Join(dir, "docs", "nested", "b.md"), "## B\nbody\n")
	mustWriteFile(t, filepath.Join(dir, "docs", "c.txt"), "not markdown\n")

	root, err := filesafe.NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	files, err := ScanFilesystem(root, nil)
	if err != nil {
		t.Fatalf("ScanFilesystem returned error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("unexpected file count: %d", len(files))
	}
	if files[0].Path != "docs/a.md" || files[1].Path != "docs/nested/b.md" {
		t.Fatalf("unexpected file order: %#v", files)
	}
	if files[0].Blocks[0].RefID != "md:docs/a.md#a" {
		t.Fatalf("unexpected first file block: %#v", files[0].Blocks[0])
	}
}

func TestScanFilesystemPropagatesReadTextErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte{0x00, 0x01, 0x00, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	root, err := filesafe.NewRoot(dir, false, 0)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}

	if _, err := ScanFilesystem(root, nil); err == nil {
		t.Fatal("expected ScanFilesystem to propagate markdown read error")
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
