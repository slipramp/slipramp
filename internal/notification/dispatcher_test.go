package notification

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
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
	observer := &recordingNotificationDeliveryObserver{}
	dispatcher.observer = observer
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
	wantResults := []DeliveryAttemptResult{DeliveryAttemptRetryScheduled, DeliveryAttemptDelivered}
	if !slices.Equal(observer.results, wantResults) {
		t.Errorf("delivery metric results = %v, want %v", observer.results, wantResults)
	}
}

func TestDispatcherRetriesMissingConfigurationAndFailsExhaustedDeliveries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	config := DefaultConfig()
	store := &dispatcherStore{attempt: deliveryAttempt(now)}
	dispatcher := NewDispatcher(config, store, time.Second, discardLogger())
	configurationObserver := &recordingNotificationDeliveryObserver{}
	dispatcher.observer = configurationObserver
	dispatcher.now = func() time.Time { return now }
	dispatcher.jitter = func() float64 { return 1 }

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed || store.retryDiagnostic != "Notification destination is no longer configured." {
		t.Fatalf("missing destination ProcessNext() = %t, %v; retry = %q", processed, err, store.retryDiagnostic)
	}
	if !slices.Equal(configurationObserver.results, []DeliveryAttemptResult{DeliveryAttemptConfigurationFailed}) {
		t.Errorf("configuration metric results = %v", configurationObserver.results)
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
	terminalObserver := &recordingNotificationDeliveryObserver{}
	dispatcher.observer = terminalObserver
	dispatcher.sender = sender
	dispatcher.now = func() time.Time { return now }
	dispatcher.jitter = func() float64 { return 1 }

	processed, err = dispatcher.ProcessNext(t.Context())
	if err != nil || !processed || !strings.Contains(store.failedDiagnostic, "[REDACTED]") ||
		strings.Contains(store.failedDiagnostic, "secret-value") {
		t.Fatalf("exhausted ProcessNext() = %t, %v; failure = %q", processed, err, store.failedDiagnostic)
	}
	if !slices.Equal(terminalObserver.results, []DeliveryAttemptResult{DeliveryAttemptTerminalFailed}) {
		t.Errorf("terminal metric results = %v", terminalObserver.results)
	}
}

func TestDispatcherRecordsPersistenceFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	destination := Destination{Type: DestinationTypeSlack, webhookURL: "https://example.com/hook"}
	config := DefaultConfig()
	config.PublicURL = "https://slipramp.example.com"
	config.Destinations["operations"] = destination
	store := &dispatcherStore{
		attempt:   deliveryAttempt(now),
		markError: errors.New("database unavailable"),
	}
	observer := &recordingNotificationDeliveryObserver{}
	dispatcher := NewDispatcher(
		config,
		store,
		time.Second,
		discardLogger(),
		WithDeliveryObserver(observer),
	)
	dispatcher.sender = &recordingDeliverySender{}
	dispatcher.now = func() time.Time { return now }

	processed, err := dispatcher.ProcessNext(t.Context())
	if !processed || err == nil {
		t.Fatalf("ProcessNext() = (%t, %v), want persistence error", processed, err)
	}
	if !slices.Equal(observer.results, []DeliveryAttemptResult{DeliveryAttemptRecordFailed}) {
		t.Errorf("delivery metric results = %v, want record failure", observer.results)
	}
}

func TestDispatcherDoesNotRecordIdleOrShutdownCancellation(t *testing.T) {
	t.Parallel()

	observer := &recordingNotificationDeliveryObserver{}
	idle := NewDispatcher(
		DefaultConfig(),
		&dispatcherStore{claimError: ErrNoEligibleDelivery},
		time.Second,
		discardLogger(),
		WithDeliveryObserver(observer),
	)
	processed, err := idle.ProcessNext(t.Context())
	if err != nil || processed {
		t.Fatalf("idle ProcessNext() = (%t, %v), want no work", processed, err)
	}

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	canceled := NewDispatcher(
		DefaultConfig(),
		&dispatcherStore{attempt: deliveryAttempt(now)},
		time.Second,
		discardLogger(),
		WithDeliveryObserver(observer),
	)
	processed, err = canceled.ProcessNext(ctx)
	if err != nil || !processed {
		t.Fatalf("canceled ProcessNext() = (%t, %v)", processed, err)
	}
	if len(observer.results) != 0 {
		t.Errorf("idle or canceled delivery recorded metrics: %v", observer.results)
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

type recordingNotificationDeliveryObserver struct {
	results []DeliveryAttemptResult
}

func (o *recordingNotificationDeliveryObserver) ObserveNotificationDelivery(
	_ context.Context,
	result DeliveryAttemptResult,
	_ time.Duration,
) {
	o.results = append(o.results, result)
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
	markError        error
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
	if s.markError != nil {
		return false, s.markError
	}
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
