package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwizsophy/scriptorium/internal/config"
)

func TestRunServerStreamListsToolsAndHandlesUnknownTool(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", docsRoot)
	t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "full")
	t.Setenv("SCRIPTORIUM_MCP_PROFILE", "azure")
	t.Setenv("SCRIPTORIUM_MCP_TOOL_PREFIX", "azure")
	t.Setenv("SCRIPTORIUM_MCP_DOMAIN_DESCRIPTION", "Azure architecture guidance")
	t.Setenv("SCRIPTORIUM_MCP_CORPUS_SUMMARY", "Azure docs and Bicep samples")

	input := framedMessages(
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params":  map[string]any{},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
			"params":  map[string]any{},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "unknown_tool",
				"arguments": map[string]any{},
			},
		}),
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunServerStream(strings.NewReader(input), &stdout, &stderr); err != nil {
		t.Fatalf("RunServerStream returned error: %v", err)
	}

	responses := readFramedResponses(t, stdout.Bytes())
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	var initializeResponse struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(responses[0], &initializeResponse); err != nil {
		t.Fatalf("Unmarshal initialize returned error: %v", err)
	}
	if initializeResponse.Result.ProtocolVersion != legacyProtocolVersion {
		t.Fatalf("expected empty initialize params to keep legacy protocol version, got %q", initializeResponse.Result.ProtocolVersion)
	}
	if initializeResponse.Result.ServerInfo.Name != "scriptorium-azure" {
		t.Fatalf("expected profile-derived server name, got %#v", initializeResponse.Result.ServerInfo)
	}

	var listResponse struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(responses[1], &listResponse); err != nil {
		t.Fatalf("Unmarshal list returned error: %v", err)
	}
	if len(listResponse.Result.Tools) != 6 {
		t.Fatalf("expected 6 tools, got %#v", listResponse.Result.Tools)
	}
	if listResponse.Result.Tools[0].Name != "azure_diagnostics" || listResponse.Result.Tools[len(listResponse.Result.Tools)-1].Name != "azure_guide_implementation" {
		t.Fatalf("expected prefixed tool names, got %#v", listResponse.Result.Tools)
	}
	if !strings.Contains(listResponse.Result.Tools[1].Description, "Azure architecture guidance") || !strings.Contains(listResponse.Result.Tools[1].Description, "Azure docs and Bicep samples") {
		t.Fatalf("expected profile-aware tool description, got %#v", listResponse.Result.Tools[1])
	}

	var callResponse struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(responses[2], &callResponse); err != nil {
		t.Fatalf("Unmarshal call returned error: %v", err)
	}
	if !callResponse.Result.IsError {
		t.Fatalf("expected unknown tool call to return tool error result, got %s", string(responses[2]))
	}
	if !strings.Contains(stderr.String(), "MCP server ready") {
		t.Fatalf("expected startup logs, got %q", stderr.String())
	}
}

func TestRunServerStreamSupportsLineDelimitedTransportAndVersionNegotiation(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", docsRoot)
	t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "full")

	input := lineDelimitedMessages(
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": currentProtocolVersion,
			},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "ping",
		}),
	)

	var stdout bytes.Buffer
	if err := RunServerStream(strings.NewReader(input), &stdout, io.Discard); err != nil {
		t.Fatalf("RunServerStream returned error: %v", err)
	}

	responses := readLineDelimitedResponses(t, stdout.String())
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}

	var initializeResponse struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(responses[0]), &initializeResponse); err != nil {
		t.Fatalf("Unmarshal initialize returned error: %v", err)
	}
	if initializeResponse.Result.ProtocolVersion != currentProtocolVersion {
		t.Fatalf("expected negotiated current protocol version, got %q", initializeResponse.Result.ProtocolVersion)
	}
	if !strings.Contains(responses[1], `"result":{}`) {
		t.Fatalf("expected ping response, got %q", responses[1])
	}
}

