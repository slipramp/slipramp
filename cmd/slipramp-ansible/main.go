package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	ansiblesource "github.com/slipramp/slipramp/internal/runreport/ansible/source"
)

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer signal.Stop(signals)

	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, signals))
}

func run(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	signals <-chan os.Signal,
) int {
	if len(arguments) == 0 || (arguments[0] != string(ansiblesource.ModePreview) &&
		arguments[0] != string(ansiblesource.ModeApply)) {
		_, _ = fmt.Fprintln(stderr, "usage: slipramp-ansible <preview|apply> [options] <playbook>")
		return 64
	}

	config := ansiblesource.Config{
		Mode:    ansiblesource.Mode(arguments[0]),
		Stdout:  stdout,
		Stderr:  stderr,
		Signals: signals,
	}
	flags := flag.NewFlagSet("slipramp-ansible "+arguments[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&config.Inventory, "inventory", "", "inventory file, directory, or plugin configuration")
	flags.StringVar(&config.Limit, "limit", "", "Ansible host or group limit")
	flags.StringVar(&config.ReportPath, "report", "", "structured event report destination")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 64
	}
	if flags.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "exactly one repository-relative playbook is required")
		return 64
	}
	config.Playbook = flags.Arg(0)

	exitCode, err := ansiblesource.Run(ctx, config)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
	}

	return exitCode
}
