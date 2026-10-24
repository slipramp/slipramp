package inbox

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

// ActiveRun contains only the lifecycle facts used by the Inbox.
type ActiveRun struct {
	ID          uuid.UUID
	TargetName  string
	ActionName  string
	ClusterName string
	CreatedAt   time.Time
	Status      run.ObservationStatus
}

// PromotionState contains only the current Promotion facts used by the Inbox.
type PromotionState struct {
	ID              uuid.UUID
	StageSnapshot   json.RawMessage
	CandidateBranch string
	TargetBranch    string
	Status          promotion.PromotionStatus
	Message         *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
