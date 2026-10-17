package run

import "time"

const observationFreshnessWindow = time.Minute

func DeriveObservationState(
	record Run,
	clusterConnected bool,
	now time.Time,
) (ObservationState, bool) {
	switch record.Phase {
	case PhaseAssigned, PhasePreparing, PhaseRunning:
	case PhaseQueued, PhaseFinished:
		return "", false
	default:
		return "", false
	}

	if !clusterConnected || record.LastObservedAt == nil {
		return ObservationStateUnknown, true
	}

	staleBefore := now.Add(-observationFreshnessWindow)
	if record.LastObservedAt.Before(staleBefore) {
		return ObservationStateUnknown, true
	}

	return ObservationStateCurrent, true
}
