package app

import (
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/diagnostics"
	"github.com/silvekt/scriptorium/internal/docsindex"
	"github.com/silvekt/scriptorium/internal/filesafe"
	"github.com/silvekt/scriptorium/internal/gitsnapshot"
	"github.com/silvekt/scriptorium/internal/protocol"
	"github.com/silvekt/scriptorium/internal/source"
)

func ExecuteDiagnostics(cfg config.Runtime, stderr io.Writer) protocol.ToolResult {
	started := time.Now()
	logger := newLogger(cfg.ServerName, stderr)
	payload := buildDiagnosticsPayload(cfg)
	elapsedMs := time.Since(started).Milliseconds()
	logger.Printf("tool=diagnostics elapsedMs=%d", elapsedMs)
	return protocol.CreateToolResult(payload, "Loaded runtime diagnostics.")
}

func buildDiagnosticsPayload(cfg config.Runtime) diagnostics.Payload {
	indexState := docsindex.OpenRuntimeSet(cfg)
	snapshotState := gitsnapshot.OpenRuntimeSet(cfg.EffectiveGitSnapshotPaths())
	textFileCache := filesafe.GetTextFileCacheStats()
	codeSourceCache := source.GetCodeSourceCacheStats()

	var indexMeta map[string]any
	if indexState.Enabled && len(indexState.Indexes) > 0 {
		artifacts := make([]map[string]any, 0, len(indexState.Indexes))
		for idx, artifact := range indexState.Indexes {
			artifacts = append(artifacts, map[string]any{
				"path":              indexState.Paths[idx],
				"schemaVersion":     artifact.Meta.SchemaVersion,
				"generatedAt":       artifact.Meta.GeneratedAt,
				"docsRoot":          artifact.Meta.DocsRoot,
				"fileCount":         artifact.Meta.FileCount,
				"blockCount":        artifact.Meta.BlockCount,
				"sourceFingerprint": artifact.Meta.SourceFingerprint,
				"sourceFileCount":   artifact.Meta.SourceFileCount,
				"sourceMaxMtimeMs":  artifact.Meta.SourceMaxMtimeMs,
			})
		}
		indexMeta = map[string]any{
			"artifactCount": len(artifacts),
			"artifacts":     artifacts,
		}
	}

	var snapshotMeta map[string]any
	var snapshotRoots []map[string]any
	if snapshotState.Enabled {
		artifacts := make([]map[string]any, 0, len(snapshotState.Indexes))
		for idx, artifact := range snapshotState.Indexes {
			artifacts = append(artifacts, map[string]any{
				"path":          snapshotState.Paths[idx],
				"schemaVersion": artifact.Meta.SchemaVersion,
				"generatedAt":   artifact.Meta.GeneratedAt,
				"rootCount":     artifact.Meta.RootCount,
				"fileCount":     artifact.Meta.FileCount,
				"repoPath":      artifact.Meta.RepoPath,
				"fingerprint":   artifact.Meta.Fingerprint,
			})
			for _, root := range artifact.Roots {
				snapshotRoots = append(snapshotRoots, map[string]any{
					"id":          root.ID,
					"sourceKind":  root.SourceKind,
					"repoPath":    root.RepoPath,
					"ref":         root.Ref,
					"description": root.Description,
					"labels":      root.Labels,
				})
			}
		}
		snapshotMeta = map[string]any{
			"artifactCount": len(artifacts),
			"artifacts":     artifacts,
		}
	}

	docsFileCount, docsBlockCount := docsCoverage(cfg, indexState)
	sampleSourceCount := len(cfg.SampleRoots) + len(snapshotRoots)
	filesystemSampleCount := len(cfg.SampleRoots)
	snapshotSampleCount := len(snapshotRoots)
	sampleFileCount := sampleCoverage(cfg, snapshotState)
	docsIndexFallback := !indexState.Enabled && strings.Contains(strings.ToLower(indexState.Warning), "falling back to filesystem")
	snippetOnlyLikely := docsFileCount == 0 || sampleFileCount == 0
	warnings := retrievalWarnings(cfg, indexState.Warning, snapshotState.Warning, docsFileCount, sampleFileCount, snapshotSampleCount)

	return diagnostics.BuildRuntimePayload(diagnostics.RuntimeInput{
		ServerName:    cfg.ServerName,
		ServerVersion: ServerVersion,
		Profile: diagnostics.ProfileInput{
			ID:                cfg.Profile.ID,
			ToolPrefix:        cfg.Profile.ToolPrefix,
			DomainDescription: cfg.Profile.DomainDescription,
			CorpusSummary:     cfg.Profile.CorpusSummary,
			ExampleQueries:    append([]string(nil), cfg.Profile.ExampleQueries...),
		},
		Runtime:             "go",
		RuntimeVersion:      runtime.Version(),
		Platform:            runtime.GOOS,
		PID:                 os.Getpid(),
		CWD:                 mustGetwd(),
		MCPBaseDir:          cfg.MCPBaseDir,
		DocsRoot:            cfg.DocsRoot,
		DocsIndexVerifyMode: cfg.DocsIndexVerify,
		DocsIndexEnabled:    indexState.Enabled,
		DocsIndexPath:       strings.Join(indexState.Paths, ";"),
		DocsIndexPaths:      append([]string(nil), indexState.Paths...),
		DocsIndexMeta:       indexMeta,
		DocsOnlyMode:        len(cfg.SampleRoots) == 0 && len(cfg.EffectiveGitSnapshotPaths()) == 0,
		CodeExtensions:      cfg.CodeExtensions,
		SampleRoots:         cfg.SampleRoots,
		GitSnapshotPath:     strings.Join(snapshotState.Paths, ";"),
		GitSnapshotPaths:    append([]string(nil), snapshotState.Paths...),
		GitSnapshotMeta:     snapshotMeta,
		GitSnapshotRoots:    snapshotRoots,
		TextFilesCache: diagnostics.CacheStats{
			MaxEntries: textFileCache.MaxEntries,
			Entries:    textFileCache.Entries,
			Hits:       textFileCache.Hits,
			Misses:     textFileCache.Misses,
			Evictions:  textFileCache.Evictions,
		},
		SampleRootsCache: diagnostics.CodeSourceCacheStats{
			MaxEntries:      codeSourceCache.MaxEntries,
			Entries:         codeSourceCache.Entries,
			Hits:            codeSourceCache.Hits,
			Misses:          codeSourceCache.Misses,
			Evictions:       codeSourceCache.Evictions,
			FilesystemRoots: codeSourceCache.FilesystemRoots,
			SnapshotRoots:   codeSourceCache.SnapshotRoots,
		},
		Retrieval: diagnostics.RetrievalInput{
			DocsAvailable:           docsFileCount > 0,
			DocsArtifactCount:       len(indexState.Indexes),
			DocsFileCount:           docsFileCount,
			DocsBlockCount:          docsBlockCount,
			SampleCodeAvailable:     sampleFileCount > 0,
			SampleSourceCount:       sampleSourceCount,
			FilesystemSampleCount:   filesystemSampleCount,
			SnapshotSampleCount:     snapshotSampleCount,
			SampleFileCount:         sampleFileCount,
			SnapshotSampleAvailable: snapshotSampleCount > 0,
			DocsIndexFallback:       docsIndexFallback,
			SnippetOnlyLikely:       snippetOnlyLikely,
			CrossSourceRelations:    docsFileCount > 0 && sampleFileCount > 0,
			Warnings:                warnings,
		},
	})
}

