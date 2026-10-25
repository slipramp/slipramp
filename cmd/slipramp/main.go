package main

import (
	"os"

	"github.com/slipramp/slipramp/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.App{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: version,
	}.Run(os.Args[1:]))
}
