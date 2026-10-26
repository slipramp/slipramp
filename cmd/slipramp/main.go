package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/slipramp/slipramp/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	exitCode := cli.App{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: version,
		Context: ctx,
	}.Run(os.Args[1:])
	stop()
	os.Exit(exitCode)
}
