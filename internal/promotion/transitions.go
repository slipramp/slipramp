package promotion

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type CreationDispatchClaimTransition struct {
	State     CreationDispatchState
	Lifecycle PromotionLifecycle
	Exhausted bool
}

func TransitionCreationDispatchClaim(
	source PromotionSource,
	lifecycle PromotionLifecycle,
	state CreationDispatchState,
	claim CreationDispatchClaim,
) (CreationDispatchClaimTransition, error) {
	if source != SourceSlipramp || state.NextAttemptAt == nil || state.DispatchedAt != nil || state.FailedAt != nil ||
		state.NextAttemptAt.After(claim.ClaimedAt) {
		return CreationDispatchClaimTransition{}, ErrCreationDispatchConflict
	}
	if state.AttemptCount >= len(claim.RetryDelays) {
		state.NextAttemptAt = nil
		state.FailedAt = new(claim.ClaimedAt)
		state.Diagnostic = new(claim.ExhaustedMessage)
		if lifecycle.State == LifecycleCreating {
			failed, err := TransitionLifecycle(lifecycle, LifecycleError, true, claim.ClaimedAt)
			if err != nil {
				return CreationDispatchClaimTransition{}, err
			}
			lifecycle = failed
		}
		return CreationDispatchClaimTransition{State: state, Lifecycle: lifecycle, Exhausted: true}, nil
	}

	delay := claim.RetryDelays[state.AttemptCount]
	state.AttemptCount++
	state.LastAttemptAt = new(claim.ClaimedAt)
	state.NextAttemptAt = new(claim.ClaimedAt.Add(delay))

	return CreationDispatchClaimTransition{State: state, Lifecycle: lifecycle}, nil
}

func TransitionCreationDispatchReport(
	current Promotion,
	report CreationDispatchReport,
) (Promotion, bool, error) {
	if current.Source != SourceSlipramp || report.Attempt > current.CreationDispatch.AttemptCount {
		return Promotion{}, false, ErrCreationDispatchConflict
	}

	transitioned := current
	switch report.Result {
	case CreationDispatchCreated:
		if current.CreationDispatch.FailedAt != nil {
			return current, false, nil
		}
		if current.KargoExecution.PromotionUID != nil {
			if *current.KargoExecution.PromotionUID != *report.KargoUID ||
				current.ExternalPromotionID != *report.KargoName {
				return Promotion{}, false, ErrCreationDispatchConflict
			}
			return current, false, nil
		}
		transitioned.ExternalPromotionID = *report.KargoName
		transitioned.CreationDispatch.NextAttemptAt = nil
		transitioned.CreationDispatch.DispatchedAt = new(report.ReportedAt)
		transitioned.CreationDispatch.Diagnostic = nil
		transitioned.KargoExecution.PromotionUID = new(*report.KargoUID)
		if transitioned.KargoExecution.Outcome == nil &&
			(transitioned.Lifecycle.State == LifecycleCreating ||
				transitioned.Lifecycle.State == LifecycleApproved) {
			transitioned.KargoExecution.NextReconciliationAt = new(report.ReportedAt)
		}
	case CreationDispatchRetryable:
		if report.Attempt != current.CreationDispatch.AttemptCount ||
			current.CreationDispatch.DispatchedAt != nil || current.CreationDispatch.FailedAt != nil ||
			current.KargoExecution.PromotionUID != nil {
			return current, false, nil
		}
		transitioned.CreationDispatch.Diagnostic = cloneString(report.Diagnostic)
	case CreationDispatchFailed:
		if report.Attempt != current.CreationDispatch.AttemptCount ||
			current.CreationDispatch.DispatchedAt != nil || current.CreationDispatch.FailedAt != nil ||
			current.KargoExecution.PromotionUID != nil {
			return current, false, nil
		}
		if current.Lifecycle.State == LifecycleCreating {
			lifecycle, err := TransitionLifecycle(
				current.Lifecycle,
				LifecycleError,
				current.ReviewRequired,
				report.ReportedAt,
			)
			if err != nil {
				return Promotion{}, false, err
			}
			transitioned.Lifecycle = lifecycle
		}
		transitioned.CreationDispatch.NextAttemptAt = nil
		transitioned.CreationDispatch.FailedAt = new(report.ReportedAt)
		transitioned.CreationDispatch.Diagnostic = cloneString(report.Diagnostic)
		transitioned.KargoExecution.NextReconciliationAt = nil
	default:
		return Promotion{}, false, fmt.Errorf("%w: dispatch result is invalid", ErrInvalid)
	}
	if transitioned.UpdatedAt.Before(report.ReportedAt) {
		transitioned.UpdatedAt = report.ReportedAt
	}

	return transitioned, true, nil
}

