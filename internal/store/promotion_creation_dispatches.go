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

		if current.DispatchAttemptCount >= len(claim.RetryDelays) {
			message := claim.ExhaustedMessage
			if _, exhaustErr := queries.ExhaustPromotionCreationDispatch(
				ctx,
				storedb.ExhaustPromotionCreationDispatchParams{
					Message:  &message,
					FailedAt: databaseTime(claim.ClaimedAt),
					ID:       databaseUUID(current.ID),
				},
			); exhaustErr != nil {
				return promotion.Promotion{}, fmt.Errorf("exhaust Promotion creation dispatch: %w", exhaustErr)
			}
			exhausted = true
			continue
		}

		nextDispatchAt := claim.ClaimedAt.Add(claim.RetryDelays[current.DispatchAttemptCount])
		claimedRow, claimErr := queries.ClaimPromotionCreationDispatch(
			ctx,
			storedb.ClaimPromotionCreationDispatchParams{
				NextDispatchAt:       databaseTime(nextDispatchAt),
				ClaimedAt:            databaseTime(claim.ClaimedAt),
				ID:                   databaseUUID(current.ID),
				ExpectedAttemptCount: int32(current.DispatchAttemptCount),
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

	row, err := queries.LockPromotionCreationDispatch(ctx, databaseUUID(report.PromotionID))
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
	var stage promotion.PromotionStageSnapshot
	if err := json.Unmarshal(current.StageSnapshot, &stage); err != nil {
		return promotion.Promotion{}, fmt.Errorf("decode Promotion creation Stage snapshot: %w", err)
	}
	if current.Source != promotion.SourceSlipramp || stage.ClusterName != report.ClusterName {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	if report.Attempt > current.DispatchAttemptCount {
		return promotion.Promotion{}, promotion.ErrCreationDispatchConflict
	}

	updated := current
	switch report.Result {
	case promotion.CreationDispatchCreated:
		if current.KargoPromotionUID != nil && *current.KargoPromotionUID != *report.KargoUID {
			return promotion.Promotion{}, promotion.ErrCreationDispatchConflict
		}
		if current.KargoPromotionUID == nil {
			updatedRow, updateErr := queries.RecordCreatedPromotionDispatch(
				ctx,
				storedb.RecordCreatedPromotionDispatchParams{
					DispatchedAt:      databaseTime(report.ReportedAt),
					KargoPromotionUid: report.KargoUID,
					ID:                databaseUUID(report.PromotionID),
				},
			)
			if updateErr != nil {
				return promotion.Promotion{}, fmt.Errorf("record created Kargo Promotion: %w", updateErr)
			}
			updated, err = promotionFromRow(updatedRow)
		}
	case promotion.CreationDispatchRetryable:
		if report.Attempt == current.DispatchAttemptCount && current.Status == promotion.StatusCreating &&
			current.KargoPromotionUID == nil {
			updatedRow, updateErr := queries.RecordRetryablePromotionDispatch(
				ctx,
				storedb.RecordRetryablePromotionDispatchParams{
					Message:    report.Diagnostic,
					ReportedAt: databaseTime(report.ReportedAt),
					ID:         databaseUUID(report.PromotionID),
				},
			)
			if updateErr != nil {
				return promotion.Promotion{}, fmt.Errorf("record retryable Promotion creation dispatch: %w", updateErr)
			}
			updated, err = promotionFromRow(updatedRow)
		}
	case promotion.CreationDispatchFailed:
		if report.Attempt == current.DispatchAttemptCount && current.Status == promotion.StatusCreating &&
			current.KargoPromotionUID == nil {
			updatedRow, updateErr := queries.RecordFailedPromotionDispatch(
				ctx,
				storedb.RecordFailedPromotionDispatchParams{
					Message:    report.Diagnostic,
					ReportedAt: databaseTime(report.ReportedAt),
					ID:         databaseUUID(report.PromotionID),
				},
			)
			if updateErr != nil {
				return promotion.Promotion{}, fmt.Errorf("record failed Promotion creation dispatch: %w", updateErr)
			}
			updated, err = promotionFromRow(updatedRow)
		}
	}
	if err != nil {
		return promotion.Promotion{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion creation dispatch report: %w", err)
	}

	return updated, nil
}
