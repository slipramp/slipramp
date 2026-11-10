package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ClaimPromotionCreation(
	ctx context.Context,
	claim promotion.CreationDispatchClaim,
) (promotion.Promotion, error) {
	if err := claim.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Promotion creation dispatch claim: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)
	exhausted := false

	for {
		row, lockErr := queries.LockNextPromotionCreationDispatch(
			ctx,
			storedb.LockNextPromotionCreationDispatchParams{
				DueAt:       databaseTime(claim.ClaimedAt),
				ClusterName: claim.ClusterName,
			},
		)
		if errors.Is(lockErr, pgx.ErrNoRows) {
			if exhausted {
				if commitErr := transaction.Commit(ctx); commitErr != nil {
					return promotion.Promotion{}, fmt.Errorf(
						"commit exhausted Promotion creation dispatches: %w",
						commitErr,
					)
				}
			}
			return promotion.Promotion{}, promotion.ErrNoCreationDispatch
		}
		if lockErr != nil {
			return promotion.Promotion{}, fmt.Errorf("lock next Promotion creation dispatch: %w", lockErr)
		}
		current, decodeErr := promotionFromRow(row)
		if decodeErr != nil {
			return promotion.Promotion{}, decodeErr
		}

		transition, transitionErr := promotion.TransitionCreationDispatchClaim(
			current.Source,
			current.Lifecycle,
			current.CreationDispatch,
			claim,
		)
		if transitionErr != nil {
			return promotion.Promotion{}, transitionErr
		}
		if transition.Exhausted {
			diagnostic := transition.State.Diagnostic
			if _, exhaustErr := queries.ExhaustPromotionCreationDispatch(
				ctx,
				storedb.ExhaustPromotionCreationDispatchParams{
					Status:     string(transition.Lifecycle.State),
					FailedAt:   databaseTime(claim.ClaimedAt),
					Diagnostic: diagnostic,
					ID:         databaseUUID(current.ID),
				},
			); exhaustErr != nil {
				return promotion.Promotion{}, fmt.Errorf("exhaust Promotion creation dispatch: %w", exhaustErr)
			}
			exhausted = true
			continue
		}

		claimedRow, claimErr := queries.ClaimPromotionCreationDispatch(
			ctx,
			storedb.ClaimPromotionCreationDispatchParams{
				NextDispatchAt:       databaseTime(*transition.State.NextAttemptAt),
				ClaimedAt:            databaseTime(claim.ClaimedAt),
				ID:                   databaseUUID(current.ID),
				ExpectedAttemptCount: int32(current.CreationDispatch.AttemptCount),
			},
		)
		if claimErr != nil {
			return promotion.Promotion{}, fmt.Errorf("claim Promotion creation dispatch: %w", claimErr)
		}
		claimed, decodeErr := promotionFromRow(claimedRow)
		if decodeErr != nil {
			return promotion.Promotion{}, decodeErr
		}
		if commitErr := transaction.Commit(ctx); commitErr != nil {
			return promotion.Promotion{}, fmt.Errorf("commit Promotion creation dispatch claim: %w", commitErr)
		}

		return claimed, nil
	}
}

func (d *Database) ReportPromotionCreation(
	ctx context.Context,
	report promotion.CreationDispatchReport,
) (promotion.Promotion, error) {
	if err := report.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Promotion creation dispatch report: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	row, err := queries.LockPromotion(ctx, databaseUUID(report.PromotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("lock Promotion creation dispatch: %w", err)
	}
	current, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, err
	}
	stage := current.StageSnapshot
	if current.Source != promotion.SourceSlipramp || stage.ClusterName != report.ClusterName {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	transitioned, changed, err := promotion.TransitionCreationDispatchReport(current, report)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if !changed {
		if err := transaction.Commit(ctx); err != nil {
			return promotion.Promotion{}, fmt.Errorf("commit repeated Promotion creation dispatch report: %w", err)
		}
		return current, nil
	}

	updatedRow, err := queries.UpdatePromotionCreationDispatch(
		ctx,
		storedb.UpdatePromotionCreationDispatchParams{
			ExternalPromotionID:       transitioned.ExternalPromotionID,
			Status:                    string(transitioned.Lifecycle.State),
			Message:                   transitioned.Message,
			NextDispatchAt:            optionalDatabaseTime(transitioned.CreationDispatch.NextAttemptAt),
			DispatchedAt:              optionalDatabaseTime(transitioned.CreationDispatch.DispatchedAt),
			DispatchFailedAt:          optionalDatabaseTime(transitioned.CreationDispatch.FailedAt),
			DispatchDiagnostic:        transitioned.CreationDispatch.Diagnostic,
			KargoPromotionUid:         transitioned.KargoExecution.PromotionUID,
			NextReconciliationAt:      optionalDatabaseTime(transitioned.KargoExecution.NextReconciliationAt),
			UpdatedAt:                 databaseTime(transitioned.UpdatedAt),
			ID:                        databaseUUID(report.PromotionID),
			ExpectedStatus:            string(current.Lifecycle.State),
			ExpectedAttemptCount:      int32(current.CreationDispatch.AttemptCount),
			ExpectedKargoPromotionUid: current.KargoExecution.PromotionUID,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrCreationDispatchConflict
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("update Promotion creation dispatch: %w", err)
	}
	updated, err := promotionFromRow(updatedRow)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion creation dispatch report: %w", err)
	}

	return updated, nil
}