func logStartup(cfg config.Runtime, stderr io.Writer) {
	logger := newLogger(cfg.ServerName, stderr)
	indexState := docsindex.OpenRuntimeSet(cfg)
	snapshotState := gitsnapshot.OpenRuntimeSet(cfg.EffectiveGitSnapshotPaths())

	logger.Printf("SCRIPTORIUM_HOME=%s", cfg.MCPBaseDir)
	logger.Printf("SCRIPTORIUM_MARKDOWN_DIR=%s", cfg.DocsRoot)
	if cfg.Profile.ID != "" {
		logger.Printf("SCRIPTORIUM_MCP_PROFILE=%s", cfg.Profile.ID)
	}
	if cfg.Profile.ToolPrefix != "" {
		logger.Printf("SCRIPTORIUM_MCP_TOOL_PREFIX=%s", cfg.Profile.ToolPrefix)
	}
	if cfg.Profile.CorpusSummary != "" {
		logger.Printf("SCRIPTORIUM_MCP_CORPUS_SUMMARY=%s", cfg.Profile.CorpusSummary)
	}
	if len(cfg.SampleRoots) == 0 && len(cfg.EffectiveGitSnapshotPaths()) == 0 {
		logger.Printf("SAMPLE_SOURCE=(none) docs-only mode")
	} else {
		for idx, root := range cfg.SampleRoots {
			logger.Printf("SAMPLE_SOURCE[%d]=%s", idx, root)
		}
		for idx, path := range cfg.EffectiveGitSnapshotPaths() {
			logger.Printf("SCRIPTORIUM_SNAPSHOT_FILE[%d]=%s", idx, path)
		}
	}
	if indexState.Enabled {
		for idx, artifact := range indexState.Indexes {
			logger.Printf("DOCS_INDEX[%d]=%s generatedAt=%s", idx, indexState.Paths[idx], artifact.Meta.GeneratedAt)
		}
	} else {
		logger.Printf("DOCS_INDEX=disabled warning=%s", indexState.Warning)
	}
	if snapshotState.Enabled {
		for idx, artifact := range snapshotState.Indexes {
			logger.Printf("GIT_SNAPSHOT[%d]=%s generatedAt=%s", idx, snapshotState.Paths[idx], artifact.Meta.GeneratedAt)
		}
	} else if snapshotState.Warning != "" {
		logger.Printf("GIT_SNAPSHOT=disabled warning=%s", snapshotState.Warning)
	}
	logger.Printf("SCRIPTORIUM_INDEX_VERIFY=%s", cfg.DocsIndexVerify)
	logger.Printf("SCRIPTORIUM_CODE_EXTENSIONS=%s", strings.Join(cfg.CodeExtensions, ","))
	for idx, warning := range buildDiagnosticsPayload(cfg).Retrieval.Warnings {
		logger.Printf("RETRIEVAL_WARNING[%d]=%s", idx, warning)
	}
	logger.Printf("MCP server ready")
}

