package gittrigger

import (
	"context"
	"errors"
	"fmt"
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

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("ProcessNext() = (%t, %v), want success", processed, err)
	}
	if store.failureCalls != 0 {
		t.Fatal("successful creation recorded a failure")
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

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("ProcessNext() = (%t, %v), want consumed without error", processed, err)
	}
	if store.failureCalls != 0 {
		t.Fatal("supersession recorded a failure")
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

	testDispatcher(store, creator).drain(t.Context())

	if createCalls != 2 || store.failureCalls != 1 || len(store.queue) != 0 {
		t.Fatalf("create calls=%d failure calls=%d remaining=%d", createCalls, store.failureCalls, len(store.queue))
	}
	if store.diagnostic != FailureDiagnostic(errors.New("configuration unavailable")) {
		t.Errorf("diagnostic = %q", store.diagnostic)
	}
}

func TestFailureDiagnostic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "known error",
			err:  fmt.Errorf("%w: parameter password=secret", run.ErrInvalidParameters),
			want: run.ErrInvalidParameters.Error(),
		},
		{
			name: "unknown error",
			err:  errors.New("postgres://admin:secret@db private-token"),
			want: "Run creation failed; check Server configuration and dependency health.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := FailureDiagnostic(test.err); got != test.want {
				t.Errorf("FailureDiagnostic() = %q, want %q", got, test.want)
			}
		})
	}
}

func testDelivery() Delivery {
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
	queue        []Delivery
	failureCalls int
	diagnostic   string
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
	_ time.Time,
	diagnostic string,
) (FailureState, bool, error) {
	s.failureCalls++
	s.diagnostic = diagnostic
	s.queue = s.queue[1:]
	return FailureState{Count: s.failureCalls}, true, nil
}
