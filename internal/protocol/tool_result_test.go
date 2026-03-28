package protocol

import (
	"math"
	"strings"
	"testing"
)

func TestCreateToolResult(t *testing.T) {
	result := CreateToolResult(map[string]any{
		"results": []string{"a"},
	}, "Found 1 result.")

	if len(result.Content) != 1 {
		t.Fatalf("unexpected content length: %d", len(result.Content))
	}
	if !strings.Contains(result.Content[0].Text, "Found 1 result.") {
		t.Fatalf("summary missing from tool result: %q", result.Content[0].Text)
	}
	if result.StructuredContent == nil {
		t.Fatal("structuredContent should be set")
	}
}

func TestCreateToolErrorResult(t *testing.T) {
	result := CreateToolErrorResult("search requires query", map[string]any{
		"tool": "search",
	})

	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(result.Content[0].Text, "search requires query") {
		t.Fatalf("error summary missing: %q", result.Content[0].Text)
	}
}

func TestCreateToolResultWithoutSummaryRendersPrettyJSON(t *testing.T) {
	result := CreateToolResult(map[string]any{"ok": true}, "")
	if strings.TrimSpace(result.Content[0].Text) != "{\n  \"ok\": true\n}" {
		t.Fatalf("unexpected tool text without summary: %q", result.Content[0].Text)
	}
}

func TestRenderToolTextFallsBackToMarshalErrorPayload(t *testing.T) {
	got := renderToolText(map[string]any{"bad": math.Inf(1)}, "")
	if !strings.Contains(got, `"error":{"message":"json: unsupported value: +Inf"}`) {
		t.Fatalf("unexpected fallback error payload: %q", got)
	}
}

func TestRenderToolTextFallsBackToSummaryWhenMarshalFails(t *testing.T) {
	got := renderToolText(map[string]any{"bad": math.Inf(1)}, "summary")
	if got != "summary" {
		t.Fatalf("expected summary fallback, got %q", got)
	}
}
