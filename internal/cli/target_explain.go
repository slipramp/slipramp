package cli

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

func (a App) newTargetExplainCommand(exitCode *int) *cobra.Command {
	options := targetRecordOptions{}
	command := &cobra.Command{
		Use:   "explain TARGET_ID ACTION",
		Short: "Explain a Target Action using Server Placement configuration",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 2 {
				return errors.New("target explain requires TARGET_ID ACTION [flags]")
			}

			return nil
		},
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runTargetExplain(options, arguments[0], arguments[1])
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runTargetExplain(options targetRecordOptions, identity, actionName string) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	targetID, err := uuid.Parse(identity)
	if err != nil {
		return a.invocationError("Target ID must be a UUID")
	}
	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	response, err := client.API.ExplainTargetAction(a.commandContext(), targetID, actionName)
	if err != nil {
		return a.remoteError(client.NetworkError("explain Target Action", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseExplainTargetActionResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("explain Target Action", err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return a.remoteError(client.ResponseError("explain Target Action", parsed.StatusCode(), parsed.Body))
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON200); err != nil {
			return a.internalError("write Target Action explanation", err)
		}

		return ExitValid
	}
	if err := renderExplanationText(a.Stdout, *parsed.JSON200); err != nil {
		return a.internalError("write Target Action explanation", err)
	}

	return ExitValid
}
