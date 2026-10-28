package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNextRunAssignmentRedeliversAssignedRun(t *testing.T) {
	t.Parallel()

	want := Run{
		ID:    uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		Phase: PhaseAssigned,
	}
	runs := &recordingAssignments{assigned: want}
	service := &Service{assignments: runs, now: time.Now}

	assigned, err := service.NextRunAssignment(context.Background(), "colo")
	if err != nil {
		t.Fatalf("NextRunAssignment() error = %v", err)
	}
	if assigned.ID != want.ID {
		t.Errorf("assigned Run ID = %s, want %s", assigned.ID, want.ID)
	}
	if runs.claimCalls != 0 {
		t.Errorf("claim calls = %d, want 0", runs.claimCalls)
	}
}

func TestNextRunAssignmentClaimsQueuedRun(t *testing.T) {
	t.Parallel()

	want := Run{
		ID:    uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		Phase: PhaseAssigned,
	}
	runs := &recordingAssignments{
		assignedError: ErrNoRunAssignment,
		claimed:       want,
	}
	service := &Service{assignments: runs}
	location := time.FixedZone("test", -6*60*60)
	claimedAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, location)
	service.now = func() time.Time { return claimedAt }

	assigned, err := service.NextRunAssignment(context.Background(), "colo")
	if err != nil {
		t.Fatalf("NextRunAssignment() error = %v", err)
	}
	if assigned.ID != want.ID {
		t.Errorf("assigned Run ID = %s, want %s", assigned.ID, want.ID)
	}
	if runs.claimClusterName != "colo" {
		t.Errorf("claim cluster = %q, want %q", runs.claimClusterName, "colo")
	}
	if runs.claimCapacity.ClusterMaximumActiveRuns != DefaultMaximumActiveRuns {
		t.Errorf(
			"claim cluster maximum = %d, want %d",
			runs.claimCapacity.ClusterMaximumActiveRuns,
			DefaultMaximumActiveRuns,
		)
	}
	if !runs.claimedAt.Equal(claimedAt) || runs.claimedAt.Location() != time.UTC {
		t.Errorf("claimed at = %s, want %s in UTC", runs.claimedAt, claimedAt)
	}
}

func TestNextRunAssignmentReturnsNoAssignment(t *testing.T) {
	t.Parallel()

	runs := &recordingAssignments{
		assignedError: ErrNoRunAssignment,
		claimError:    ErrNoRunAssignment,
	}
	service := &Service{assignments: runs, now: time.Now}

	_, err := service.NextRunAssignment(context.Background(), "colo")
	if !errors.Is(err, ErrNoRunAssignment) {
		t.Fatalf("NextRunAssignment() error = %v, want %v", err, ErrNoRunAssignment)
	}
}

func TestNextRunAssignmentRejectsBlankClusterName(t *testing.T) {
	t.Parallel()

	runs := &recordingAssignments{}
	service := &Service{assignments: runs, now: time.Now}

	_, err := service.NextRunAssignment(context.Background(), " ")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NextRunAssignment() error = %v, want %v", err, ErrInvalidRequest)
	}
	if runs.assignedCalls != 0 || runs.claimCalls != 0 {
		t.Errorf("store calls = (%d, %d), want (0, 0)", runs.assignedCalls, runs.claimCalls)
	}
}

type recordingAssignments struct {
	assignedCalls    int
	assigned         Run
	assignedError    error
	claimCalls       int
	claimClusterName string
	claimCapacity    SchedulingCapacity
	claimedAt        time.Time
	claimed          Run
	claimError       error
}

func (s *recordingAssignments) GetAssignedRunForCluster(context.Context, string) (Run, error) {
	s.assignedCalls++

	return s.assigned, s.assignedError
}

func (s *recordingAssignments) ClaimQueuedRun(
	_ context.Context,
	clusterName string,
	capacity SchedulingCapacity,
	claimedAt time.Time,
) (Run, error) {
	s.claimCalls++
	s.claimClusterName = clusterName
	s.claimCapacity = capacity
	s.claimedAt = claimedAt

	return s.claimed, s.claimError
}
