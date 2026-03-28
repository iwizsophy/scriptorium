package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/content"
	"github.com/silvekt/scriptorium/internal/docsindex"
	"github.com/silvekt/scriptorium/internal/filesafe"
	"github.com/silvekt/scriptorium/internal/flow"
	"github.com/silvekt/scriptorium/internal/gitsnapshot"
	"github.com/silvekt/scriptorium/internal/guide"
	"github.com/silvekt/scriptorium/internal/protocol"
	"github.com/silvekt/scriptorium/internal/ref"
	"github.com/silvekt/scriptorium/internal/related"
	"github.com/silvekt/scriptorium/internal/search"
	"github.com/silvekt/scriptorium/internal/source"
	"github.com/silvekt/scriptorium/internal/textdecode"
	"golang.org/x/text/encoding/japanese"
)

func TestAcceptanceDiagnosticsPayloadIncludesRuntimeSections(t *testing.T) {
	filesafe.ResetTextFileCacheForTesting()
	source.ResetCodeSourceCacheForTesting()
	docsRoot := filepath.Join(t.TempDir(), "docs")
	mustWriteAcceptanceFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	sampleRoot := filepath.Join(t.TempDir(), "samples")
	mustWriteAcceptanceFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "export function run() {\n  return 1\n}\n")

	t.Setenv("SCRIPTORIUM_MARKDOWN_DIR", docsRoot)
	t.Setenv("SCRIPTORIUM_CODE_ROOTS", sampleRoot)
	t.Setenv("SCRIPTORIUM_INDEX_VERIFY", "full")
	t.Setenv("SCRIPTORIUM_CODE_EXTENSIONS", ".ts")

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
			"method":  "tools/call",
			"params": map[string]any{
				"name": "get_content",
				"arguments": map[string]any{
					"refId": "md:guide.md#guide",
					"mode":  "multi_range",
				},
			},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "get_content",
				"arguments": map[string]any{
					"refId": "md:guide.md#guide",
					"mode":  "multi_range",
				},
			},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      4,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "get_content",
				"arguments": map[string]any{
					"refId": "code:src/app.ts@L1",
					"mode":  "snippet",
				},
			},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      5,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "get_content",
				"arguments": map[string]any{
					"refId": "code:src/app.ts@L1",
					"mode":  "snippet",
				},
			},
		}),
		mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      6,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "diagnostics",
				"arguments": map[string]any{},
			},
		}),
	)
	var stdout bytes.Buffer
	if err := RunServerStream(strings.NewReader(input), &stdout, ioDiscard{}); err != nil {
		t.Fatalf("RunServerStream returned error: %v", err)
	}

	responses := readFramedResponses(t, stdout.Bytes())
	if len(responses) != 6 {
		t.Fatalf("expected 6 framed responses, got %d", len(responses))
	}
	var rpc struct {
		Result protocol.ToolResult `json:"result"`
	}
	if err := json.Unmarshal(responses[5], &rpc); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	payload, ok := rpc.Result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected diagnostics payload map, got %#v", rpc.Result.StructuredContent)
	}
	for _, key := range []string{"server", "runtime", "docs", "code", "caches"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing diagnostics key %q in %#v", key, payload)
		}
	}
	caches, ok := payload["caches"].(map[string]any)
	if !ok {
		t.Fatalf("expected caches payload map, got %#v", payload["caches"])
	}
	textFiles, ok := caches["textFiles"].(map[string]any)
	if !ok {
		t.Fatalf("expected textFiles cache map, got %#v", caches["textFiles"])
	}
	sampleRoots, ok := caches["sampleRoots"].(map[string]any)
	if !ok {
		t.Fatalf("expected sampleRoots cache map, got %#v", caches["sampleRoots"])
	}
	if asFloat(t, textFiles["entries"]) < 1 || asFloat(t, textFiles["hits"]) < 1 || asFloat(t, textFiles["misses"]) < 1 {
		t.Fatalf("expected live text file cache stats, got %#v", textFiles)
	}
	if asFloat(t, textFiles["evictions"]) != 0 {
		t.Fatalf("expected no text file cache evictions in acceptance test, got %#v", textFiles)
	}
	if asFloat(t, sampleRoots["entries"]) < 1 || asFloat(t, sampleRoots["hits"]) < 1 || asFloat(t, sampleRoots["misses"]) < 1 {
		t.Fatalf("expected live sample root cache stats, got %#v", sampleRoots)
	}
	if asFloat(t, sampleRoots["filesystemRoots"]) != 1 || asFloat(t, sampleRoots["snapshotRoots"]) != 0 || asFloat(t, sampleRoots["evictions"]) != 0 {
		t.Fatalf("expected sample root cache breakdown, got %#v", sampleRoots)
	}
}

