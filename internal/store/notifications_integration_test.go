package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/notification"
)

func TestNotificationEnqueueDeduplicationHistoryAndRetention(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	operations, err := notification.NewSlackDestination(
		"/var/run/secrets/slipramp/operations",
		"https://hooks.slack.com/services/do-not-persist",
	)
	if err != nil {
		t.Fatal(err)
	}
	security, err := notification.NewSlackDestination(
		"/var/run/secrets/slipramp/security",
		"https://hooks.slack.com/services/also-do-not-persist",
	)
	if err != nil {
		t.Fatal(err)
	}
	config := notification.DefaultConfig()
	config.PublicURL = "https://slipramp.example.com"
	config.Destinations = map[string]notification.Destination{
		"operations": operations,
		"security":   security,
	}
	config.Routes = []notification.Route{
		{
			Events:       []notification.EventType{notification.EventTypeReviewRequested},
			Destinations: []string{"operations", "security"},
		},
	}
	service, err := notification.NewService(config, database)
	if err != nil {
		t.Fatal(err)
	}

	event := notificationTestEvent(t)
	created, err := service.Enqueue(ctx, event)
	if err != nil || !created {
		t.Fatalf("first Enqueue() = %t, %v", created, err)
	}
	created, err = service.Enqueue(ctx, event)
	if err != nil || created {
		t.Fatalf("duplicate Enqueue() = %t, %v", created, err)
	}

	page, err := service.ListDeliveries(ctx, notification.DeliveryHistoryFilter{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("delivery page = %#v", page)
	}
	for _, item := range page.Items {
		if item.EventID != event.ID || item.State != notification.DeliveryStatePending ||
			item.AttemptCount != 0 || item.NextAttemptAt == nil || item.LastError != "" {
			t.Errorf("delivery = %#v", item)
		}
	}

	var persisted string
	err = database.pool.QueryRow(ctx, `
SELECT coalesce(string_agg(value, ' '), '')
FROM (
    SELECT payload_json::text AS value FROM notification_events
    UNION ALL
    SELECT destination_name || ' ' || destination_type || ' ' || coalesce(last_error, '')
    FROM notification_deliveries
) AS persisted_notifications
`).Scan(&persisted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, "hooks.slack.com") || strings.Contains(persisted, "/var/run/secrets") {
		t.Fatalf("notification persistence contains secret material: %q", persisted)
	}

	if _, err := database.pool.Exec(ctx, `
UPDATE notification_events SET event_type = event_type WHERE id = $1
`, databaseUUID(event.ID)); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable notification event update error = %v", err)
	}

	deleted, err := database.DeleteTerminalNotificationHistoryBefore(ctx, event.CreatedAt.Add(time.Hour))
	if err != nil || deleted != 0 {
		t.Fatalf("retention with pending deliveries = %d, %v", deleted, err)
	}
	if _, err := database.pool.Exec(ctx, `
UPDATE notification_deliveries
SET state = 'delivered', delivered_at = $2
WHERE event_id = $1
`, databaseUUID(event.ID), databaseTime(event.CreatedAt.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	deleted, err = database.DeleteTerminalNotificationHistoryBefore(ctx, event.CreatedAt.Add(time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("retention with terminal deliveries = %d, %v", deleted, err)
	}
	page, err = service.ListDeliveries(ctx, notification.DeliveryHistoryFilter{Limit: 25})
	if err != nil || page.Total != 0 {
		t.Fatalf("delivery page after retention = %#v, %v", page, err)
	}
}

func TestNotificationEnqueueRejectsDeduplicationKeyCollision(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	config := notification.DefaultConfig()
	destination, err := notification.NewSlackDestination(
		"/var/run/secrets/slipramp/operations",
		"https://hooks.slack.com/services/do-not-persist",
	)
	if err != nil {
		t.Fatal(err)
	}
	config.PublicURL = "https://slipramp.example.com"
	config.Destinations["operations"] = destination
	config.Routes = []notification.Route{{
		Events:       []notification.EventType{notification.EventTypeReviewRequested},
		Destinations: []string{"operations"},
	}}
	service, err := notification.NewService(config, database)
	if err != nil {
		t.Fatal(err)
	}

	event := notificationTestEvent(t)
	if created, err := service.Enqueue(ctx, event); err != nil || !created {
		t.Fatalf("first Enqueue() = %t, %v", created, err)
	}
	collision, err := notification.NewReviewRequestedEvent(notification.ReviewRequestedInput{
		ID:               uuid.New(),
		SubjectID:        event.SubjectID,
		OccurredAt:       event.OccurredAt,
		DeduplicationKey: event.DeduplicationKey,
		Title:            "Different transition snapshot",
		Summary:          "different",
		Action: notification.Action{
			Label: "Review",
			Path:  "/promotions/" + event.SubjectID.String(),
		},
		CreatedAt: event.CreatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created, err := service.Enqueue(ctx, collision); err == nil || created ||
		!strings.Contains(err.Error(), "belongs to another event") {
		t.Fatalf("collision Enqueue() = %t, %v", created, err)
	}
}

func TestNotificationDeliveryClaimsPersistAcrossRestartAndProgressIndependently(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	service := notificationServiceForDestinations(t, database, "operations", "security")
	event := notificationTestEvent(t)
	created, err := service.Enqueue(ctx, event)
	if err != nil || !created {
		t.Fatalf("Enqueue() = %t, %v", created, err)
	}

	first, err := database.ClaimNextNotificationDelivery(ctx, notification.DeliveryClaim{
		AttemptedAt:    event.CreatedAt,
		MaximumBackoff: time.Minute,
		Jitter:         1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.AttemptCount != 1 || !first.NextAttemptAt.Equal(event.CreatedAt.Add(5*time.Second)) {
		t.Fatalf("first claim = %#v", first)
	}

	second, err := database.ClaimNextNotificationDelivery(ctx, notification.DeliveryClaim{
		AttemptedAt:    event.CreatedAt,
		MaximumBackoff: time.Minute,
		Jitter:         1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.Destination.Name == first.Destination.Name {
		t.Fatalf("independent claims = %#v and %#v", first, second)
	}
	if updated, err := database.MarkNotificationDeliveryDelivered(
		ctx,
		second.ID,
		second.AttemptCount,
		event.CreatedAt.Add(time.Second),
	); err != nil || !updated {
		t.Fatalf("MarkNotificationDeliveryDelivered() = %t, %v", updated, err)
	}

	_, err = database.ClaimNextNotificationDelivery(ctx, notification.DeliveryClaim{
		AttemptedAt:    event.CreatedAt.Add(time.Second),
		MaximumBackoff: time.Minute,
		Jitter:         1,
	})
	if !errors.Is(err, notification.ErrNoEligibleDelivery) {
		t.Fatalf("claim before persisted retry = %v", err)
	}

	restartedAttempt, err := database.ClaimNextNotificationDelivery(ctx, notification.DeliveryClaim{
		AttemptedAt:    event.CreatedAt.Add(5 * time.Second),
		MaximumBackoff: time.Minute,
		Jitter:         1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if restartedAttempt.ID != first.ID || restartedAttempt.AttemptCount != 2 ||
		!restartedAttempt.NextAttemptAt.Equal(event.CreatedAt.Add(15*time.Second)) {
		t.Fatalf("restart claim = %#v", restartedAttempt)
	}
	if updated, err := database.FailNotificationDelivery(
		ctx,
		restartedAttempt.ID,
		restartedAttempt.AttemptCount,
		event.CreatedAt.Add(6*time.Second),
		"Slack rejected the delivery (HTTP 400).",
	); err != nil || !updated {
		t.Fatalf("FailNotificationDelivery() = %t, %v", updated, err)
	}

	page, err := service.ListDeliveries(ctx, notification.DeliveryHistoryFilter{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	states := map[notification.DeliveryState]int{}
	for _, item := range page.Items {
		states[item.State]++
		if item.State == notification.DeliveryStateFailed &&
			(item.AttemptCount != 2 || item.LastError != "Slack rejected the delivery (HTTP 400).") {
			t.Errorf("failed delivery = %#v", item)
		}
	}
	if states[notification.DeliveryStateDelivered] != 1 || states[notification.DeliveryStateFailed] != 1 {
		t.Fatalf("delivery states = %#v", states)
	}
}

func notificationServiceForDestinations(
	t *testing.T,
	database *Database,
	names ...string,
) *notification.Service {
	t.Helper()
	config := notification.DefaultConfig()
	config.PublicURL = "https://slipramp.example.com"
	for _, name := range names {
		destination, err := notification.NewSlackDestination(
			"/var/run/secrets/slipramp/"+name,
			"https://hooks.slack.com/services/"+name,
		)
		if err != nil {
			t.Fatal(err)
		}
		config.Destinations[name] = destination
	}
	config.Routes = []notification.Route{{
		Events:       []notification.EventType{notification.EventTypeReviewRequested},
		Destinations: names,
	}}
	service, err := notification.NewService(config, database)
	if err != nil {
		t.Fatal(err)
	}

	return service
}

func notificationTestEvent(t *testing.T) notification.Event {
	t.Helper()
	at := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	subjectID := uuid.MustParse("00000000-0000-0000-0000-000000000202")
	event, err := notification.NewReviewRequestedEvent(notification.ReviewRequestedInput{
		ID:               uuid.MustParse("00000000-0000-0000-0000-000000000201"),
		SubjectID:        subjectID,
		OccurredAt:       at,
		DeduplicationKey: "promotion:" + subjectID.String() + ":review-requested",
		Title:            "Promotion to production needs review",
		Summary:          "external-dns - candidate to production",
		Facts:            []notification.Fact{{Label: "Repository", Value: "kcloud-ops"}},
		Action: notification.Action{
			Label: "Review in Slipramp",
			Path:  "/promotions/" + subjectID.String(),
		},
		CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}
