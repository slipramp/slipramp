package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// NextControllerWork returns stable work for a cluster. Preparing Runs are
// returned again until the controller reports another state, making a lost
// HTTP response safe to retry with the same deterministic Run identity.
func (s *Service) NextControllerWork(
	ctx context.Context,
	clusterName string,
) (Run, error) {
	if strings.TrimSpace(clusterName) == "" {
		return Run{}, fmt.Errorf("%w: cluster name is required", ErrInvalidRequest)
	}

	assigned, err := s.runs.GetPreparingRunForCluster(ctx, clusterName)
	if err == nil {
		return assigned, nil
	}
	if !errors.Is(err, ErrNoControllerWork) {
		return Run{}, fmt.Errorf("get preparing controller work: %w", err)
	}

	assigned, err = s.runs.ClaimQueuedRun(
		ctx,
		clusterName,
		s.now().UTC(),
	)
	if err != nil {
		if errors.Is(err, ErrNoControllerWork) {
			return Run{}, ErrNoControllerWork
		}

		return Run{}, fmt.Errorf("claim queued controller work: %w", err)
	}

	return assigned, nil
}
