package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ClaimPromotionReconciliation(
	ctx context.Context,
	claim promotion.ReconciliationClaim,
) (promotion.Promotion, error) {
	if err := claim.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Promotion reconciliation claim: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	row, err := queries.LockNextPromotionReconciliation(
		ctx,
		storedb.LockNextPromotionReconciliationParams{
			DueAt:       databaseTime(claim.ClaimedAt),
			ClusterName: claim.ClusterName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrNoReconciliation
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("lock next Promotion reconciliation: %w", err)
	}
	current, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if current.NextReconciliationAt == nil {
		return promotion.Promotion{}, fmt.Errorf("%w: locked Promotion is not due", promotion.ErrInvalid)
	}

	nextReconciliationAt := claim.ClaimedAt.Add(claim.Interval)
	claimedRow, err := queries.ClaimPromotionReconciliation(
		ctx,
		storedb.ClaimPromotionReconciliationParams{
			NextReconciliationAt:         databaseTime(nextReconciliationAt),
			ClaimedAt:                    databaseTime(claim.ClaimedAt),
			ID:                           databaseUUID(current.ID),
			ExpectedNextReconciliationAt: databaseTime(*current.NextReconciliationAt),
		},
	)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("claim Promotion reconciliation: %w", err)
	}
	claimed, err := promotionFromRow(claimedRow)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion reconciliation claim: %w", err)
	}

	return claimed, nil
}

func (d *Database) ReportPromotionReconciliation(
	ctx context.Context,
	report promotion.ReconciliationReport,
) (promotion.Promotion, error) {
	if err := report.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Promotion reconciliation report: %w", err)
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
		return promotion.Promotion{}, fmt.Errorf("lock Promotion reconciliation: %w", err)
	}
	current, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, err
	}
	stage := current.StageSnapshot
	if stage.ClusterName != report.ClusterName {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	if current.ExternalPromotionID != report.KargoName {
		return promotion.Promotion{}, promotion.ErrReconciliationConflict
	}

	updated := current
	if current.KargoPromotionUID == nil {
		if current.Source != promotion.SourceKargo || current.KargoOutcome != nil {
			return promotion.Promotion{}, promotion.ErrReconciliationConflict
		}
		kargoUID := report.KargoUID
		updatedRow, updateErr := queries.BindKargoPromotionReconciliationIdentity(
			ctx,
			storedb.BindKargoPromotionReconciliationIdentityParams{
				KargoPromotionUid: &kargoUID,
				ObservedAt:        databaseTime(report.ObservedAt),
				ID:                databaseUUID(report.PromotionID),
			},
		)
		if updateErr != nil {
			return promotion.Promotion{}, fmt.Errorf("bind Kargo Promotion reconciliation identity: %w", updateErr)
		}
		updated, err = promotionFromRow(updatedRow)
		if err != nil {
			return promotion.Promotion{}, err
		}
	} else if *current.KargoPromotionUID != report.KargoUID {
		return promotion.Promotion{}, promotion.ErrReconciliationConflict
	}

	if report.Phase != nil {
		if updated.KargoOutcome != nil {
			if updated.KargoOutcome.Phase != *report.Phase {
				return promotion.Promotion{}, promotion.ErrReconciliationConflict
			}
		} else {
			phase := string(*report.Phase)
			updatedRow, updateErr := queries.RecordTerminalKargoPromotionOutcome(
				ctx,
				storedb.RecordTerminalKargoPromotionOutcomeParams{
					KargoOutcomePhase:      &phase,
					KargoOutcomeObservedAt: databaseTime(report.ObservedAt),
					KargoOutcomeDiagnostic: report.Diagnostic,
					ID:                     databaseUUID(report.PromotionID),
				},
			)
			if updateErr != nil {
				return promotion.Promotion{}, fmt.Errorf("record terminal Kargo Promotion outcome: %w", updateErr)
			}
			updated, err = promotionFromRow(updatedRow)
			if err != nil {
				return promotion.Promotion{}, err
			}
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion reconciliation report: %w", err)
	}

	return updated, nil
}
