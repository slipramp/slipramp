package preparation

import (
	"errors"
	"time"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

// preparationFailure classifies only known, fixed errors. Wrapped errors can
// include credentials, parameters, artifact contents, or database addresses.
func preparationFailure(record promotion.Promotion, at time.Time, err error) promotion.PreparationFailure {
	failure := promotion.PreparationFailure{
		PromotionID:      record.ID,
		PreparationRunID: record.Preparation.RunID,
		FailedAt:         at,
		Diagnostic:       "Promotion preparation could not complete; check Server configuration and dependency health.",
	}
	for _, known := range []error{
		promotion.ErrInvalid, promotion.ErrIntegrationNotFound, promotion.ErrInvalidPreparationArtifact,
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
