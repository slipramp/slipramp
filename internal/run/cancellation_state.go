package run

import (
	"fmt"
	"time"
)

// CancellationStatus contains the authoritative Run facts that determine the
// operator-cancellation workflow. Execution phase remains an independent fact.
type CancellationStatus struct {
	Phase                   Phase
	Outcome                 *Outcome
	OutcomeReason           *string
	CreatedAt               time.Time
	FinishedAt              *time.Time
	CancellationRequestedAt *time.Time
	CanceledByPresent       bool
}

type CancellationOperation string

const (
	CancellationOperationNone          CancellationOperation = "none"
	CancellationOperationRequestActive CancellationOperation = "request-active"
	CancellationOperationFinishQueued  CancellationOperation = "finish-queued"
)

// CancellationTransition is the domain plan for an idempotent operator cancel
// request. Storage applies the operation under the same lock used to read the
// current facts.
type CancellationTransition struct {
	From      CancellationState
	To        CancellationState
	Operation CancellationOperation
}

func (r Run) CancellationStatus() CancellationStatus {
	return CancellationStatus{
		Phase:                   r.Phase,
		Outcome:                 r.Outcome,
		OutcomeReason:           r.OutcomeReason,
		CreatedAt:               r.CreatedAt,
		FinishedAt:              r.FinishedAt,
		CancellationRequestedAt: r.CancellationRequestedAt,
		CanceledByPresent:       r.CanceledBy != nil,
	}
}

// CancellationState derives operator-cancellation state without adding a
// persisted status alongside phase, outcome, reason, and request time.
func (r Run) CancellationState() (CancellationState, error) {
	return r.CancellationStatus().CancellationState()
}

func (s CancellationStatus) CancellationState() (CancellationState, error) {
	if err := s.validateLifecycle(); err != nil {
		return "", err
	}

	requested := s.CancellationRequestedAt != nil
	if !requested {
		if s.CanceledByPresent {
			return "", invalidCancellation("canceler requires a cancellation request")
		}
		if s.Phase == PhaseFinished && s.Outcome != nil && *s.Outcome == OutcomeCanceled {
			if !reasonIs(s.OutcomeReason, ReasonSupersededByGitChange) &&
				!reasonIs(s.OutcomeReason, ReasonReviewRejected) {
				return "", invalidCancellation(
					"canceled Run without an operator request requires a non-operator cancellation reason",
				)
			}
		}
		if reasonIs(s.OutcomeReason, ReasonCancelRequested) {
			return "", invalidCancellation("CancelRequested reason requires a cancellation request")
		}

		return CancellationStateNotRequested, nil
	}

	if s.CancellationRequestedAt.IsZero() {
		return "", invalidCancellation("cancellation request time must not be zero")
	}
	if !s.CreatedAt.IsZero() && s.CancellationRequestedAt.Before(s.CreatedAt) {
		return "", invalidCancellation("cancellation request cannot precede Run creation")
	}
	if s.FinishedAt != nil && s.CancellationRequestedAt.After(*s.FinishedAt) {
		return "", invalidCancellation("cancellation request cannot follow Run completion")
	}

	switch s.Phase {
	case PhaseAssigned, PhasePreparing, PhaseRunning:
		return CancellationStateRequested, nil
	case PhaseQueued:
		return "", invalidCancellation("queued cancellation must finish atomically")
	case PhaseFinished:
		if s.Outcome != nil && *s.Outcome == OutcomeCanceled {
			if !reasonIs(s.OutcomeReason, ReasonCancelRequested) {
				return "", invalidCancellation(
					"completed operator cancellation requires the CancelRequested reason",
				)
			}

			return CancellationStateCompleted, nil
		}
		if reasonIs(s.OutcomeReason, ReasonCancelRequested) {
			return "", invalidCancellation("overtaken cancellation cannot use the CancelRequested reason")
		}

		return CancellationStateOvertaken, nil
	default:
		panic("cancellation lifecycle validation accepted an unknown phase")
	}
}

func (s CancellationStatus) validateLifecycle() error {
	if !validPhase(s.Phase) {
		return invalidCancellation("phase %q is invalid", s.Phase)
	}
	if s.FinishedAt != nil {
		if s.FinishedAt.IsZero() {
			return invalidCancellation("finish time must not be zero")
		}
		if s.Phase != PhaseFinished {
			return invalidCancellation("unfinished Run must not have a finish time")
		}
	}

	if s.Phase == PhaseFinished {
		if s.Outcome == nil || !validOutcome(*s.Outcome) {
			return invalidCancellation("finished Run requires a valid outcome")
		}
	} else if s.Outcome != nil {
		return invalidCancellation("unfinished Run must not have a terminal outcome")
	}
	if s.Phase != PhaseFinished && s.OutcomeReason != nil {
		return invalidCancellation("unfinished Run must not have a terminal reason")
	}

	return nil
}

// PlanCancellationRequest defines the only operator-request transitions:
// queued Runs finish atomically, active Runs persist intent, and an existing
// request is unchanged even after it completes or is overtaken.
func PlanCancellationRequest(current CancellationStatus, requestedAt time.Time) (CancellationTransition, error) {
	from, err := current.CancellationState()
	if err != nil {
		return CancellationTransition{}, err
	}
	if requestedAt.IsZero() {
		return CancellationTransition{}, invalidCancellation("requested transition time must not be zero")
	}
	if !current.CreatedAt.IsZero() && requestedAt.Before(current.CreatedAt) {
		return CancellationTransition{}, invalidCancellation("requested transition cannot precede Run creation")
	}

	transition := CancellationTransition{From: from, To: from, Operation: CancellationOperationNone}
	if from != CancellationStateNotRequested {
		return transition, nil
	}

	switch current.Phase {
	case PhaseQueued:
		transition.To = CancellationStateCompleted
		transition.Operation = CancellationOperationFinishQueued
	case PhaseAssigned, PhasePreparing, PhaseRunning:
		transition.To = CancellationStateRequested
		transition.Operation = CancellationOperationRequestActive
	case PhaseFinished:
		return CancellationTransition{}, ErrRunFinished
	default:
		return CancellationTransition{}, invalidCancellation("phase %q cannot be canceled", current.Phase)
	}

	return transition, nil
}

// ValidateCancellationTransition checks changes produced by execution
// observations as well as the immediate queued-cancellation operation.
func ValidateCancellationTransition(current, next CancellationStatus) error {
	from, err := current.CancellationState()
	if err != nil {
		return err
	}
	to, err := next.CancellationState()
	if err != nil {
		return err
	}

	if from == to {
		return nil
	}
	switch from {
	case CancellationStateNotRequested:
		if to == CancellationStateRequested && current.Phase != PhaseFinished {
			return nil
		}
		if to == CancellationStateCompleted && current.Phase == PhaseQueued &&
			next.Phase == PhaseFinished && next.CancellationRequestedAt != nil &&
			next.FinishedAt != nil && next.CancellationRequestedAt.Equal(*next.FinishedAt) {
			return nil
		}
	case CancellationStateRequested:
		if to == CancellationStateCompleted || to == CancellationStateOvertaken {
			return nil
		}
	}

	return fmt.Errorf(
		"%w: transition from %q to %q is not legal",
		ErrInvalidCancellation,
		from,
		to,
	)
}

func reasonIs(reason *string, expected string) bool {
	return reason != nil && *reason == expected
}

func invalidCancellation(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCancellation, fmt.Sprintf(format, arguments...))
}
