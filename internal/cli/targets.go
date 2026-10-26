package cli

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

func (a App) newTargetCommand(exitCode *int) *cobra.Command {
	command := &cobra.Command{
		Use:   "target",
		Short: "Inspect Targets",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("target requires the list command")
		},
	}
	command.AddCommand(a.newTargetListCommand(exitCode))

	return command
}

type targetListOptions struct {
	serverURL    string
	format       string
	query        string
	repositoryID string
	runtime      string
	placement    string
	limit        int
	offset       int
}

func (a App) newTargetListCommand(exitCode *int) *cobra.Command {
	options := targetListOptions{}
	command := &cobra.Command{
		Use:   "list",
		Short: "List Targets from the Slipramp Server",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 0 {
				return fmt.Errorf("target list does not accept positional arguments")
			}

			return nil
		},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = a.runTargetList(options)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.query, "query", "", "target, repository, or action search")
	flags.StringVar(&options.repositoryID, "repository-id", "", "exact Repository UUID")
	flags.StringVar(&options.runtime, "runtime", "", "exact target-level Runtime")
	flags.StringVar(&options.placement, "placement", "", "exact target-level Placement")
	flags.IntVar(&options.limit, "limit", 25, "maximum number of Targets")
	flags.IntVar(&options.offset, "offset", 0, "zero-based result offset")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runTargetList(options targetListOptions) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	if options.limit < 1 || options.limit > 100 {
		return a.invocationError("--limit must be between 1 and 100")
	}
	if options.offset < 0 || int64(options.offset) > math.MaxInt32 {
		return a.invocationError("--offset must be between 0 and 2147483647")
	}

	params := api.ListTargetsParams{Limit: &options.limit}
	offset32 := int32(options.offset)
	params.Offset = &offset32
	if options.query != "" {
		params.Q = &options.query
	}
	if options.runtime != "" {
		runtime := api.CatalogName(options.runtime)
		params.Runtime = &runtime
	}
	if options.placement != "" {
		placement := api.CatalogName(options.placement)
		params.Placement = &placement
	}
	if options.repositoryID != "" {
		repositoryID, err := uuid.Parse(options.repositoryID)
		if err != nil {
			return a.invocationError("--repository-id must be a UUID")
		}
		params.RepositoryID = &repositoryID
	}

	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	response, err := client.API.ListTargets(a.commandContext(), &params)
	if err != nil {
		return a.remoteError(client.NetworkError("list Targets", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseListTargetsResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("list Targets", err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return a.remoteError(client.ResponseError("list Targets", parsed.StatusCode(), parsed.Body))
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON200); err != nil {
			return a.internalError("write Target list", err)
		}

		return ExitValid
	}
	if err := renderTargetListText(a.Stdout, *parsed.JSON200); err != nil {
		return a.internalError("write Target list", err)
	}

	return ExitValid
}

func renderTargetListText(output io.Writer, list api.TargetSummaryList) error {
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "ID\tNAME\tTITLE\tREPOSITORY\tRUNTIME\tPLACEMENT\tRECONCILIATION\tACTIONS"); err != nil {
		return err
	}
	for _, target := range list.Items {
		title := "-"
		if target.Title != nil {
			title = *target.Title
		}
		runtimeName := optionalString(target.Runtime)
		placementName := optionalString(target.Placement)
		reconciliation := "-"
		if slices.Contains(target.Actions, "sync") {
			reconciliation = string(target.Reconciliation.Comparison)
		}
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			target.ID,
			target.Name,
			title,
			target.RepositoryName,
			runtimeName,
			placementName,
			reconciliation,
			strings.Join(target.Actions, ","),
		); err != nil {
			return err
		}
	}

	return table.Flush()
}

func optionalString(value *string) string {
	if value == nil || *value == "" {
		return "-"
	}

	return *value
}