func TestAcceptanceDiagnosticsPayloadIncludesSnapshotSampleSource(t *testing.T) {
	filesafe.ResetTextFileCacheForTesting()
	source.ResetCodeSourceCacheForTesting()
	docsRoot := filepath.Join(t.TempDir(), "docs")
	mustWriteAcceptanceFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	mustWriteAcceptanceFile(t, filepath.Join(repo, "src", "branch.ts"), "export function branchOnly() {\n  return 1\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build snapshot returned error: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := gitsnapshot.Write(snapshotPath, snapshot); err != nil {
		t.Fatalf("Write snapshot returned error: %v", err)
	}

	cfg := config.Runtime{
		ServerName:       "scriptorium",
		DocsRoot:         docsRoot,
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
		MCPBaseDir:       t.TempDir(),
		DocsIndexVerify:  "full",
	}
	if _, _, err := source.BuildCodeSources(cfg); err != nil {
		t.Fatalf("BuildCodeSources returned error: %v", err)
	}
	payload := buildDiagnosticsPayload(cfg)

	if len(payload.Code.SampleSources) != 1 {
		t.Fatalf("expected one snapshot sample source, got %#v", payload.Code.SampleSources)
	}
	if payload.Code.SampleSources[0].Kind != "git_snapshot" || payload.Caches.SampleRoots.SnapshotRoots != 1 {
		t.Fatalf("expected snapshot sample source coverage, got payload=%#v caches=%#v", payload.Code.SampleSources, payload.Caches.SampleRoots)
	}
	if payload.Server.Runtime != "go" || payload.Server.RuntimeVersion == "" {
		t.Fatalf("expected runtime metadata, got %#v", payload.Server)
	}
}