func TestHandleRPCParseErrorAndNotification(t *testing.T) {
	cfg := config.Runtime{ServerName: "scriptorium"}
	response, respond := handleRPC(cfg, []byte("{"), bytes.NewBuffer(nil))
	if !respond {
		t.Fatal("expected parse error response")
	}
	if !strings.Contains(string(response), "parse error") {
		t.Fatalf("expected parse error payload, got %s", string(response))
	}

	notification, respond := handleRPC(cfg, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), bytes.NewBuffer(nil))
	if respond || notification != nil {
		t.Fatalf("expected notifications to be ignored, got respond=%v payload=%s", respond, string(notification))
	}
}

func TestExecuteToolCallCoversDecodeAndUnknownMethodBranches(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	cfg := config.Runtime{
		ServerName:      "scriptorium",
		DocsRoot:        docsRoot,
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}

	cfg.Profile = config.MCPProfile{
		ID:                "azure",
		ToolPrefix:        "azure",
		DomainDescription: "Azure architecture guidance",
		CorpusSummary:     "Azure docs and Bicep samples",
	}

	invalid := executeToolCall(cfg, []byte(`{"name":"azure_search","arguments":"bad"}`), bytes.NewBuffer(nil))
	if !invalid.IsError {
		t.Fatalf("expected decode failure result, got %#v", invalid)
	}
	for _, params := range []string{
		`{"name":"azure_get_content","arguments":"bad"}`,
		`{"name":"azure_expand_related","arguments":"bad"}`,
		`{"name":"azure_summarize_flow","arguments":"bad"}`,
		`{"name":"azure_guide_implementation","arguments":"bad"}`,
	} {
		result := executeToolCall(cfg, []byte(params), bytes.NewBuffer(nil))
		if !result.IsError {
			t.Fatalf("expected decode failure result for %s, got %#v", params, result)
		}
	}

	pingPayload, respond := handleRPC(cfg, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), bytes.NewBuffer(nil))
	if !respond || !strings.Contains(string(pingPayload), `"result":{}`) {
		t.Fatalf("expected ping result, got respond=%v payload=%s", respond, string(pingPayload))
	}

	methodMissing, respond := handleRPC(cfg, []byte(`{"jsonrpc":"2.0","id":2,"method":"unknown"}`), bytes.NewBuffer(nil))
	if !respond || !strings.Contains(string(methodMissing), "method not found") {
		t.Fatalf("expected method-not-found response, got respond=%v payload=%s", respond, string(methodMissing))
	}
}

func TestRunServerUsesProcessStdinAndEOF(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", docsRoot)
	t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "full")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe returned error: %v", err)
	}
	defer reader.Close()
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	originalStdin := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = originalStdin
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunServer(nil, &stdout, &stderr); err != nil {
		t.Fatalf("RunServer returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no framed output on EOF-only stdin, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "MCP server ready") {
		t.Fatalf("expected startup log output, got %q", stderr.String())
	}
}

func TestRunServerStreamPropagatesWriteError(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", docsRoot)
	t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "full")

	input := framedMessages(
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "ping",
		}),
	)

	err := RunServerStream(strings.NewReader(input), errorWriter{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "closed pipe") {
		t.Fatalf("expected forced write failure, got %v", err)
	}
}

func TestRunServerStreamIgnoresNotificationsAndPropagatesReadErrors(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", docsRoot)
	t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "full")

	input := framedMessages(
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"method":  "notifications/custom",
			"params":  map[string]any{"ok": true},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "ping",
		}),
	)

	var stdout bytes.Buffer
	if err := RunServerStream(strings.NewReader(input), &stdout, io.Discard); err != nil {
		t.Fatalf("RunServerStream returned error: %v", err)
	}
	responses := readFramedResponses(t, stdout.Bytes())
	if len(responses) != 1 || !strings.Contains(string(responses[0]), `"result":{}`) {
		t.Fatalf("expected only the ping response, got %#v", responses)
	}

	err := RunServerStream(strings.NewReader("Content-Length: 4\r\n\r\n{}"), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("expected framed read error, got %v", err)
	}
}

