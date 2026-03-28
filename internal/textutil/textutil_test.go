package textutil

import (
	"slices"
	"testing"
)

func TestNormalizeLineEndings(t *testing.T) {
	got := NormalizeLineEndings("a\r\nb\rc\n")
	if got != "a\nb\nc\n" {
		t.Fatalf("unexpected normalized text: %q", got)
	}
}

func TestSplitLinesNormalizesAndDropsTrailingEmptyLine(t *testing.T) {
	got := SplitLines("a\r\nb\rc\n")
	want := []string{"a", "b", "c"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected split lines: got=%v want=%v", got, want)
	}
}

func TestSplitLinesKeepsInteriorEmptyLines(t *testing.T) {
	got := SplitLines("a\n\nb")
	want := []string{"a", "", "b"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected split lines with interior gap: got=%v want=%v", got, want)
	}
}

func TestSplitLinesReturnsNilForEmptyInput(t *testing.T) {
	if got := SplitLines(""); got != nil {
		t.Fatalf("expected nil lines for empty input, got %#v", got)
	}
}

func TestCenteredRangeClampsToBounds(t *testing.T) {
	start, end := CenteredRange(5, 5, 4)
	if start != 2 || end != 5 {
		t.Fatalf("unexpected centered range: start=%d end=%d", start, end)
	}
}

func TestCenteredRangeHandlesEmptyAndLeadingBounds(t *testing.T) {
	start, end := CenteredRange(0, 1, 4)
	if start != 1 || end != 1 {
		t.Fatalf("unexpected empty centered range: start=%d end=%d", start, end)
	}

	start, end = CenteredRange(5, 1, 4)
	if start != 1 || end != 4 {
		t.Fatalf("unexpected leading centered range: start=%d end=%d", start, end)
	}

	start, end = CenteredRange(3, 3, 5)
	if start != 1 || end != 3 {
		t.Fatalf("unexpected oversized centered range: start=%d end=%d", start, end)
	}
}

func TestCenteredSnippetReturnsJoinedSnippet(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five"}
	start, end, snippet := CenteredSnippet(lines, 3, 3)
	if start != 2 || end != 4 {
		t.Fatalf("unexpected range: start=%d end=%d", start, end)
	}
	if snippet != "two\nthree\nfour" {
		t.Fatalf("unexpected snippet: %q", snippet)
	}
}

func TestCenteredSnippetReturnsEmptyForNoLines(t *testing.T) {
	start, end, snippet := CenteredSnippet(nil, 1, 3)
	if start != 0 || end != 0 || snippet != "" {
		t.Fatalf("unexpected empty snippet response: start=%d end=%d snippet=%q", start, end, snippet)
	}
}
