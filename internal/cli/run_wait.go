package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

const defaultRunPollInterval = time.Second

type runWaitOptions struct {
	serverURL    string
	format       string
	timeout      time.Duration
	pollInterval time.Duration
}

func (a App) newRunWaitCommand(exitCode *int) *cobra.Command {
	options := runWaitOptions{pollInterval: defaultRunPollInterval}
	command := &cobra.Command{
		Use:   "wait RUN_ID",
		Short: "Wait for a Run to finish",
		Args:  requireRunIdentity,
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runRunWait(options, arguments[0])
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.DurationVar(&options.timeout, "timeout", 0, "maximum wait duration (default: no timeout)")
	flags.DurationVar(&options.pollInterval, "poll-interval", defaultRunPollInterval, "Run status poll interval")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runRunWait(options runWaitOptions, identity string) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	if options.timeout < 0 {
		return a.invocationError("--timeout must be zero or positive")
	}
	if options.pollInterval <= 0 {
		return a.invocationError("--poll-interval must be positive")
	}
	runID, err := parseRunIdentity(identity)
	if err != nil {
		return a.invocationError(err.Error())
	}
	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}

	ctx := a.commandContext()
	if options.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.timeout)
		defer cancel()
	}

	for {
		runRecord, exitCode := a.loadRun(ctx, client, runID, "wait for Run")
		if exitCode != ExitValid {
			return exitCode
		}
		if runRecord.Phase == api.RunPhaseFinished {
			return a.finishRunWait(options.format, runRecord)
		}
		if err := waitForNextRunPoll(ctx, options.pollInterval); err != nil {
			return a.remoteError(fmt.Errorf("wait for Run: %w", err))
		}
	}
}

func (a App) finishRunWait(format string, runRecord api.Run) int {
	if runRecord.Outcome == nil || !runRecord.Outcome.Valid() {
		return a.remoteError(errors.New("wait for Run: invalid Server response: finished Run has no valid outcome"))
	}
	if format == "json" {
		if err := renderRemoteJSON(a.Stdout, &runRecord); err != nil {
			return a.internalError("write finished Run", err)
		}
	} else if err := renderRunText(a.Stdout, runRecord); err != nil {
		return a.internalError("write finished Run", err)
	}
	if *runRecord.Outcome != api.RunOutcomeSucceeded {
		return ExitRunUnsuccessful
	}

	return ExitValid
}

func waitForNextRunPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
