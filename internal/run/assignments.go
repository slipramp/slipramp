package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// NextRunAssignment returns a stable assignment for a cluster. Assigned Runs
// are returned again until the controller reports that the Job was
// materialized, making a lost HTTP response safe to retry with the same
// deterministic Run identity.
func (s *Service) NextRunAssignment(
	ctx context.Context,
	clusterName string,
) (Run, error) {
	if strings.TrimSpace(clusterName) == "" {
		return Run{}, fmt.Errorf("%w: cluster name is required", ErrInvalidRequest)
	}

	assigned, err := s.runs.GetAssignedRunForCluster(ctx, clusterName)
	if err == nil {
		return assigned, nil
	}
	if !errors.Is(err, ErrNoRunAssignment) {
		return Run{}, fmt.Errorf("get assigned run: %w", err)
	}

	assigned, err = s.runs.ClaimQueuedRun(
		ctx,
		clusterName,
		s.now().UTC(),
	)
	if err != nil {
		if errors.Is(err, ErrNoRunAssignment) {
			return Run{}, ErrNoRunAssignment
		}

		return Run{}, fmt.Errorf("claim queued run assignment: %w", err)
	}

	return assigned, nil
}
