package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/silvekt/scriptorium/internal/app"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("build-git-snapshot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "", "Git repository path")
	outPath := fs.String("out", "", "Output artifact path")
	samples := fs.String("samples", "", "Snapshot sample selectors")
	codeExtensions := fs.String("code-extensions", ".go", "Comma-separated code extensions")
	fetch := fs.Bool("fetch", false, "Enable git fetch before snapshot generation")
	fetchOnStart := fs.Bool("fetch-on-start", false, "Fetch before snapshot build when --fetch is also enabled")
	fetchRemote := fs.String("fetch-remote", "", "Remote name used when build-time fetch is enabled")
	allowSymlinks := fs.Bool("allow-symlinks", false, "Allow symlink traversal")
	maxFileBytes := fs.Int64("max-file-bytes", 0, "Maximum file size")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	options := app.BuildGitSnapshotOptions{
		RepoPath:       *repo,
		OutPath:        *outPath,
		Samples:        *samples,
		CodeExtensions: *codeExtensions,
		Fetch:          *fetch,
		FetchOnStart:   *fetchOnStart,
		FetchRemote:    *fetchRemote,
		AllowSymlinks:  *allowSymlinks,
		MaxFileBytes:   *maxFileBytes,
	}
	if err := app.RunBuildGitSnapshot(options, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "build-git-snapshot: %v\n", err)
		return 1
	}
	return 0
}
