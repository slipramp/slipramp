package run

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReportObservationRecordsObservation(t *testing.T) {
	t.Parallel()

	runs := &recordingObservations{}
	service := &Service{observations: runs}
	location := time.FixedZone("test", -6*60*60)
	reportedAt := time.Date(2026, time.October, 17, 10, 0, 0, 0, location)
	service.now = func() time.Time { return reportedAt }
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	jobUID := "job-uid"
	observation := Observation{ID: runID, ClusterName: "colo", Phase: PhaseRunning, JobUID: &jobUID}

	if err := service.ReportObservation(context.Background(), observation); err != nil {
		t.Fatalf("ReportObservation() error = %v", err)
	}
	if runs.calls != 1 || runs.observation.ID != runID ||
		runs.observation.ClusterName != "colo" || runs.observation.Phase != PhaseRunning {
		t.Errorf("observation = %#v", runs)
	}
	if !runs.observation.ObservedAt.Equal(reportedAt) || runs.observation.ObservedAt.Location() != time.UTC {
		t.Errorf("observed at = %s, want %s in UTC", runs.observation.ObservedAt, reportedAt)
	}
}

func TestValidateObservationTransitionAllowsPhaseSkipsAndRepeatedReports(t *testing.T) {
	t.Parallel()

	succeeded := OutcomeSucceeded
	jobUID := "job-uid"
	tests := []struct {
		name        string
		observation Observation
		from        []Phase
	}{
		{
			name:        "preparing",
			observation: Observation{Phase: PhasePreparing, JobUID: &jobUID},
			from:        []Phase{PhaseAssigned, PhasePreparing},
		},
		{
			name:        "running",
			observation: Observation{Phase: PhaseRunning, JobUID: &jobUID},
			from:        []Phase{PhaseAssigned, PhasePreparing, PhaseRunning},
		},
		{
			name: "finished",
			observation: Observation{
				Phase:   PhaseFinished,
				Outcome: &succeeded,
			},
			from: []Phase{
				PhaseAssigned,
				PhasePreparing,
				PhaseRunning,
				PhaseFinished,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, phase := range []Phase{PhaseQueued, PhaseAssigned, PhasePreparing, PhaseRunning, PhaseFinished, "unknown"} {
				current := ObservationTransitionState{Phase: phase, Outcome: &succeeded}
				err := ValidateObservationTransition(current, test.observation)
				if slices.Contains(test.from, phase) {
					if err != nil {
						t.Errorf("transition from %s: %v", phase, err)
					}
				} else if !errors.Is(err, ErrObservationConflict) {
					t.Errorf("transition from %s error = %v, want observation conflict", phase, err)
				}
			}
		})
	}
}

func TestReportObservationRejectsInvalidPhaseOutcomeCombinations(t *testing.T) {
	t.Parallel()

	succeeded := OutcomeSucceeded
	invalidOutcome := Outcome("unknown")
	blank := " "
	negativeExitCode := int32(-1)
	tests := []struct {
		name        string
		observation Observation
	}{
		{name: "queued", observation: Observation{Phase: PhaseQueued}},
		{name: "assigned", observation: Observation{Phase: PhaseAssigned}},
		{name: "preparing without Job UID", observation: Observation{Phase: PhasePreparing}},
		{name: "running without Job UID", observation: Observation{Phase: PhaseRunning}},
		{name: "blank Job UID", observation: Observation{Phase: PhaseRunning, JobUID: &blank}},
		{name: "unfinished outcome", observation: Observation{
			Phase: PhaseRunning, Outcome: &succeeded,
		}},
		{name: "finished without outcome", observation: Observation{Phase: PhaseFinished}},
		{name: "invalid outcome", observation: Observation{Phase: PhaseFinished, Outcome: &invalidOutcome}},
		{name: "blank reason", observation: Observation{Phase: PhaseFinished, Outcome: &succeeded, Reason: &blank}},
		{name: "blank message", observation: Observation{Phase: PhaseFinished, Outcome: &succeeded, Message: &blank}},
		{name: "blank image ID", observation: Observation{
			Phase: PhaseRunning, JobUID: new("job-uid"), Images: ExecutionImages{Action: &blank},
		}},
		{name: "negative exit code", observation: Observation{
			Phase: PhaseFinished, Outcome: &succeeded, ExitCode: &negativeExitCode,
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &recordingObservations{}
			service := &Service{observations: runs, now: time.Now}
			test.observation.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
			test.observation.ClusterName = "colo"
			err := service.ReportObservation(context.Background(), test.observation)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("ReportObservation() error = %v, want %v", err, ErrInvalidRequest)
			}
			if runs.calls != 0 {
				t.Errorf("observation calls = %d, want 0", runs.calls)
			}
			current := ObservationTransitionState{Phase: PhaseAssigned}
			if err := ValidateObservationTransition(current, test.observation); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("ValidateObservationTransition() error = %v, want invalid request", err)
			}
		})
	}
}

