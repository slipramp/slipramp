package cli

import (
	"errors"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func (a App) newRunCommand(exitCode *int) *cobra.Command {
	command := &cobra.Command{
		Use:   "run",
		Short: "Operate Runs",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("run requires the list, create, get, logs, wait, cancel, rerun, or review command")
		},
	}
	command.AddCommand(
		a.newRunListCommand(exitCode),
		a.newRunCreateCommand(exitCode),
		a.newRunGetCommand(exitCode),
		a.newRunLogsCommand(exitCode),
		a.newRunWaitCommand(exitCode),
		a.newRunCancelCommand(exitCode),
		a.newRunRerunCommand(exitCode),
		a.newRunReviewCommand(exitCode),
	)

	return command
}

type runRecordOptions struct {
	serverURL string
	format    string
}

func addRunRecordFlags(command *cobra.Command, options *runRecordOptions) {
	command.Flags().StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	command.Flags().StringVar(&options.format, "format", "text", "output format: text or json")
}

func requireRunIdentity(_ *cobra.Command, arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("exactly one Run ID is required")
	}

	return nil
}

func parseRunIdentity(identity string) (uuid.UUID, error) {
	runID, err := uuid.Parse(identity)
	if err != nil {
		return uuid.Nil, errors.New("Run ID must be a UUID")
	}

	return runID, nil
}
