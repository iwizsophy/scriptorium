package filesafe

import (
	"errors"
	iofs "io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// These tests intentionally exercise the package-local filesystem adapter seam.
// They are architecture-seam tests: the repository wants failure injection to
// stay anchored at this boundary so cross-platform filesystem policy can be
// tested without widening the exported API surface.
type fakeFilesystem struct {
	abs          func(string) (string, error)
	evalSymlinks func(string) (string, error)
	stat         func(string) (iofs.FileInfo, error)
	lstat        func(string) (iofs.FileInfo, error)
	readDir      func(string) ([]iofs.DirEntry, error)
	readFile     func(string) ([]byte, error)
}

func (f fakeFilesystem) Abs(path string) (string, error) {
	if f.abs != nil {
		return f.abs(path)
	}
	return path, nil
}

func (f fakeFilesystem) EvalSymlinks(path string) (string, error) {
	if f.evalSymlinks != nil {
		return f.evalSymlinks(path)
	}
	return path, nil
}

func (f fakeFilesystem) Stat(path string) (iofs.FileInfo, error) {
	if f.stat != nil {
		return f.stat(path)
	}
	return fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
}

func (f fakeFilesystem) Lstat(path string) (iofs.FileInfo, error) {
	if f.lstat != nil {
		return f.lstat(path)
	}
	return fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
}

func (f fakeFilesystem) ReadDir(path string) ([]iofs.DirEntry, error) {
	if f.readDir != nil {
		return f.readDir(path)
	}
	return nil, nil
}

func (f fakeFilesystem) ReadFile(path string) ([]byte, error) {
	if f.readFile != nil {
		return f.readFile(path)
	}
	return []byte("hello"), nil
}

type fakeFileInfo struct {
	name    string
	size    int64
	mode    iofs.FileMode
	modTime time.Time
}

func (f fakeFileInfo) Name() string        { return f.name }
func (f fakeFileInfo) Size() int64         { return f.size }
func (f fakeFileInfo) Mode() iofs.FileMode { return f.mode }
func (f fakeFileInfo) ModTime() time.Time {
	if f.modTime.IsZero() {
		return time.Unix(0, 0)
	}
	return f.modTime
}
func (f fakeFileInfo) IsDir() bool { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any    { return nil }

type fakeDirEntry struct {
	name    string
	typ     iofs.FileMode
	dir     bool
	info    iofs.FileInfo
	infoErr error
}

func (f fakeDirEntry) Name() string        { return f.name }
func (f fakeDirEntry) IsDir() bool         { return f.dir }
func (f fakeDirEntry) Type() iofs.FileMode { return f.typ }
func (f fakeDirEntry) Info() (iofs.FileInfo, error) {
	if f.infoErr != nil {
		return nil, f.infoErr
	}
	if f.info != nil {
		return f.info, nil
	}
	return fakeFileInfo{name: f.name, mode: f.typ}, nil
}

func TestNewRootWithFSPropagatesAdapterErrors(t *testing.T) {
	dir := t.TempDir()
	absErr := errors.New("abs failed")
	statErr := errors.New("stat failed")
	evalErr := errors.New("eval failed")

	tests := []struct {
		name string
		host filesystem
		want error
	}{
		{
			name: "abs error",
			host: fakeFilesystem{
				abs: func(string) (string, error) { return "", absErr },
			},
			want: absErr,
		},
		{
			name: "stat error",
			host: fakeFilesystem{
				abs:  func(string) (string, error) { return dir, nil },
				stat: func(string) (iofs.FileInfo, error) { return nil, statErr },
			},
			want: statErr,
		},
		{
			name: "non directory root",
			host: fakeFilesystem{
				abs:  func(string) (string, error) { return dir, nil },
				stat: func(string) (iofs.FileInfo, error) { return fakeFileInfo{name: "file", mode: 0o644}, nil },
			},
		},
		{
			name: "eval error",
			host: fakeFilesystem{
				abs: func(string) (string, error) { return dir, nil },
				stat: func(string) (iofs.FileInfo, error) {
					return fakeFileInfo{name: filepath.Base(dir), mode: iofs.ModeDir | 0o755}, nil
				},
				evalSymlinks: func(string) (string, error) { return "", evalErr },
			},
			want: evalErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRootWithFS("ignored", false, 0, tt.host)
			if tt.want == nil {
				if err == nil || !strings.Contains(err.Error(), "root path is not a directory") {
					t.Fatalf("expected directory validation error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestResolveUsesFilesystemAdapter(t *testing.T) {
	dir := t.TempDir()
	evalErr := errors.New("eval failed")
	lstatErr := errors.New("lstat failed")

	enabledRoot := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: true,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			evalSymlinks: func(path string) (string, error) {
				if strings.HasSuffix(path, "missing.txt") {
					return "", evalErr
				}
				return path, nil
			},
		},
	}
	if _, err := enabledRoot.Resolve("missing.txt"); !errors.Is(err, evalErr) {
		t.Fatalf("expected EvalSymlinks error, got %v", err)
	}

	disabledRoot := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: false,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			lstat: func(string) (iofs.FileInfo, error) { return nil, lstatErr },
		},
	}
	if _, err := disabledRoot.Resolve("blocked.txt"); !errors.Is(err, lstatErr) {
		t.Fatalf("expected Lstat error, got %v", err)
	}

	symlinkRoot := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: false,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			lstat: func(string) (iofs.FileInfo, error) {
				return fakeFileInfo{name: "blocked.txt", mode: iofs.ModeSymlink}, nil
			},
		},
	}
	if _, err := symlinkRoot.Resolve("blocked.txt"); err == nil || !strings.Contains(err.Error(), "symlink access is disabled") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestReadTextUsesFilesystemAdapter(t *testing.T) {
	ResetTextFileCacheForTesting()
	dir := t.TempDir()
	statErr := errors.New("stat failed")
	readErr := errors.New("read failed")

	root := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: true,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			evalSymlinks: func(path string) (string, error) { return path, nil },
			stat: func(path string) (iofs.FileInfo, error) {
				if strings.HasSuffix(path, "stat.txt") {
					return nil, statErr
				}
				return fakeFileInfo{name: filepath.Base(path), mode: 0o644, size: 5}, nil
			},
			readFile: func(path string) ([]byte, error) {
				if strings.HasSuffix(path, "read.txt") {
					return nil, readErr
				}
				return []byte("hello"), nil
			},
		},
	}

	if _, err := root.ReadText("stat.txt", nil); !errors.Is(err, statErr) {
		t.Fatalf("expected Stat error, got %v", err)
	}
	if _, err := root.ReadText("read.txt", nil); !errors.Is(err, readErr) {
		t.Fatalf("expected ReadFile error, got %v", err)
	}
}

