package run

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReportObservationRecordsAllowedTransitions(t *testing.T) {
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

			runs := &recordingRuns{}
			service := &Service{runs: runs}
			location := time.FixedZone("test", -6*60*60)
			reportedAt := time.Date(2026, time.October, 17, 10, 0, 0, 0, location)
			service.now = func() time.Time { return reportedAt }
			runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
			test.observation.ID = runID
			test.observation.ClusterName = "colo"

			err := service.ReportObservation(context.Background(), test.observation)
			if err != nil {
				t.Fatalf("ReportObservation() error = %v", err)
			}
			if runs.observationCalls != 1 || runs.observation.ID != runID ||
				runs.observation.ClusterName != "colo" ||
				runs.observation.Phase != test.observation.Phase ||
				!slices.Equal(runs.observationFrom, test.from) {
				t.Errorf("observation = %#v", runs)
			}
			if !runs.observation.ObservedAt.Equal(reportedAt) ||
				runs.observation.ObservedAt.Location() != time.UTC {
				t.Errorf("observed at = %s, want %s in UTC", runs.observation.ObservedAt, reportedAt)
			}
		})
	}
}

func TestReportObservationRejectsInvalidPhaseOutcomeCombinations(t *testing.T) {
	t.Parallel()

	succeeded := OutcomeSucceeded
	tests := []struct {
		name        string
		observation Observation
	}{
		{name: "queued", observation: Observation{Phase: PhaseQueued}},
		{name: "assigned", observation: Observation{Phase: PhaseAssigned}},
		{name: "preparing without Job UID", observation: Observation{Phase: PhasePreparing}},
		{name: "unfinished outcome", observation: Observation{
			Phase: PhaseRunning, Outcome: &succeeded,
		}},
		{name: "finished without outcome", observation: Observation{Phase: PhaseFinished}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &recordingRuns{}
			service := &Service{runs: runs, now: time.Now}
			test.observation.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
			test.observation.ClusterName = "colo"
			err := service.ReportObservation(context.Background(), test.observation)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("ReportObservation() error = %v, want %v", err, ErrInvalidRequest)
			}
			if runs.observationCalls != 0 {
				t.Errorf("observation calls = %d, want 0", runs.observationCalls)
			}
		})
	}
}
