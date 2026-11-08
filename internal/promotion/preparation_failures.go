package promotion

import (
	"time"

	"github.com/google/uuid"
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
