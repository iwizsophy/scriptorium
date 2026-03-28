package protocol

import (
	"encoding/json"
	"fmt"
)

type ToolResult struct {
	Content           []TextContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ErrorPayload struct {
	Error map[string]any `json:"error"`
}

func CreateToolResult(payload any, summary string) ToolResult {
	return ToolResult{
		Content: []TextContent{
			{
				Type: "text",
				Text: renderToolText(payload, summary),
			},
		},
		StructuredContent: payload,
	}
}

func CreateToolErrorResult(message string, details map[string]any) ToolResult {
	errorBody := map[string]any{
		"message": message,
	}
	for key, value := range details {
		errorBody[key] = value
	}

	payload := ErrorPayload{
		Error: errorBody,
	}

	return ToolResult{
		Content: []TextContent{
			{
				Type: "text",
				Text: renderToolText(payload, message),
			},
		},
		StructuredContent: payload,
		IsError:           true,
	}
}

func renderToolText(payload any, summary string) string {
	pretty, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		if summary == "" {
			return fmt.Sprintf(`{"error":{"message":"%s"}}`, err.Error())
		}
		return summary
	}
	if summary == "" {
		return string(pretty)
	}
	return summary + "\n\n" + string(pretty)
}