func TestReadTextRejectsResolveErrorBeforeFilesystemReads(t *testing.T) {
	root := Root{
		path:          t.TempDir(),
		realPath:      t.TempDir(),
		allowSymlinks: true,
		maxFileBytes:  defaultMaxFileBytes,
		host:          fakeFilesystem{},
	}

	if _, err := root.ReadText("../bad.txt", nil); err == nil {
		t.Fatal("expected ReadText to surface resolve error for unsafe path")
	}
}

func TestWalkUsesFilesystemAdapterErrorBranches(t *testing.T) {
	dir := t.TempDir()
	evalErr := errors.New("eval failed")
	statErr := errors.New("stat failed")
	infoErr := errors.New("info failed")

	t.Run("symlink eval error", func(t *testing.T) {
		root := Root{
			path:          dir,
			realPath:      dir,
			allowSymlinks: true,
			maxFileBytes:  defaultMaxFileBytes,
			host: fakeFilesystem{
				readDir: func(string) ([]iofs.DirEntry, error) {
					return []iofs.DirEntry{fakeDirEntry{name: "link.md", typ: iofs.ModeSymlink}}, nil
				},
				evalSymlinks: func(string) (string, error) { return "", evalErr },
			},
		}

		var results []string
		if err := root.walk(dir, "", map[string]struct{}{dir: {}}, map[string]struct{}{".md": {}}, &results); !errors.Is(err, evalErr) {
			t.Fatalf("expected EvalSymlinks error, got %v", err)
		}
	})

	t.Run("symlink stat error", func(t *testing.T) {
		root := Root{
			path:          dir,
			realPath:      dir,
			allowSymlinks: true,
			maxFileBytes:  defaultMaxFileBytes,
			host: fakeFilesystem{
				readDir: func(string) ([]iofs.DirEntry, error) {
					return []iofs.DirEntry{fakeDirEntry{name: "link.md", typ: iofs.ModeSymlink}}, nil
				},
				evalSymlinks: func(path string) (string, error) { return path, nil },
				stat:         func(string) (iofs.FileInfo, error) { return nil, statErr },
			},
		}

		var results []string
		if err := root.walk(dir, "", map[string]struct{}{dir: {}}, map[string]struct{}{".md": {}}, &results); !errors.Is(err, statErr) {
			t.Fatalf("expected Stat error, got %v", err)
		}
	})

	t.Run("entry info error", func(t *testing.T) {
		root := Root{
			path:          dir,
			realPath:      dir,
			allowSymlinks: false,
			maxFileBytes:  defaultMaxFileBytes,
			host: fakeFilesystem{
				readDir: func(string) ([]iofs.DirEntry, error) {
					return []iofs.DirEntry{fakeDirEntry{name: "guide.md", infoErr: infoErr}}, nil
				},
			},
		}

		var results []string
		if err := root.walk(dir, "", map[string]struct{}{dir: {}}, map[string]struct{}{".md": {}}, &results); !errors.Is(err, infoErr) {
			t.Fatalf("expected Info error, got %v", err)
		}
	})

	t.Run("symlink disabled skips link and keeps regular file", func(t *testing.T) {
		root := Root{
			path:          dir,
			realPath:      dir,
			allowSymlinks: false,
			maxFileBytes:  defaultMaxFileBytes,
			host: fakeFilesystem{
				readDir: func(string) ([]iofs.DirEntry, error) {
					return []iofs.DirEntry{
						fakeDirEntry{name: "link.md", typ: iofs.ModeSymlink},
						fakeDirEntry{name: "guide.md", info: fakeFileInfo{name: "guide.md", mode: 0o644}},
					}, nil
				},
			},
		}

		var results []string
		if err := root.walk(dir, "", map[string]struct{}{dir: {}}, map[string]struct{}{".md": {}}, &results); err != nil {
			t.Fatalf("walk returned error: %v", err)
		}
		if !slices.Equal(results, []string{"guide.md"}) {
			t.Fatalf("unexpected walk results: %#v", results)
		}
	})
}

