package catalog

import (
	"time"

	"github.com/google/uuid"
)

const (
	DefaultTargetListLimit    = 25
	MaximumTargetListLimit    = 100
	MaximumTargetListOffset   = 2_147_483_647
	MaximumTargetSearchLength = 200
)

type TargetSummary struct {
	ID                 uuid.UUID
	RepositoryID       uuid.UUID
	RepositoryName     string
	CatalogRevisionID  uuid.UUID
	CatalogCommitSHA   string
	DesiredCommitSHA   string
	LastAttemptedSync  *SyncRunCheckpoint
	LastSuccessfulSync *SyncRunCheckpoint
	ArchivedAt         *time.Time
	Target             Target
}

type SyncRunCheckpoint struct {
	RunID                   uuid.UUID
	RelevanceCommitSHA      *string
	Phase                   string
	Outcome                 *string
	CreatedAt               time.Time
	AssignedAt              time.Time
	PreparingAt             *time.Time
	StartedAt               *time.Time
	FinishedAt              *time.Time
	CancellationRequestedAt *time.Time
}

type ReconciliationComparison string

const (
	ReconciliationComparisonMatched   ReconciliationComparison = "matched"
	ReconciliationComparisonDifferent ReconciliationComparison = "different"
	ReconciliationComparisonUnknown   ReconciliationComparison = "unknown"
)

func (s TargetSummary) ReconciliationComparison() ReconciliationComparison {
	return CompareReconciliation(
		s.Target,
		s.DesiredCommitSHA,
		s.ArchivedAt != nil,
		s.LastSuccessfulSync,
	)
}

func CompareReconciliation(
	target Target,
	desiredCommitSHA string,
	archived bool,
	lastSuccessfulSync *SyncRunCheckpoint,
) ReconciliationComparison {
	if archived {
		return ReconciliationComparisonUnknown
	}
	if _, found := target.Spec.Actions["sync"]; !found {
		return ReconciliationComparisonUnknown
	}
	if lastSuccessfulSync == nil || lastSuccessfulSync.RelevanceCommitSHA == nil {
		return ReconciliationComparisonUnknown
	}
	if *lastSuccessfulSync.RelevanceCommitSHA == desiredCommitSHA {
		return ReconciliationComparisonMatched
	}

	return ReconciliationComparisonDifferent
}

type TargetList struct {
	Items []TargetSummary
	Total int64
}

type TargetListFilter struct {
	RepositoryID *uuid.UUID
	Query        string
	Runtime      string
	Placement    string
	Limit        int32
	Offset       int32
}
