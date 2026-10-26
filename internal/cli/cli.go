package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

const (
	ExitValid           = 0
	ExitInvalid         = 1
	ExitInvocation      = 2
	ExitInternal        = 3
	ExitRemote          = 4
	ExitIncomplete      = 5
	ExitRunUnsuccessful = 6
)

type App struct {
	Stdout     io.Writer
	Stderr     io.Writer
	WorkDir    string
	Version    string
	Context    context.Context
	Getenv     func(string) string
	HTTPClient *http.Client
}

func (a App) Run(arguments []string) int {
	exitCode := ExitValid
	command := a.newRootCommand(&exitCode)
	command.SetArgs(arguments)
	if err := command.ExecuteContext(a.commandContext()); err != nil {
		return a.invocationError(normalizeCommandError(err))
	}

	return exitCode
}

func (a App) newRootCommand(exitCode *int) *cobra.Command {
	command := &cobra.Command{
		Use:           "slipramp",
		Short:         "Operate Slipramp catalogs, Runs, and Artifacts",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) == 0 {
				return nil
			}

			return fmt.Errorf("unknown command %q", arguments[0])
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("a command is required")
		},
	}
	command.SetOut(a.Stdout)
	command.SetErr(a.Stderr)
	command.CompletionOptions.DisableDefaultCmd = true
	command.DisableSuggestions = true
	command.AddCommand(
		a.newVersionCommand(exitCode),
		a.newCatalogCommand(exitCode),
		a.newTargetCommand(exitCode),
		a.newRunCommand(exitCode),
		a.newArtifactCommand(exitCode),
	)

	return command
}

func (a App) invocationError(message string) int {
	if _, err := fmt.Fprintf(a.Stderr, "slipramp: %s\n", message); err != nil {
		return ExitInternal
	}

	return ExitInvocation
}

func normalizeCommandError(err error) string {
	message := err.Error()
	if name, found := strings.CutPrefix(message, "unknown flag: --"); found {
		return "flag provided but not defined: -" + name
	}

	return message
}

func (a App) internalError(operation string, err error) int {
	_, _ = fmt.Fprintf(a.Stderr, "slipramp: %s: %v\n", operation, err)

	return ExitInternal
}
