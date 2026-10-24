package promotion

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/run"
)

// Preparation retries only dispatch an idempotent Run request or inspect its
// completed outputs. They never retry the infrastructure command.
const (
	PreparationFailureLimit      = 8
	PreparationInitialRetryDelay = 5 * time.Second
	PreparationMaximumRetryDelay = 5 * time.Minute
)

type PreparationFailure struct {
	PromotionID      uuid.UUID
	PreparationRunID *uuid.UUID
	FailedAt         time.Time
	Diagnostic       string
	Permanent        bool
}

type PreparationFailureState struct {
	Count    int
	Terminal bool
}

// preparationFailure classifies only known, fixed errors. Wrapped errors can
// include credentials, parameters, artifact contents, or database addresses.
func preparationFailure(record Promotion, at time.Time, err error) PreparationFailure {
	failure := PreparationFailure{
		PromotionID:      record.ID,
		PreparationRunID: record.PreparationRunID,
		FailedAt:         at,
		Diagnostic:       "Promotion preparation could not complete; check Server configuration and dependency health.",
	}
	for _, known := range []error{
		ErrInvalid, ErrIntegrationNotFound, ErrInvalidPreparationArtifact,
		run.ErrInvalidRequest, run.ErrInvalidParameters, run.ErrInvalidInputs,
		run.ErrTargetNotFound, run.ErrActionNotFound, run.ErrRuntimeNotFound,
		run.ErrPlacementNotFound, run.ErrIdempotencyConflict, run.ErrRunNotFound,
	} {
		if errors.Is(err, known) {
			failure.Permanent = true
			failure.Diagnostic = "Promotion preparation cannot continue: " + known.Error() + "."
			break
		}
	}

	return failure
}
