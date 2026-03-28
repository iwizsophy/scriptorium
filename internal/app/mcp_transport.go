package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/content"
	"github.com/iwizsophy/scriptorium/internal/flow"
	"github.com/iwizsophy/scriptorium/internal/guide"
	"github.com/iwizsophy/scriptorium/internal/protocol"
	"github.com/iwizsophy/scriptorium/internal/related"
	"github.com/iwizsophy/scriptorium/internal/search"
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type transportMode int

const (
	transportModeUnset transportMode = iota
	transportModeLineDelimited
	transportModeContentLength
)

const (
	legacyProtocolVersion  = "2024-11-05"
	currentProtocolVersion = "2025-11-25"
)

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion,omitempty"`
}

func RunServer(_ []string, stdout io.Writer, stderr io.Writer) error {
	return RunServerStream(defaultStdinReader(), stdout, stderr)
}

func RunServerStream(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := config.LoadRuntime()
	if err != nil {
		return err
	}

	logStartup(cfg, stderr)

	reader := bufio.NewReader(stdin)
	for {
		payload, mode, err := readTransportMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		response, respond := handleRPC(cfg, payload, stderr)
		if !respond {
			continue
		}
		if err := writeTransportMessage(stdout, response, mode); err != nil {
			return err
		}
	}
}

func handleRPC(cfg config.Runtime, payload []byte, stderr io.Writer) ([]byte, bool) {
	var request jsonRPCRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		response := jsonRPCResponse{
			JSONRPC: "2.0",
			Error: &jsonRPCError{
				Code:    -32700,
				Message: "parse error",
			},
		}
		data, _ := json.Marshal(response)
		return data, true
	}

	if request.Method == "notifications/initialized" || strings.HasPrefix(request.Method, "notifications/") {
		return nil, false
	}

	switch request.Method {
	case "initialize":
		var params initializeParams
		if len(request.Params) > 0 {
			_ = json.Unmarshal(request.Params, &params)
		}
		response := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result: map[string]any{
				"protocolVersion": negotiateProtocolVersion(params.ProtocolVersion),
				"serverInfo": map[string]any{
					"name":    cfg.ServerName,
					"version": ServerVersion,
				},
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
				},
			},
		}
		data, _ := json.Marshal(response)
		return data, true
	case "ping":
		response := jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{}}
		data, _ := json.Marshal(response)
		return data, true
	case "tools/list":
		response := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result: map[string]any{
				"tools": toolDefinitions(cfg),
			},
		}
		data, _ := json.Marshal(response)
		return data, true
	case "tools/call":
		result := executeToolCall(cfg, request.Params, stderr)
		response := jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: result}
		data, _ := json.Marshal(response)
		return data, true
	default:
		response := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &jsonRPCError{
				Code:    -32601,
				Message: "method not found",
			},
		}
		data, _ := json.Marshal(response)
		return data, true
	}
}

func executeToolCall(cfg config.Runtime, params json.RawMessage, stderr io.Writer) protocol.ToolResult {
	var call toolsCallParams
	if err := json.Unmarshal(params, &call); err != nil {
		return protocol.CreateToolErrorResult("tool call failed", map[string]any{
			"message": "invalid tools/call params",
		})
	}

	call.Name = canonicalToolName(cfg, call.Name)

	switch call.Name {
	case "diagnostics":
		return ExecuteDiagnostics(cfg, stderr)
	case "search":
		var input search.Request
		if err := decodeArguments(call.Arguments, &input); err != nil {
			return protocol.CreateToolErrorResult("search failed", map[string]any{"message": err.Error()})
		}
		return ExecuteSearch(cfg, input, stderr)
	case "get_content":
		var input content.Request
		if err := decodeArguments(call.Arguments, &input); err != nil {
			return protocol.CreateToolErrorResult("get_content failed", map[string]any{"message": err.Error()})
		}
		return ExecuteGetContent(cfg, input, stderr)
	case "expand_related":
		var input related.Request
		if err := decodeArguments(call.Arguments, &input); err != nil {
			return protocol.CreateToolErrorResult("expand_related failed", map[string]any{"message": err.Error()})
		}
		return ExecuteExpandRelated(cfg, input, stderr)
	case "summarize_flow":
		var input flow.Request
		if err := decodeArguments(call.Arguments, &input); err != nil {
			return protocol.CreateToolErrorResult("summarize_flow failed", map[string]any{"message": err.Error()})
		}
		return ExecuteSummarizeFlow(cfg, input, stderr)
	case "guide_implementation":
		var input guide.Request
		if err := decodeArguments(call.Arguments, &input); err != nil {
			return protocol.CreateToolErrorResult("guide_implementation failed", map[string]any{"message": err.Error()})
		}
		return ExecuteGuideImplementation(cfg, input, stderr)
	default:
		return protocol.CreateToolErrorResult("unknown tool", map[string]any{
			"tool":    call.Name,
			"message": fmt.Sprintf("unknown tool: %s", call.Name),
		})
	}
}

func decodeArguments(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	return json.Unmarshal(raw, target)
}

