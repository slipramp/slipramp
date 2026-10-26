package run

import (
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/placement"
)

type Service struct {
	creation      CreationStore
	history       HistoryStore
	assignments   AssignmentStore
	cancellations CancellationStore
	reviews       ReviewStore
	observations  ObservationStore
	repositories  RepositoryService
	catalogs      CatalogService
	placements    placement.Set
	notifications CreationNotifications
	newID         func() uuid.UUID
	now           func() time.Time
}

type Dependencies struct {
	Creation      CreationStore
	History       HistoryStore
	Assignments   AssignmentStore
	Cancellations CancellationStore
	Reviews       ReviewStore
	Observations  ObservationStore
	Repositories  RepositoryService
	Catalogs      CatalogService
	Placements    placement.Set
	Notifications CreationNotifications
}

func NewService(dependencies Dependencies) *Service {
	return &Service{
		creation:      dependencies.Creation,
		history:       dependencies.History,
		assignments:   dependencies.Assignments,
		cancellations: dependencies.Cancellations,
		reviews:       dependencies.Reviews,
		observations:  dependencies.Observations,
		repositories:  dependencies.Repositories,
		catalogs:      dependencies.Catalogs,
		placements:    dependencies.Placements,
		notifications: dependencies.Notifications,
		newID:         uuid.New,
		now:           time.Now,
	}
}
