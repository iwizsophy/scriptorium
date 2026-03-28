package ref

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type ParsedRef struct {
	Kind        string
	Path        string
	HeadingSlug string
	Line        int
}

var (
	mdRefPattern   = regexp.MustCompile(`^md:(.+)#([^#]+)$`)
	codeRefPattern = regexp.MustCompile(`^code:(.+)@L([0-9]+)$`)
	fileRefPattern = regexp.MustCompile(`^file:(.+)$`)
)

func NormalizePath(input string) string {
	replaced := strings.ReplaceAll(input, `\`, `/`)
	normalized := path.Clean(replaced)
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "." {
		return ""
	}
	return normalized
}

func SlugifyHeading(heading string, used map[string]struct{}) string {
	if used == nil {
		used = map[string]struct{}{}
	}

	normalized := strings.TrimSpace(norm.NFKC.String(heading))
	normalized = strings.ToLower(normalized)

	var b strings.Builder
	lastDash := false
	for _, r := range normalized {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if lastDash {
			continue
		}
		b.WriteRune('-')
		lastDash = true
	}

	base := collapseDashes(strings.Trim(b.String(), "-"))
	if base == "" {
		base = "section"
	}

	slug := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[slug]; !exists {
			used[slug] = struct{}{}
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func BuildMDRefID(filePath, headingSlug string) string {
	return "md:" + NormalizePath(filePath) + "#" + headingSlug
}

func BuildCodeRefID(filePath string, line int) string {
	return fmt.Sprintf("code:%s@L%d", NormalizePath(filePath), line)
}

func BuildFileRefID(filePath string) string {
	return "file:" + NormalizePath(filePath)
}

func ParseRefID(refID string) (ParsedRef, error) {
	if matches := mdRefPattern.FindStringSubmatch(refID); matches != nil {
		return ParsedRef{
			Kind:        "md",
			Path:        NormalizePath(matches[1]),
			HeadingSlug: matches[2],
		}, nil
	}
	if matches := codeRefPattern.FindStringSubmatch(refID); matches != nil {
		line, err := strconv.Atoi(matches[2])
		if err != nil {
			// COVERAGE_EXCEPTION: codeRefPattern only matches decimal digits, so this branch is defensive and not reachable via textual refId input alone.
			return ParsedRef{}, fmt.Errorf("invalid refId: %s", refID)
		}
		return ParsedRef{
			Kind: "code",
			Path: NormalizePath(matches[1]),
			Line: line,
		}, nil
	}
	if matches := fileRefPattern.FindStringSubmatch(refID); matches != nil {
		return ParsedRef{
			Kind: "file",
			Path: NormalizePath(matches[1]),
		}, nil
	}
	return ParsedRef{}, fmt.Errorf("invalid refId: %s", refID)
}

func collapseDashes(input string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range input {
		if r == '-' {
			if lastDash {
				continue
			}
			lastDash = true
			b.WriteRune(r)
			continue
		}
		lastDash = false
		b.WriteRune(r)
	}
	return b.String()
}
