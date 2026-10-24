package catalog

import "github.com/google/uuid"

const (
	DefaultTargetListLimit    = 25
	MaximumTargetListLimit    = 100
	MaximumTargetListOffset   = 2_147_483_647
	MaximumTargetSearchLength = 200
)

type TargetSummary struct {
	RepositoryID      uuid.UUID
	RepositoryName    string
	CatalogRevisionID uuid.UUID
	CatalogCommitSHA  string
	DesiredCommitSHA  string
	Target            Target
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
