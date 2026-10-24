package run

import "time"

const observationFreshnessWindow = time.Minute

type ObservationStatus struct {
	Phase          Phase
	AssignedAt     *time.Time
	LastObservedAt *time.Time
}

func (r Run) ObservationStatus() ObservationStatus {
	return ObservationStatus{
		Phase:          r.Phase,
		AssignedAt:     r.AssignedAt,
		LastObservedAt: r.LastObservedAt,
	}
}

func DeriveObservationState(status ObservationStatus, clusterConnected bool, now time.Time) (ObservationState, bool) {
	switch status.Phase {
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
	if status.LastObservedAt == nil {
		if status.AssignedAt != nil && !status.AssignedAt.Before(staleBefore) {
			return ObservationStatePending, true
		}

		return ObservationStateUnknown, true
	}

	if status.LastObservedAt.Before(staleBefore) {
		return ObservationStateUnknown, true
	}

	return ObservationStateCurrent, true
}
