package diagnostics

type RuntimeInput struct {
	ServerName          string
	ServerVersion       string
	Profile             ProfileInput
	Runtime             string
	RuntimeVersion      string
	Platform            string
	PID                 int
	CWD                 string
	MCPBaseDir          string
	DocsRoot            string
	DocsIndexVerifyMode string
	DocsIndexEnabled    bool
	DocsIndexPath       string
	DocsIndexPaths      []string
	DocsIndexMeta       map[string]any
	DocsOnlyMode        bool
	CodeExtensions      []string
	SampleRoots         []string
	GitSnapshotPath     string
	GitSnapshotPaths    []string
	GitSnapshotMeta     map[string]any
	GitSnapshotRoots    []map[string]any
	TextFilesCache      CacheStats
	SampleRootsCache    CodeSourceCacheStats
	Retrieval           RetrievalInput
}

type Payload struct {
	Server    ServerInfo    `json:"server"`
	Profile   ProfileInfo   `json:"profile"`
	Runtime   RuntimeInfo   `json:"runtime"`
	Docs      DocsInfo      `json:"docs"`
	Code      CodeInfo      `json:"code"`
	Caches    CacheInfo     `json:"caches"`
	Retrieval RetrievalInfo `json:"retrieval"`
}

type ProfileInput struct {
	ID                string
	ToolPrefix        string
	DomainDescription string
	CorpusSummary     string
	ExampleQueries    []string
}

type ProfileInfo struct {
	ID                string   `json:"id,omitempty"`
	ToolPrefix        string   `json:"toolPrefix,omitempty"`
	DomainDescription string   `json:"domainDescription"`
	CorpusSummary     string   `json:"corpusSummary,omitempty"`
	ExampleQueries    []string `json:"exampleQueries,omitempty"`
}

type ServerInfo struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	Runtime        string `json:"runtime"`
	RuntimeVersion string `json:"runtimeVersion"`
	Platform       string `json:"platform"`
	PID            int    `json:"pid"`
}

type RuntimeInfo struct {
	CWD                 string `json:"cwd"`
	MCPBaseDir          string `json:"mcpBaseDir"`
	DocsIndexVerifyMode string `json:"docsIndexVerifyMode"`
}

type DocsInfo struct {
	Root         string       `json:"root"`
	DocsOnlyMode bool         `json:"docsOnlyMode"`
	Index        DocsIndexRef `json:"index"`
}

