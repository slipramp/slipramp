package run

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

type HistoryStore interface {
	GetRun(context.Context, uuid.UUID) (Run, error)
	ListRuns(context.Context, RunListFilter) (RunList, error)
}

func (s *Service) GetRun(ctx context.Context, runID uuid.UUID) (Run, error) {
	if runID == uuid.Nil {
		return Run{}, fmt.Errorf("%w: Run ID is required", ErrInvalidRequest)
	}

	record, err := s.history.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}

	return record, nil
}

func (s *Service) ListRuns(ctx context.Context, filter RunListFilter) (RunList, error) {
	if filter.RepositoryID != nil && *filter.RepositoryID == uuid.Nil {
		return RunList{}, fmt.Errorf("%w: repository ID must not be empty", ErrInvalidRequest)
	}
	if filter.TargetID != nil && *filter.TargetID == uuid.Nil {
		return RunList{}, fmt.Errorf("%w: Target ID must not be empty", ErrInvalidRequest)
	}
	if filter.InputArtifactID != nil && *filter.InputArtifactID == uuid.Nil {
		return RunList{}, fmt.Errorf("%w: input artifact ID must not be empty", ErrInvalidRequest)
	}
	if filter.CommitSHA != "" && !repository.ValidCommitSHA(filter.CommitSHA) {
		return RunList{}, fmt.Errorf("%w: commit SHA must be a lowercase full Git object ID", ErrInvalidRequest)
	}
	if filter.CommitSHA != "" && filter.RepositoryID == nil {
		return RunList{}, fmt.Errorf("%w: commit filter requires a repository ID", ErrInvalidRequest)
	}
	if filter.ClusterName != strings.TrimSpace(filter.ClusterName) {
		return RunList{}, fmt.Errorf("%w: cluster name must not have surrounding whitespace", ErrInvalidRequest)
	}
	if filter.Phase != "" && !validPhase(filter.Phase) {
		return RunList{}, fmt.Errorf("%w: phase is invalid", ErrInvalidRequest)
	}
	if filter.ReviewState != "" && !validReviewState(filter.ReviewState) {
		return RunList{}, fmt.Errorf("%w: review state is invalid", ErrInvalidRequest)
	}
	if filter.Limit == 0 {
		filter.Limit = DefaultRunListLimit
	}
	if filter.Limit < 1 || filter.Limit > MaximumRunListLimit {
		return RunList{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidRequest, MaximumRunListLimit)
	}
	if filter.Offset < 0 || filter.Offset > MaximumRunListOffset {
		return RunList{}, fmt.Errorf("%w: offset must be between 0 and %d", ErrInvalidRequest, MaximumRunListOffset)
	}

	records, err := s.history.ListRuns(ctx, filter)
	if err != nil {
		return RunList{}, fmt.Errorf("list Runs: %w", err)
	}

	return records, nil
}

func validReviewState(state ReviewState) bool {
	switch state {
	case ReviewStateNotRequired, ReviewStatePending, ReviewStateApproved, ReviewStateRejected:
		return true
	default:
		return false
	}
}

func validPhase(phase Phase) bool {
	switch phase {
	case PhaseQueued, PhaseAssigned, PhasePreparing, PhaseRunning, PhaseFinished:
		return true
	default:
		return false
	}
}
