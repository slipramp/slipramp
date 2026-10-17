package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNextControllerWorkRedeliversPreparingRun(t *testing.T) {
	t.Parallel()

	want := Run{
		ID:    uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		State: StatePreparing,
	}
	runs := &recordingRuns{preparing: want}
	service := &Service{runs: runs, now: time.Now}

	assigned, err := service.NextControllerWork(context.Background(), "colo")
	if err != nil {
		t.Fatalf("NextControllerWork() error = %v", err)
	}
	if assigned.ID != want.ID {
		t.Errorf("assigned Run ID = %s, want %s", assigned.ID, want.ID)
	}
	if runs.claimCalls != 0 {
		t.Errorf("claim calls = %d, want 0", runs.claimCalls)
	}
}

func TestNextControllerWorkClaimsQueuedRun(t *testing.T) {
	t.Parallel()

	want := Run{
		ID:    uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		State: StatePreparing,
	}
	runs := &recordingRuns{
		preparingError: ErrNoControllerWork,
		claimed:        want,
	}
	service := &Service{runs: runs}
	location := time.FixedZone("test", -6*60*60)
	claimedAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, location)
	service.now = func() time.Time { return claimedAt }

	assigned, err := service.NextControllerWork(context.Background(), "colo")
	if err != nil {
		t.Fatalf("NextControllerWork() error = %v", err)
	}
	if assigned.ID != want.ID {
		t.Errorf("assigned Run ID = %s, want %s", assigned.ID, want.ID)
	}
	if runs.claimClusterName != "colo" {
		t.Errorf("claim cluster = %q, want %q", runs.claimClusterName, "colo")
	}
	if !runs.claimedAt.Equal(claimedAt) || runs.claimedAt.Location() != time.UTC {
		t.Errorf("claimed at = %s, want %s in UTC", runs.claimedAt, claimedAt)
	}
}

func TestNextControllerWorkReturnsNoWork(t *testing.T) {
	t.Parallel()

	runs := &recordingRuns{
		preparingError: ErrNoControllerWork,
		claimError:     ErrNoControllerWork,
	}
	service := &Service{runs: runs, now: time.Now}

	_, err := service.NextControllerWork(context.Background(), "colo")
	if !errors.Is(err, ErrNoControllerWork) {
		t.Fatalf("NextControllerWork() error = %v, want %v", err, ErrNoControllerWork)
	}
}

func TestNextControllerWorkRejectsBlankClusterName(t *testing.T) {
	t.Parallel()

	runs := &recordingRuns{}
	service := &Service{runs: runs, now: time.Now}

	_, err := service.NextControllerWork(context.Background(), " ")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NextControllerWork() error = %v, want %v", err, ErrInvalidRequest)
	}
	if runs.preparingCalls != 0 || runs.claimCalls != 0 {
		t.Errorf(
			"store calls = (%d, %d), want (0, 0)",
			runs.preparingCalls,
			runs.claimCalls,
		)
	}
}
