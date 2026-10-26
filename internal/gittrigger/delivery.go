// Package gittrigger owns durable delivery of Runs caused by accepted Git
// repository heads.
package gittrigger

import (
	"errors"

	"github.com/google/uuid"
)

var ErrNoEligibleDelivery = errors.New("no eligible Git-trigger delivery")

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
}