func TestWalkUsesFilesystemAdapterForDeterministicSymlinkDirectoryCases(t *testing.T) {
	dir := t.TempDir()
	loopRealPath := filepath.Join(dir, "real-loop")
	entries := map[string][]iofs.DirEntry{
		dir: {
			fakeDirEntry{name: ".git", typ: iofs.ModeSymlink},
			fakeDirEntry{name: "loop", typ: iofs.ModeSymlink},
			fakeDirEntry{
				name: "guide.md",
				info: fakeFileInfo{name: "guide.md", mode: 0o644},
			},
		},
	}

	root := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: true,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			readDir: func(path string) ([]iofs.DirEntry, error) { return entries[path], nil },
			evalSymlinks: func(path string) (string, error) {
				switch filepath.Base(path) {
				case ".git":
					return filepath.Join(dir, "ignored"), nil
				case "loop":
					return loopRealPath, nil
				default:
					return path, nil
				}
			},
			stat: func(path string) (iofs.FileInfo, error) {
				if base := filepath.Base(path); base == ".git" || base == "loop" {
					return fakeFileInfo{name: base, mode: iofs.ModeDir | 0o755}, nil
				}
				return fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
			},
		},
	}

	var results []string
	if err := root.walk(dir, "", map[string]struct{}{dir: {}, loopRealPath: {}}, map[string]struct{}{".md": {}}, &results); err != nil {
		t.Fatalf("walk returned error: %v", err)
	}
	if len(results) != 1 || results[0] != "guide.md" {
		t.Fatalf("unexpected walk results: %#v", results)
	}
}

func TestHostFSAndListFilesUseFilesystemAdapterFallbacks(t *testing.T) {
	if _, ok := (Root{}).hostFS().(osFilesystem); !ok {
		t.Fatal("expected zero-value root to fall back to osFilesystem")
	}

	dir := t.TempDir()
	readDirErr := errors.New("read dir failed")
	root := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: false,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			readDir: func(string) ([]iofs.DirEntry, error) { return nil, readDirErr },
		},
	}

	if _, err := root.ListFiles([]string{".md"}); !errors.Is(err, readDirErr) {
		t.Fatalf("expected ListFiles to return the adapter ReadDir error, got %v", err)
	}
}

