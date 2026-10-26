package run

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/notification"
)

type CreationNotifications interface {
	Plan(notification.Event) (*notification.EnqueuePlan, error)
}

func (s *Service) planReviewRequestedNotification(record Run) (*notification.EnqueuePlan, error) {
	if record.Review.State != ReviewStatePending || s.notifications == nil {
		return nil, nil
	}

	event, err := runReviewRequestedEvent(record)
	if err != nil {
		return nil, err
	}
	plan, err := s.notifications.Plan(event)
	if err != nil {
		return nil, fmt.Errorf("plan Run review-requested notification: %w", err)
	}

	return plan, nil
}

func runReviewRequestedEvent(record Run) (notification.Event, error) {
	if record.ID == uuid.Nil || record.CreatedAt.IsZero() || record.Source.Type != SourceTypeGit ||
		record.Source.Git == nil {
		return notification.Event{}, errors.New("reviewed Run notification snapshot is incomplete")
	}

	event, err := notification.NewReviewRequestedEvent(notification.ReviewRequestedInput{
		ID:               uuid.New(),
		SubjectType:      notification.SubjectTypeRun,
		SubjectID:        record.ID,
		OccurredAt:       record.CreatedAt,
		DeduplicationKey: "run:" + record.ID.String() + ":review-requested",
		Title:            "Run needs review",
		Summary: fmt.Sprintf(
			"%s/%s %s is ready for review",
			record.Snapshot.Repository.Name,
			record.TargetName,
			record.ActionName,
		),
		Facts: []notification.Fact{
			{Label: "Repository", Value: record.Snapshot.Repository.Name},
			{Label: "Target", Value: record.TargetName},
			{Label: "Action", Value: record.ActionName},
			{Label: "Trigger", Value: record.Source.Git.TriggerName},
			{Label: "Commit", Value: record.CommitSHA},
		},
		Action: notification.Action{
			Label: "Review in Slipramp",
			Path:  "/runs/" + record.ID.String(),
		},
		CreatedAt: record.CreatedAt,
	})
	if err != nil {
		return notification.Event{}, fmt.Errorf("build Run review-requested notification: %w", err)
	}

	return event, nil
}