func toolDefinitions(cfg config.Runtime) []toolDefinition {
	return []toolDefinition{
		{
			Name:        advertisedToolName(cfg, "diagnostics"),
			Description: toolDescription(cfg, "Return runtime diagnostics information, including the active MCP profile and corpus coverage."),
			InputSchema: map[string]any{"type": "object", "additionalProperties": false},
		},
		{
			Name:        advertisedToolName(cfg, "search"),
			Description: toolDescription(cfg, "Search imported docs and code references."),
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":        map[string]any{"type": "string"},
					"extensions":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"topK":         map[string]any{"type": "integer"},
					"snippetLines": map[string]any{"type": "integer"},
					"mode":         map[string]any{"type": "string"},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
		{
			Name:        advertisedToolName(cfg, "get_content"),
			Description: toolDescription(cfg, "Resolve a refId to imported content."),
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"refId":        map[string]any{"type": "string"},
					"mode":         map[string]any{"type": "string"},
					"snippetLines": map[string]any{"type": "integer"},
					"maxLines":     map[string]any{"type": "integer"},
				},
				"required":             []string{"refId"},
				"additionalProperties": false,
			},
		},
		{
			Name:        advertisedToolName(cfg, "expand_related"),
			Description: toolDescription(cfg, "Expand related references from seed refs."),
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"seeds":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"extensions":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"budget":       map[string]any{"type": "integer"},
					"signals":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"snippetLines": map[string]any{"type": "integer"},
					"mode":         map[string]any{"type": "string"},
				},
				"required":             []string{"seeds"},
				"additionalProperties": false,
			},
		},
		{
			Name:        advertisedToolName(cfg, "summarize_flow"),
			Description: toolDescription(cfg, "Build a deterministic flow summary from imported refs."),
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic":    map[string]any{"type": "string"},
					"seeds":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"sources":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"style":    map[string]any{"type": "string"},
					"maxSteps": map[string]any{"type": "integer"},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        advertisedToolName(cfg, "guide_implementation"),
			Description: toolDescription(cfg, "Build grounded implementation guidance from imported docs and sample code."),
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic":      map[string]any{"type": "string"},
					"framework":  map[string]any{"type": "string"},
					"language":   map[string]any{"type": "string"},
					"preference": map[string]any{"type": "string"},
					"maxRefs":    map[string]any{"type": "integer"},
				},
				"required":             []string{"topic"},
				"additionalProperties": false,
			},
		},
	}
}

func canonicalToolName(cfg config.Runtime, name string) string {
	for _, canonical := range []string{
		"diagnostics",
		"search",
		"get_content",
		"expand_related",
		"summarize_flow",
		"guide_implementation",
	} {
		if name == canonical || name == advertisedToolName(cfg, canonical) {
			return canonical
		}
	}
	return name
}

func advertisedToolName(cfg config.Runtime, base string) string {
	prefix := cfg.Profile.ToolPrefix
	if prefix == "" {
		return base
	}
	if strings.HasSuffix(prefix, "_") || strings.HasSuffix(prefix, "-") {
		return prefix + base
	}
	return prefix + "_" + base
}

func toolDescription(cfg config.Runtime, action string) string {
	action = strings.TrimSpace(action)
	action = strings.TrimRight(action, ".")
	parts := []string{action + " for " + cfg.Profile.DomainDescription + "."}
	if cfg.Profile.CorpusSummary != "" {
		parts = append(parts, "Current corpus: "+cfg.Profile.CorpusSummary+".")
	}
	return strings.Join(parts, " ")
}

func negotiateProtocolVersion(requested string) string {
	switch requested {
	case legacyProtocolVersion, currentProtocolVersion:
		return requested
	case "":
		return legacyProtocolVersion
	default:
		return currentProtocolVersion
	}
}

func readTransportMessage(reader *bufio.Reader) ([]byte, transportMode, error) {
	for {
		firstBytes, err := reader.Peek(1)
		if err != nil {
			return nil, transportModeUnset, err
		}
		switch firstBytes[0] {
		case '\r', '\n':
			if _, err := reader.ReadByte(); err != nil {
				return nil, transportModeUnset, err
			}
			continue
		case '{', '[':
			line, err := reader.ReadBytes('\n')
			if err != nil && err != io.EOF {
				return nil, transportModeUnset, err
			}
			return bytes.TrimSpace(line), transportModeLineDelimited, nil
		default:
			payload, err := readFramedMessage(reader)
			return payload, transportModeContentLength, err
		}
	}
}

func readFramedMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			var length int
			if _, err := fmt.Sscanf(line, "Content-Length: %d", &length); err == nil {
				contentLength = length
				continue
			}
			if _, err := fmt.Sscanf(line, "content-length: %d", &length); err == nil {
				contentLength = length
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeTransportMessage(writer io.Writer, payload []byte, mode transportMode) error {
	if mode == transportModeLineDelimited {
		_, err := writer.Write(append(payload, '\n'))
		return err
	}
	return writeFramedMessage(writer, payload)
}

func writeFramedMessage(writer io.Writer, payload []byte) error {
	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload)))
	buffer.Write(payload)
	_, err := writer.Write(buffer.Bytes())
	return err
}

func defaultStdinReader() io.Reader {
	return os.Stdin
}
