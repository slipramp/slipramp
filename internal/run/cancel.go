package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CancelRun records cancel intent. Queued Runs finish without controller
// involvement; active Runs remain active until their controller reports the
// canceled outcome.
func (s *Service) CancelRun(ctx context.Context, request CancelRequest) (Run, error) {
	if request.RunID == uuid.Nil {
		return Run{}, fmt.Errorf("%w: Run ID is required", ErrInvalidRequest)
	}
	canceledBy, err := actorSnapshot(request.Actor)
	if err != nil {
		return Run{}, err
	}

	record, err := s.runs.CancelRun(ctx, request.RunID, s.now().UTC(), canceledBy)
	if err != nil {
		switch {
		case errors.Is(err, ErrRunNotFound), errors.Is(err, ErrRunFinished):
			return Run{}, err
		default:
			return Run{}, fmt.Errorf("cancel Run: %w", err)
		}
	}

	return record, nil
}

// NextRunToCancel returns the oldest active cancel request for a cluster. The
// Run remains available until the controller reports a terminal observation,
// making a lost command or response safe to retry.
func (s *Service) NextRunToCancel(ctx context.Context, clusterName string) (Run, error) {
	if strings.TrimSpace(clusterName) == "" {
		return Run{}, fmt.Errorf("%w: cluster name is required", ErrInvalidRequest)
	}

	record, err := s.runs.GetRunToCancelForCluster(ctx, clusterName)
	if err != nil {
		if errors.Is(err, ErrNoRunToCancel) {
			return Run{}, ErrNoRunToCancel
		}

		return Run{}, fmt.Errorf("get Run to cancel: %w", err)
	}

	return record, nil
}
