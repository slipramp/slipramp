package catalog

import (
	"time"

	"github.com/google/uuid"
)

// Revision is an immutable catalog accepted from one exact repository commit.
type Revision struct {
	ID           uuid.UUID
	CommitSHA    string
	CreatedAt    time.Time
	Catalog      Catalog
	TargetStates map[string]TargetState
}

// TargetState is the mutable projection associated with a target definition.
type TargetState struct {
	DesiredCommitSHA string
}
