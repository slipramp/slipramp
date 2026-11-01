package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ListPromotionCandidateBranchReferences(
	ctx context.Context,
	repositoryID uuid.UUID,
) ([]promotion.CandidateBranchReference, error) {
	rows, err := d.queries.ListPromotionCandidateBranchReferences(ctx, databaseUUID(repositoryID))
	if err != nil {
		return nil, fmt.Errorf("list Promotion candidate branch references: %w", err)
	}

	references := make([]promotion.CandidateBranchReference, 0, len(rows))
	for _, row := range rows {
		references = append(references, promotion.CandidateBranchReference{
			PromotionID:       uuid.UUID(row.PromotionID.Bytes),
			Integration:       row.Integration,
			RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
			CandidateBranch:   textValue(row.CandidateBranch),
			CandidateRevision: textValue(row.CandidateRevision),
			Status:            promotion.PromotionStatus(row.Status),
			UpdatedAt:         row.UpdatedAt.Time.UTC(),
			KargoOutcomeAt:    applicationTime(row.KargoOutcomeObservedAt),
		})
	}

	return references, nil
}

func (d *Database) UpsertPromotionCandidateBranchCleanup(
	ctx context.Context,
	cleanup promotion.CandidateBranchCleanup,
) error {
	err := d.queries.UpsertPromotionCandidateBranchCleanup(
		ctx,
		storedb.UpsertPromotionCandidateBranchCleanupParams{
			RepositoryID:     databaseUUID(cleanup.RepositoryID),
			Status:           string(cleanup.Status),
			StartedAt:        databaseTime(cleanup.StartedAt),
			FinishedAt:       optionalDatabaseTime(cleanup.FinishedAt),
			ObservedBranches: int32(cleanup.ObservedBranches),
			DeletedBranches:  int32(cleanup.DeletedBranches),
			RetainedBranches: int32(cleanup.RetainedBranches),
			Error:            nullableText(cleanup.Error),
		},
	)
	if err != nil {
		return fmt.Errorf("upsert Promotion candidate branch cleanup: %w", err)
	}

	return nil
}

func (d *Database) GetPromotionCandidateBranchCleanup(
	ctx context.Context,
	repositoryID uuid.UUID,
) (promotion.CandidateBranchCleanup, error) {
	row, err := d.queries.GetPromotionCandidateBranchCleanup(ctx, databaseUUID(repositoryID))
	if err != nil {
		return promotion.CandidateBranchCleanup{}, fmt.Errorf("get Promotion candidate branch cleanup: %w", err)
	}

	return promotion.CandidateBranchCleanup{
		RepositoryID:     uuid.UUID(row.RepositoryID.Bytes),
		Status:           promotion.CandidateBranchCleanupStatus(row.Status),
		StartedAt:        row.StartedAt.Time.UTC(),
		FinishedAt:       applicationTime(row.FinishedAt),
		ObservedBranches: int(row.ObservedBranches),
		DeletedBranches:  int(row.DeletedBranches),
		RetainedBranches: int(row.RetainedBranches),
		Error:            textValue(row.Error),
	}, nil
}
