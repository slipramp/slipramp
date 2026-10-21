package gittrigger

import (
	"context"
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

	delivery := Delivery{
		ID:                uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200"),
		RepositoryID:      uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201"),
		CatalogRevisionID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		TargetName:        "network",
		ActionName:        "sync",
		TriggerName:       "reconcile-on-change",
		Parameters: map[string]any{
			"environment":  "production",
			"auto-approve": true,
			"retries":      float64(3),
		},
	}
	store := &recordingDeliveryStore{delivery: delivery}
	runs := &recordingRunCreator{
		runID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203"),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := NewDispatcher(store, runs, time.Second, logger)

	processed, err := dispatcher.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessNext() = (%t, %v), want success", processed, err)
	}
	if store.completedRunID != runs.runID {
		t.Errorf("completed Run ID = %s, want %s", store.completedRunID, runs.runID)
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
	if request.Source.Git == nil ||
		request.Source.Git.TriggerName != delivery.TriggerName ||
		request.Source.Git.DeliveryID != delivery.ID {
		t.Errorf("Git source = %#v", request.Source)
	}
}

func TestDispatcherContinuesAfterDeliveryIsSupersededAtRunCreation(t *testing.T) {
	t.Parallel()

	delivery := Delivery{
		ID:                uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1210"),
		RepositoryID:      uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1211"),
		CatalogRevisionID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1212"),
		TargetName:        "network",
		ActionName:        "sync",
		TriggerName:       "reconcile-on-change",
		Parameters:        map[string]any{},
	}
	store := &recordingDeliveryStore{delivery: delivery}
	runs := supersededRunCreator{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := NewDispatcher(store, runs, time.Second, logger)

	processed, err := dispatcher.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("superseded dispatch = (%t, %v), want consumed without error", processed, err)
	}
	if store.completedRunID != uuid.Nil {
		t.Errorf("completed Run ID = %s, want none", store.completedRunID)
	}
}

type recordingDeliveryStore struct {
	delivery       Delivery
	completedRunID uuid.UUID
}

func (s *recordingDeliveryStore) GetPendingGitTriggerDelivery(context.Context) (Delivery, error) {
	if s.completedRunID != uuid.Nil {
		return Delivery{}, ErrNoPendingDelivery
	}

	return s.delivery, nil
}

func (s *recordingDeliveryStore) CompleteGitTriggerDelivery(_ context.Context, _ uuid.UUID, runID uuid.UUID) error {
	s.completedRunID = runID

	return nil
}

type recordingRunCreator struct {
	runID   uuid.UUID
	request run.CreateRequest
}

type supersededRunCreator struct{}

func (supersededRunCreator) CreateRun(context.Context, run.CreateRequest) (run.Run, error) {
	return run.Run{}, run.ErrGitRunSuperseded
}

func (c *recordingRunCreator) CreateRun(_ context.Context, request run.CreateRequest) (run.Run, error) {
	c.request = request

	return run.Run{ID: c.runID}, nil
}
