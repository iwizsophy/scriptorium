package app

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/iwizsophy/scriptorium/internal/docsindex"
	"github.com/iwizsophy/scriptorium/internal/gitsnapshot"
)

const (
	DefaultName   = "scriptorium"
)

var ServerVersion = "1.0.0"

type BuildGitSnapshotOptions struct {
	RepoPath       string
	OutPath        string
	Samples        string
	CodeExtensions string
	Fetch          bool
	FetchOnStart   bool
	FetchRemote    string
	AllowSymlinks  bool
	MaxFileBytes   int64
}

func RunBuildDocsIndex(docsRoot, outPath string, allowSymlinks bool, stdout, stderr io.Writer) error {
	if strings.TrimSpace(docsRoot) == "" {
		return errors.New("--docs-root is required")
	}
	if strings.TrimSpace(outPath) == "" {
		return errors.New("--out is required")
	}

	index, err := docsindex.Build(docsRoot, nil, allowSymlinks)
	if err != nil {
		return err
	}
	if err := docsindex.Write(outPath, index); err != nil {
		return err
	}

	fmt.Fprintf(
		stderr,
		"[%s] docs index built docsRoot=%s out=%s files=%d blocks=%d\n",
		DefaultName,
		index.Meta.DocsRoot,
		outPath,
		index.Meta.FileCount,
		index.Meta.BlockCount,
	)
	fmt.Fprintf(
		stdout,
		"docsRoot=%s\nout=%s\nfiles=%d\nblocks=%d\nstatus=ok\n",
		index.Meta.DocsRoot,
		outPath,
		index.Meta.FileCount,
		index.Meta.BlockCount,
	)
	return nil
}

func RunBuildGitSnapshot(options BuildGitSnapshotOptions, stdout, stderr io.Writer) error {
	if strings.TrimSpace(options.RepoPath) == "" {
		return errors.New("--repo is required")
	}
	if strings.TrimSpace(options.OutPath) == "" {
		return errors.New("--out is required")
	}
	if strings.TrimSpace(options.Samples) == "" {
		return errors.New("--samples is required")
	}
	artifact, err := gitsnapshot.Build(gitsnapshot.BuildOptions{
		RepoPath:       options.RepoPath,
		Samples:        options.Samples,
		CodeExtensions: configCodeExtensions(options.CodeExtensions),
		FetchEnabled:   options.Fetch,
		FetchOnStart:   options.FetchOnStart,
		FetchRemote:    options.FetchRemote,
		AllowSymlinks:  options.AllowSymlinks,
		MaxFileBytes:   options.MaxFileBytes,
	})
	if err != nil {
		return err
	}
	if err := gitsnapshot.Write(options.OutPath, artifact); err != nil {
		return err
	}

	fmt.Fprintf(
		stderr,
		"[%s] git snapshot built repo=%s out=%s roots=%d files=%d\n",
		DefaultName,
		artifact.Meta.RepoPath,
		options.OutPath,
		artifact.Meta.RootCount,
		artifact.Meta.FileCount,
	)
	fmt.Fprintf(
		stdout,
		"repo=%s\nout=%s\nroots=%d\nfiles=%d\nstatus=ok\n",
		artifact.Meta.RepoPath,
		options.OutPath,
		artifact.Meta.RootCount,
		artifact.Meta.FileCount,
	)
	return nil
}

type logger struct {
	prefix string
	out    io.Writer
}

func newLogger(serverName string, out io.Writer) logger {
	name := strings.TrimSpace(serverName)
	if name == "" {
		name = DefaultName
	}
	return logger{
		prefix: fmt.Sprintf("[%s] ", name),
		out:    out,
	}
}

func (l logger) Printf(format string, args ...any) {
	fmt.Fprintf(l.out, l.prefix+format+"\n", args...)
}

func configCodeExtensions(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	if len(parts) == 0 {
		return []string{".cs", ".ts"}
	}
	result := make([]string, 0, len(parts))
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
		result = append(result, part)
	}
	return result
}
