package gittrigger

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/run"
)

func TestProcessNextCreatesPinnedGitRun(t *testing.T) {
	t.Parallel()

	delivery := testDelivery()
	store := &deliveryStore{queue: []Delivery{delivery}}
	runs := &recordingRunCreator{
		runID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203"),
	}
	dispatcher := testDispatcher(store, runs)
	observer := &recordingDeliveryObserver{}
	dispatcher.observer = observer

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("ProcessNext() = (%t, %v), want success", processed, err)
	}
	if store.failureCalls != 0 {
		t.Fatal("successful creation recorded a failure")
	}
	if !reflect.DeepEqual(observer.results, []DeliveryAttemptResult{DeliveryAttemptRunLinked}) {
		t.Errorf("delivery metric results = %v, want run-linked", observer.results)
	}

	request := runs.request
	if request.CatalogRevisionID == nil || *request.CatalogRevisionID != delivery.CatalogRevisionID {
		t.Errorf("catalog revision pin = %v, want %s", request.CatalogRevisionID, delivery.CatalogRevisionID)
	}
	if request.Actor != nil {
		t.Errorf("Git-trigger actor = %#v, want nil", request.Actor)
	}
	if request.IdempotencyKey == nil || *request.IdempotencyKey != delivery.ID.String() {
		t.Errorf("idempotency key = %v, want %s", request.IdempotencyKey, delivery.ID)
	}
	if !reflect.DeepEqual(request.Parameters, delivery.Parameters) {
		t.Errorf("parameters = %#v, want %#v", request.Parameters, delivery.Parameters)
	}
	if request.ReviewRequired != delivery.ReviewRequired {
		t.Errorf("review requirement = %t, want %t", request.ReviewRequired, delivery.ReviewRequired)
	}
	if request.Source.Git == nil ||
		request.Source.Git.TriggerName != delivery.TriggerName ||
		request.Source.Git.DeliveryID != delivery.ID {
		t.Errorf("Git source = %#v", request.Source)
	}
}

func TestProcessNextConsumesSupersededDeliveryWithoutRecordingFailure(t *testing.T) {
	t.Parallel()

	store := &deliveryStore{queue: []Delivery{testDelivery()}}
	dispatcher := testDispatcher(store, runCreatorFunc(func(context.Context, run.CreateRequest) (run.Run, error) {
		return run.Run{}, run.ErrGitRunSuperseded
	}))
	observer := &recordingDeliveryObserver{}
	dispatcher.observer = observer

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("ProcessNext() = (%t, %v), want consumed without error", processed, err)
	}
	if store.failureCalls != 0 {
		t.Fatal("supersession recorded a failure")
	}
	if !reflect.DeepEqual(observer.results, []DeliveryAttemptResult{DeliveryAttemptSuperseded}) {
		t.Errorf("delivery metric results = %v, want superseded", observer.results)
	}
}

func TestDispatcherContinuesAfterRunCreationFailure(t *testing.T) {
	t.Parallel()

	store := &deliveryStore{queue: []Delivery{{ID: uuid.New()}, {ID: uuid.New()}}}
	createCalls := 0
	creator := runCreatorFunc(func(context.Context, run.CreateRequest) (run.Run, error) {
		createCalls++
		if createCalls == 1 {
			return run.Run{}, errors.New("configuration unavailable")
		}
		store.queue = store.queue[1:]
		return run.Run{ID: uuid.New()}, nil
	})

	dispatcher := testDispatcher(store, creator)
	observer := &recordingDeliveryObserver{}
	dispatcher.observer = observer
	dispatcher.drain(t.Context())

	if createCalls != 2 || store.failureCalls != 1 || len(store.queue) != 0 {
		t.Fatalf("create calls=%d failure calls=%d remaining=%d", createCalls, store.failureCalls, len(store.queue))
	}
	if store.diagnostic != FailureDiagnostic(errors.New("configuration unavailable")) {
		t.Errorf("diagnostic = %q", store.diagnostic)
	}
	wantResults := []DeliveryAttemptResult{DeliveryAttemptPendingRetry, DeliveryAttemptRunLinked}
	if !reflect.DeepEqual(observer.results, wantResults) {
		t.Errorf("delivery metric results = %v, want %v", observer.results, wantResults)
	}
}

func TestProcessNextRecordsTerminalAndPersistenceFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		store      *deliveryStore
		wantResult DeliveryAttemptResult
		wantError  bool
	}{
		{
			name: "terminal failure",
			store: &deliveryStore{
				queue:           []Delivery{testDelivery()},
				failureTerminal: true,
			},
			wantResult: DeliveryAttemptFailed,
		},
		{
			name: "record failure",
			store: &deliveryStore{
				queue:       []Delivery{testDelivery()},
				recordError: errors.New("database unavailable"),
			},
			wantResult: DeliveryAttemptRecordFailed,
			wantError:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dispatcher := testDispatcher(test.store, runCreatorFunc(
				func(context.Context, run.CreateRequest) (run.Run, error) {
					return run.Run{}, errors.New("creation failed")
				},
			))
			observer := &recordingDeliveryObserver{}
			dispatcher.observer = observer

			processed, err := dispatcher.ProcessNext(t.Context())
			if processed != !test.wantError || (err != nil) != test.wantError {
				t.Fatalf("ProcessNext() = (%t, %v), want error %t", processed, err, test.wantError)
			}
			if !reflect.DeepEqual(observer.results, []DeliveryAttemptResult{test.wantResult}) {
				t.Errorf("delivery metric results = %v, want %s", observer.results, test.wantResult)
			}
		})
	}
}

