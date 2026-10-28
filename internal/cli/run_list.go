package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cli/remote"
)

var fullCommitSHA = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type runListOptions struct {
	serverURL       string
	format          string
	repositoryID    string
	targetID        string
	commitSHA       string
	inputArtifactID string
	cluster         string
	phase           string
	reviewState     string
	all             bool
	limit           int
	offset          int
}

func (a App) newRunListCommand(exitCode *int) *cobra.Command {
	options := runListOptions{}
	command := &cobra.Command{
		Use:   "list",
		Short: "List Runs from the Slipramp Server",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 0 {
				return errors.New("run list does not accept positional arguments")
			}

			return nil
		},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = a.runRunList(options)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.repositoryID, "repository-id", "", "exact Repository UUID")
	flags.StringVar(&options.targetID, "target-id", "", "exact Target UUID")
	flags.StringVar(&options.commitSHA, "commit-sha", "", "exact full commit SHA (requires --repository-id)")
	flags.StringVar(&options.inputArtifactID, "input-artifact-id", "", "exact input Artifact UUID")
	flags.StringVar(&options.cluster, "cluster", "", "exact execution Cluster")
	flags.StringVar(&options.phase, "phase", "", "Run phase: queued, assigned, preparing, running, or finished")
	flags.StringVar(
		&options.reviewState,
		"review-state",
		"",
		"review state: not-required, pending, approved, or rejected",
	)
	flags.BoolVar(&options.all, "all", false, "retrieve every matching Run at or after --offset")
	flags.IntVar(&options.limit, "limit", 25, "maximum number of Runs requested per page")
	flags.IntVar(&options.offset, "offset", 0, "zero-based result offset")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runRunList(options runListOptions) int {
	params, err := runListParams(options)
	if err != nil {
		return a.invocationError(err.Error())
	}
	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	list, err := a.loadRunListPage(client, params)
	if err != nil {
		return a.remoteError(err)
	}
	if options.all {
		items, total, collectErr := collectListPages(options.offset, func(offset int) (listPage[api.Run], error) {
			if offset == options.offset {
				return runListPage(list), nil
			}
			offset32 := int32(offset)
			params.Offset = &offset32
			page, loadErr := a.loadRunListPage(client, params)
			if loadErr != nil {
				return listPage[api.Run]{}, loadErr
			}

			return runListPage(page), nil
		})
		if collectErr != nil {
			if isInvalidListPage(collectErr) {
				return a.remoteError(client.InvalidResponse("list Runs", collectErr))
			}

			return a.remoteError(collectErr)
		}
		list.Items = items
		list.Total = int(total)
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, &list); err != nil {
			return a.internalError("write Run list", err)
		}

		return ExitValid
	}
	if err := renderRunListText(a.Stdout, list); err != nil {
		return a.internalError("write Run list", err)
	}

	return ExitValid
}

func (a App) loadRunListPage(client *remote.Client, params api.ListRunsParams) (api.RunList, error) {
	response, err := client.API.ListRuns(a.commandContext(), &params)
	if err != nil {
		return api.RunList{}, client.NetworkError("list Runs", err)
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseListRunsResponse(response)
	if err != nil {
		return api.RunList{}, client.InvalidResponse("list Runs", err)
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return api.RunList{}, client.ResponseError("list Runs", parsed.StatusCode(), parsed.Body)
	}

	return *parsed.JSON200, nil
}

func runListPage(list api.RunList) listPage[api.Run] {
	return listPage[api.Run]{items: list.Items, offset: list.Offset, total: int64(list.Total)}
}

func runListParams(options runListOptions) (api.ListRunsParams, error) {
	if err := validateRemoteFormat(options.format); err != nil {
		return api.ListRunsParams{}, err
	}
	if options.limit < 1 || options.limit > 100 {
		return api.ListRunsParams{}, errors.New("--limit must be between 1 and 100")
	}
	if options.offset < 0 || int64(options.offset) > math.MaxInt32 {
		return api.ListRunsParams{}, errors.New("--offset must be between 0 and 2147483647")
	}
	if options.commitSHA != "" {
		if options.repositoryID == "" {
			return api.ListRunsParams{}, errors.New("--commit-sha requires --repository-id")
		}
		if !fullCommitSHA.MatchString(options.commitSHA) {
			return api.ListRunsParams{}, errors.New("--commit-sha must be a full lowercase 40- or 64-character SHA")
		}
	}

	params := api.ListRunsParams{Limit: &options.limit}
	offset32 := int32(options.offset)
	params.Offset = &offset32
	if options.repositoryID != "" {
		repositoryID, err := uuid.Parse(options.repositoryID)
		if err != nil {
			return api.ListRunsParams{}, errors.New("--repository-id must be a UUID")
		}
		params.RepositoryID = &repositoryID
	}
	if options.targetID != "" {
		targetID, err := uuid.Parse(options.targetID)
		if err != nil {
			return api.ListRunsParams{}, errors.New("--target-id must be a UUID")
		}
		params.TargetID = &targetID
	}
	if options.inputArtifactID != "" {
		artifactID, err := uuid.Parse(options.inputArtifactID)
		if err != nil {
			return api.ListRunsParams{}, errors.New("--input-artifact-id must be a UUID")
		}
		params.InputArtifactID = &artifactID
	}
	if options.commitSHA != "" {
		params.CommitSha = &options.commitSHA
	}
	if options.cluster != "" {
		cluster := api.CatalogName(options.cluster)
		params.Cluster = &cluster
	}
	if options.phase != "" {
		phase := api.ListRunsParamsPhase(options.phase)
		if !phase.Valid() {
			return api.ListRunsParams{}, fmt.Errorf("unsupported --phase %q", options.phase)
		}
		params.Phase = &phase
	}
	if options.reviewState != "" {
		reviewState := api.RunReviewState(options.reviewState)
		if !reviewState.Valid() {
			return api.ListRunsParams{}, fmt.Errorf("unsupported --review-state %q", options.reviewState)
		}
		params.ReviewState = &reviewState
	}

	return params, nil
}

func renderRunListText(output io.Writer, list api.RunList) error {
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		table,
		"ID\tTARGET\tACTION\tPHASE\tSCHEDULING\tOUTCOME\tREVIEW\tOBSERVATION\tCLUSTER\tSOURCE\tCREATED",
	); err != nil {
		return err
	}
	for _, runRecord := range list.Items {
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			runRecord.ID,
			runRecord.Target,
			runRecord.Action,
			runRecord.Phase,
			optionalSchedulingState(runRecord.SchedulingState),
			optionalRunOutcome(runRecord.Outcome),
			runRecord.Review.State,
			optionalObservation(runRecord.ObservationState),
			runRecord.Cluster,
			renderRunSource(runRecord.Source),
			renderTime(&runRecord.CreatedAt),
		); err != nil {
			return err
		}
	}

	return table.Flush()
}
