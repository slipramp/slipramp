package main

import (
	"fmt"
	"os"

	"github.com/slipramp/slipramp/internal/catalogschema"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/catalogschema/generate <output-path>")
		os.Exit(2)
	}

	contents, err := catalogschema.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate catalog schema: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], contents, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write catalog schema: %v\n", err)
		os.Exit(1)
	}
}