func TestProcessNextDoesNotRecordIdleOrShutdownCancellation(t *testing.T) {
	t.Parallel()

	observer := &recordingDeliveryObserver{}
	idle := testDispatcher(&deliveryStore{}, &recordingRunCreator{})
	idle.observer = observer
	processed, err := idle.ProcessNext(t.Context())
	if err != nil || processed {
		t.Fatalf("idle ProcessNext() = (%t, %v), want no work", processed, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	store := &deliveryStore{queue: []Delivery{testDelivery()}}
	canceled := testDispatcher(store, runCreatorFunc(func(context.Context, run.CreateRequest) (run.Run, error) {
		cancel()

		return run.Run{}, context.Canceled
	}))
	canceled.observer = observer
	processed, err = canceled.ProcessNext(ctx)
	if err != nil || !processed {
		t.Fatalf("canceled ProcessNext() = (%t, %v)", processed, err)
	}
	if len(observer.results) != 0 || store.failureCalls != 0 {
		t.Errorf("idle or canceled delivery recorded metrics %v or failures %d", observer.results, store.failureCalls)
	}
}

func TestClassifyCreationFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		wantPermanent  bool
		wantDiagnostic string
	}{
		{
			name:          "invalid frozen parameters",
			err:           errors.Join(run.ErrInvalidParameters, errors.New("password=secret")),
			wantPermanent: true, wantDiagnostic: run.ErrInvalidParameters.Error(),
		},
		{
			name: "missing frozen action", err: run.ErrActionNotFound,
			wantPermanent: true, wantDiagnostic: run.ErrActionNotFound.Error(),
		},
		{
			name: "placement can be configured", err: run.ErrPlacementNotFound,
			wantPermanent: false, wantDiagnostic: run.ErrPlacementNotFound.Error(),
		},
		{
			name: "dependency outage", err: errors.New("postgres://admin:secret@db private-token"),
			wantPermanent:  false,
			wantDiagnostic: "Run creation failed; check Server configuration and dependency health.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			failure := ClassifyCreationFailure(test.err)
			if failure.Permanent != test.wantPermanent || failure.Diagnostic != test.wantDiagnostic {
				t.Errorf(
					"ClassifyCreationFailure() = %#v, want permanent %t diagnostic %q",
					failure,
					test.wantPermanent,
					test.wantDiagnostic,
				)
			}
		})
	}
}

func testDelivery() Delivery {
	createdAt := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	return Delivery{
		ID:                uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200"),
		RepositoryID:      uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201"),
		CatalogRevisionID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		TargetName:        "network",
		ActionName:        "sync",
		TriggerName:       "reconcile-on-change",
		ReviewRequired:    true,
		Parameters: map[string]any{
			"environment":  "production",
			"auto-approve": true,
			"retries":      float64(3),
		},
		Lifecycle: Lifecycle{CreatedAt: createdAt, NextAttemptAt: createdAt},
	}
}

func testDispatcher(store Store, creator RunCreator) *Dispatcher {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewDispatcher(store, creator, time.Second, logger)
}

type runCreatorFunc func(context.Context, run.CreateRequest) (run.Run, error)

func (f runCreatorFunc) CreateRun(ctx context.Context, request run.CreateRequest) (run.Run, error) {
	return f(ctx, request)
}

type recordingRunCreator struct {
	runID   uuid.UUID
	request run.CreateRequest
}

func (c *recordingRunCreator) CreateRun(_ context.Context, request run.CreateRequest) (run.Run, error) {
	c.request = request
	return run.Run{ID: c.runID}, nil
}

type deliveryStore struct {
	queue           []Delivery
	failureCalls    int
	diagnostic      string
	failureTerminal bool
	recordError     error
}

type recordingDeliveryObserver struct {
	results []DeliveryAttemptResult
}

func (o *recordingDeliveryObserver) ObserveGitTriggerDelivery(
	_ context.Context,
	result DeliveryAttemptResult,
	_ time.Duration,
) {
	o.results = append(o.results, result)
}

func (s *deliveryStore) GetEligibleGitTriggerDelivery(context.Context, time.Time) (Delivery, error) {
	if len(s.queue) == 0 {
		return Delivery{}, ErrNoEligibleDelivery
	}
	return s.queue[0], nil
}

func (s *deliveryStore) RecordGitTriggerDeliveryFailure(
	_ context.Context,
	_ uuid.UUID,
	expectedFailureCount int,
	attemptedAt time.Time,
	failure CreationFailure,
) (Delivery, bool, error) {
	s.failureCalls++
	s.diagnostic = failure.Diagnostic
	if s.recordError != nil {
		return Delivery{}, false, s.recordError
	}
	s.queue = s.queue[1:]
	failedAt := (*time.Time)(nil)
	if s.failureTerminal || failure.Permanent {
		failedAt = &attemptedAt
	}
	return Delivery{Lifecycle: Lifecycle{
		CreatedAt:     attemptedAt.Add(-time.Minute),
		FailureCount:  expectedFailureCount + 1,
		NextAttemptAt: attemptedAt.Add(InitialRetryDelay),
		Diagnostic:    failure.Diagnostic,
		FailedAt:      failedAt,
	}}, true, nil
}
