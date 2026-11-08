package gittrigger

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLifecycleState(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	supersededAt := createdAt.Add(time.Minute)
	failedAt := createdAt.Add(2 * time.Minute)

	tests := []struct {
		name      string
		lifecycle Lifecycle
		at        time.Time
		want      State
	}{
		{
			name: "newly created eligible delivery",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt,
			},
			at:   createdAt,
			want: StatePendingEligible,
		},
		{
			name: "retryable failure before boundary",
			lifecycle: retryLifecycle(
				createdAt,
				createdAt.Add(InitialRetryDelay),
			),
			at:   createdAt.Add(InitialRetryDelay - time.Nanosecond),
			want: StatePendingRetry,
		},
		{
			name: "retryable failure at boundary",
			lifecycle: retryLifecycle(
				createdAt,
				createdAt.Add(InitialRetryDelay),
			),
			at:   createdAt.Add(InitialRetryDelay),
			want: StatePendingEligible,
		},
		{
			name: "retryable failure after boundary",
			lifecycle: retryLifecycle(
				createdAt,
				createdAt.Add(InitialRetryDelay),
			),
			at:   createdAt.Add(InitialRetryDelay + time.Nanosecond),
			want: StatePendingEligible,
		},
		{
			name: "successful Run linkage",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt, RunID: &runID,
			},
			at:   createdAt,
			want: StateRunLinked,
		},
		{
			name: "supersession",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt, SupersededAt: &supersededAt,
			},
			at:   supersededAt,
			want: StateSuperseded,
		},
		{
			name: "terminal failure",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, FailureCount: FailureLimit,
				NextAttemptAt: failedAt.Add(MaximumRetryDelay),
				Diagnostic:    "Run creation failed.", FailedAt: &failedAt,
			},
			at:   failedAt,
			want: StateFailed,
		},
		{
			name: "permanent terminal failure before retry exhaustion",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, FailureCount: 1,
				NextAttemptAt: failedAt.Add(InitialRetryDelay),
				Diagnostic:    "invalid run parameters", FailedAt: &failedAt,
			},
			at:   failedAt,
			want: StateFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.lifecycle.State(test.at)
			if err != nil || got != test.want {
				t.Fatalf("State() = (%q, %v), want (%q, nil)", got, err, test.want)
			}
		})
	}
}

func TestLifecycleValidation(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	nextAttemptAt := createdAt.Add(InitialRetryDelay)
	failedAt := createdAt.Add(time.Second)
	supersededAt := createdAt.Add(-time.Second)
	emptyRunID := uuid.Nil
	runID := uuid.New()
	terminalAt := createdAt.Add(time.Minute)
	terminalNextAttemptAt := terminalAt.Add(InitialRetryDelay)

	tests := []struct {
		name      string
		lifecycle Lifecycle
	}{
		{name: "missing creation time", lifecycle: Lifecycle{NextAttemptAt: createdAt}},
		{name: "missing next attempt time", lifecycle: Lifecycle{CreatedAt: createdAt}},
		{
			name:      "next attempt before creation",
			lifecycle: Lifecycle{CreatedAt: createdAt, NextAttemptAt: createdAt.Add(-time.Second)},
		},
		{
			name:      "negative failure count",
			lifecycle: Lifecycle{CreatedAt: createdAt, NextAttemptAt: createdAt, FailureCount: -1},
		},
		{
			name: "failure count above limit",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: nextAttemptAt,
				FailureCount: FailureLimit + 1, Diagnostic: "failure",
			},
		},
		{
			name:      "new delivery with diagnostic",
			lifecycle: Lifecycle{CreatedAt: createdAt, NextAttemptAt: createdAt, Diagnostic: "failure"},
		},
		{
			name:      "new delivery scheduled later",
			lifecycle: Lifecycle{CreatedAt: createdAt, NextAttemptAt: nextAttemptAt},
		},
		{
			name:      "failed attempt without diagnostic",
			lifecycle: Lifecycle{CreatedAt: createdAt, NextAttemptAt: nextAttemptAt, FailureCount: 1},
		},
		{
			name: "failed attempt without later retry",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt,
				FailureCount: 1, Diagnostic: "failure",
			},
		},
		{
			name: "exhausted without terminal failure",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: nextAttemptAt,
				FailureCount: FailureLimit, Diagnostic: "failure",
			},
		},
		{
			name: "terminal failure without attempt",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt, FailedAt: &failedAt,
			},
		},
		{
			name: "terminal failure before creation",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: nextAttemptAt,
				FailureCount: 1, Diagnostic: "failure", FailedAt: &supersededAt,
			},
		},
		{
			name: "terminal failure at retry time",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: nextAttemptAt,
				FailureCount: 1, Diagnostic: "failure", FailedAt: &nextAttemptAt,
			},
		},
		{
			name: "supersession before creation",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt, SupersededAt: &supersededAt,
			},
		},
		{
			name: "empty linked Run ID",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt, RunID: &emptyRunID,
			},
		},
		{
			name: "linked and superseded",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, NextAttemptAt: createdAt,
				RunID: &runID, SupersededAt: &terminalAt,
			},
		},
		{
			name: "linked and failed",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, FailureCount: 1, NextAttemptAt: terminalNextAttemptAt,
				Diagnostic: "permanent failure", RunID: &runID, FailedAt: &terminalAt,
			},
		},
		{
			name: "superseded and failed",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, FailureCount: 1, NextAttemptAt: terminalNextAttemptAt,
				Diagnostic: "permanent failure", SupersededAt: &terminalAt, FailedAt: &terminalAt,
			},
		},
		{
			name: "linked, superseded, and failed",
			lifecycle: Lifecycle{
				CreatedAt: createdAt, FailureCount: 1, NextAttemptAt: terminalNextAttemptAt,
				Diagnostic: "permanent failure", RunID: &runID,
				SupersededAt: &terminalAt, FailedAt: &terminalAt,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.lifecycle.Validate(); !errors.Is(err, ErrInvalidDelivery) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDelivery", err)
			}
		})
	}
}

func TestStateAllowsGuardedOperations(t *testing.T) {
	t.Parallel()

	allowed := map[State][]Operation{
		StatePendingEligible: {OperationLinkRun, OperationRecordFailure, OperationSupersede},
		StatePendingRetry:    {OperationSupersede},
		StateRunLinked:       {OperationLinkRun},
		StateSuperseded:      nil,
		StateFailed:          nil,
	}
	operations := []Operation{OperationLinkRun, OperationRecordFailure, OperationSupersede}
	for state, allowedOperations := range allowed {
		for _, operation := range operations {
			want := slices.Contains(allowedOperations, operation)
			if got := state.Allows(operation); got != want {
				t.Errorf("%s.Allows(%s) = %t, want %t", state, operation, got, want)
			}
		}
	}
}

func retryLifecycle(createdAt, nextAttemptAt time.Time) Lifecycle {
	return Lifecycle{
		CreatedAt: createdAt, FailureCount: 1, NextAttemptAt: nextAttemptAt,
		Diagnostic: "Run creation failed.",
	}
}