func TransitionPreparationReservation(
	lifecycle PromotionLifecycle,
	reviewRequired bool,
	state PreparationState,
	repositoryID uuid.UUID,
	catalogRevisionID uuid.UUID,
) (PreparationState, bool, error) {
	if !reviewRequired || lifecycle.State != LifecyclePreparing || state.RunID != nil ||
		repositoryID == uuid.Nil || catalogRevisionID == uuid.Nil {
		return PreparationState{}, false, ErrTransitionConflict
	}
	if state.RepositoryID != nil || state.CatalogRevisionID != nil {
		if state.RepositoryID != nil && state.CatalogRevisionID != nil &&
			*state.RepositoryID == repositoryID && *state.CatalogRevisionID == catalogRevisionID {
			return state, false, nil
		}
		return PreparationState{}, false, ErrTransitionConflict
	}
	state.RepositoryID = new(repositoryID)
	state.CatalogRevisionID = new(catalogRevisionID)

	return state, true, nil
}

func TransitionPreparationRunLink(
	lifecycle PromotionLifecycle,
	reviewRequired bool,
	state PreparationState,
	repositoryID uuid.UUID,
	catalogRevisionID uuid.UUID,
	runID uuid.UUID,
) (PreparationState, error) {
	if !reviewRequired || lifecycle.State != LifecyclePreparing || runID == uuid.Nil || state.RunID != nil ||
		state.RepositoryID == nil || state.CatalogRevisionID == nil ||
		*state.RepositoryID != repositoryID || *state.CatalogRevisionID != catalogRevisionID {
		return PreparationState{}, ErrTransitionConflict
	}
	state.RunID = new(runID)
	state.FailureCount = 0
	state.NextAttemptAt = nil

	return state, nil
}

type PreparationFailureTransition struct {
	State     PreparationState
	Lifecycle PromotionLifecycle
	Message   string
	Changed   bool
}

func TransitionPreparationFailure(
	lifecycle PromotionLifecycle,
	reviewRequired bool,
	state PreparationState,
	failure PreparationFailure,
) (PreparationFailureTransition, error) {
	if !reviewRequired {
		return PreparationFailureTransition{}, ErrTransitionConflict
	}
	if lifecycle.State != LifecyclePreparing {
		return PreparationFailureTransition{State: state, Lifecycle: lifecycle}, nil
	}
	if !optionalUUIDsEqual(state.RunID, failure.PreparationRunID) {
		return PreparationFailureTransition{}, ErrTransitionConflict
	}
	if state.NextAttemptAt != nil && state.NextAttemptAt.After(failure.FailedAt) {
		return PreparationFailureTransition{State: state, Lifecycle: lifecycle}, nil
	}

	state.FailureCount++
	terminal := failure.Permanent || state.FailureCount >= PreparationFailureLimit
	message := failure.Diagnostic
	if terminal {
		if !failure.Permanent {
			message = "Promotion preparation retry budget exhausted. " + failure.Diagnostic
		}
		failed, err := TransitionLifecycle(lifecycle, LifecycleError, reviewRequired, failure.FailedAt)
		if err != nil {
			return PreparationFailureTransition{}, err
		}
		state.NextAttemptAt = nil
		state.CompletedAt = new(failure.FailedAt)
		return PreparationFailureTransition{
			State: state, Lifecycle: failed, Message: message, Changed: true,
		}, nil
	}

	delay := PreparationInitialRetryDelay << (state.FailureCount - 1)
	delay = min(delay, PreparationMaximumRetryDelay)
	state.NextAttemptAt = new(failure.FailedAt.Add(delay))

	return PreparationFailureTransition{
		State: state, Lifecycle: lifecycle, Message: message, Changed: true,
	}, nil
}

func TransitionPreparationCompletion(
	lifecycle PromotionLifecycle,
	reviewRequired bool,
	state PreparationState,
	runID uuid.UUID,
	gitArtifactID uuid.UUID,
	argoArtifactID uuid.UUID,
	completedAt time.Time,
) (PromotionLifecycle, PreparationState, error) {
	if !reviewRequired || lifecycle.State != LifecyclePreparing || state.RunID == nil ||
		*state.RunID != runID || gitArtifactID == uuid.Nil || argoArtifactID == uuid.Nil || completedAt.IsZero() {
		return PromotionLifecycle{}, PreparationState{}, ErrTransitionConflict
	}
	next, err := TransitionLifecycle(lifecycle, LifecyclePending, reviewRequired, completedAt)
	if err != nil {
		return PromotionLifecycle{}, PreparationState{}, err
	}
	state.NextAttemptAt = nil
	state.GitDiffArtifactID = new(gitArtifactID)
	state.ArgoDiffArtifactID = new(argoArtifactID)
	state.CompletedAt = new(completedAt)

	return next, state, nil
}

