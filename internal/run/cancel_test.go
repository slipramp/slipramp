package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestCancelRunRecordsIntentInUTC(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	want := Run{ID: runID, Phase: PhaseAssigned}
	runs := &recordingRuns{canceled: want}
	service := &Service{runs: runs}
	location := time.FixedZone("test", -6*60*60)
	requestedAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, location)
	service.now = func() time.Time { return requestedAt }
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
	}

	record, err := service.CancelRun(context.Background(), CancelRequest{RunID: runID, Actor: &actor})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if record.ID != want.ID || runs.cancelID != runID {
		t.Errorf("canceled Run = %#v, store ID = %s", record, runs.cancelID)
	}
	if !runs.cancelAt.Equal(requestedAt) || runs.cancelAt.Location() != time.UTC {
		t.Errorf("requested at = %s, want %s in UTC", runs.cancelAt, requestedAt)
	}
	if runs.canceledBy == nil || runs.canceledBy.PrincipalID != actor.ID ||
		runs.canceledBy.DisplayName != actor.DisplayName {
		t.Errorf("cancellation actor = %#v", runs.canceledBy)
	}
}

func TestCancelRunMapsExpectedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		runID uuid.UUID
		err   error
		want  error
		calls int
	}{
		{name: "missing ID", want: ErrInvalidRequest},
		{name: "missing Run", runID: uuid.New(), err: ErrRunNotFound, want: ErrRunNotFound, calls: 1},
		{name: "finished Run", runID: uuid.New(), err: ErrRunFinished, want: ErrRunFinished, calls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &recordingRuns{cancelError: test.err}
			service := &Service{runs: runs, now: time.Now}
			_, err := service.CancelRun(context.Background(), CancelRequest{RunID: test.runID})
			if !errors.Is(err, test.want) {
				t.Fatalf("CancelRun() error = %v, want %v", err, test.want)
			}
			if runs.cancelCalls != test.calls {
				t.Errorf("store calls = %d, want %d", runs.cancelCalls, test.calls)
			}
		})
	}
}

func TestNextRunToCancelReturnsPendingRun(t *testing.T) {
	t.Parallel()

	want := Run{ID: uuid.New(), Phase: PhaseRunning}
	runs := &recordingRuns{nextCancel: want}
	service := &Service{runs: runs}

	record, err := service.NextRunToCancel(context.Background(), "colo")
	if err != nil {
		t.Fatalf("NextRunToCancel() error = %v", err)
	}
	if record.ID != want.ID || runs.nextCancelCluster != "colo" {
		t.Errorf("Run to cancel = %#v, cluster = %q", record, runs.nextCancelCluster)
	}
}

func TestNextRunToCancelRejectsBlankClusterName(t *testing.T) {
	t.Parallel()

	runs := &recordingRuns{}
	service := &Service{runs: runs}
	_, err := service.NextRunToCancel(context.Background(), " ")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NextRunToCancel() error = %v, want %v", err, ErrInvalidRequest)
	}
	if runs.nextCancelCalls != 0 {
		t.Errorf("store calls = %d, want 0", runs.nextCancelCalls)
	}
}
