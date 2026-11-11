package promotion

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type LifecycleState string

const (
	LifecycleCreating  LifecycleState = "creating"
	LifecyclePreparing LifecycleState = "preparing"
	LifecyclePending   LifecycleState = "pending"
	LifecycleApproved  LifecycleState = "approved"
	LifecycleRejected  LifecycleState = "rejected"
	LifecycleExpired   LifecycleState = "expired"
	LifecycleError     LifecycleState = "error"
)

func (p CreationDispatchPhase) Valid() bool {
	switch p {
	case CreationDispatchNotApplicable, CreationDispatchAwaitingDelivery,
		CreationDispatchDelivered, CreationDispatchPhaseFailed:
		return true
	default:
		return false
	}
}

// PromotionLifecycle is the public review lifecycle. It does not include the
// independently observed Kargo execution outcome.
type PromotionLifecycle struct {
	State     LifecycleState
	DecidedAt *time.Time
	ExpiresAt *time.Time
}

func (s LifecycleState) Valid() bool {
	switch s {
	case LifecycleCreating, LifecyclePreparing, LifecyclePending, LifecycleApproved,
		LifecycleRejected, LifecycleExpired, LifecycleError:
		return true
	default:
		return false
	}
}

func (s LifecycleState) Terminal() bool {
	return s == LifecycleApproved || s == LifecycleRejected || s == LifecycleExpired || s == LifecycleError
}

func (s LifecycleState) CanTransitionTo(next LifecycleState) bool {
	switch s {
	case LifecycleCreating:
		return next == LifecyclePreparing || next == LifecycleApproved || next == LifecycleError
	case LifecyclePreparing:
		return next == LifecyclePending || next == LifecycleError
	case LifecyclePending:
		return next == LifecycleApproved || next == LifecycleRejected || next == LifecycleExpired
	default:
		return false
	}
}

func TransitionLifecycle(
	current PromotionLifecycle,
	next LifecycleState,
	reviewRequired bool,
	at time.Time,
) (PromotionLifecycle, error) {
	if !current.State.Valid() || !next.Valid() || at.IsZero() || !current.State.CanTransitionTo(next) {
		return PromotionLifecycle{}, ErrTransitionConflict
	}
	if !reviewRequired && next != LifecycleApproved && next != LifecycleError {
		return PromotionLifecycle{}, ErrTransitionConflict
	}

	transitioned := current
	transitioned.State = next
	transitioned.DecidedAt = nil
	if reviewRequired && (next == LifecycleApproved || next == LifecycleRejected) {
		transitioned.DecidedAt = new(at)
	}

	return transitioned, nil
}

func (l PromotionLifecycle) Validate(reviewRequired bool, createdAt time.Time) error {
	if !l.State.Valid() {
		return fmt.Errorf("%w: Promotion lifecycle is invalid", ErrInvalid)
	}
	if !reviewRequired && l.State != LifecycleCreating && l.State != LifecycleApproved && l.State != LifecycleError {
		return fmt.Errorf("%w: automatic Promotion lifecycle is invalid", ErrInvalid)
	}
	if reviewRequired && (l.State == LifecycleApproved || l.State == LifecycleRejected) && l.DecidedAt == nil {
		return fmt.Errorf("%w: decided Promotion requires a decision time", ErrInvalid)
	}
	if l.State != LifecycleApproved && l.State != LifecycleRejected && l.DecidedAt != nil {
		return fmt.Errorf("%w: undecided Promotion cannot have a decision time", ErrInvalid)
	}
	if !reviewRequired && l.DecidedAt != nil {
		return fmt.Errorf("%w: automatic Promotion cannot have a decision time", ErrInvalid)
	}
	if l.DecidedAt != nil && l.DecidedAt.Before(createdAt) {
		return fmt.Errorf("%w: Promotion decision time precedes creation", ErrInvalid)
	}
	if l.ExpiresAt != nil && !l.ExpiresAt.After(createdAt) {
		return fmt.Errorf("%w: Promotion expiry must follow creation", ErrInvalid)
	}

	return nil
}

type CreationDispatchPhase string

const (
	CreationDispatchNotApplicable    CreationDispatchPhase = "not-applicable"
	CreationDispatchAwaitingDelivery CreationDispatchPhase = "awaiting-delivery"
	CreationDispatchDelivered        CreationDispatchPhase = "delivered"
	CreationDispatchPhaseFailed      CreationDispatchPhase = "failed"
)

// CreationDispatchState contains only durable Kargo object-creation delivery
// facts. It is independent from the review lifecycle.
type CreationDispatchState struct {
	AttemptCount  int
	NextAttemptAt *time.Time
	LastAttemptAt *time.Time
	DispatchedAt  *time.Time
	FailedAt      *time.Time
	Diagnostic    *string
}

