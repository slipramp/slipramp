package run

import (
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/placement"
)

type Service struct {
	creation           CreationStore
	history            HistoryStore
	scheduling         SchedulingStore
	assignments        AssignmentStore
	cancellations      CancellationStore
	reviews            ReviewStore
	observations       ObservationStore
	controllerCommands ControllerCommandNotifier
	gateEvaluations    GateEvaluationNotifier
	gateProcessID      uuid.UUID
	repositories       RepositoryService
	catalogs           CatalogService
	placements         placement.Set
	notifications      CreationNotifications
	newID              func() uuid.UUID
	now                func() time.Time
}

type Dependencies struct {
	Creation           CreationStore
	History            HistoryStore
	Scheduling         SchedulingStore
	Assignments        AssignmentStore
	Cancellations      CancellationStore
	Reviews            ReviewStore
	Observations       ObservationStore
	ControllerCommands ControllerCommandNotifier
	GateEvaluations    GateEvaluationNotifier
	GateProcessID      uuid.UUID
	Repositories       RepositoryService
	Catalogs           CatalogService
	Placements         placement.Set
	Notifications      CreationNotifications
}

func NewService(dependencies Dependencies) *Service {
	return &Service{
		creation:           dependencies.Creation,
		history:            dependencies.History,
		scheduling:         dependencies.Scheduling,
		assignments:        dependencies.Assignments,
		cancellations:      dependencies.Cancellations,
		reviews:            dependencies.Reviews,
		observations:       dependencies.Observations,
		controllerCommands: dependencies.ControllerCommands,
		gateEvaluations:    dependencies.GateEvaluations,
		gateProcessID:      dependencies.GateProcessID,
		repositories:       dependencies.Repositories,
		catalogs:           dependencies.Catalogs,
		placements:         dependencies.Placements,
		notifications:      dependencies.Notifications,
		newID:              uuid.New,
		now:                time.Now,
	}
}

type ControllerCommandNotifier interface {
	Notify(clusterName string)
}

type GateEvaluationNotifier interface {
	Notify()
}

func (s *Service) notifyController(record Run) {
	s.notifyControllerCluster(record.ClusterName)
}

func (s *Service) notifyControllerCluster(clusterName string) {
	if s.controllerCommands != nil && clusterName != "" {
		s.controllerCommands.Notify(clusterName)
	}
}

func (s *Service) notifyGateEvaluation(record Run) {
	if s.gateEvaluations != nil && len(record.Snapshot.Action.Gates) > 0 {
		s.gateEvaluations.Notify()
	}
}
