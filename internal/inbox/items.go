package inbox

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

func promotionItem(record PromotionState) (Item, error) {
	var stage promotion.PromotionStageSnapshot
	if err := json.Unmarshal(record.StageSnapshot, &stage); err != nil {
		return Item{}, fmt.Errorf("decode Promotion %s Stage snapshot: %w", record.ID, err)
	}

	destination := Destination{Type: DestinationPromotion, ID: record.ID.String()}
	switch record.Status {
	case promotion.StatusPending:
		title := "Promotion awaits review"
		if stage.Name != "" {
			title = fmt.Sprintf("Promotion to %s awaits review", stage.Name)
		}
		context := make([]string, 0, 2)
		if stage.Application != "" {
			context = append(context, stage.Application)
		}
		context = append(context, fmt.Sprintf("%s → %s", record.CandidateBranch, record.TargetBranch))

		return Item{
			ID:          string(KindPromotionReview) + ":" + record.ID.String(),
			Kind:        KindPromotionReview,
			Title:       truncateText(title, maximumTitleLength),
			Description: strings.Join(context, " · "),
			OccurredAt:  record.CreatedAt.UTC(),
			ActionLabel: "Review",
			Destination: destination,
		}, nil
	case promotion.StatusError:
		description := "Slipramp could not prepare the review artifacts."
		if record.Message != nil {
			description = *record.Message
		}
		title := "Promotion preparation failed"
		if stage.Name != "" {
			title = fmt.Sprintf("Promotion to %s could not be prepared", stage.Name)
		}

		return newProblem(
			KindPromotionError,
			record.ID.String(),
			record.UpdatedAt,
			title,
			description,
			"View promotion",
			destination,
		), nil
	default:
		return Item{}, fmt.Errorf("promotion status %q cannot produce an Inbox item", record.Status)
	}
}

func repositoryRefreshItem(record repository.Repository) (Item, error) {
	if record.Refresh.AttemptedAt == nil {
		return Item{}, fmt.Errorf("failed Repository %s has no refresh attempt time", record.ID)
	}
	description := "The latest branch refresh failed. The last accepted catalog remains active."
	if record.Refresh.FailureReason != nil {
		description = *record.Refresh.FailureReason
	}

	return newProblem(
		KindRepositoryRefresh,
		record.ID.String(),
		*record.Refresh.AttemptedAt,
		fmt.Sprintf("%s could not refresh", record.Name),
		description,
		"View repository",
		Destination{Type: DestinationRepository, ID: record.ID.String()},
	), nil
}

func clusterUnreachableItem(observation cluster.Observation, affectedRuns int) Item {
	description := "The Controller has not checked in recently."
	if affectedRuns == 1 {
		description = "1 active Run has an unknown observation."
	} else if affectedRuns > 1 {
		description = fmt.Sprintf("%d active Runs have unknown observations.", affectedRuns)
	}

	return newProblem(
		KindClusterUnreachable,
		observation.Name,
		observation.LastSeenAt,
		fmt.Sprintf("%s is unreachable", observation.Name),
		description,
		"View cluster",
		Destination{Type: DestinationCluster, ID: observation.Name},
	)
}

func runObservationItem(record ActiveRun) Item {
	occurredAt := record.CreatedAt
	if record.Status.AssignedAt != nil {
		occurredAt = *record.Status.AssignedAt
	}
	if record.Status.LastObservedAt != nil {
		occurredAt = *record.Status.LastObservedAt
	}

	return newProblem(
		KindRunObservation,
		record.ID.String(),
		occurredAt,
		fmt.Sprintf("%s · %s needs investigation", record.TargetName, record.ActionName),
		fmt.Sprintf(
			"The Run remains %s on %s, but its execution state is stale.",
			record.Status.Phase,
			record.ClusterName,
		),
		"View run",
		Destination{Type: DestinationRun, ID: record.ID.String()},
	)
}

func gitTriggerFailureItem(failure gittrigger.FailedDelivery) Item {
	diagnostic := failure.Diagnostic
	if diagnostic == "" {
		diagnostic = "Run creation failed; check Server configuration and dependency health."
	}
	attemptNoun := "attempts"
	if failure.FailureCount == 1 {
		attemptNoun = "attempt"
	}
	description := fmt.Sprintf(
		"Trigger %s in %s exhausted %d %s. %s",
		failure.TriggerName,
		failure.RepositoryName,
		failure.FailureCount,
		attemptNoun,
		diagnostic,
	)

	return newProblem(
		KindGitTriggerFailure,
		failure.ID.String(),
		failure.FailedAt,
		fmt.Sprintf("%s · %s automatic Run could not be created", failure.TargetName, failure.ActionName),
		description,
		"View repository",
		Destination{Type: DestinationRepository, ID: failure.RepositoryID.String()},
	)
}

func newProblem(
	kind Kind,
	resourceID string,
	occurredAt time.Time,
	title string,
	description string,
	actionLabel string,
	destination Destination,
) Item {
	return Item{
		ID:          problemOccurrenceID(kind, resourceID, occurredAt),
		Kind:        kind,
		Title:       truncateText(title, maximumTitleLength),
		Description: truncateText(description, maximumDescriptionLength),
		OccurredAt:  occurredAt.UTC(),
		ActionLabel: actionLabel,
		Destination: destination,
	}
}

func truncateText(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}

	return string(runes[:maximum-1]) + "…"
}
