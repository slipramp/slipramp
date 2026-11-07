package store

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	nextExecution, err := promotion.TransitionReconciliationClaim(
		current.Source,
		current.Lifecycle,
		current.KargoExecution,
		claim,
	)
	if err != nil {
		return promotion.Promotion{}, err
	}

	claimedRow, err := queries.ClaimPromotionReconciliation(
		ctx,
		storedb.ClaimPromotionReconciliationParams{
			NextReconciliationAt:         databaseTime(*nextExecution.NextReconciliationAt),
			ClaimedAt:                    databaseTime(claim.ClaimedAt),
			ID:                           databaseUUID(current.ID),
			ExpectedNextReconciliationAt: databaseTime(*current.KargoExecution.NextReconciliationAt),
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

	row, err := queries.LockPromotion(ctx, databaseUUID(report.PromotionID))
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
	transition, err := promotion.TransitionReconciliationReport(current, report)
	if err != nil {
		return promotion.Promotion{}, err
	}

	if !transition.IdentityBound && !transition.OutcomeRecorded {
		if err := transaction.Commit(ctx); err != nil {
			return promotion.Promotion{}, fmt.Errorf("commit repeated Promotion reconciliation report: %w", err)
		}
		return current, nil
	}

	var outcomePhase *string
	var outcomeObservedAt *time.Time
	var outcomeDiagnostic *string
	if transition.Promotion.KargoExecution.Outcome != nil {
		phase := string(transition.Promotion.KargoExecution.Outcome.Phase)
		outcomePhase = &phase
		outcomeObservedAt = &transition.Promotion.KargoExecution.Outcome.ObservedAt
		outcomeDiagnostic = transition.Promotion.KargoExecution.Outcome.Diagnostic
	}
	var expectedOutcomePhase *string
	if current.KargoExecution.Outcome != nil {
		phase := string(current.KargoExecution.Outcome.Phase)
		expectedOutcomePhase = &phase
	}
	updatedRow, err := queries.UpdatePromotionReconciliation(
		ctx,
		storedb.UpdatePromotionReconciliationParams{
			Status:                    string(transition.Promotion.Lifecycle.State),
			Message:                   transition.Promotion.Message,
			KargoPromotionUid:         transition.Promotion.KargoExecution.PromotionUID,
			NextReconciliationAt:      optionalDatabaseTime(transition.Promotion.KargoExecution.NextReconciliationAt),
			KargoOutcomePhase:         outcomePhase,
			KargoOutcomeObservedAt:    optionalDatabaseTime(outcomeObservedAt),
			KargoOutcomeDiagnostic:    outcomeDiagnostic,
			UpdatedAt:                 databaseTime(transition.Promotion.UpdatedAt),
			ID:                        databaseUUID(report.PromotionID),
			ExpectedStatus:            string(current.Lifecycle.State),
			ExpectedKargoPromotionUid: current.KargoExecution.PromotionUID,
			ExpectedKargoOutcomePhase: expectedOutcomePhase,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrReconciliationConflict
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("update Promotion reconciliation: %w", err)
	}
	updated, err := promotionFromRow(updatedRow)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion reconciliation report: %w", err)
	}

	return updated, nil
}