func TestExecuteToolCallSuccessBranches(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(docsRoot, "guide.md"), []byte("# Guide\n1. first\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sampleRoot, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sampleRoot, "src", "app.ts"), []byte("export function run() {\n  return 1\n}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.Runtime{
		ServerName:      "scriptorium",
		DocsRoot:        docsRoot,
		SampleRoots:     []string{sampleRoot},
		CodeExtensions:  []string{".ts"},
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
		MCPBaseDir:      t.TempDir(),
		Profile: config.MCPProfile{
			ID:                "azure",
			ToolPrefix:        "azure",
			DomainDescription: "Azure architecture guidance",
			CorpusSummary:     "Azure docs and Bicep samples",
		},
	}

	cases := []struct {
		name   string
		params string
	}{
		{name: "diagnostics", params: `{"name":"diagnostics"}`},
		{name: "prefixed-diagnostics", params: `{"name":"azure_diagnostics"}`},
		{name: "search", params: `{"name":"search","arguments":{"query":"Guide"}}`},
		{name: "prefixed-search", params: `{"name":"azure_search","arguments":{"query":"Guide"}}`},
		{name: "get_content", params: `{"name":"get_content","arguments":{"refId":"md:guide.md#guide","mode":"snippet"}}`},
		{name: "expand_related", params: `{"name":"expand_related","arguments":{"seeds":["code:src/app.ts@L1"]}}`},
		{name: "summarize_flow", params: `{"name":"summarize_flow","arguments":{"seeds":["md:guide.md#guide"],"sources":["code:src/app.ts@L1"]}}`},
		{name: "guide_implementation", params: `{"name":"guide_implementation","arguments":{"topic":"Guide","language":"typescript","maxRefs":4}}`},
		{name: "decode-empty-arguments", params: `{"name":"search"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := executeToolCall(cfg, []byte(tc.params), &bytes.Buffer{})
			if result.IsError {
				t.Fatalf("expected success result, got %#v", result)
			}
		})
	}
}

func TestTransportHelperBranches(t *testing.T) {
	if err := decodeArguments([]byte(`{"query":"x"}`), &struct {
		Query string `json:"query"`
	}{}); err != nil {
		t.Fatalf("decodeArguments returned error: %v", err)
	}
	if err := decodeArguments(nil, &struct{}{}); err != nil {
		t.Fatalf("decodeArguments should accept empty payload, got %v", err)
	}

	defs := toolDefinitions(config.Runtime{})
	if len(defs) != 6 || defs[0].Name != "diagnostics" || defs[len(defs)-1].Name != "guide_implementation" {
		t.Fatalf("unexpected tool definitions: %#v", defs)
	}
	profiledDefs := toolDefinitions(config.Runtime{
		Profile: config.MCPProfile{
			ToolPrefix:        "azure",
			DomainDescription: "Azure architecture guidance",
			CorpusSummary:     "Azure docs and Bicep samples",
		},
	})
	if profiledDefs[0].Name != "azure_diagnostics" || !strings.Contains(profiledDefs[0].Description, "Azure architecture guidance") {
		t.Fatalf("expected profile-aware tool definitions, got %#v", profiledDefs[0])
	}
	if got := canonicalToolName(config.Runtime{Profile: config.MCPProfile{ToolPrefix: "azure"}}, "azure_search"); got != "search" {
		t.Fatalf("expected canonical tool name mapping, got %q", got)
	}
	if got := canonicalToolName(config.Runtime{Profile: config.MCPProfile{ToolPrefix: "azure"}}, "search"); got != "search" {
		t.Fatalf("expected canonical base tool name mapping, got %q", got)
	}
	if got := advertisedToolName(config.Runtime{Profile: config.MCPProfile{ToolPrefix: "azure"}}, "search"); got != "azure_search" {
		t.Fatalf("expected advertised prefixed tool name, got %q", got)
	}
	if got := advertisedToolName(config.Runtime{Profile: config.MCPProfile{ToolPrefix: "azure-"}}, "search"); got != "azure-search" {
		t.Fatalf("expected advertised tool name to respect explicit separator, got %q", got)
	}
	if got := toolDescription(config.Runtime{Profile: config.MCPProfile{DomainDescription: "Azure guidance", CorpusSummary: "Azure docs"}}, "Search imported docs."); got != "Search imported docs for Azure guidance. Current corpus: Azure docs." {
		t.Fatalf("unexpected profile-aware description: %q", got)
	}

	payload := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	var framed bytes.Buffer
	if err := writeFramedMessage(&framed, payload); err != nil {
		t.Fatalf("writeFramedMessage returned error: %v", err)
	}
	decoded, err := readFramedMessage(bufio.NewReader(bytes.NewReader(framed.Bytes())))
	if err != nil || !bytes.Equal(decoded, payload) {
		t.Fatalf("unexpected framed roundtrip: decoded=%s err=%v", string(decoded), err)
	}
	lowercasePayload := []byte(`{"jsonrpc":"2.0","id":2,"result":{"ok":true}}`)
	lowercaseFramed := "content-length: 40\r\n\r\n" + string(lowercasePayload[:40])
	if decoded, err := readFramedMessage(bufio.NewReader(strings.NewReader(lowercaseFramed))); err != nil || string(decoded) != string(lowercasePayload[:40]) {
		t.Fatalf("expected lowercase content-length header to work, got decoded=%q err=%v", string(decoded), err)
	}

	lineDelimited := payloadString(payload)
	if decoded, mode, err := readTransportMessage(bufio.NewReader(strings.NewReader(lineDelimited + "\n"))); err != nil || mode != transportModeLineDelimited || string(decoded) != lineDelimited {
		t.Fatalf("expected line-delimited transport message to decode, got decoded=%q mode=%v err=%v", string(decoded), mode, err)
	}
	var lineOutput bytes.Buffer
	if err := writeTransportMessage(&lineOutput, payload, transportModeLineDelimited); err != nil {
		t.Fatalf("writeTransportMessage returned error: %v", err)
	}
	if lineOutput.String() != lineDelimited+"\n" {
		t.Fatalf("expected line-delimited write output, got %q", lineOutput.String())
	}

	if got := negotiateProtocolVersion(legacyProtocolVersion); got != legacyProtocolVersion {
		t.Fatalf("expected legacy protocol version to echo, got %q", got)
	}
	if got := negotiateProtocolVersion(currentProtocolVersion); got != currentProtocolVersion {
		t.Fatalf("expected current protocol version to echo, got %q", got)
	}
	if got := negotiateProtocolVersion(""); got != legacyProtocolVersion {
		t.Fatalf("expected empty protocol version to use legacy compatibility default, got %q", got)
	}
	if got := negotiateProtocolVersion("2099-01-01"); got != currentProtocolVersion {
		t.Fatalf("expected unsupported protocol version to negotiate to current support, got %q", got)
	}

	if _, err := readFramedMessage(bufio.NewReader(strings.NewReader("Header: x\r\n\r\n{}"))); err == nil {
		t.Fatal("expected missing content-length error")
	}
	if _, err := readFramedMessage(bufio.NewReader(strings.NewReader("Content-Length: 5\r\n\r\n{}"))); err == nil {
		t.Fatal("expected short body read error")
	}

	originalStdin := os.Stdin
	defer func() { os.Stdin = originalStdin }()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe returned error: %v", err)
	}
	defer reader.Close()
	defer writer.Close()
	os.Stdin = reader
	if defaultStdinReader() != reader {
		t.Fatalf("expected defaultStdinReader to expose os.Stdin")
	}
}

func TestRunServerStreamReturnsConfigErrorWithoutDocsRoot(t *testing.T) {
	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", "")
	if err := RunServerStream(strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("expected RunServerStream to fail when runtime config is invalid")
	}
}

func TestExecuteToolCallRejectsMalformedParamsEnvelope(t *testing.T) {
	cfg := config.Runtime{ServerName: "scriptorium"}
	result := executeToolCall(cfg, []byte(`{"name":`), io.Discard)
	if !result.IsError {
		t.Fatalf("expected malformed tools/call params to return error result, got %#v", result)
	}
}

type errorWriter struct{}

func (errorWriter) Write(_ []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func lineDelimitedMessages(messages ...string) string {
	return strings.Join(messages, "\n") + "\n"
}

func readLineDelimitedResponses(t *testing.T, data string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(data), "\n")
	responses := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		responses = append(responses, line)
	}
	return responses
}

func payloadString(payload []byte) string {
	return string(payload)
}
