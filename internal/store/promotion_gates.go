package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ReleasePromotion(
	ctx context.Context, id, process uuid.UUID,
) (promotion.KargoResponseStatus, error) {
	return d.releasePromotion(ctx, id, process, (*storedb.Queries).ServerGateCheckTime)
}

func (d *Database) releasePromotion(
	ctx context.Context, id, process uuid.UUID, checkTime gateCheckClock,
) (promotion.KargoResponseStatus, error) {
	if id == uuid.Nil || process == uuid.Nil {
		return "", fmt.Errorf("%w: Promotion release context is incomplete", promotion.ErrInvalid)
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin Promotion release: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := d.queries.WithTx(tx)
	row, err := q.LockPromotion(ctx, databaseUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", promotion.ErrPromotionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock Promotion for release: %w", err)
	}
	if row.CancellationRequestedAt.Valid {
		return promotion.KargoResponseStatusCanceling, nil
	}
	if row.Status != string(promotion.LifecycleApproved) {
		return "", promotion.ErrTransitionConflict
	}
	approved := promotion.KargoResponseStatus(promotion.LifecycleApproved)
	if row.CallbackReleased {
		return approved, nil
	}
	// The first automatic creation claim is the release, including for historical deliveries.
	if row.Source == string(promotion.SourceSlipramp) && !row.ReviewRequired {
		if row.DispatchAttemptCount > 0 {
			return approved, nil
		}
		return promotion.KargoResponseStatusGated, nil
	}
	if row.KargoOutcomePhase != nil || row.DispatchFailedAt.Valid {
		return "", promotion.ErrTransitionConflict
	}
	record, err := promotionFromRow(row)
	if err != nil {
		return "", err
	}
	evidence, _, err := releasePromotionEvidence(ctx, q, record, process, checkTime)
	if err != nil {
		return "", err
	}
	if evidence == nil {
		return promotion.KargoResponseStatusGated, nil
	}
	if err := q.MarkPromotionCallbackReleased(ctx, storedb.MarkPromotionCallbackReleasedParams{ID: row.ID, ReleaseEvidence: evidence}); err != nil {
		return "", fmt.Errorf("record Promotion callback release: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit Promotion release: %w", err)
	}
	return approved, nil
}

// Both release paths hold the Promotion row. A blocked check rolls back the transaction,
// including any repository requirements updated before a missing pass was detected.
func releasePromotionEvidence(
	ctx context.Context, q *storedb.Queries, record promotion.Promotion, process uuid.UUID, checkTime gateCheckClock,
) ([]byte, pgtype.Timestamptz, error) {
	var at pgtype.Timestamptz
	id := databaseUUID(record.ID)
	scopes, err := q.SharePromotionGateScopes(ctx, databaseUUID(record.StageID))
	if err != nil {
		return nil, at, fmt.Errorf("lock Promotion Gate scopes: %w", err)
	}
	if len(record.StageSnapshot.Gates) > 0 {
		if _, err := q.LockPromotionGateRepositories(ctx, id); err != nil {
			return nil, at, fmt.Errorf("lock Promotion Gate repositories: %w", err)
		}
	}
	at, err = checkTime(q, ctx)
	if err != nil {
		return nil, at, fmt.Errorf("read Promotion release time: %w", err)
	}
	evidence, err := freezeServerGates(ctx, q, gate.PromotionSubject, record.ID, scopes, at.Time.UTC())
	if err != nil || evidence == nil {
		return nil, at, err
	}
	released, err := q.ReleasePromotionGates(ctx, storedb.ReleasePromotionGatesParams{
		PromotionID: id, EvaluatorProcessID: databaseUUID(process), ReleasedAt: at,
	})
	if err != nil {
		return nil, at, fmt.Errorf("release Promotion repository Gates: %w", err)
	}
	if released != int64(len(record.StageSnapshot.Gates)) {
		return nil, at, nil
	}
	return evidence, at, nil
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
