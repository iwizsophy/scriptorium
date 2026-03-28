package textsearch

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}_./-]+`)

func Normalize(value string) string {
	return strings.ToLower(norm.NFKC.String(value))
}

func TokenizeSearchText(value string, minLength int) []string {
	if minLength < 1 {
		minLength = 1
	}
	matches := tokenPattern.FindAllString(Normalize(value), -1)
	seen := map[string]struct{}{}
	tokens := make([]string, 0, len(matches))
	for _, match := range matches {
		if utf8.RuneCountInString(match) < minLength {
			continue
		}
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		tokens = append(tokens, match)
	}
	return tokens
}

func TokenizeFullTextSearch(value string) []string {
	return collectFullTextTokens(value, true)
}

func BuildFullTextSearchContent(value string) string {
	return strings.Join(collectFullTextTokens(value, false), " ")
}

func BuildFullTextSearchQuery(value string) string {
	terms := TokenizeFullTextSearch(value)
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, quoteTerm(term))
	}
	return strings.Join(quoted, " OR ")
}

func TokenizeOverlapText(value string, maxTokens int) []string {
	if maxTokens <= 0 {
		maxTokens = 30
	}
	counts := map[string]int{}
	for _, token := range TokenizeSearchText(value, 2) {
		if !isSearchableToken(token) {
			continue
		}
		if containsCJK(token) {
			for _, item := range expandCJKToken(token, false) {
				counts[item]++
			}
			continue
		}
		if utf8.RuneCountInString(token) >= 3 {
			counts[token]++
		}
	}

	type counted struct {
		token string
		count int
	}
	items := make([]counted, 0, len(counts))
	for token, count := range counts {
		items = append(items, counted{token: token, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].token < items[j].token
	})
	if len(items) > maxTokens {
		items = items[:maxTokens]
	}
	tokens := make([]string, 0, len(items))
	for _, item := range items {
		tokens = append(tokens, item.token)
	}
	return tokens
}

func ScoreTokenMatches(value string, tokens []string, phrase string, tokenWeight int, phraseWeight int) int {
	normalized := Normalize(value)
	score := 0
	for _, token := range tokens {
		if strings.Contains(normalized, token) {
			score += tokenWeight
		}
	}
	if phrase != "" && strings.Contains(normalized, phrase) {
		score += phraseWeight
	}
	return score
}

func ScorePathBoost(value string, tokens []string, phrase string) int {
	normalized := Normalize(filepath.ToSlash(value))
	basename := Normalize(filepath.Base(filepath.ToSlash(value)))
	score := 0
	if phrase != "" {
		if strings.Contains(basename, phrase) {
			score += 5
		} else if strings.Contains(normalized, phrase) {
			score += 3
		}
	}
	for _, token := range tokens {
		if strings.Contains(basename, token) {
			score += 2
		} else if strings.Contains(normalized, token) {
			score += 1
		}
	}
	return score
}

func ScoreLabelBoost(labels []string, phrase string, tokens []string) int {
	score := 0
	for _, label := range labels {
		normalized := Normalize(label)
		if phrase != "" && strings.Contains(normalized, phrase) {
			score += 2
		}
		for _, token := range tokens {
			if strings.Contains(normalized, token) {
				score++
			}
		}
	}
	return score
}

func collectFullTextTokens(value string, dedupe bool) []string {
	tokens := make([]string, 0, 32)
	seen := map[string]struct{}{}
	for _, token := range TokenizeSearchText(value, 2) {
		if containsCJK(token) {
			for _, item := range expandCJKToken(token, dedupe) {
				if !pushToken(&tokens, seen, item, dedupe) {
					continue
				}
			}
			continue
		}
		pushToken(&tokens, seen, token, dedupe)
	}
	return tokens
}

func expandCJKToken(token string, dedupe bool) []string {
	runes := []rune(token)
	result := make([]string, 0, len(runes)*2)
	if len(runes) >= 2 && len(runes) <= 12 {
		result = append(result, token)
	}
	for size := 2; size <= 3; size++ {
		if len(runes) < size {
			continue
		}
		for start := 0; start+size <= len(runes); start++ {
			result = append(result, string(runes[start:start+size]))
		}
	}
	if !dedupe {
		return result
	}
	seen := map[string]struct{}{}
	deduped := result[:0]
	for _, item := range result {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		deduped = append(deduped, item)
	}
	return deduped
}

func containsCJK(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) || r == 'ー' {
			return true
		}
	}
	return false
}

func isSearchableToken(token string) bool {
	for _, r := range token {
		if r < '0' || r > '9' {
			return true
		}
	}
	return token == ""
}

func pushToken(tokens *[]string, seen map[string]struct{}, token string, dedupe bool) bool {
	token = strings.TrimSpace(token)
	if token == "" || !isSearchableToken(token) {
		return false
	}
	if !dedupe {
		*tokens = append(*tokens, token)
		return true
	}
	if _, ok := seen[token]; ok {
		return false
	}
	seen[token] = struct{}{}
	*tokens = append(*tokens, token)
	return true
}

func quoteTerm(term string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(term, `"`, `""`))
}
