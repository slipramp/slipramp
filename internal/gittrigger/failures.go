package gittrigger

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/run"
)

// The single-Server dispatcher retries Run creation up to eight failures,
// starting at five seconds and doubling to a five-minute cap. PostgreSQL owns
// the count and due time; restarts never reset this policy. Execution is never retried.
const (
	FailureLimit      = 8
	InitialRetryDelay = 5 * time.Second
	MaximumRetryDelay = 5 * time.Minute
)

type FailureState struct {
	Count    int
	Terminal bool
}

// FailureDiagnostic maps Run-creation errors to fixed, non-sensitive text.
// Wrapped errors can contain database URLs, credentials, parameters, or catalog contents.
func FailureDiagnostic(err error) string {
	for _, known := range []error{
		run.ErrInvalidRequest, run.ErrInvalidParameters, run.ErrInvalidInputs,
		run.ErrRepositoryNotFound, run.ErrCatalogNotFound, run.ErrTargetNotFound,
		run.ErrActionNotFound, run.ErrRuntimeNotFound, run.ErrPlacementNotFound,
		run.ErrIdempotencyConflict,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "Run creation failed; check Server configuration and dependency health."
}

type FailedDelivery struct {
	ID                uuid.UUID
	TargetID          uuid.UUID
	RepositoryID      uuid.UUID
	RepositoryName    string
	CatalogRevisionID uuid.UUID
	CommitSHA         string
	TargetName        string
	ActionName        string
	TriggerName       string
	FailureCount      int
	Diagnostic        string
	FailedAt          time.Time
}

type FailedDeliveryPage struct {
	Items []FailedDelivery
	Total int
}
