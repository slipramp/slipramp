package notification

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDispatcherRecordsSuccessAndRetryableFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	destination, err := NewSlackDestination(
		"/var/run/secrets/slipramp/operations",
		"https://hooks.slack.com/services/secret-value",
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
	store := &dispatcherStore{attempt: deliveryAttempt(now)}
	sender := &recordingDeliverySender{outcome: deliveryOutcome{
		diagnostic: "Slack rate limited the delivery.",
		retryAfter: 45 * time.Second,
	}}
	dispatcher := NewDispatcher(config, store, time.Second, discardLogger())
	dispatcher.sender = sender
	dispatcher.now = func() time.Time { return now }
	dispatcher.jitter = func() float64 { return 1 }

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("ProcessNext() = %t, %v", processed, err)
	}
	if store.retryDiagnostic != sender.outcome.diagnostic ||
		!store.retryAt.Equal(now.Add(45*time.Second)) || store.failedDiagnostic != "" {
		t.Fatalf("retry result = at %s, diagnostic %q", store.retryAt, store.retryDiagnostic)
	}

	store = &dispatcherStore{attempt: deliveryAttempt(now)}
	sender.outcome = deliveryOutcome{}
	dispatcher.deliveries = store
	processed, err = dispatcher.ProcessNext(t.Context())
	if err != nil || !processed || store.deliveredAt.IsZero() {
		t.Fatalf("successful ProcessNext() = %t, %v; store = %#v", processed, err, store)
	}
}

func TestDispatcherRetriesMissingConfigurationAndFailsExhaustedDeliveries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	config := DefaultConfig()
	store := &dispatcherStore{attempt: deliveryAttempt(now)}
	dispatcher := NewDispatcher(config, store, time.Second, discardLogger())
	dispatcher.now = func() time.Time { return now }
	dispatcher.jitter = func() float64 { return 1 }

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed || store.retryDiagnostic != "Notification destination is no longer configured." {
		t.Fatalf("missing destination ProcessNext() = %t, %v; retry = %q", processed, err, store.retryDiagnostic)
	}

	destination := Destination{
		Type:       DestinationTypeSlack,
		webhookURL: "https://hooks.slack.com/services/secret-value",
	}
	config.Destinations["operations"] = destination
	config.Retry.MaximumAttempts = 2
	store = &dispatcherStore{attempt: deliveryAttempt(now)}
	store.attempt.AttemptCount = 2
	sender := &recordingDeliverySender{outcome: deliveryOutcome{
		diagnostic: "retry failed for https://hooks.slack.com/services/secret-value",
	}}
	dispatcher = NewDispatcher(config, store, time.Second, discardLogger())
	dispatcher.sender = sender
	dispatcher.now = func() time.Time { return now }
	dispatcher.jitter = func() float64 { return 1 }

	processed, err = dispatcher.ProcessNext(t.Context())
	if err != nil || !processed || !strings.Contains(store.failedDiagnostic, "[REDACTED]") ||
		strings.Contains(store.failedDiagnostic, "secret-value") {
		t.Fatalf("exhausted ProcessNext() = %t, %v; failure = %q", processed, err, store.failedDiagnostic)
	}
}

func TestRetryBackoffIsExponentialJitteredAndBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempt int
		jitter  float64
		want    time.Duration
	}{
		{attempt: 1, jitter: 1, want: 5 * time.Second},
		{attempt: 2, jitter: 1, want: 10 * time.Second},
		{attempt: 3, jitter: 0.8, want: 16 * time.Second},
		{attempt: 8, jitter: 1.2, want: time.Minute},
	}
	for _, test := range tests {
		if got := RetryBackoff(test.attempt, time.Minute, test.jitter); got != test.want {
			t.Errorf("RetryBackoff(%d, %s, %v) = %s, want %s", test.attempt, time.Minute, test.jitter, got, test.want)
		}
	}
}

func deliveryAttempt(now time.Time) DeliveryAttempt {
	return DeliveryAttempt{
		ID:    uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		Event: validReviewRequestedEventWithoutTest(),
		Destination: DestinationSnapshot{
			Name: "operations",
			Type: DestinationTypeSlack,
		},
		AttemptCount:  1,
		NextAttemptAt: now.Add(5 * time.Second),
	}
}

func validReviewRequestedEventWithoutTest() Event {
	event, err := NewReviewRequestedEvent(validReviewRequestedInput())
	if err != nil {
		panic(err)
	}

	return event
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type recordingDeliverySender struct {
	outcome deliveryOutcome
	calls   int
}

func (s *recordingDeliverySender) Send(context.Context, Destination, Event) deliveryOutcome {
	s.calls++

	return s.outcome
}

type dispatcherStore struct {
	attempt          DeliveryAttempt
	claimError       error
	deliveredAt      time.Time
	retryAt          time.Time
	retryDiagnostic  string
	failedAt         time.Time
	failedDiagnostic string
}

func (s *dispatcherStore) ClaimNextNotificationDelivery(
	context.Context,
	DeliveryClaim,
) (DeliveryAttempt, error) {
	return s.attempt, s.claimError
}

func (s *dispatcherStore) MarkNotificationDeliveryDelivered(
	_ context.Context,
	_ uuid.UUID,
	_ int,
	at time.Time,
) (bool, error) {
	s.deliveredAt = at

	return true, nil
}

func (s *dispatcherStore) RetryNotificationDelivery(
	_ context.Context,
	_ uuid.UUID,
	_ int,
	at time.Time,
	diagnostic string,
) (bool, error) {
	s.retryAt = at
	s.retryDiagnostic = diagnostic

	return true, nil
}

func (s *dispatcherStore) FailNotificationDelivery(
	_ context.Context,
	_ uuid.UUID,
	_ int,
	at time.Time,
	diagnostic string,
) (bool, error) {
	s.failedAt = at
	s.failedDiagnostic = diagnostic

	return true, nil
}
