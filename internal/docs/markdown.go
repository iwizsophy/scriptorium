package docs

import (
	"regexp"
	"strings"

	"github.com/iwizsophy/scriptorium/internal/ref"
	"github.com/iwizsophy/scriptorium/internal/textutil"
)

type File struct {
	Path      string  `json:"path"`
	LineCount int     `json:"lineCount"`
	Content   string  `json:"content"`
	Blocks    []Block `json:"blocks"`
}

type Block struct {
	RefID       string `json:"refId"`
	Path        string `json:"path"`
	HeadingSlug string `json:"headingSlug"`
	Heading     string `json:"heading"`
	Level       int    `json:"level"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	Content     string `json:"content"`
}

type heading struct {
	Heading   string
	Slug      string
	Level     int
	StartLine int
}

var headingPattern = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)\s*$`)

func ParseMarkdownFile(path, text string) File {
	normalizedPath := ref.NormalizePath(path)
	normalizedText := textutil.NormalizeLineEndings(text)
	lines := textutil.SplitLines(normalizedText)

	usedSlugs := map[string]struct{}{}
	headings := make([]heading, 0, 16)
	for idx, line := range lines {
		level, title, ok := parseHeadingLine(line)
		if !ok {
			continue
		}
		headings = append(headings, heading{
			Heading:   title,
			Slug:      ref.SlugifyHeading(title, usedSlugs),
			Level:     level,
			StartLine: idx + 1,
		})
	}

	blocks := make([]Block, 0, len(headings))
	for idx, current := range headings {
		endLine := len(lines)
		for next := idx + 1; next < len(headings); next++ {
			if headings[next].Level <= current.Level {
				endLine = headings[next].StartLine - 1
				break
			}
		}

		content := ""
		if endLine >= current.StartLine {
			content = strings.Join(lines[current.StartLine-1:endLine], "\n")
		}

		blocks = append(blocks, Block{
			RefID:       ref.BuildMDRefID(normalizedPath, current.Slug),
			Path:        normalizedPath,
			HeadingSlug: current.Slug,
			Heading:     current.Heading,
			Level:       current.Level,
			StartLine:   current.StartLine,
			EndLine:     endLine,
			Content:     content,
		})
	}

	return File{
		Path:      normalizedPath,
		LineCount: len(lines),
		Content:   normalizedText,
		Blocks:    blocks,
	}
}

func parseHeadingLine(line string) (int, string, bool) {
	matches := headingPattern.FindStringSubmatch(line)
	if matches == nil {
		return 0, "", false
	}
	return len(matches[1]), strings.TrimSpace(matches[2]), true
}
