package main

import (
	"fmt"
	"io"
	"os"

	"github.com/iwizsophy/scriptorium/internal/app"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if err := app.RunServer(args, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "scriptorium: %v\n", err)
		return 1
	}
	return 0
}
