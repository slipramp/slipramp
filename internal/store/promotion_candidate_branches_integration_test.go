package store

import (
	"errors"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestPromotionCandidateBranchCleanupIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	request := fixture.request("cleanup-1")
	record, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		fixture.acceptance(t, request, createdAt.Add(2*time.Minute)),
	)
	if err != nil {
		t.Fatalf("AcceptPromotionRequest() error = %v", err)
	}

	conflictRequest := fixture.request("cleanup-2")
	conflictRequest.CandidateBranch = request.CandidateBranch
	_, err = fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		fixture.acceptance(t, conflictRequest, createdAt.Add(3*time.Minute)),
	)
	if !errors.Is(err, promotion.ErrIdentityConflict) {
		t.Fatalf("candidate branch reuse error = %v", err)
	}

	references, err := fixture.database.ListPromotionCandidateBranchReferences(
		fixture.ctx,
		fixture.repositoryID,
	)
	if err != nil || len(references) != 1 || references[0].PromotionID != record.ID {
		t.Fatalf("candidate branch references = %#v, %v", references, err)
	}

	startedAt := record.UpdatedAt.Add(time.Hour)
	cleanup := promotion.CandidateBranchCleanup{
		RepositoryID: fixture.repositoryID,
		Status:       promotion.CandidateBranchCleanupRunning,
		StartedAt:    startedAt,
	}
	if err := fixture.database.UpsertPromotionCandidateBranchCleanup(fixture.ctx, cleanup); err != nil {
		t.Fatalf("UpsertPromotionCandidateBranchCleanup() running error = %v", err)
	}

	finishedAt := startedAt.Add(time.Minute)
	cleanup.Status = promotion.CandidateBranchCleanupSucceeded
	cleanup.FinishedAt = &finishedAt
	cleanup.ObservedBranches = 3
	cleanup.DeletedBranches = 1
	cleanup.RetainedBranches = 2
	if err := fixture.database.UpsertPromotionCandidateBranchCleanup(fixture.ctx, cleanup); err != nil {
		t.Fatalf("UpsertPromotionCandidateBranchCleanup() succeeded error = %v", err)
	}

	stored, err := fixture.database.GetPromotionCandidateBranchCleanup(fixture.ctx, fixture.repositoryID)
	if err != nil || stored.Status != promotion.CandidateBranchCleanupSucceeded || stored.FinishedAt == nil ||
		!stored.FinishedAt.Equal(finishedAt) || stored.ObservedBranches != 3 ||
		stored.DeletedBranches != 1 || stored.RetainedBranches != 2 {
		t.Fatalf("candidate branch cleanup = %#v, %v", stored, err)
	}
}
