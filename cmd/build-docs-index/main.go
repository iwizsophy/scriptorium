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
	fs := flag.NewFlagSet("build-docs-index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	docsRoot := fs.String("docs-root", "", "Markdown docs root")
	outPath := fs.String("out", "", "Output artifact path")
	allowSymlinks := fs.Bool("allow-symlinks", false, "Allow symlink traversal")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	if err := app.RunBuildDocsIndex(*docsRoot, *outPath, *allowSymlinks, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "build-docs-index: %v\n", err)
		return 1
	}
	return 0
}
