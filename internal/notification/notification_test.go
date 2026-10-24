package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestConfigRoutesExactEventsWithoutDuplicatingDestinations(t *testing.T) {
	t.Parallel()

	operations, err := NewSlackDestination(
		"/var/run/secrets/slipramp/operations",
		"https://hooks.slack.com/services/secret-operations",
	)
	if err != nil {
		t.Fatalf("NewSlackDestination() error = %v", err)
	}
	security, err := NewSlackDestination(
		"/var/run/secrets/slipramp/security",
		"https://hooks.slack.com/services/secret-security",
	)
	if err != nil {
		t.Fatalf("NewSlackDestination() error = %v", err)
	}
	config := DefaultConfig()
	config.PublicURL = "https://slipramp.example.com"
	config.Destinations = map[string]Destination{
		"operations": operations,
		"security":   security,
	}
	config.Routes = []Route{
		{Events: []EventType{EventTypeReviewRequested}, Destinations: []string{"security", "operations"}},
		{Events: []EventType{EventTypeReviewRequested}, Destinations: []string{"operations"}},
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	destinations := config.DestinationsFor(EventTypeReviewRequested)
	if len(destinations) != 2 || destinations[0].Name != "operations" || destinations[1].Name != "security" {
		t.Fatalf("DestinationsFor() = %#v", destinations)
	}
	if secret, ok := operations.SlackWebhookURL(); !ok || secret != "https://hooks.slack.com/services/secret-operations" {
		t.Fatal("SlackWebhookURL() did not return the in-memory credential")
	}
	formattedDestination := fmt.Sprintf("%#v", operations)
	if strings.Contains(operations.webhookURL.GoString(), "secret-operations") ||
		strings.Contains(operations.webhookURL.LogValue().String(), "secret-operations") ||
		strings.Contains(formattedDestination, "secret-operations") ||
		strings.Contains(formattedDestination, "/var/run/secrets") {
		t.Fatal("secret value is exposed by formatting or structured logging")
	}
}

func TestConfigRejectsInvalidRoutingAndPolicy(t *testing.T) {
	t.Parallel()

	destination, err := NewSlackDestination(
		"/var/run/secrets/slipramp/operations",
		"https://hooks.slack.com/services/secret",
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{
			name: "relative public URL",
			configure: func(config *Config) {
				config.PublicURL = "/slipramp"
			},
			wantError: "absolute",
		},
		{
			name: "unknown route event",
			configure: func(config *Config) {
				config.Routes[0].Events = []EventType{"run.failed"}
			},
			wantError: "unknown event type",
		},
		{
			name: "unknown route destination",
			configure: func(config *Config) {
				config.Routes[0].Destinations = []string{"missing"}
			},
			wantError: "unknown destination",
		},
		{
			name: "empty route",
			configure: func(config *Config) {
				config.Routes[0].Events = nil
			},
			wantError: "at least one event",
		},
		{
			name: "unbounded attempts",
			configure: func(config *Config) {
				config.Retry.MaximumAttempts = 0
			},
			wantError: "maximum attempts",
		},
		{
			name: "invalid retention",
			configure: func(config *Config) {
				config.RetentionPeriod = 0
			},
			wantError: "retention period",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := DefaultConfig()
			config.PublicURL = "https://slipramp.example.com"
			config.Destinations = map[string]Destination{"operations": destination}
			config.Routes = []Route{{
				Events:       []EventType{EventTypeReviewRequested},
				Destinations: []string{"operations"},
			}}
			test.configure(&config)

			testcheck.ErrorContains(t, config.Validate(), test.wantError)
		})
	}
}

func TestReviewRequestedEventValidatesChannelNeutralPayload(t *testing.T) {
	t.Parallel()

	event := validReviewRequestedEvent(t)
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	var payload ReviewRequestedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Action.Path != "/promotions/00000000-0000-0000-0000-000000000002" ||
		payload.Subject.ID != event.SubjectID || payload.Type != EventTypeReviewRequested {
		t.Fatalf("payload = %#v", payload)
	}

	input := validReviewRequestedInput()
	input.Action.Path = "https://malicious.example.com/review"
	_, err := NewReviewRequestedEvent(input)
	testcheck.ErrorContains(t, err, "application-relative")

	event.Payload = append(event.Payload[:len(event.Payload)-1], []byte(`,"secret":"must-not-pass"}`)...)
	testcheck.ErrorContains(t, event.Validate(), "does not match")
}

func TestServiceSkipsUnroutedEventsAndPassesOnlySnapshots(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	service, err := NewService(DefaultConfig(), store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Enqueue(t.Context(), validReviewRequestedEvent(t))
	if err != nil || created || store.enqueueCalls != 0 {
		t.Fatalf("Enqueue() = %t, %v; calls = %d", created, err, store.enqueueCalls)
	}

	destination, err := NewSlackDestination(
		"/var/run/secrets/slipramp/operations",
		"https://hooks.slack.com/services/never-persist-this",
	)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.PublicURL = "https://slipramp.example.com"
	config.Destinations["operations"] = destination
	config.Routes = []Route{{
		Events:       []EventType{EventTypeReviewRequested},
		Destinations: []string{"operations"},
	}}
	service, err = NewService(config, store)
	if err != nil {
		t.Fatal(err)
	}
	created, err = service.Enqueue(t.Context(), validReviewRequestedEvent(t))
	if err != nil || !created || store.enqueueCalls != 1 {
		t.Fatalf("Enqueue() = %t, %v; calls = %d", created, err, store.enqueueCalls)
	}
	if len(store.destinations) != 1 || store.destinations[0] != (DestinationSnapshot{
		Name: "operations",
		Type: DestinationTypeSlack,
	}) {
		t.Fatalf("destination snapshots = %#v", store.destinations)
	}
}

func validReviewRequestedEvent(t *testing.T) Event {
	t.Helper()
	event, err := NewReviewRequestedEvent(validReviewRequestedInput())
	if err != nil {
		t.Fatalf("NewReviewRequestedEvent() error = %v", err)
	}

	return event
}

func validReviewRequestedInput() ReviewRequestedInput {
	at := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	return ReviewRequestedInput{
		ID:               uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		SubjectID:        uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		OccurredAt:       at,
		DeduplicationKey: "promotion:00000000-0000-0000-0000-000000000002:review-requested",
		Title:            "Promotion to production needs review",
		Summary:          "external-dns - candidate to production",
		Facts:            []Fact{{Label: "Repository", Value: "kcloud-ops"}},
		Action: Action{
			Label: "Review in Slipramp",
			Path:  "/promotions/00000000-0000-0000-0000-000000000002",
		},
		CreatedAt: at,
	}
}

type recordingStore struct {
	enqueueCalls int
	destinations []DestinationSnapshot
}

func (s *recordingStore) EnqueueNotification(
	_ context.Context,
	_ Event,
	destinations []DestinationSnapshot,
) (bool, error) {
	s.enqueueCalls++
	s.destinations = append([]DestinationSnapshot(nil), destinations...)

	return true, nil
}

func (*recordingStore) ListNotificationDeliveries(
	context.Context,
	DeliveryHistoryFilter,
) (DeliveryHistoryPage, error) {
	return DeliveryHistoryPage{}, nil
}