func (s CreationDispatchState) Phase(source PromotionSource) CreationDispatchPhase {
	if source == SourceKargo {
		return CreationDispatchNotApplicable
	}
	if s.DispatchedAt != nil {
		return CreationDispatchDelivered
	}
	if s.FailedAt != nil {
		return CreationDispatchPhaseFailed
	}
	return CreationDispatchAwaitingDelivery
}

func (s CreationDispatchState) Validate(source PromotionSource, lifecycle LifecycleState, createdAt time.Time) error {
	if source == SourceKargo {
		if s != (CreationDispatchState{}) {
			return fmt.Errorf("%w: Kargo-originated Promotion cannot have creation dispatch state", ErrInvalid)
		}
		return nil
	}
	if s.AttemptCount < 0 {
		return fmt.Errorf("%w: Promotion dispatch attempt count cannot be negative", ErrInvalid)
	}
	if s.NextAttemptAt != nil && (s.DispatchedAt != nil || s.FailedAt != nil) {
		return fmt.Errorf("%w: completed Promotion delivery cannot remain due for dispatch", ErrInvalid)
	}
	if s.DispatchedAt != nil && s.FailedAt != nil {
		return fmt.Errorf("%w: Promotion delivery cannot be both delivered and failed", ErrInvalid)
	}
	if s.NextAttemptAt == nil && s.DispatchedAt == nil && s.FailedAt == nil {
		return fmt.Errorf("%w: Slipramp-originated Promotion requires delivery state", ErrInvalid)
	}
	if s.Diagnostic != nil {
		if err := validateText("creation dispatch diagnostic", *s.Diagnostic, maximumMessageLength); err != nil {
			return err
		}
	}
	if s.FailedAt != nil && s.Diagnostic == nil {
		return fmt.Errorf("%w: failed Promotion delivery requires a diagnostic", ErrInvalid)
	}
	for label, value := range map[string]*time.Time{
		"next dispatch": s.NextAttemptAt,
		"last dispatch": s.LastAttemptAt,
		"dispatched":    s.DispatchedAt,
		"failed":        s.FailedAt,
	} {
		if value != nil && value.Before(createdAt) {
			return fmt.Errorf("%w: Promotion %s time precedes creation", ErrInvalid, label)
		}
	}

	return nil
}

type PreparationPhase string

const (
	PreparationNotRequired PreparationPhase = "not-required"
	PreparationUnreserved  PreparationPhase = "unreserved"
	PreparationReserved    PreparationPhase = "reserved"
	PreparationRunning     PreparationPhase = "running"
	PreparationRetrying    PreparationPhase = "retrying"
	PreparationCompleted   PreparationPhase = "completed"
	PreparationFailed      PreparationPhase = "failed"
)

// PreparationState contains the revision reservation, Run linkage, retry, and
// validated Artifact facts for review preparation.
type PreparationState struct {
	RepositoryID       *uuid.UUID
	CatalogRevisionID  *uuid.UUID
	RunID              *uuid.UUID
	FailureCount       int
	NextAttemptAt      *time.Time
	GitDiffArtifactID  *uuid.UUID
	ArgoDiffArtifactID *uuid.UUID
	CompletedAt        *time.Time
}

func (s PreparationState) Phase(reviewRequired bool, lifecycle LifecycleState) PreparationPhase {
	if !reviewRequired {
		return PreparationNotRequired
	}
	if lifecycle == LifecycleError {
		return PreparationFailed
	}
	if s.CompletedAt != nil {
		return PreparationCompleted
	}
	if s.NextAttemptAt != nil {
		return PreparationRetrying
	}
	if s.RunID != nil {
		return PreparationRunning
	}
	if s.CatalogRevisionID != nil {
		return PreparationReserved
	}
	return PreparationUnreserved
}