type DocsIndexRef struct {
	Enabled bool           `json:"enabled"`
	Path    string         `json:"path,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type CodeInfo struct {
	Enabled        bool         `json:"enabled"`
	CodeExtensions []string     `json:"codeExtensions"`
	SampleSources  []CodeSource `json:"sampleSources"`
	GitSnapshot    SnapshotInfo `json:"gitSnapshot"`
}

type CodeSource struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

type SnapshotInfo struct {
	Enabled bool             `json:"enabled"`
	Path    string           `json:"path,omitempty"`
	Meta    map[string]any   `json:"meta,omitempty"`
	Roots   []map[string]any `json:"roots,omitempty"`
}

type CacheInfo struct {
	TextFiles   CacheStats           `json:"textFiles"`
	SampleRoots CodeSourceCacheStats `json:"sampleRoots"`
}

type CacheStats struct {
	MaxEntries int `json:"maxEntries"`
	Entries    int `json:"entries"`
	Hits       int `json:"hits"`
	Misses     int `json:"misses"`
	Evictions  int `json:"evictions"`
}

type CodeSourceCacheStats struct {
	MaxEntries      int `json:"maxEntries"`
	Entries         int `json:"entries"`
	Hits            int `json:"hits"`
	Misses          int `json:"misses"`
	Evictions       int `json:"evictions"`
	FilesystemRoots int `json:"filesystemRoots"`
	SnapshotRoots   int `json:"snapshotRoots"`
}

type RetrievalInput struct {
	DocsAvailable           bool
	DocsArtifactCount       int
	DocsFileCount           int
	DocsBlockCount          int
	SampleCodeAvailable     bool
	SampleSourceCount       int
	FilesystemSampleCount   int
	SnapshotSampleCount     int
	SampleFileCount         int
	SnapshotSampleAvailable bool
	DocsIndexFallback       bool
	SnippetOnlyLikely       bool
	CrossSourceRelations    bool
	Warnings                []string
}

type RetrievalInfo struct {
	Corpus   RetrievalCorpus   `json:"corpus"`
	Fallback RetrievalFallback `json:"fallback"`
	Warnings []string          `json:"warnings"`
}

type RetrievalCorpus struct {
	DocsAvailable           bool `json:"docsAvailable"`
	DocsArtifactCount       int  `json:"docsArtifactCount"`
	DocsFileCount           int  `json:"docsFileCount"`
	DocsBlockCount          int  `json:"docsBlockCount"`
	SampleCodeAvailable     bool `json:"sampleCodeAvailable"`
	SampleSourceCount       int  `json:"sampleSourceCount"`
	FilesystemSampleCount   int  `json:"filesystemSampleCount"`
	SnapshotSampleCount     int  `json:"snapshotSampleCount"`
	SampleFileCount         int  `json:"sampleFileCount"`
	SnapshotSampleAvailable bool `json:"snapshotSampleAvailable"`
	CrossSourceRelations    bool `json:"crossSourceRelations"`
}

type RetrievalFallback struct {
	DocsIndexToFilesystem bool `json:"docsIndexToFilesystem"`
	SnippetOnlyLikely     bool `json:"snippetOnlyLikely"`
}

func BuildRuntimePayload(input RuntimeInput) Payload {
	sources := make([]CodeSource, 0, len(input.SampleRoots)+len(input.GitSnapshotRoots))
	for _, root := range input.SampleRoots {
		sources = append(sources, CodeSource{
			ID:          root,
			Kind:        "fs",
			Description: root,
		})
	}
	for _, root := range input.GitSnapshotRoots {
		id, _ := root["id"].(string)
		description, _ := root["description"].(string)
		sources = append(sources, CodeSource{
			ID:          id,
			Kind:        "git_snapshot",
			Description: description,
		})
	}

	return Payload{
		Server: ServerInfo{
			Name:           input.ServerName,
			Version:        input.ServerVersion,
			Runtime:        input.Runtime,
			RuntimeVersion: input.RuntimeVersion,
			Platform:       input.Platform,
			PID:            input.PID,
		},
		Profile: ProfileInfo{
			ID:                input.Profile.ID,
			ToolPrefix:        input.Profile.ToolPrefix,
			DomainDescription: input.Profile.DomainDescription,
			CorpusSummary:     input.Profile.CorpusSummary,
			ExampleQueries:    append([]string(nil), input.Profile.ExampleQueries...),
		},
		Runtime: RuntimeInfo{
			CWD:                 input.CWD,
			MCPBaseDir:          input.MCPBaseDir,
			DocsIndexVerifyMode: input.DocsIndexVerifyMode,
		},
		Docs: DocsInfo{
			Root:         input.DocsRoot,
			DocsOnlyMode: input.DocsOnlyMode,
			Index: DocsIndexRef{
				Enabled: input.DocsIndexEnabled,
				Path:    input.DocsIndexPath,
				Meta:    input.DocsIndexMeta,
			},
		},
		Code: CodeInfo{
			Enabled:        !input.DocsOnlyMode,
			CodeExtensions: append([]string(nil), input.CodeExtensions...),
			SampleSources:  sources,
			GitSnapshot: SnapshotInfo{
				Enabled: input.GitSnapshotPath != "",
				Path:    input.GitSnapshotPath,
				Meta:    input.GitSnapshotMeta,
				Roots:   input.GitSnapshotRoots,
			},
		},
		Caches: CacheInfo{
			TextFiles:   input.TextFilesCache,
			SampleRoots: input.SampleRootsCache,
		},
		Retrieval: RetrievalInfo{
			Corpus: RetrievalCorpus{
				DocsAvailable:           input.Retrieval.DocsAvailable,
				DocsArtifactCount:       input.Retrieval.DocsArtifactCount,
				DocsFileCount:           input.Retrieval.DocsFileCount,
				DocsBlockCount:          input.Retrieval.DocsBlockCount,
				SampleCodeAvailable:     input.Retrieval.SampleCodeAvailable,
				SampleSourceCount:       input.Retrieval.SampleSourceCount,
				FilesystemSampleCount:   input.Retrieval.FilesystemSampleCount,
				SnapshotSampleCount:     input.Retrieval.SnapshotSampleCount,
				SampleFileCount:         input.Retrieval.SampleFileCount,
				SnapshotSampleAvailable: input.Retrieval.SnapshotSampleAvailable,
				CrossSourceRelations:    input.Retrieval.CrossSourceRelations,
			},
			Fallback: RetrievalFallback{
				DocsIndexToFilesystem: input.Retrieval.DocsIndexFallback,
				SnippetOnlyLikely:     input.Retrieval.SnippetOnlyLikely,
			},
			Warnings: append([]string(nil), input.Retrieval.Warnings...),
		},
	}
}
