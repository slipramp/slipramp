package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ClaimPromotionCreationReconciliation(
	ctx context.Context,
	claim promotion.CreationReconciliationClaim,
) (promotion.Promotion, error) {
	if err := claim.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Promotion creation reconciliation claim: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	row, err := queries.LockNextPromotionCreationReconciliation(
		ctx,
		storedb.LockNextPromotionCreationReconciliationParams{
			DueAt:       databaseTime(claim.ClaimedAt),
			ClusterName: claim.ClusterName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrNoCreationReconciliation
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("lock next Promotion creation reconciliation: %w", err)
	}
	current, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if current.NextReconciliationAt == nil {
		return promotion.Promotion{}, fmt.Errorf("%w: locked Promotion is not due", promotion.ErrInvalid)
	}

	nextReconciliationAt := claim.ClaimedAt.Add(claim.Interval)
	claimedRow, err := queries.ClaimPromotionCreationReconciliation(
		ctx,
		storedb.ClaimPromotionCreationReconciliationParams{
			NextReconciliationAt:         databaseTime(nextReconciliationAt),
			ClaimedAt:                    databaseTime(claim.ClaimedAt),
			ID:                           databaseUUID(current.ID),
			ExpectedNextReconciliationAt: databaseTime(*current.NextReconciliationAt),
		},
	)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("claim Promotion creation reconciliation: %w", err)
	}
	claimed, err := promotionFromRow(claimedRow)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion creation reconciliation claim: %w", err)
	}

	return claimed, nil
}

func (d *Database) ReportPromotionCreationReconciliation(
	ctx context.Context,
	report promotion.CreationReconciliationReport,
) (promotion.Promotion, error) {
	if err := report.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Promotion creation reconciliation report: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	row, err := queries.LockPromotionCreation(ctx, databaseUUID(report.PromotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("lock Promotion creation reconciliation: %w", err)
	}
	current, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, err
	}
	var stage promotion.PromotionStageSnapshot
	if err := json.Unmarshal(current.StageSnapshot, &stage); err != nil {
		return promotion.Promotion{}, fmt.Errorf("decode Promotion reconciliation Stage snapshot: %w", err)
	}
	if current.Source != promotion.SourceSlipramp || stage.ClusterName != report.ClusterName {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	if current.KargoPromotionUID == nil || current.ExternalPromotionID != report.KargoName ||
		*current.KargoPromotionUID != report.KargoUID {
		return promotion.Promotion{}, promotion.ErrCreationReconciliationConflict
	}

	updated := current
	if current.Status == promotion.StatusCreating && current.RequestDigest == (promotion.RequestDigest{}) {
		updatedRow, updateErr := queries.RecordTerminalPromotionCreationReconciliation(
			ctx,
			storedb.RecordTerminalPromotionCreationReconciliationParams{
				Message:    &report.Diagnostic,
				ReportedAt: databaseTime(report.ReportedAt),
				ID:         databaseUUID(report.PromotionID),
			},
		)
		if updateErr != nil {
			return promotion.Promotion{}, fmt.Errorf("record terminal Promotion creation reconciliation: %w", updateErr)
		}
		updated, err = promotionFromRow(updatedRow)
		if err != nil {
			return promotion.Promotion{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion creation reconciliation report: %w", err)
	}

	return updated, nil
}