func docsCoverage(cfg config.Runtime, indexState docsindex.RuntimeSet) (int, int) {
	if indexState.Enabled {
		fileCount := 0
		blockCount := 0
		for _, artifact := range indexState.Indexes {
			fileCount += artifact.Meta.FileCount
			blockCount += artifact.Meta.BlockCount
		}
		return fileCount, blockCount
	}
	root, err := filesafe.NewRoot(cfg.DocsRoot, cfg.AllowSymlinks, cfg.MaxFileBytes)
	if err != nil {
		return 0, 0
	}
	paths, err := root.ListFiles([]string{".md"})
	if err != nil {
		return 0, 0
	}
	return len(paths), 0
}

func sampleCoverage(cfg config.Runtime, snapshotState gitsnapshot.RuntimeSet) int {
	total := 0
	for _, rootPath := range cfg.SampleRoots {
		root, err := filesafe.NewRoot(rootPath, cfg.AllowSymlinks, cfg.MaxFileBytes)
		if err != nil {
			continue
		}
		paths, err := root.ListFiles(cfg.CodeExtensions)
		if err != nil {
			continue
		}
		total += len(paths)
	}
	for _, artifact := range snapshotState.Indexes {
		total += artifact.Meta.FileCount
	}
	return total
}

func retrievalWarnings(cfg config.Runtime, docsWarning string, snapshotWarning string, docsFileCount int, sampleFileCount int, snapshotSampleCount int) []string {
	warnings := make([]string, 0, 6)
	if docsFileCount == 0 {
		warnings = append(warnings, "No markdown docs are currently available for retrieval.")
	}
	if sampleFileCount == 0 {
		warnings = append(warnings, "No sample code is currently available for retrieval.")
	}
	if strings.TrimSpace(docsWarning) != "" {
		warnings = append(warnings, "Docs retrieval is running with a fallback or warning state: "+docsWarning)
	}
	if strings.TrimSpace(snapshotWarning) != "" {
		warnings = append(warnings, "Snapshot retrieval is running with a fallback or warning state: "+snapshotWarning)
	}
	if len(cfg.SampleRoots) > 0 && snapshotSampleCount == 0 {
		warnings = append(warnings, "Only filesystem-backed sample roots are available; snapshot-backed samples are not configured.")
	}
	if docsFileCount == 0 || sampleFileCount == 0 {
		warnings = append(warnings, "Implementation guidance quality is likely limited because docs and sample code are not both available.")
	}
	return warnings
}

func mustGetwd() string {
	// COVERAGE_EXCEPTION: Best-effort only. The os.Getwd failure path depends on
	// process cwd state and is not stable to reproduce in unit tests on Windows,
	// so diagnostics falls back to an empty string instead of treating it as a
	// runtime error.
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
