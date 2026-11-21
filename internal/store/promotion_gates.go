package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ReleasePromotionGates(
	ctx context.Context,
	promotionID uuid.UUID,
	evaluatorProcessID uuid.UUID,
	releasedAt time.Time,
) (bool, error) {
	if promotionID == uuid.Nil || evaluatorProcessID == uuid.Nil || releasedAt.IsZero() {
		return false, fmt.Errorf("%w: Promotion Gate release context is incomplete", promotion.ErrInvalid)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin Promotion Gate release: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	row, err := queries.LockPromotion(ctx, databaseUUID(promotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, promotion.ErrPromotionNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock Promotion for Gate release: %w", err)
	}
	record, err := promotionFromRow(row)
	if err != nil {
		return false, err
	}
	if record.Lifecycle.State != promotion.LifecycleApproved {
		return false, promotion.ErrTransitionConflict
	}

	released, err := tryReleasePromotionGates(ctx, queries, promotionID, evaluatorProcessID, releasedAt)
	if err != nil || !released {
		return false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit Promotion Gate release: %w", err)
	}

	return true, nil
}

// The caller holds the Promotion lock and rolls back if any requirement remains unreleased.
func tryReleasePromotionGates(
	ctx context.Context,
	queries *storedb.Queries,
	promotionID, evaluatorProcessID uuid.UUID,
	releasedAt time.Time,
) (bool, error) {
	if _, err := queries.LockPromotionGateRepositories(ctx, databaseUUID(promotionID)); err != nil {
		return false, fmt.Errorf("lock Promotion Gate repositories: %w", err)
	}
	if _, err := queries.ReleasePromotionGates(
		ctx,
		storedb.ReleasePromotionGatesParams{
			PromotionID:        databaseUUID(promotionID),
			EvaluatorProcessID: databaseUUID(evaluatorProcessID),
			ReleasedAt:         databaseTime(releasedAt),
		},
	); err != nil {
		return false, fmt.Errorf("release Promotion Gates: %w", err)
	}

	unreleased, err := queries.CountUnreleasedPromotionGates(ctx, databaseUUID(promotionID))
	if err != nil {
		return false, fmt.Errorf("count unreleased Promotion Gates: %w", err)
	}
	return unreleased == 0, nil
}

func resolvePromotionGateRepository(
	ctx context.Context,
	queries *storedb.Queries,
	stage promotion.Stage,
	allowedRepositories []string,
) (*uuid.UUID, error) {
	if len(stage.Gates) == 0 {
		return nil, nil
	}
	if stage.RepositoryName == "" || stage.RepositoryURL == "" ||
		!slices.Contains(allowedRepositories, stage.RepositoryName) {
		return nil, promotion.ErrGateContextUnavailable
	}

	repositoryRow, err := queries.GetActivePromotionRepositoryByName(ctx, stage.RepositoryName)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && repositoryRow.CloneUrl != stage.RepositoryURL) {
		return nil, promotion.ErrGateContextUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("get Promotion Gate repository: %w", err)
	}

	repositoryID := uuid.UUID(repositoryRow.ID.Bytes)
	count, err := queries.CountActivePromotionGates(ctx, storedb.CountActivePromotionGatesParams{
		RepositoryID: databaseUUID(repositoryID),
		GateNames:    stage.Gates,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve Promotion Gates: %w", err)
	}
	if count != int64(len(stage.Gates)) {
		return nil, promotion.ErrGateContextUnavailable
	}

	return &repositoryID, nil
}

func createPromotionGates(
	ctx context.Context,
	queries *storedb.Queries,
	record promotion.Promotion,
	repositoryID *uuid.UUID,
) error {
	if len(record.StageSnapshot.Gates) == 0 {
		return nil
	}
	if repositoryID == nil {
		return promotion.ErrGateContextUnavailable
	}

	for index, gateName := range record.StageSnapshot.Gates {
		if err := queries.CreatePromotionGate(ctx, storedb.CreatePromotionGateParams{
			PromotionID:     databaseUUID(record.ID),
			RepositoryID:    databaseUUID(*repositoryID),
			GateName:        gateName,
			AttachmentOrder: int32(index),
		}); err != nil {
			return fmt.Errorf("record Promotion Gate %q: %w", gateName, err)
		}
	}

	return nil
}
