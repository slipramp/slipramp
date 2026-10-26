package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

type runRerunOptions struct {
	serverURL      string
	format         string
	mode           string
	idempotencyKey string
}

func (a App) newRunRerunCommand(exitCode *int) *cobra.Command {
	options := runRerunOptions{}
	command := &cobra.Command{
		Use:   "rerun RUN_ID",
		Short: "Create a Run from a finished Run",
		Args:  requireRunIdentity,
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runRunRerun(options, arguments[0])
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.mode, "mode", "", "rerun mode: exact or latest (required)")
	flags.StringVar(&options.idempotencyKey, "idempotency-key", "", "stable request key (default: fresh random UUID)")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runRunRerun(options runRerunOptions, identity string) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	runID, err := parseRunIdentity(identity)
	if err != nil {
		return a.invocationError(err.Error())
	}
	mode := api.RunRerunMode(options.mode)
	if !mode.Valid() {
		return a.invocationError(fmt.Sprintf("--mode must be %q or %q", api.RunRerunModeExact, api.RunRerunModeLatest))
	}
	idempotencyKey, err := runIdempotencyKey(options.idempotencyKey)
	if err != nil {
		return a.invocationError(err.Error())
	}
	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}

	params := api.RerunRunParams{IdempotencyKey: &idempotencyKey}
	response, err := client.API.RerunRun(a.commandContext(), runID, &params, api.RunRerun{Mode: mode})
	if err != nil {
		return a.remoteError(client.NetworkError("rerun Run", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseRerunRunResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("rerun Run", err))
	}
	if parsed.StatusCode() != http.StatusCreated || parsed.JSON201 == nil {
		return a.remoteError(client.ResponseError("rerun Run", parsed.StatusCode(), parsed.Body))
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON201); err != nil {
			return a.internalError("write rerun", err)
		}
	} else if err := renderCreatedRunText(a.Stdout, *parsed.JSON201); err != nil {
		return a.internalError("write rerun", err)
	}

	return ExitValid
}
