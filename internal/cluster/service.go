package cluster

import (
	"context"
	"time"
)

const connectionWindow = time.Minute

type Store interface {
	RecordClusterObservation(
		ctx context.Context,
		clusterName string,
		observedAt time.Time,
	) error
	ListClusterObservations(ctx context.Context) ([]Observation, error)
}

type Service struct {
	clusters Store
	now      func() time.Time
}

func NewService(clusters Store) *Service {
	return &Service{
		clusters: clusters,
		now:      time.Now,
	}
}

func (s *Service) ListClusters(ctx context.Context) ([]Status, error) {
	observations, err := s.clusters.ListClusterObservations(ctx)
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	statuses := make([]Status, 0, len(observations))
	for _, observation := range observations {
		connectionState := ConnectionStateUnreachable
		if now.Sub(observation.LastSeenAt) <= connectionWindow {
			connectionState = ConnectionStateConnected
		}

		statuses = append(statuses, Status{
			Observation:     observation,
			ConnectionState: connectionState,
		})
	}

	return statuses, nil
}

func (s *Service) WaitForControllerWork(
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