func TestAcceptanceChecklistBehaviors(t *testing.T) {
	docsRoot := t.TempDir()
	sampleRoot := t.TempDir()

	mustWriteAcceptanceFile(t, filepath.Join(docsRoot, "auth", "guide.md"), "# 認証フロー\nログインの説明\n")
	mustWriteAcceptanceFile(t, filepath.Join(docsRoot, "flow.md"), "# Flow\n1. step one\n2. step two\n")
	mustWriteAcceptanceFile(t, filepath.Join(sampleRoot, "src", "app.ts"), "const start = 1\nconst specialToken = 1\nconst specialToken = 2\nreturn specialToken\n")

	cfg := config.Runtime{
		DocsRoot:       docsRoot,
		SampleRoots:    []string{sampleRoot},
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}

	if slug := ref.SlugifyHeading("認証フロー", map[string]struct{}{}); slug != "認証フロー" {
		t.Fatalf("unexpected slug: %q", slug)
	}

	docsOnlyCfg := config.Runtime{
		DocsRoot:       docsRoot,
		CodeExtensions: []string{".ts"},
		MaxFileBytes:   1_000_000,
	}
	docsOnlyResult, err := search.Execute(docsOnlyCfg, search.Request{Query: "認証"})
	if err != nil {
		t.Fatalf("docs-only search returned error: %v", err)
	}
	if len(docsOnlyResult.Results) != 1 || docsOnlyResult.Results[0].Kind != "md_heading_block" {
		t.Fatalf("unexpected docs-only search result: %#v", docsOnlyResult.Results)
	}

	japaneseResult, err := search.Execute(cfg, search.Request{Query: "認証", TopK: 5, SnippetLines: 2})
	if err != nil {
		t.Fatalf("japanese search returned error: %v", err)
	}
	if len(japaneseResult.Results) != 1 || japaneseResult.Results[0].Path != "auth/guide.md" {
		t.Fatalf("unexpected japanese search result: %#v", japaneseResult.Results)
	}

	pathResult, err := search.Execute(cfg, search.Request{Query: "guide", TopK: 5, SnippetLines: 2})
	if err != nil {
		t.Fatalf("path search returned error: %v", err)
	}
	if len(pathResult.Results) != 1 || pathResult.Results[0].Path != "auth/guide.md" {
		t.Fatalf("unexpected path result: %#v", pathResult.Results)
	}

	codeResult, err := search.Execute(cfg, search.Request{Query: "specialToken", TopK: 10, SnippetLines: 5, Extensions: []string{".ts"}})
	if err != nil {
		t.Fatalf("code search returned error: %v", err)
	}
	if len(codeResult.Results) != 1 || codeResult.Results[0].Range.StartLine != 1 || codeResult.Results[0].Range.EndLine != 4 {
		t.Fatalf("unexpected clustered code result: %#v", codeResult.Results)
	}

	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	mustWriteAcceptanceFile(t, filepath.Join(repo, "src", "branch.ts"), "export function branchOnly() {\n  return 1\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "checkout", "-b", "feature/label")
	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "feature/label",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build snapshot returned error: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := gitsnapshot.Write(snapshotPath, snapshot); err != nil {
		t.Fatalf("Write snapshot returned error: %v", err)
	}
	snapshotCfg := config.Runtime{
		DocsRoot:         docsRoot,
		GitSnapshotPaths: []string{snapshotPath},
		CodeExtensions:   []string{".ts"},
		MaxFileBytes:     1_000_000,
	}
	labelResult, err := search.Execute(snapshotCfg, search.Request{Query: "feature/label", Extensions: []string{".ts"}})
	if err != nil {
		t.Fatalf("label fallback search returned error: %v", err)
	}
	if len(labelResult.Results) == 0 || labelResult.Results[0].Kind != "code_range" || labelResult.Results[0].RefID != "code:@feature-label/src/branch.ts@L1" {
		t.Fatalf("unexpected label fallback result: %#v", labelResult.Results)
	}

	codeFullFallback, err := content.Execute(cfg, content.Request{RefID: "code:src/app.ts@L4", Mode: "full", MaxLines: 3})
	if err != nil {
		t.Fatalf("code full fallback returned error: %v", err)
	}
	if !codeFullFallback.Truncated || codeFullFallback.Mode != "snippet" {
		t.Fatalf("unexpected code full fallback: %#v", codeFullFallback)
	}

	codeMultiRange, err := content.Execute(cfg, content.Request{RefID: "code:src/app.ts@L4", Mode: "multi_range"})
	if err != nil {
		t.Fatalf("code multi_range returned error: %v", err)
	}
	if !strings.Contains(codeMultiRange.Content, "@@ L") {
		t.Fatalf("expected marker content, got %q", codeMultiRange.Content)
	}

	relatedResponse, err := related.Execute(cfg, related.Request{Seeds: []string{"code:src/app.ts@L2"}})
	if err != nil {
		t.Fatalf("expand_related returned error: %v", err)
	}
	if len(relatedResponse.Related) == 0 || len(relatedResponse.Related[0].Reasons) == 0 {
		t.Fatalf("unexpected related response: %#v", relatedResponse)
	}

	flowResponse, err := flow.Execute(cfg, flow.Request{
		Seeds:   []string{"md:flow.md#flow"},
		Sources: []string{"code:src/app.ts@L4"},
	})
	if err != nil {
		t.Fatalf("summarize_flow returned error: %v", err)
	}
	if len(flowResponse.Flow) < 3 {
		t.Fatalf("expected merged flow steps, got %#v", flowResponse.Flow)
	}
}

func TestAcceptanceArtifactsDecodeAndSafety(t *testing.T) {
	docsRoot := t.TempDir()
	mustWriteAcceptanceFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	index, err := docsindex.Build(docsRoot, nil, false)
	if err != nil {
		t.Fatalf("Build docs index returned error: %v", err)
	}
	if index.Meta.SourceFingerprint == "" || index.Meta.FileCount != 1 || index.Meta.SourceMaxMtimeMs == 0 {
		t.Fatalf("unexpected docs index meta: %#v", index.Meta)
	}

	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	mustWriteAcceptanceFile(t, filepath.Join(repo, "src", "app.ts"), "export function run() {\n  return 1\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	snapshot, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       repo,
		Samples:        "HEAD",
		CodeExtensions: []string{".ts"},
	})
	if err != nil {
		t.Fatalf("Build snapshot returned error: %v", err)
	}
	if snapshot.Meta.Fingerprint == "" || snapshot.Meta.RootCount != 1 || snapshot.Meta.FileCount != 1 {
		t.Fatalf("unexpected snapshot meta: %#v", snapshot.Meta)
	}

	utf16, err := textdecode.Decode([]byte{0xFF, 0xFE, 'A', 0x00, 'B', 0x00}, nil)
	if err != nil || utf16.Text != "AB" {
		t.Fatalf("unexpected utf16 decode result: %#v err=%v", utf16, err)
	}

	shiftJISBytes, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte("テスト"))
	if err != nil {
		t.Fatalf("shift_jis encode returned error: %v", err)
	}
	decoded, err := textdecode.Decode(shiftJISBytes, []string{"shift_jis"})
	if err != nil || decoded.Text != "テスト" {
		t.Fatalf("unexpected shift_jis decode result: %#v err=%v", decoded, err)
	}

	rootDir := t.TempDir()
	root, err := filesafe.NewRoot(rootDir, false, 4)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	if _, err := root.Resolve("../secret.txt"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}

	mustWriteAcceptanceFile(t, filepath.Join(rootDir, "large.txt"), "0123456789")
	if _, err := root.ReadText("large.txt", nil); err == nil {
		t.Fatal("expected oversized file to be rejected")
	}

	binaryDir := t.TempDir()
	binaryRoot, err := filesafe.NewRoot(binaryDir, false, 1_000_000)
	if err != nil {
		t.Fatalf("NewRoot returned error: %v", err)
	}
	binaryPath := filepath.Join(binaryDir, "binary.dat")
	if err := os.WriteFile(binaryPath, []byte{0x00, 0x01, 0x00, 0x02, 0x00}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, err := binaryRoot.ReadText("binary.dat", nil); !errors.Is(err, textdecode.ErrBinary) {
		t.Fatalf("expected ErrBinary, got %v", err)
	}
}