func TestWalkUsesFilesystemAdapterForRecursiveDirectoryAndNonRegularSymlinkBranches(t *testing.T) {
	dir := t.TempDir()
	nestedErr := errors.New("nested read dir failed")

	root := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: true,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			readDir: func(path string) ([]iofs.DirEntry, error) {
				switch path {
				case dir:
					return []iofs.DirEntry{
						fakeDirEntry{name: "pipe.md", typ: iofs.ModeSymlink},
						fakeDirEntry{name: "nested", dir: true, typ: iofs.ModeDir},
					}, nil
				case filepath.Join(dir, "nested"):
					return nil, nestedErr
				default:
					return nil, nil
				}
			},
			evalSymlinks: func(path string) (string, error) { return path, nil },
			stat: func(path string) (iofs.FileInfo, error) {
				if filepath.Base(path) == "pipe.md" {
					return fakeFileInfo{name: "pipe.md", mode: iofs.ModeNamedPipe}, nil
				}
				return fakeFileInfo{name: filepath.Base(path), mode: iofs.ModeDir | 0o755}, nil
			},
		},
	}

	var results []string
	err := root.walk(dir, "", map[string]struct{}{dir: {}}, map[string]struct{}{".md": {}}, &results)
	if !errors.Is(err, nestedErr) {
		t.Fatalf("expected nested ReadDir error, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected non-regular symlink target to be skipped before the nested error, got %#v", results)
	}
}

func TestWalkUsesFilesystemAdapterForSymlinkDirectoryRecursiveErrors(t *testing.T) {
	dir := t.TempDir()
	realNested := filepath.Join(dir, "real-nested")
	nestedErr := errors.New("symlink nested read dir failed")

	root := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: true,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			readDir: func(path string) ([]iofs.DirEntry, error) {
				switch path {
				case dir:
					return []iofs.DirEntry{fakeDirEntry{name: "nested", typ: iofs.ModeSymlink}}, nil
				case filepath.Join(dir, "nested"):
					return nil, nestedErr
				default:
					return nil, nil
				}
			},
			evalSymlinks: func(path string) (string, error) {
				if filepath.Base(path) == "nested" {
					return realNested, nil
				}
				return path, nil
			},
			stat: func(path string) (iofs.FileInfo, error) {
				return fakeFileInfo{name: filepath.Base(path), mode: iofs.ModeDir | 0o755}, nil
			},
		},
	}

	var results []string
	err := root.walk(dir, "", map[string]struct{}{dir: {}}, map[string]struct{}{".md": {}}, &results)
	if !errors.Is(err, nestedErr) {
		t.Fatalf("expected nested ReadDir error from symlink directory recursion, got %v", err)
	}
}

func TestWalkUsesFilesystemAdapterForSymlinkDirectoryAndRegularFileSuccessBranches(t *testing.T) {
	dir := t.TempDir()
	realNested := filepath.Join(dir, "real-nested")

	root := Root{
		path:          dir,
		realPath:      dir,
		allowSymlinks: true,
		maxFileBytes:  defaultMaxFileBytes,
		host: fakeFilesystem{
			readDir: func(path string) ([]iofs.DirEntry, error) {
				switch path {
				case dir:
					return []iofs.DirEntry{
						fakeDirEntry{name: "nested", typ: iofs.ModeSymlink},
						fakeDirEntry{name: "guide.md", typ: iofs.ModeSymlink},
					}, nil
				case filepath.Join(dir, "nested"):
					return []iofs.DirEntry{
						fakeDirEntry{name: "child.md", info: fakeFileInfo{name: "child.md", mode: 0o644}},
					}, nil
				default:
					return nil, nil
				}
			},
			evalSymlinks: func(path string) (string, error) {
				switch filepath.Base(path) {
				case "nested":
					return realNested, nil
				default:
					return path, nil
				}
			},
			stat: func(path string) (iofs.FileInfo, error) {
				switch filepath.Base(path) {
				case "nested":
					return fakeFileInfo{name: "nested", mode: iofs.ModeDir | 0o755}, nil
				default:
					return fakeFileInfo{name: filepath.Base(path), mode: 0o644}, nil
				}
			},
		},
	}

	var results []string
	if err := root.walk(dir, "", map[string]struct{}{dir: {}}, map[string]struct{}{".md": {}}, &results); err != nil {
		t.Fatalf("walk returned error: %v", err)
	}
	if !slices.Equal(results, []string{"guide.md", "nested/child.md"}) {
		t.Fatalf("unexpected walk results: %#v", results)
	}
}
