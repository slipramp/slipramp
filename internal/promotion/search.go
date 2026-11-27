package promotion

import "github.com/google/uuid"

type InventorySearchResult struct {
	Name        string
	StageID     uuid.UUID
	StageName   string
	Integration string
	ClusterName string
	Project     string
}

type InventorySearchResults struct {
	Stages    []InventorySearchResult
	Pipelines []InventorySearchResult
}