func TestAcceptanceGuideImplementationUsesDocsAndSampleCode(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	sampleRoot := filepath.Join(t.TempDir(), "samples")

	mustWriteAcceptanceFile(t, filepath.Join(docsRoot, "aspnet-auth.md"), strings.Join([]string{
		"# ASP.NET Core Authentication",
		"Use ASP.NET Core authentication in C#.",
		"1. Add authentication services.",
		"2. Map a protected endpoint.",
	}, "\n"))
	mustWriteAcceptanceFile(t, filepath.Join(sampleRoot, "Program.cs"), strings.Join([]string{
		"var builder = WebApplication.CreateBuilder(args);",
		"builder.Services.AddAuthentication();",
		"var app = builder.Build();",
		`app.MapGet("/secure", async () => {`,
		"    await authService.ValidateAsync();",
		"    return Results.Ok();",
		"});",
	}, "\n"))

	cfg := config.Runtime{
		ServerName:      "scriptorium",
		DocsRoot:        docsRoot,
		SampleRoots:     []string{sampleRoot},
		CodeExtensions:  []string{".cs"},
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	}

	result := ExecuteGuideImplementation(cfg, guide.Request{
		Topic:      "authentication",
		Framework:  "ASP.NET Core",
		Language:   "csharp",
		Preference: "balanced",
		MaxRefs:    4,
	}, io.Discard)
	if result.IsError {
		t.Fatalf("expected guide_implementation success, got %#v", result)
	}

	payload, ok := result.StructuredContent.(guide.Response)
	if !ok {
		t.Fatalf("expected guide response payload, got %#v", result.StructuredContent)
	}
	if len(payload.Docs) == 0 || len(payload.SampleCode) == 0 {
		t.Fatalf("expected docs and sample refs, got %#v", payload)
	}
	if payload.Confidence == "low" {
		t.Fatalf("expected grounded confidence, got %#v", payload)
	}
}

