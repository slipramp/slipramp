package run

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/notification"
)

func TestReviewedGitRunPlansReviewRequestedNotification(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	planner := &recordingCreationNotifications{}
	service.notifications = planner
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1600")
	deliveryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1601")
	createdAt := time.Date(2026, time.October, 25, 18, 0, 0, 0, time.UTC)
	service.newID = func() uuid.UUID { return runID }
	service.now = func() time.Time { return createdAt }
	idempotencyKey := deliveryID.String()
	revisionID := testRevisionID

	created, err := service.CreateRun(t.Context(), CreateRequest{
		TargetID:          testTargetID,
		ActionName:        "preview",
		Parameters:        map[string]any{"mode": "safe"},
		CatalogRevisionID: &revisionID,
		ReviewRequired:    true,
		Source: Source{
			Type: SourceTypeGit,
			Git: &GitRunSource{
				TriggerName: "preview-on-change",
				DeliveryID:  deliveryID,
			},
		},
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.Review.State != ReviewStatePending || planner.calls != 1 || runs.notificationPlan == nil {
		t.Fatalf(
			"created review = %q, planner calls = %d, persistence plan = %#v",
			created.Review.State,
			planner.calls,
			runs.notificationPlan,
		)
	}

	event := runs.notificationPlan.Event
	if event.SubjectType != notification.SubjectTypeRun || event.SubjectID != runID ||
		event.DeduplicationKey != "run:"+runID.String()+":review-requested" ||
		!event.OccurredAt.Equal(createdAt) {
		t.Fatalf("notification event = %#v", event)
	}
	var payload notification.ReviewRequestedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Title != "Run needs review" ||
		payload.Summary != "infrastructure/network preview is ready for review" ||
		payload.Action.Path != "/runs/"+runID.String() || len(payload.Facts) != 5 ||
		payload.Facts[3] != (notification.Fact{Label: "Trigger", Value: "preview-on-change"}) ||
		payload.Facts[4] != (notification.Fact{Label: "Commit", Value: testCommitSHA}) {
		t.Fatalf("notification payload = %#v", payload)
	}
}

func TestRunCreationStopsWhenNotificationPlanningFails(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	service.notifications = &recordingCreationNotifications{err: errors.New("routing failed")}
	deliveryID := uuid.New()
	idempotencyKey := deliveryID.String()
	revisionID := testRevisionID

	_, err := service.CreateRun(context.Background(), CreateRequest{
		TargetID:          testTargetID,
		ActionName:        "preview",
		Parameters:        map[string]any{"mode": "safe"},
		CatalogRevisionID: &revisionID,
		ReviewRequired:    true,
		Source: Source{
			Type: SourceTypeGit,
			Git:  &GitRunSource{TriggerName: "preview-on-change", DeliveryID: deliveryID},
		},
		IdempotencyKey: &idempotencyKey,
	})
	if err == nil || !contains(err, "plan Run review-requested notification: routing failed") {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if runs.calls != 0 {
		t.Fatalf("persistence calls = %d, want 0", runs.calls)
	}
}

type recordingCreationNotifications struct {
	calls int
	event notification.Event
	err   error
}

func (s *recordingCreationNotifications) Plan(event notification.Event) (*notification.EnqueuePlan, error) {
	s.calls++
	s.event = event
	if s.err != nil {
		return nil, s.err
	}

	return &notification.EnqueuePlan{
		Event: event,
		Destinations: []notification.DestinationSnapshot{{
			Name: "operations",
			Type: notification.DestinationTypeSlack,
		}},
	}, nil
}
