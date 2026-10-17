package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/slipramp/slipramp/internal/harness"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(
	command []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(command) == 2 && command[0] == "--install" {
		if err := harness.Install(command[1]); err != nil {
			_, _ = fmt.Fprintf(stderr, "slipramp-harness: %v\n", err)
			return 1
		}

		return 0
	}

	if len(command) == 0 {
		_, _ = fmt.Fprintln(
			stderr,
			"usage: slipramp-harness <command> [argument ...] | --install <path>",
		)
		return 2
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(
		signals,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
	)
	defer signal.Stop(signals)

	exitCode, err := harness.Run(
		context.Background(),
		command,
		harness.Streams{
			Stdin:  stdin,
			Stdout: stdout,
			Stderr: stderr,
		},
		signals,
	)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "slipramp-harness: %v\n", err)
		return 1
	}

	return exitCode
}
