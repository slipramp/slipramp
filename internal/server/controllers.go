package server

import (
	"context"
	"time"

	"github.com/slipramp/slipramp/internal/api/handler"
	"github.com/slipramp/slipramp/internal/cluster"
)

const clusterConnectionWindow = time.Minute

type clusterObservationStore interface {
	RecordClusterObservation(
		ctx context.Context,
		clusterName string,
		observedAt time.Time,
	) error
	ListClusterObservations(ctx context.Context) ([]cluster.Observation, error)
}

type controllerOperations interface {
	handler.ControllerWorkWaiter
	handler.ClusterLister
}

type controllerService struct {
	clusters clusterObservationStore
	now      func() time.Time
}

func (s *controllerService) ListClusters(
	ctx context.Context,
) ([]cluster.Status, error) {
	observations, err := s.clusters.ListClusterObservations(ctx)
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	statuses := make([]cluster.Status, 0, len(observations))
	for _, observation := range observations {
		connectionState := cluster.ConnectionStateUnreachable
		if now.Sub(observation.LastSeenAt) <= clusterConnectionWindow {
			connectionState = cluster.ConnectionStateConnected
		}

		statuses = append(statuses, cluster.Status{
			Observation:     observation,
			ConnectionState: connectionState,
		})
	}

	return statuses, nil
}

func newControllerService(clusters clusterObservationStore) *controllerService {
	return &controllerService{
		clusters: clusters,
		now:      time.Now,
	}
}

func (s *controllerService) WaitForControllerWork(
	ctx context.Context,
	clusterName string,
	wait time.Duration,
) error {
	observedAt := s.now().UTC()
	if err := s.clusters.RecordClusterObservation(
		ctx,
		clusterName,
		observedAt,
	); err != nil {
		return err
	}

	if wait == 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
