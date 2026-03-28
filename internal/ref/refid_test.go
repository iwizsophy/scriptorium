package ref

import "testing"

func TestNormalizePath(t *testing.T) {
	got := NormalizePath(`./docs\../docs/readme.md`)
	if got != "docs/readme.md" {
		t.Fatalf("unexpected normalized path: %q", got)
	}
	if got := NormalizePath("./"); got != "" {
		t.Fatalf("expected current-dir path to normalize to empty string, got %q", got)
	}
}

func TestSlugifyHeading(t *testing.T) {
	used := map[string]struct{}{}
	if got := SlugifyHeading("Hello World!", used); got != "hello-world" {
		t.Fatalf("unexpected slug: %q", got)
	}
	if got := SlugifyHeading("Hello World!", used); got != "hello-world-2" {
		t.Fatalf("unexpected duplicate slug: %q", got)
	}
}

func TestSlugifyJapaneseHeading(t *testing.T) {
	used := map[string]struct{}{}
	if got := SlugifyHeading("認証フロー", used); got != "認証フロー" {
		t.Fatalf("unexpected japanese slug: %q", got)
	}
	if got := SlugifyHeading("認証フロー", used); got != "認証フロー-2" {
		t.Fatalf("unexpected duplicate japanese slug: %q", got)
	}
}

func TestSlugifyHeadingAppliesNFKC(t *testing.T) {
	used := map[string]struct{}{}
	if got := SlugifyHeading(" Ｆｏｏ／Ｂａｒ ① ", used); got != "foo-bar-1" {
		t.Fatalf("unexpected NFKC slug: %q", got)
	}
}

func TestSlugifyHeadingFallsBackToSection(t *testing.T) {
	used := map[string]struct{}{}
	if got := SlugifyHeading("!!!", used); got != "section" {
		t.Fatalf("unexpected fallback slug: %q", got)
	}
	if got := SlugifyHeading("???", used); got != "section-2" {
		t.Fatalf("unexpected duplicate fallback slug: %q", got)
	}
}

func TestSlugifyHeadingInitializesNilUsedMap(t *testing.T) {
	if got := SlugifyHeading("  API Reference  ", nil); got != "api-reference" {
		t.Fatalf("unexpected slug with nil used map: %q", got)
	}
}

func TestParseRefID(t *testing.T) {
	parsed, err := ParseRefID("code:@auth/src/login.cs@L10")
	if err != nil {
		t.Fatalf("ParseRefID returned error: %v", err)
	}
	if parsed.Kind != "code" || parsed.Path != "@auth/src/login.cs" || parsed.Line != 10 {
		t.Fatalf("unexpected parsed ref: %#v", parsed)
	}
}

func TestParseRefIDMarkdownFileAndInvalid(t *testing.T) {
	mdParsed, err := ParseRefID("md:docs/guide.md#auth-flow")
	if err != nil {
		t.Fatalf("ParseRefID markdown returned error: %v", err)
	}
	if mdParsed.Kind != "md" || mdParsed.Path != "docs/guide.md" || mdParsed.HeadingSlug != "auth-flow" {
		t.Fatalf("unexpected markdown ref: %#v", mdParsed)
	}

	fileParsed, err := ParseRefID("file:./samples/readme.md")
	if err != nil {
		t.Fatalf("ParseRefID file returned error: %v", err)
	}
	if fileParsed.Kind != "file" || fileParsed.Path != "samples/readme.md" {
		t.Fatalf("unexpected file ref: %#v", fileParsed)
	}

	if _, err := ParseRefID("bad:guide.md"); err == nil {
		t.Fatal("expected invalid refId error")
	}
}

func TestParseRefIDNormalizesMarkdownAndFilePaths(t *testing.T) {
	mdParsed, err := ParseRefID(`md:.\docs\guide.md#auth-flow`)
	if err != nil {
		t.Fatalf("ParseRefID markdown returned error: %v", err)
	}
	if mdParsed.Path != "docs/guide.md" {
		t.Fatalf("expected normalized markdown path, got %#v", mdParsed)
	}

	fileParsed, err := ParseRefID(`file:.\samples\README.md`)
	if err != nil {
		t.Fatalf("ParseRefID file returned error: %v", err)
	}
	if fileParsed.Path != "samples/README.md" {
		t.Fatalf("expected normalized file path, got %#v", fileParsed)
	}
}

func TestBuildRefIDsNormalizePaths(t *testing.T) {
	if got := BuildMDRefID(`.\docs\guide.md`, "auth-flow"); got != "md:docs/guide.md#auth-flow" {
		t.Fatalf("unexpected md ref: %q", got)
	}
	if got := BuildCodeRefID(`.\src\auth.ts`, 42); got != "code:src/auth.ts@L42" {
		t.Fatalf("unexpected code ref: %q", got)
	}
	if got := BuildFileRefID(`.\samples\README.md`); got != "file:samples/README.md" {
		t.Fatalf("unexpected file ref: %q", got)
	}
}

func TestCollapseDashes(t *testing.T) {
	if got := collapseDashes("a---b--c"); got != "a-b-c" {
		t.Fatalf("unexpected collapsed dashes: %q", got)
	}
}
