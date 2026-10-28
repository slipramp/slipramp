package cli

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

type targetRecordOptions struct {
	serverURL string
	format    string
}

func (a App) newTargetGetCommand(exitCode *int) *cobra.Command {
	options := targetRecordOptions{}
	command := &cobra.Command{
		Use:   "get TARGET_ID",
		Short: "Get a Target",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 1 {
				return errors.New("exactly one Target ID is required")
			}

			return nil
		},
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runTargetGet(options, arguments[0])
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runTargetGet(options targetRecordOptions, identity string) int {
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
	response, err := client.API.GetTarget(a.commandContext(), targetID)
	if err != nil {
		return a.remoteError(client.NetworkError("get Target", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseGetTargetResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("get Target", err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return a.remoteError(client.ResponseError("get Target", parsed.StatusCode(), parsed.Body))
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON200); err != nil {
			return a.internalError("write Target", err)
		}

		return ExitValid
	}
	if err := renderTargetText(a.Stdout, *parsed.JSON200); err != nil {
		return a.internalError("write Target", err)
	}

	return ExitValid
}

func renderTargetText(output io.Writer, target api.Target) error {
	definition := target.Definition
	state := "active"
	if target.ArchivedAt != nil {
		state = "archived at " + renderTime(target.ArchivedAt)
	}
	watch := "-"
	if definition.Spec.Watch != nil && len(*definition.Spec.Watch) != 0 {
		watch = strings.Join(*definition.Spec.Watch, ",")
	}
	actions := strings.Join(slices.Sorted(maps.Keys(definition.Spec.Actions)), ",")
	lines := []string{
		fmt.Sprintf("Target: %s (%s)", definition.Metadata.Name, target.ID),
		"Title: " + optionalString(definition.Metadata.Title),
		fmt.Sprintf("Repository: %s (%s)", target.RepositoryName, target.RepositoryID),
		"State: " + state,
		"Catalog revision: " + target.CatalogRevisionID.String(),
		"Catalog commit: " + target.CatalogCommit.Sha,
		"Desired commit: " + definition.Status.DesiredCommit.Sha,
		"Reconciliation: " + string(definition.Status.Reconciliation.Comparison),
		fmt.Sprintf("Source: %s document %d", definition.Source.Path, definition.Source.Document),
		"Path: " + optionalString(definition.Spec.Path),
		"Runtime: " + optionalString(definition.Spec.Runtime),
		"Placement: " + optionalString(definition.Spec.Placement),
		"Timeout: " + optionalString(definition.Spec.Timeout),
		"Watch: " + watch,
		"Actions: " + actions,
		fmt.Sprintf("Web: /targets/%s", target.ID),
	}
	_, err := fmt.Fprintln(output, strings.Join(lines, "\n"))

	return err
}