func TestAcceptanceDiagnosticsPayloadIncludesRetrievalObservability(t *testing.T) {
	docsRoot := filepath.Join(t.TempDir(), "docs")
	sampleRoot := filepath.Join(t.TempDir(), "samples")
	mustWriteAcceptanceFile(t, filepath.Join(docsRoot, "guide.md"), "# Guide\nbody\n")
	mustWriteAcceptanceFile(t, filepath.Join(sampleRoot, "Program.cs"), "builder.Services.AddAuthentication();\n")

	mixedPayload := buildDiagnosticsPayload(config.Runtime{
		ServerName:      "scriptorium",
		DocsRoot:        docsRoot,
		SampleRoots:     []string{sampleRoot},
		CodeExtensions:  []string{".cs"},
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	})
	if !mixedPayload.Retrieval.Corpus.DocsAvailable || !mixedPayload.Retrieval.Corpus.SampleCodeAvailable {
		t.Fatalf("expected mixed corpus availability, got %#v", mixedPayload.Retrieval)
	}
	if !mixedPayload.Retrieval.Corpus.CrossSourceRelations {
		t.Fatalf("expected cross-source relation coverage, got %#v", mixedPayload.Retrieval)
	}

	docsOnlyPayload := buildDiagnosticsPayload(config.Runtime{
		ServerName:      "scriptorium",
		DocsRoot:        docsRoot,
		CodeExtensions:  []string{".cs"},
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	})
	if !docsOnlyPayload.Retrieval.Fallback.SnippetOnlyLikely {
		t.Fatalf("expected snippetOnlyLikely for docs-only payload, got %#v", docsOnlyPayload.Retrieval)
	}
	if len(docsOnlyPayload.Retrieval.Warnings) == 0 {
		t.Fatalf("expected diagnostics warnings for docs-only payload, got %#v", docsOnlyPayload.Retrieval)
	}

	samplesOnlyPayload := buildDiagnosticsPayload(config.Runtime{
		ServerName:      "scriptorium",
		DocsRoot:        t.TempDir(),
		SampleRoots:     []string{sampleRoot},
		CodeExtensions:  []string{".cs"},
		DocsIndexVerify: "full",
		MaxFileBytes:    1_000_000,
	})
	if !samplesOnlyPayload.Retrieval.Corpus.SampleCodeAvailable || samplesOnlyPayload.Retrieval.Corpus.DocsAvailable {
		t.Fatalf("expected samples-only corpus visibility, got %#v", samplesOnlyPayload.Retrieval)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func mustWriteAcceptanceFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	return string(data)
}

func framedMessages(messages ...string) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString("Content-Length: ")
		builder.WriteString(strconv.Itoa(len(message)))
		builder.WriteString("\r\n\r\n")
		builder.WriteString(message)
	}
	return builder.String()
}

func readFramedResponses(t *testing.T, data []byte) [][]byte {
	t.Helper()
	reader := bytes.NewReader(data)
	responses := make([][]byte, 0, 4)
	for reader.Len() > 0 {
		var header bytes.Buffer
		for {
			b, err := reader.ReadByte()
			if err != nil {
				t.Fatalf("ReadByte returned error: %v", err)
			}
			header.WriteByte(b)
			if strings.HasSuffix(header.String(), "\r\n\r\n") {
				break
			}
		}
		headerText := header.String()
		var contentLength int
		if _, err := fmt.Sscanf(headerText, "Content-Length: %d\r\n\r\n", &contentLength); err != nil {
			t.Fatalf("failed to parse header %q: %v", headerText, err)
		}
		payload := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, payload); err != nil {
			t.Fatalf("ReadFull returned error: %v", err)
		}
		responses = append(responses, payload)
	}
	return responses
}

func asFloat(t *testing.T, value any) float64 {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("expected float64 JSON number, got %#v", value)
	}
	return number
}