func (s PreparationState) Validate(reviewRequired bool, lifecycle LifecycleState, createdAt time.Time) error {
	if (s.RepositoryID == nil) != (s.CatalogRevisionID == nil) {
		return fmt.Errorf("%w: preparation Repository and catalog revision must be reserved together", ErrInvalid)
	}
	if s.RunID != nil && s.CatalogRevisionID == nil {
		return fmt.Errorf("%w: preparation Run requires a reserved catalog revision", ErrInvalid)
	}
	if s.FailureCount < 0 || (!reviewRequired && s.FailureCount != 0) {
		return fmt.Errorf("%w: preparation failure count is invalid", ErrInvalid)
	}
	if s.NextAttemptAt != nil && (lifecycle != LifecyclePreparing || s.FailureCount == 0 ||
		s.NextAttemptAt.Before(createdAt)) {
		return fmt.Errorf("%w: preparation retry time is invalid", ErrInvalid)
	}
	if !reviewRequired && s != (PreparationState{}) {
		return fmt.Errorf("%w: automatic Promotion cannot have preparation state", ErrInvalid)
	}
	if lifecycle == LifecycleCreating && s != (PreparationState{}) {
		return fmt.Errorf("%w: creating Promotion cannot have preparation state", ErrInvalid)
	}
	if lifecycle == LifecyclePreparing && (s.CompletedAt != nil || s.GitDiffArtifactID != nil ||
		s.ArgoDiffArtifactID != nil) {
		return fmt.Errorf("%w: preparing Promotion cannot have completed preparation state", ErrInvalid)
	}
	if reviewRequired && (lifecycle == LifecyclePending || lifecycle == LifecycleApproved ||
		lifecycle == LifecycleRejected || lifecycle == LifecycleExpired) &&
		(s.RepositoryID == nil || s.CatalogRevisionID == nil || s.RunID == nil || s.CompletedAt == nil ||
			s.GitDiffArtifactID == nil || s.ArgoDiffArtifactID == nil) {
		return fmt.Errorf("%w: prepared Promotion requires its Run and artifacts", ErrInvalid)
	}
	if s.CompletedAt != nil && s.CompletedAt.Before(createdAt) {
		return fmt.Errorf("%w: Promotion preparation completion precedes creation", ErrInvalid)
	}

	return nil
}

type KargoExecutionPhase string

const (
	KargoExecutionWaiting     KargoExecutionPhase = "waiting"
	KargoExecutionReconciling KargoExecutionPhase = "reconciling"
	KargoExecutionSucceeded   KargoExecutionPhase = "succeeded"
	KargoExecutionFailed      KargoExecutionPhase = "failed"
	KargoExecutionErrored     KargoExecutionPhase = "errored"
	KargoExecutionAborted     KargoExecutionPhase = "aborted"
)

// KargoExecutionState is independent from review. In particular, approval can
// coexist with a still-reconciling Kargo Promotion.
type KargoExecutionState struct {
	PromotionUID         *string
	NextReconciliationAt *time.Time
	LastReconciliationAt *time.Time
	Outcome              *KargoOutcome
}

func (s KargoExecutionState) Phase() KargoExecutionPhase {
	if s.Outcome == nil {
		if s.NextReconciliationAt != nil {
			return KargoExecutionReconciling
		}
		return KargoExecutionWaiting
	}
	switch s.Outcome.Phase {
	case KargoPhaseSucceeded:
		return KargoExecutionSucceeded
	case KargoPhaseFailed:
		return KargoExecutionFailed
	case KargoPhaseErrored:
		return KargoExecutionErrored
	default:
		return KargoExecutionAborted
	}
}

func (s KargoExecutionState) Validate(
	source PromotionSource,
	lifecycle LifecycleState,
	creation CreationDispatchState,
	createdAt time.Time,
) error {
	if s.PromotionUID != nil {
		if err := validateText("Kargo Promotion UID", *s.PromotionUID, maximumTextLength); err != nil {
			return err
		}
		if source == SourceSlipramp && creation.DispatchedAt == nil {
			return fmt.Errorf("%w: Kargo Promotion UID requires successful dispatch time", ErrInvalid)
		}
	}
	reconciliationEligible := s.Outcome == nil && ((source == SourceKargo && lifecycle == LifecycleApproved) ||
		(source == SourceSlipramp && creation.DispatchedAt != nil && s.PromotionUID != nil &&
			(lifecycle == LifecycleCreating || lifecycle == LifecycleApproved)))
	if s.NextReconciliationAt != nil && !reconciliationEligible {
		return fmt.Errorf("%w: Promotion is not eligible for Kargo reconciliation", ErrInvalid)
	}
	if reconciliationEligible && s.NextReconciliationAt == nil {
		return fmt.Errorf("%w: Promotion awaiting a Kargo outcome must be due for reconciliation", ErrInvalid)
	}
	if s.LastReconciliationAt != nil && source == SourceSlipramp && s.PromotionUID == nil {
		return fmt.Errorf("%w: Slipramp-originated reconciliation requires a materialized Kargo resource", ErrInvalid)
	}
	if (s.NextReconciliationAt != nil && s.NextReconciliationAt.Before(createdAt)) ||
		(s.LastReconciliationAt != nil && s.LastReconciliationAt.Before(createdAt)) {
		return fmt.Errorf("%w: Promotion reconciliation times cannot precede creation", ErrInvalid)
	}
	if s.Outcome != nil {
		if s.PromotionUID == nil {
			return fmt.Errorf("%w: Kargo outcome requires a bound Kubernetes UID", ErrInvalid)
		}
		if s.NextReconciliationAt != nil {
			return fmt.Errorf("%w: terminal Kargo execution cannot remain scheduled", ErrInvalid)
		}
		if err := s.Outcome.Validate(createdAt); err != nil {
			return err
		}
	}

	return nil
}
