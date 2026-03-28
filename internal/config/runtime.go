package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/iwizsophy/scriptorium/internal/textdecode"
)

type Runtime struct {
	ServerName            string
	Profile               MCPProfile
	MCPBaseDir            string
	DocsRoot              string
	DocsRootEnvVar        string
	SampleRoots           []string
	GitSnapshotPaths      []string
	DocsIndexPaths        []string
	DocsIndexVerify       string
	CodeExtensions        []string
	TextEncodingFallbacks []string
	AllowSymlinks         bool
	MaxFileBytes          int64
}

type MCPProfile struct {
	ID                string
	ToolPrefix        string
	DomainDescription string
	CorpusSummary     string
	ExampleQueries    []string
}

var (
	getwd   = os.Getwd
	absPath = filepath.Abs
)

func LoadRuntime() (Runtime, error) {
	baseDir := os.Getenv("SCRIPTORIUM_HOME")
	if strings.TrimSpace(baseDir) == "" {
		wd, err := getwd()
		if err != nil {
			return Runtime{}, err
		}
		baseDir = wd
	}
	baseDir, err := absPath(baseDir)
	if err != nil {
		return Runtime{}, err
	}

	docsRoot, docsRootEnvVar := resolveDocsRootEnv()
	if docsRoot == "" {
		return Runtime{}, errors.New("missing required env: SCRIPTORIUM_MARKDOWN_DIR")
	}

	verifyMode := normalizeVerifyMode(os.Getenv("SCRIPTORIUM_INDEX_VERIFY"))
	if verifyMode == "" {
		return Runtime{}, errors.New("invalid SCRIPTORIUM_INDEX_VERIFY")
	}

	maxFileBytes, err := parseMaxFileBytes(os.Getenv("SCRIPTORIUM_MAX_FILE_BYTES"))
	if err != nil {
		return Runtime{}, err
	}

	profile, err := parseMCPProfile(
		os.Getenv("SCRIPTORIUM_MCP_PROFILE"),
		os.Getenv("SCRIPTORIUM_MCP_TOOL_PREFIX"),
		os.Getenv("SCRIPTORIUM_MCP_DOMAIN_DESCRIPTION"),
		os.Getenv("SCRIPTORIUM_MCP_CORPUS_SUMMARY"),
		os.Getenv("SCRIPTORIUM_MCP_EXAMPLE_QUERIES"),
	)
	if err != nil {
		return Runtime{}, err
	}

	cfg := Runtime{
		ServerName:      normalizeServerName(os.Getenv("SCRIPTORIUM_SERVER_NAME"), profile.ID),
		Profile:         profile,
		MCPBaseDir:      baseDir,
		DocsRoot:        resolveWithBase(baseDir, docsRoot),
		DocsRootEnvVar:  docsRootEnvVar,
		SampleRoots:     resolveAll(baseDir, splitPathListEnv(os.Getenv("SCRIPTORIUM_CODE_ROOTS"))),
		DocsIndexVerify: verifyMode,
		CodeExtensions:  normalizeExtensions(os.Getenv("SCRIPTORIUM_CODE_EXTENSIONS")),
		TextEncodingFallbacks: textdecode.ParseFallbackEncodings(
			os.Getenv("SCRIPTORIUM_TEXT_ENCODING_FALLBACK"),
			runtime.GOOS,
		),
		AllowSymlinks: parseBoolEnv(os.Getenv("SCRIPTORIUM_ALLOW_SYMLINKS")),
		MaxFileBytes:  maxFileBytes,
	}
	cfg.GitSnapshotPaths = resolveAll(baseDir, splitPathListEnv(os.Getenv("SCRIPTORIUM_SNAPSHOT_FILE")))
	cfg.DocsIndexPaths = resolveAll(baseDir, splitPathListEnv(os.Getenv("SCRIPTORIUM_INDEX_FILE")))
	return cfg, nil
}

func resolveDocsRootEnv() (string, string) {
	if value := strings.TrimSpace(os.Getenv("SCRIPTORIUM_MARKDOWN_DIR")); value != "" {
		return value, "SCRIPTORIUM_MARKDOWN_DIR"
	}
	return "", ""
}

func normalizeServerName(value string, profileID string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		if profileID != "" {
			return "scriptorium-" + profileID
		}
		return "scriptorium"
	}
	return value
}

func parseMCPProfile(id, toolPrefix, domainDescription, corpusSummary, exampleQueries string) (MCPProfile, error) {
	normalizedPrefix, err := normalizeToolPrefix(toolPrefix)
	if err != nil {
		return MCPProfile{}, err
	}
	return MCPProfile{
		ID:                strings.TrimSpace(id),
		ToolPrefix:        normalizedPrefix,
		DomainDescription: normalizeDomainDescription(domainDescription),
		CorpusSummary:     strings.TrimSpace(corpusSummary),
		ExampleQueries:    splitLineList(exampleQueries),
	}, nil
}

func normalizeDomainDescription(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "the configured documentation and code corpus"
	}
	return value
}

func normalizeToolPrefix(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return "", errors.New("invalid SCRIPTORIUM_MCP_TOOL_PREFIX")
		}
	}
	return strings.ToLower(value), nil
}

func normalizeVerifyMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "full":
		return "full"
	case "mtime":
		return "mtime"
	case "off":
		return "off"
	default:
		return ""
	}
}

func normalizeExtensions(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{".cs", ".ts"}
	}
	parts := splitTokenList(value)
	if len(parts) == 0 {
		return []string{".cs", ".ts"}
	}
	extensions := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if !strings.HasPrefix(part, ".") {
			part = "." + part
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		extensions = append(extensions, part)
	}
	return extensions
}

func parseBoolEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseMaxFileBytes(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1_000_000, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid SCRIPTORIUM_MAX_FILE_BYTES")
	}
	return parsed, nil
}

func resolveOptional(baseDir, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return resolveWithBase(baseDir, value)
}

func (r Runtime) EffectiveGitSnapshotPaths() []string {
	if len(r.GitSnapshotPaths) == 0 {
		return nil
	}
	return append([]string(nil), r.GitSnapshotPaths...)
}

func (r Runtime) EffectiveDocsIndexPaths() []string {
	if len(r.DocsIndexPaths) == 0 {
		return nil
	}
	return append([]string(nil), r.DocsIndexPaths...)
}

func resolveAll(baseDir string, values []string) []string {
	paths := make([]string, 0, len(values))
	for _, value := range values {
		paths = append(paths, resolveWithBase(baseDir, value))
	}
	return paths
}

func resolveWithBase(baseDir, value string) string {
	if filepath.IsAbs(value) {
		cleaned, _ := absPath(value)
		return cleaned
	}
	joined := filepath.Join(baseDir, value)
	cleaned, _ := absPath(joined)
	return cleaned
}

func splitPathListEnv(value string) []string {
	return splitPathList(value, os.PathListSeparator)
}

func splitPathList(value string, separator rune) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == separator || r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		result = append(result, field)
	}
	return result
}

func splitTokenList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		result = append(result, field)
	}
	return result
}

func splitLineList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		result = append(result, field)
	}
	return result
}
