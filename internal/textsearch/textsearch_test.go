package textsearch

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildFullTextSearchContentExpandsMixedJapaneseASCII(t *testing.T) {
	content := BuildFullTextSearchContent("生成AIが話題になっている")
	for _, token := range []string{"ai", "生成", "話題"} {
		if !strings.Contains(content, token) {
			t.Fatalf("expected token %q in content %q", token, content)
		}
	}
}

func TestBuildFullTextSearchQuerySupportsMixedJapaneseASCII(t *testing.T) {
	if got := BuildFullTextSearchQuery("AI"); got != `"ai"` {
		t.Fatalf("unexpected AI query: %q", got)
	}

	got := BuildFullTextSearchQuery("生成AI")
	for _, token := range []string{`"生成"`, `"ai"`} {
		if !strings.Contains(got, token) {
			t.Fatalf("expected token %q in query %q", token, got)
		}
	}
}

func TestTokenizeOverlapTextKeepsFrequentUsefulTokens(t *testing.T) {
	tokens := TokenizeOverlapText("Auth auth auth token token 123 生成AI 生成AI", 10)

	for _, want := range []string{"auth", "token", "生成", "ai"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("expected overlap token %q in %#v", want, tokens)
		}
	}
	if slices.Contains(tokens, "123") {
		t.Fatalf("did not expect numeric-only token in %#v", tokens)
	}
}

func TestScorePathBoostPrefersBasenamePhraseMatches(t *testing.T) {
	scoreBase := ScorePathBoost("src/auth/login.ts", []string{"login"}, "login")
	scorePath := ScorePathBoost("src/login/auth.ts", []string{"login"}, "login")
	if scoreBase <= scorePath {
		t.Fatalf("expected basename phrase match to score higher: base=%d path=%d", scoreBase, scorePath)
	}
}

func TestScoreTokenMatchesAndLabelBoostUseNormalizedText(t *testing.T) {
	score := ScoreTokenMatches("認証フロー AddJwtBearer", []string{"認証", "addjwtbearer"}, "認証", 1, 2)
	if score < 4 {
		t.Fatalf("expected combined token and phrase score, got %d", score)
	}

	labelScore := ScoreLabelBoost([]string{"Feature/Auth", "認証フロー"}, "認証", []string{"feature", "auth"})
	if labelScore < 4 {
		t.Fatalf("expected normalized label score, got %d", labelScore)
	}
}

func TestTokenizeSearchAndQueryEdgeBranches(t *testing.T) {
	tokens := TokenizeSearchText("A a bb BB cc", 0)
	if !slices.Equal(tokens, []string{"a", "bb", "cc"}) {
		t.Fatalf("unexpected normalized tokens: %#v", tokens)
	}

	if got := BuildFullTextSearchQuery("  "); got != "" {
		t.Fatalf("expected empty FTS query for whitespace input, got %q", got)
	}
	if got := BuildFullTextSearchQuery(`say "hello"`); !strings.Contains(got, `"hello"`) {
		t.Fatalf("expected quoted token in FTS query, got %q", got)
	}
}

func TestOverlapAndHelperBranches(t *testing.T) {
	if tokens := TokenizeOverlapText("12 34 56", 0); len(tokens) != 0 {
		t.Fatalf("expected numeric-only overlap tokens to be filtered, got %#v", tokens)
	}

	if got := collectFullTextTokens("auth auth", false); !slices.Equal(got, []string{"auth"}) {
		t.Fatalf("expected TokenizeSearchText dedupe to flow through, got %#v", got)
	}

	expanded := expandCJKToken("生成生成", false)
	if len(expanded) == 0 || !slices.Contains(expanded, "生成") {
		t.Fatalf("expected expanded CJK token set, got %#v", expanded)
	}
	duplicateCount := 0
	for _, token := range expanded {
		if token == "生成" {
			duplicateCount++
		}
	}
	if duplicateCount < 2 {
		t.Fatalf("expected duplicate CJK expansions when dedupe is disabled, got %#v", expanded)
	}

	tokens := make([]string, 0, 2)
	seen := map[string]struct{}{}
	if !pushToken(&tokens, seen, "auth", true) {
		t.Fatal("expected first token insert")
	}
	if pushToken(&tokens, seen, "auth", true) {
		t.Fatal("did not expect duplicate token insert with dedupe")
	}
	if pushToken(&tokens, seen, "123", true) {
		t.Fatal("did not expect numeric-only token insert")
	}
	if !pushToken(&tokens, seen, "auth", false) {
		t.Fatal("expected duplicate token insert when dedupe is disabled")
	}
	if !slices.Equal(tokens, []string{"auth", "auth"}) {
		t.Fatalf("unexpected pushed tokens: %#v", tokens)
	}
}

func TestAdditionalTokenizationAndCJKBranches(t *testing.T) {
	if tokens := TokenizeSearchText("... !!!", 4); len(tokens) != 0 {
		t.Fatalf("expected punctuation-only input to produce no tokens, got %#v", tokens)
	}
	if got := TokenizeSearchText("aa aaa aaaa", 4); !slices.Equal(got, []string{"aaaa"}) {
		t.Fatalf("expected minLength filtering to keep only long tokens, got %#v", got)
	}

	if got := TokenizeOverlapText("alpha beta gamma", 2); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("expected maxTokens truncation with lexical tiebreaks, got %#v", got)
	}
	if got := TokenizeOverlapText("生成生成", 10); len(got) == 0 || got[0] != "生成" {
		t.Fatalf("expected higher-frequency CJK expansion to sort first, got %#v", got)
	}

	deduped := collectFullTextTokens("生成生成", true)
	seen := 0
	for _, token := range deduped {
		if token == "生成" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("expected deduped CJK token once, got %#v", deduped)
	}
	if got := collectFullTextTokens("生成AI 開発AI", true); !slices.Contains(got, "ai") {
		t.Fatalf("expected overlapping CJK expansions to retain shared tokens once, got %#v", got)
	}

	if got := expandCJKToken("生", true); len(got) != 0 {
		t.Fatalf("expected single-rune CJK token to produce no searchable expansions, got %#v", got)
	}

	longToken := "漢字かなカナ山川海空日月星"
	expanded := expandCJKToken(longToken, true)
	if len(expanded) == 0 {
		t.Fatal("expected long CJK token to expand into grams")
	}
	if slices.Contains(expanded, longToken) {
		t.Fatalf("did not expect oversized original token to be retained, got %#v", expanded)
	}
}
