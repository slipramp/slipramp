package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/api"
)

func renderCreatedRunText(output io.Writer, runRecord api.Run) error {
	_, err := fmt.Fprintf(
		output,
		"Run: %s\nPhase: %s\nTarget: %s (%s)\nAction: %s\nCommit: %s\nCluster: %s\nWeb: /runs/%s\n",
		runRecord.ID,
		runRecord.Phase,
		runRecord.Target,
		runRecord.TargetID,
		runRecord.Action,
		runRecord.Commit.Sha,
		runRecord.Cluster,
		runRecord.ID,
	)

	return err
}

func renderRunText(output io.Writer, runRecord api.Run) error {
	lines := []string{
		fmt.Sprintf("Run: %s", runRecord.ID),
		fmt.Sprintf("Phase: %s", runRecord.Phase),
		fmt.Sprintf("Outcome: %s", optionalRunOutcome(runRecord.Outcome)),
		fmt.Sprintf("Observation: %s", optionalObservation(runRecord.ObservationState)),
		fmt.Sprintf("Target: %s (%s)", runRecord.Target, runRecord.TargetID),
		fmt.Sprintf("Action: %s", runRecord.Action),
		fmt.Sprintf("Source: %s", renderRunSource(runRecord.Source)),
		fmt.Sprintf("Commit: %s", runRecord.Commit.Sha),
		fmt.Sprintf("Placement: %s", runRecord.Placement),
		fmt.Sprintf("Cluster: %s", runRecord.Cluster),
		fmt.Sprintf("Created: %s", renderTime(&runRecord.CreatedAt)),
		fmt.Sprintf("Assigned: %s", renderTime(runRecord.AssignedAt)),
		fmt.Sprintf("Preparing: %s", renderTime(runRecord.PreparingAt)),
		fmt.Sprintf("Started: %s", renderTime(runRecord.StartedAt)),
		fmt.Sprintf("Finished: %s", renderTime(runRecord.FinishedAt)),
		fmt.Sprintf("Last observed: %s", renderTime(runRecord.LastObservedAt)),
		fmt.Sprintf("Cancellation: %s", renderCancellation(runRecord)),
		fmt.Sprintf("Exit code: %s", optionalInt32(runRecord.ExitCode)),
		fmt.Sprintf("Outcome reason: %s", optionalString(runRecord.OutcomeReason)),
		fmt.Sprintf("Outcome message: %s", optionalString(runRecord.OutcomeMessage)),
		fmt.Sprintf("Web: /runs/%s", runRecord.ID),
	}
	_, err := fmt.Fprintln(output, strings.Join(lines, "\n"))

	return err
}

func renderRunSource(source api.RunSource) string {
	result := string(source.Type)
	if source.Git != nil {
		result += fmt.Sprintf(" (trigger %s, delivery %s)", source.Git.TriggerName, source.Git.DeliveryID)
	}
	if source.Promotion != nil {
		result += fmt.Sprintf(" (Promotion %s)", source.Promotion.PromotionID)
	}

	return result
}

func renderCancellation(runRecord api.Run) string {
	if runRecord.CancellationRequestedAt == nil {
		return "not requested"
	}
	result := "requested at " + renderTime(runRecord.CancellationRequestedAt)
	if runRecord.CanceledBy != nil {
		result += " by " + runRecord.CanceledBy.DisplayName
	}

	return result
}

func renderTime(value *time.Time) string {
	if value == nil {
		return "-"
	}

	return value.UTC().Format(time.RFC3339)
}

func optionalRunOutcome(value *api.RunOutcome) string {
	if value == nil {
		return "-"
	}

	return string(*value)
}

func optionalObservation(value *api.RunObservationState) string {
	if value == nil {
		return "-"
	}

	return string(*value)
}

func optionalInt32(value *int32) string {
	if value == nil {
		return "-"
	}

	return fmt.Sprintf("%d", *value)
}
