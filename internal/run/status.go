package run

import "time"

const observationFreshnessWindow = time.Minute

func DeriveObservationState(record Run, clusterConnected bool, now time.Time) (ObservationState, bool) {
	switch record.Phase {
	case PhaseAssigned, PhasePreparing, PhaseRunning:
	case PhaseQueued, PhaseFinished:
		return "", false
	default:
		return "", false
	}

	if !clusterConnected {
		return ObservationStateUnknown, true
	}

	staleBefore := now.Add(-observationFreshnessWindow)
	if record.LastObservedAt == nil {
		if record.AssignedAt != nil && !record.AssignedAt.Before(staleBefore) {
			return ObservationStatePending, true
		}

		return ObservationStateUnknown, true
	}

	if record.LastObservedAt.Before(staleBefore) {
		return ObservationStateUnknown, true
	}

	return ObservationStateCurrent, true
}
