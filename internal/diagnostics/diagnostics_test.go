package diagnostics

import "testing"

func TestBuildRuntimePayloadUsesRuntimeVersionAndSnapshotSources(t *testing.T) {
	payload := BuildRuntimePayload(RuntimeInput{
		ServerName:          "scriptorium",
		ServerVersion:       "1.0.0",
		Profile: ProfileInput{
			ID:                "azure",
			ToolPrefix:        "azure",
			DomainDescription: "Azure architecture guidance",
			CorpusSummary:     "Azure docs and Bicep samples",
			ExampleQueries:    []string{"How do I deploy ACA?"},
		},
		Runtime:             "go",
		RuntimeVersion:      "go1.25.0",
		Platform:            "windows",
		PID:                 42,
		CWD:                 "D:/repo",
		MCPBaseDir:          "D:/repo",
		DocsRoot:            "D:/repo/docs",
		DocsIndexVerifyMode: "full",
		DocsIndexEnabled:    true,
		DocsIndexPath:       "D:/repo/scriptorium-index.sqlite",
		DocsOnlyMode:        false,
		CodeExtensions:      []string{".go", ".ts"},
		SampleRoots:         []string{"D:/repo/src"},
		GitSnapshotPath:     "D:/repo/scriptorium-snapshot.sqlite",
		GitSnapshotRoots: []map[string]any{
			{"id": "head", "description": "HEAD"},
		},
		TextFilesCache: CacheStats{Entries: 1, Hits: 2, Misses: 3, Evictions: 4},
		SampleRootsCache: CodeSourceCacheStats{
			Entries:         1,
			Hits:            2,
			Misses:          3,
			Evictions:       4,
			FilesystemRoots: 1,
			SnapshotRoots:   1,
		},
	})

	if payload.Server.Runtime != "go" || payload.Server.RuntimeVersion != "go1.25.0" {
		t.Fatalf("unexpected server runtime info: %#v", payload.Server)
	}
	if len(payload.Code.SampleSources) != 2 {
		t.Fatalf("expected filesystem and snapshot sample sources, got %#v", payload.Code.SampleSources)
	}
	if payload.Code.SampleSources[1].Kind != "git_snapshot" || payload.Caches.SampleRoots.SnapshotRoots != 1 {
		t.Fatalf("unexpected snapshot diagnostics payload: %#v %#v", payload.Code.SampleSources, payload.Caches.SampleRoots)
	}
	if payload.Profile.ID != "azure" || payload.Profile.ToolPrefix != "azure" || payload.Profile.DomainDescription != "Azure architecture guidance" {
		t.Fatalf("unexpected profile payload: %#v", payload.Profile)
	}
	if len(payload.Profile.ExampleQueries) != 1 || payload.Profile.ExampleQueries[0] != "How do I deploy ACA?" {
		t.Fatalf("unexpected profile example queries: %#v", payload.Profile)
	}
}
