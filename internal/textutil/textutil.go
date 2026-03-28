package textutil

import "strings"

func NormalizeLineEndings(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(NormalizeLineEndings(text), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func CenteredRange(totalLines, focusLine, snippetLines int) (int, int) {
	if totalLines <= 0 {
		return 1, 1
	}
	half := snippetLines / 2
	start := focusLine - half
	if start < 1 {
		start = 1
	}
	end := start + snippetLines - 1
	if end > totalLines {
		end = totalLines
		start = max(1, end-snippetLines+1)
	}
	return start, end
}

func CenteredSnippet(lines []string, focusLine, snippetLines int) (int, int, string) {
	if len(lines) == 0 {
		return 0, 0, ""
	}
	start, end := CenteredRange(len(lines), focusLine, snippetLines)
	return start, end, strings.Join(lines[start-1:end], "\n")
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
