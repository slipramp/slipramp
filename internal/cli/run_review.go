package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

const maximumRunReviewCommentLength = 4096

type runReviewOptions struct {
	serverURL string
	format    string
	comment   string
}

func (a App) newRunReviewCommand(exitCode *int) *cobra.Command {
	command := &cobra.Command{
		Use:   "review",
		Short: "Decide pending Run reviews",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("run review requires the approve or reject command")
		},
	}
	command.AddCommand(
		a.newRunReviewDecisionCommand(exitCode, api.RunReviewDecisionApproved),
		a.newRunReviewDecisionCommand(exitCode, api.RunReviewDecisionRejected),
	)

	return command
}

func (a App) newRunReviewDecisionCommand(
	exitCode *int,
	decision api.RunReviewDecision,
) *cobra.Command {
	options := runReviewOptions{}
	use := "approve RUN_ID"
	short := "Approve a pending Run review"
	if decision == api.RunReviewDecisionRejected {
		use = "reject RUN_ID"
		short = "Reject a pending Run review"
	}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  requireRunIdentity,
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runRunReviewDecision(options, decision, arguments[0])
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.comment, "comment", "", "optional immutable review comment")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runRunReviewDecision(
	options runReviewOptions,
	decision api.RunReviewDecision,
	identity string,
) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	runID, err := parseRunIdentity(identity)
	if err != nil {
		return a.invocationError(err.Error())
	}
	comment := strings.TrimSpace(options.comment)
	if !utf8.ValidString(comment) || utf8.RuneCountInString(comment) > maximumRunReviewCommentLength {
		return a.invocationError(fmt.Sprintf("--comment must be at most %d characters", maximumRunReviewCommentLength))
	}
	body := api.RunReviewDecisionRequest{Decision: decision}
	if comment != "" {
		body.Comment = &comment
	}
	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}

	response, err := client.API.DecideRunReview(a.commandContext(), runID, body)
	if err != nil {
		return a.remoteError(client.NetworkError("decide Run review", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseDecideRunReviewResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("decide Run review", err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return a.remoteError(client.ResponseError("decide Run review", parsed.StatusCode(), parsed.Body))
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON200); err != nil {
			return a.internalError("write reviewed Run", err)
		}
	} else if err := renderRunText(a.Stdout, *parsed.JSON200); err != nil {
		return a.internalError("write reviewed Run", err)
	}

	return ExitValid
}
