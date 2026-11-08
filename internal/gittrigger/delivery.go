// Package gittrigger owns durable delivery of Runs caused by accepted Git
// repository heads.
package gittrigger

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNoEligibleDelivery = errors.New("no eligible Git-trigger delivery")
	ErrInvalidDelivery    = errors.New("invalid Git-trigger delivery")
)

// State is the authoritative interpretation of a delivery's durable outcome
// markers and, while it is pending, its time-based retry eligibility.
type State string

const (
	StatePendingEligible State = "pending-eligible"
	StatePendingRetry    State = "pending-retry"
	StateRunLinked       State = "run-linked"
	StateSuperseded      State = "superseded"
	StateFailed          State = "failed"
)

// Operation names the guarded database mutations in the delivery state
// machine. Time passing from pending-retry to pending-eligible is derived and
// does not mutate the delivery.
type Operation string

const (
	OperationLinkRun       Operation = "link-run"
	OperationRecordFailure Operation = "record-failure"
	OperationSupersede     Operation = "supersede"
)

// Allows reports whether the state permits an operation. LinkRun is also
// idempotent for a delivery already linked to the same Run; the database guard
// verifies the Run identity.
func (s State) Allows(operation Operation) bool {
	switch s {
	case StatePendingEligible:
		return operation == OperationLinkRun ||
			operation == OperationRecordFailure ||
			operation == OperationSupersede
	case StatePendingRetry:
		return operation == OperationSupersede
	case StateRunLinked:
		return operation == OperationLinkRun
	case StateSuperseded, StateFailed:
		return false
	default:
		return false
	}
}

// Lifecycle contains the existing persisted fields that determine delivery
// state. It is deliberately not persisted as a separate enum.
type Lifecycle struct {
	CreatedAt     time.Time
	FailureCount  int
	NextAttemptAt time.Time
	Diagnostic    string
	FailedAt      *time.Time
	RunID         *uuid.UUID
	SupersededAt  *time.Time
}

// State validates all lifecycle fields and derives the delivery state at the
// supplied time. Terminal markers always take precedence over retry timing.
func (l Lifecycle) State(at time.Time) (State, error) {
	if err := l.Validate(); err != nil {
		return "", err
	}

	switch {
	case l.RunID != nil:
		return StateRunLinked, nil
	case l.SupersededAt != nil:
		return StateSuperseded, nil
	case l.FailedAt != nil:
		return StateFailed, nil
	case at.Before(l.NextAttemptAt):
		return StatePendingRetry, nil
	default:
		return StatePendingEligible, nil
	}
}

// Validate enforces the cross-field invariants shared by storage mappings and
// projections. A failed delivery may have fewer than FailureLimit attempts
// when the creation error was permanent.
func (l Lifecycle) Validate() error {
	if l.CreatedAt.IsZero() {
		return fmt.Errorf("%w: creation time is required", ErrInvalidDelivery)
	}
	if l.NextAttemptAt.IsZero() {
		return fmt.Errorf("%w: next attempt time is required", ErrInvalidDelivery)
	}
	if l.NextAttemptAt.Before(l.CreatedAt) {
		return fmt.Errorf("%w: next attempt precedes creation", ErrInvalidDelivery)
	}
	if l.FailureCount < 0 || l.FailureCount > FailureLimit {
		return fmt.Errorf("%w: failure count %d is outside 0..%d", ErrInvalidDelivery, l.FailureCount, FailureLimit)
	}
	if l.FailureCount == 0 {
		if l.Diagnostic != "" {
			return fmt.Errorf("%w: a delivery without failures has a diagnostic", ErrInvalidDelivery)
		}
		if !l.NextAttemptAt.Equal(l.CreatedAt) {
			return fmt.Errorf("%w: a new delivery must be immediately eligible", ErrInvalidDelivery)
		}
	} else {
		if l.Diagnostic == "" {
			return fmt.Errorf("%w: a failed attempt requires a diagnostic", ErrInvalidDelivery)
		}
		if !l.NextAttemptAt.After(l.CreatedAt) {
			return fmt.Errorf("%w: a retry must be scheduled after creation", ErrInvalidDelivery)
		}
	}
	if l.FailureCount == FailureLimit && l.FailedAt == nil {
		return fmt.Errorf("%w: an exhausted delivery must be terminally failed", ErrInvalidDelivery)
	}

	terminalMarkers := 0
	if l.RunID != nil {
		terminalMarkers++
		if *l.RunID == uuid.Nil {
			return fmt.Errorf("%w: linked Run ID is empty", ErrInvalidDelivery)
		}
	}
	if l.SupersededAt != nil {
		terminalMarkers++
		if l.SupersededAt.Before(l.CreatedAt) {
			return fmt.Errorf("%w: supersession precedes creation", ErrInvalidDelivery)
		}
	}
	if l.FailedAt != nil {
		terminalMarkers++
		if l.FailureCount == 0 {
			return fmt.Errorf("%w: terminal failure has no failed attempt", ErrInvalidDelivery)
		}
		if l.FailedAt.Before(l.CreatedAt) {
			return fmt.Errorf("%w: terminal failure precedes creation", ErrInvalidDelivery)
		}
		if !l.FailedAt.Before(l.NextAttemptAt) {
			return fmt.Errorf("%w: terminal failure must precede its computed retry time", ErrInvalidDelivery)
		}
	}
	if terminalMarkers > 1 {
		return fmt.Errorf("%w: delivery has multiple terminal outcomes", ErrInvalidDelivery)
	}

	return nil
}

type Delivery struct {
	ID                uuid.UUID
	TargetID          uuid.UUID
	RepositoryID      uuid.UUID
	CatalogRevisionID uuid.UUID
	TargetName        string
	ActionName        string
	TriggerName       string
	ReviewRequired    bool
	Parameters        map[string]any
	Lifecycle         Lifecycle
}

func (d Delivery) State(at time.Time) (State, error) {
	return d.Lifecycle.State(at)
}
