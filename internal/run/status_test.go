package run

import (
	"testing"
	"time"
)

func TestDeriveObservationState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-observationFreshnessWindow)
	stale := fresh.Add(-time.Nanosecond)

	tests := []struct {
		name             string
		phase            Phase
		lastObservedAt   *time.Time
		clusterConnected bool
		want             ObservationState
		wantPresent      bool
	}{
		{
			name:             "fresh running observation",
			phase:            PhaseRunning,
			lastObservedAt:   &fresh,
			clusterConnected: true,
			want:             ObservationStateCurrent,
			wantPresent:      true,
		},
		{
			name:             "stale observation",
			phase:            PhaseRunning,
			lastObservedAt:   &stale,
			clusterConnected: true,
			want:             ObservationStateUnknown,
			wantPresent:      true,
		},
		{
			name:             "unreachable cluster",
			phase:            PhasePreparing,
			lastObservedAt:   &fresh,
			clusterConnected: false,
			want:             ObservationStateUnknown,
			wantPresent:      true,
		},
		{
			name:             "not yet observed",
			phase:            PhaseAssigned,
			clusterConnected: true,
			want:             ObservationStateUnknown,
			wantPresent:      true,
		},
		{
			name:  "queued",
			phase: PhaseQueued,
		},
		{
			name:             "finished",
			phase:            PhaseFinished,
			lastObservedAt:   &stale,
			clusterConnected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state, present := DeriveObservationState(
				Run{
					Phase:          test.phase,
					LastObservedAt: test.lastObservedAt,
				},
				test.clusterConnected,
				now,
			)
			if state != test.want || present != test.wantPresent {
				t.Errorf(
					"DeriveObservationState() = (%q, %t), want (%q, %t)",
					state,
					present,
					test.want,
					test.wantPresent,
				)
			}
		})
	}
}