func TestValidateObservationTransitionChecksPinnedJobAndTerminalOutcome(t *testing.T) {
	t.Parallel()

	jobUID := "job-uid"
	otherJobUID := "other-job-uid"
	actionImageID := "registry.example.com/tools@sha256:first"
	otherActionImageID := "registry.example.com/tools@sha256:second"
	succeeded := OutcomeSucceeded
	failed := OutcomeFailed
	canceled := OutcomeCanceled
	tests := []struct {
		name        string
		current     ObservationTransitionState
		observation Observation
		wantError   bool
	}{
		{
			name:        "reject replacement Job UID",
			current:     ObservationTransitionState{Phase: PhasePreparing, JobUID: &jobUID},
			observation: Observation{Phase: PhaseRunning, JobUID: &otherJobUID},
			wantError:   true,
		},
		{
			name:        "reject omitted pinned Job UID",
			current:     ObservationTransitionState{Phase: PhaseRunning, JobUID: &jobUID},
			observation: Observation{Phase: PhaseFinished, Outcome: &succeeded},
			wantError:   true,
		},
		{
			name: "reject changed image ID",
			current: ObservationTransitionState{
				Phase: PhaseRunning, JobUID: &jobUID,
				Images: ExecutionImages{Action: &actionImageID},
			},
			observation: Observation{
				Phase: PhaseRunning, JobUID: &jobUID,
				Images: ExecutionImages{Action: &otherActionImageID},
			},
			wantError: true,
		},
		{
			name: "retain image ID when later observation omits it",
			current: ObservationTransitionState{
				Phase: PhaseRunning, JobUID: &jobUID,
				Images: ExecutionImages{Action: &actionImageID},
			},
			observation: Observation{Phase: PhaseRunning, JobUID: &jobUID},
		},
		{
			name:        "failure before Job exists",
			current:     ObservationTransitionState{Phase: PhaseAssigned},
			observation: Observation{Phase: PhaseFinished, Outcome: &failed},
		},
		{
			name:        "reject changed finished outcome",
			current:     ObservationTransitionState{Phase: PhaseFinished, Outcome: &succeeded},
			observation: Observation{Phase: PhaseFinished, Outcome: &failed},
			wantError:   true,
		},
		{
			name:        "reject unrequested cancellation",
			current:     ObservationTransitionState{Phase: PhaseAssigned},
			observation: Observation{Phase: PhaseFinished, Outcome: &canceled},
			wantError:   true,
		},
		{
			name:        "requested cancellation",
			current:     ObservationTransitionState{Phase: PhaseAssigned, CancellationRequested: true},
			observation: Observation{Phase: PhaseFinished, Outcome: &canceled},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateObservationTransition(test.current, test.observation)
			if test.wantError {
				if !errors.Is(err, ErrObservationConflict) {
					t.Fatalf("ValidateObservationTransition() error = %v, want observation conflict", err)
				}
			} else if err != nil {
				t.Fatalf("ValidateObservationTransition() error = %v", err)
			}
		})
	}
}

type recordingObservations struct {
	calls       int
	observation Observation
	err         error
}

func (s *recordingObservations) ApplyRunObservation(
	_ context.Context,
	observation Observation,
) error {
	s.calls++
	s.observation = observation

	return s.err
}
