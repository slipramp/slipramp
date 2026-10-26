package cli

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cli/remote"
)

func (a App) newRunGetCommand(exitCode *int) *cobra.Command {
	options := runRecordOptions{}
	command := &cobra.Command{
		Use:   "get RUN_ID",
		Short: "Get a Run",
		Args:  requireRunIdentity,
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runRunGet(options, arguments[0])
		},
	}
	addRunRecordFlags(command, &options)

	return command
}

func (a App) runRunGet(options runRecordOptions, identity string) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	runID, err := parseRunIdentity(identity)
	if err != nil {
		return a.invocationError(err.Error())
	}

	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	runRecord, exitCode := a.loadRun(client, runID, "get Run")
	if exitCode != ExitValid {
		return exitCode
	}
	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, &runRecord); err != nil {
			return a.internalError("write Run", err)
		}
	} else if err := renderRunText(a.Stdout, runRecord); err != nil {
		return a.internalError("write Run", err)
	}

	return ExitValid
}

func (a App) newRunCancelCommand(exitCode *int) *cobra.Command {
	options := runRecordOptions{}
	command := &cobra.Command{
		Use:   "cancel RUN_ID",
		Short: "Request cancellation of a Run",
		Args:  requireRunIdentity,
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runRunCancel(options, arguments[0])
		},
	}
	addRunRecordFlags(command, &options)

	return command
}

func (a App) runRunCancel(options runRecordOptions, identity string) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	runID, err := parseRunIdentity(identity)
	if err != nil {
		return a.invocationError(err.Error())
	}

	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	response, err := client.API.CancelRun(a.commandContext(), runID)
	if err != nil {
		return a.remoteError(client.NetworkError("cancel Run", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseCancelRunResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("cancel Run", err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return a.remoteError(client.ResponseError("cancel Run", parsed.StatusCode(), parsed.Body))
	}
	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON200); err != nil {
			return a.internalError("write canceled Run", err)
		}
	} else if err := renderRunText(a.Stdout, *parsed.JSON200); err != nil {
		return a.internalError("write canceled Run", err)
	}

	return ExitValid
}

func (a App) loadRun(client *remote.Client, runID uuid.UUID, operation string) (api.Run, int) {
	response, err := client.API.GetRun(a.commandContext(), runID)
	if err != nil {
		return api.Run{}, a.remoteError(client.NetworkError(operation, err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseGetRunResponse(response)
	if err != nil {
		return api.Run{}, a.remoteError(client.InvalidResponse(operation, err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return api.Run{}, a.remoteError(client.ResponseError(operation, parsed.StatusCode(), parsed.Body))
	}

	return *parsed.JSON200, ExitValid
}