func TransitionPreparationPermanentFailure(
	lifecycle PromotionLifecycle,
	reviewRequired bool,
	state PreparationState,
	runID uuid.UUID,
	failedAt time.Time,
) (PromotionLifecycle, PreparationState, error) {
	if !reviewRequired || lifecycle.State != LifecyclePreparing || state.RunID == nil ||
		*state.RunID != runID || failedAt.IsZero() {
		return PromotionLifecycle{}, PreparationState{}, ErrTransitionConflict
	}
	next, err := TransitionLifecycle(lifecycle, LifecycleError, reviewRequired, failedAt)
	if err != nil {
		return PromotionLifecycle{}, PreparationState{}, err
	}
	state.NextAttemptAt = nil
	state.CompletedAt = new(failedAt)

	return next, state, nil
}

func TransitionDecision(lifecycle PromotionLifecycle, reviewRequired bool, decision Decision) (PromotionLifecycle, error) {
	next := LifecycleApproved
	if decision.Decision == DecisionRejected {
		next = LifecycleRejected
	}
	return TransitionLifecycle(lifecycle, next, reviewRequired, decision.DecidedAt)
}

func TransitionExpiration(lifecycle PromotionLifecycle, at time.Time) (PromotionLifecycle, bool, error) {
	if lifecycle.State != LifecyclePending {
		return PromotionLifecycle{}, false, ErrTransitionConflict
	}
	if lifecycle.ExpiresAt == nil || lifecycle.ExpiresAt.After(at) {
		return lifecycle, false, nil
	}
	next, err := TransitionLifecycle(lifecycle, LifecycleExpired, true, at)
	if err != nil {
		return PromotionLifecycle{}, false, err
	}

	return next, true, nil
}

func TransitionReconciliationClaim(
	source PromotionSource,
	lifecycle PromotionLifecycle,
	state KargoExecutionState,
	claim ReconciliationClaim,
) (KargoExecutionState, error) {
	eligible := state.Outcome == nil && (lifecycle.State == LifecycleApproved ||
		(source == SourceSlipramp && lifecycle.State == LifecycleCreating && state.PromotionUID != nil))
	if !eligible || state.NextReconciliationAt == nil || state.NextReconciliationAt.After(claim.ClaimedAt) {
		return KargoExecutionState{}, ErrReconciliationConflict
	}
	state.LastReconciliationAt = new(claim.ClaimedAt)
	state.NextReconciliationAt = new(claim.ClaimedAt.Add(claim.Interval))

	return state, nil
}

type ReconciliationReportTransition struct {
	Promotion       Promotion
	IdentityBound   bool
	OutcomeRecorded bool
}

func TransitionReconciliationReport(
	current Promotion,
	report ReconciliationReport,
) (ReconciliationReportTransition, error) {
	if current.StageSnapshot.ClusterName != report.ClusterName ||
		current.ExternalPromotionID != report.KargoName {
		return ReconciliationReportTransition{}, ErrReconciliationConflict
	}

	transition := ReconciliationReportTransition{Promotion: current}
	if current.KargoExecution.PromotionUID == nil {
		if current.Source != SourceKargo || current.KargoExecution.Outcome != nil {
			return ReconciliationReportTransition{}, ErrReconciliationConflict
		}
		transition.Promotion.KargoExecution.PromotionUID = new(report.KargoUID)
		transition.IdentityBound = true
	} else if *current.KargoExecution.PromotionUID != report.KargoUID {
		return ReconciliationReportTransition{}, ErrReconciliationConflict
	}

	if report.Phase != nil {
		outcome := transition.Promotion.KargoExecution.Outcome
		if outcome != nil {
			if outcome.Phase != *report.Phase {
				return ReconciliationReportTransition{}, ErrReconciliationConflict
			}
			return transition, nil
		}
		transition.Promotion.KargoExecution.Outcome = &KargoOutcome{
			Phase: *report.Phase, ObservedAt: report.ObservedAt, Diagnostic: cloneString(report.Diagnostic),
		}
		transition.Promotion.KargoExecution.NextReconciliationAt = nil
		transition.OutcomeRecorded = true
		if transition.Promotion.ReviewRequired && transition.Promotion.Lifecycle.State == LifecycleCreating &&
			transition.Promotion.RequestDigest == (RequestDigest{}) {
			lifecycle, err := TransitionLifecycle(
				transition.Promotion.Lifecycle,
				LifecycleError,
				transition.Promotion.ReviewRequired,
				report.ObservedAt,
			)
			if err != nil {
				return ReconciliationReportTransition{}, err
			}
			transition.Promotion.Lifecycle = lifecycle
			transition.Promotion.Message = cloneString(report.Diagnostic)
		}
	}
	if transition.Promotion.UpdatedAt.Before(report.ObservedAt) {
		transition.Promotion.UpdatedAt = report.ObservedAt
	}

	return transition, nil
}

func optionalUUIDsEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	return new(*value)
}
