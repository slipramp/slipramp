package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const maximumPromotionRequestTransactionAttempts = 3

func (d *Database) AcceptPromotionRequest(
	ctx context.Context,
	acceptance promotion.PromotionAcceptance,
) (promotion.Promotion, error) {
	if err := acceptance.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	var record promotion.Promotion
	var err error
	// Concurrent inserts retry through the same row-locked acceptance path.
	for range maximumPromotionRequestTransactionAttempts {
		record, err = d.acceptPromotionRequest(ctx, acceptance)
		if !promotionRequestSerializationFailure(err) && !constraintMatches(err, promotionIdentityConstraint) {
			return record, err
		}
	}

	return promotion.Promotion{}, err
}

func (d *Database) acceptPromotionRequest(
	ctx context.Context,
	acceptance promotion.PromotionAcceptance,
) (promotion.Promotion, error) {
	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Kargo Promotion request: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	existing, err := queries.LockPromotionByExternalIdentity(
		ctx,
		storedb.LockPromotionByExternalIdentityParams{
			Integration:         acceptance.Integration,
			ExternalPromotionID: acceptance.Request.ExternalPromotionID,
		},
	)
	if err == nil {
		return finishExistingPromotionRequest(ctx, queries, transaction, existing, acceptance)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, fmt.Errorf("get existing Kargo Promotion request: %w", err)
	}

	stageRow, err := queries.GetPromotionStageByIdentity(
		ctx,
		storedb.GetPromotionStageByIdentityParams{
			Integration: acceptance.Integration,
			Project:     acceptance.Request.Project,
			Name:        acceptance.Request.Stage,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrStageNotFound
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get Kargo Promotion Stage: %w", err)
	}
	stage, err := promotionStageFromRow(stageRow)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if stage.ArchivedAt != nil {
		return promotion.Promotion{}, promotion.ErrStageArchived
	}
	if stage.LastObservedAt.Before(acceptance.AcceptedAt.Add(-acceptance.MaximumStageObservationAge)) {
		return promotion.Promotion{}, promotion.ErrStageStale
	}
	reviewContext, err := resolvePromotionReviewContext(ctx, queries, stage, acceptance.AllowedRepositories)
	if err != nil {
		return promotion.Promotion{}, err
	}
	gitContext, err := acceptance.Request.GitContext()
	if err != nil {
		return promotion.Promotion{}, err
	}

	policySnapshot, err := loadPromotionPolicySnapshot(ctx, queries, stage.ID, acceptance.AcceptedAt)
	if err != nil {
		return promotion.Promotion{}, err
	}

	stageSnapshot := stage.PromotionSnapshot()

	reviewRequired := policySnapshot.EffectiveMode == promotion.PolicyModeReviewRequired
	if reviewRequired && (reviewContext == nil || gitContext == nil) {
		return promotion.Promotion{}, promotion.ErrReviewContextUnavailable
	}
	status := promotion.LifecycleApproved
	var expiresAt *time.Time
	var nextReconciliationAt *time.Time
	if reviewRequired {
		if acceptance.PendingReviewRetention <= 0 {
			return promotion.Promotion{}, promotion.ErrReviewContextUnavailable
		}
		status = promotion.LifecyclePreparing
		expiresAt = new(acceptance.AcceptedAt.Add(acceptance.PendingReviewRetention))
	} else {
		nextReconciliationAt = new(acceptance.AcceptedAt)
	}
	if reviewContext != nil {
		reviewContext.Git = gitContext
	}
	record := promotion.Promotion{
		ID:                  acceptance.ID,
		Source:              promotion.SourceKargo,
		Integration:         acceptance.Integration,
		ExternalPromotionID: acceptance.Request.ExternalPromotionID,
		RequestDigest:       acceptance.RequestDigest,
		StageID:             stage.ID,
		StageSnapshot:       stageSnapshot,
		ReviewContext:       reviewContext,
		PolicySnapshot:      policySnapshot,
		ReviewRequired:      reviewRequired,
		Lifecycle: promotion.PromotionLifecycle{
			State:     status,
			ExpiresAt: expiresAt,
		},
		KargoExecution:         promotion.KargoExecutionState{NextReconciliationAt: nextReconciliationAt},
		RequestedByPrincipalID: acceptance.RequestedByPrincipalID,
		RequestedBySnapshot:    acceptance.RequestedBySnapshot,
		CreatedAt:              acceptance.AcceptedAt,
		UpdatedAt:              acceptance.AcceptedAt,
	}
	if err := record.Validate(); err != nil {
		return promotion.Promotion{}, err
	}
	if record.ReviewContext != nil && record.ReviewContext.Git != nil {
		if err := recordPromotionCommits(
			ctx,
			queries,
			record.ReviewContext.RepositoryID,
			record.ReviewContext.Git.BaseRevision,
			record.ReviewContext.Git.CandidateRevision,
		); err != nil {
			return promotion.Promotion{}, err
		}
	}

	params, err := promotionDatabaseParams(record)
	if err != nil {
		return promotion.Promotion{}, err
	}
	row, err := queries.CreatePromotion(ctx, params)
	if constraintMatches(err, promotionCandidateBranchConstraint) {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("insert Kargo Promotion request: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Kargo Promotion request: %w", err)
	}

	return promotionFromRow(row)
}

func finishExistingPromotionRequest(
	ctx context.Context,
	queries *storedb.Queries,
	transaction pgx.Tx,
	row storedb.Promotion,
	acceptance promotion.PromotionAcceptance,
) (promotion.Promotion, error) {
	existing, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, err
	}
	gitContext, err := acceptance.Request.GitContext()
	if err != nil {
		return promotion.Promotion{}, err
	}
	if existing.RequestDigest != (promotion.RequestDigest{}) {
		if existing.RequestDigest != acceptance.RequestDigest {
			return promotion.Promotion{}, promotion.ErrIdentityConflict
		}
		expiredRow, err := queries.ExpirePendingPromotion(ctx, storedb.ExpirePendingPromotionParams{
			ID:        databaseUUID(existing.ID),
			ExpiredAt: databaseTime(acceptance.AcceptedAt),
		})
		if err == nil {
			existing, err = promotionFromRow(expiredRow)
			if err != nil {
				return promotion.Promotion{}, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return promotion.Promotion{}, fmt.Errorf("expire repeated Kargo Promotion request: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return promotion.Promotion{}, fmt.Errorf("commit repeated Kargo Promotion request: %w", err)
		}

		return existing, nil
	}
	if existing.Source != promotion.SourceSlipramp ||
		(existing.ReviewRequired && existing.Lifecycle.State != promotion.LifecycleCreating) ||
		(!existing.ReviewRequired && existing.Lifecycle.State != promotion.LifecycleApproved) {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}
	if existing.ReviewRequired {
		if existing.ReviewContext == nil || gitContext == nil {
			return promotion.Promotion{}, promotion.ErrReviewContextUnavailable
		}
		if _, err := promotion.TransitionLifecycle(
			existing.Lifecycle,
			promotion.LifecyclePreparing,
			existing.ReviewRequired,
			acceptance.AcceptedAt,
		); err != nil {
			return promotion.Promotion{}, promotion.ErrIdentityConflict
		}
	}

	stage := existing.StageSnapshot
	if stage.Project != acceptance.Request.Project || stage.Name != acceptance.Request.Stage {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}
	if existing.ReviewContext != nil && gitContext != nil {
		if err := recordPromotionCommits(
			ctx,
			queries,
			existing.ReviewContext.RepositoryID,
			gitContext.BaseRevision,
			gitContext.CandidateRevision,
		); err != nil {
			return promotion.Promotion{}, err
		}
	}

	row, err = queries.HydrateOperatorPromotion(
		ctx,
		storedb.HydrateOperatorPromotionParams{
			RequestDigest:     acceptance.RequestDigest[:],
			HasGitContext:     gitContext != nil,
			BaseRevision:      acceptance.Request.BaseRevision,
			CandidateRevision: acceptance.Request.CandidateRevision,
			CandidateBranch:   acceptance.Request.CandidateBranch,
			HydratedAt:        databaseTime(acceptance.AcceptedAt),
			ID:                databaseUUID(existing.ID),
		},
	)
	if constraintMatches(err, promotionCandidateBranchConstraint) {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("hydrate operator Promotion request: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit operator Promotion hydration: %w", err)
	}

	return promotionFromRow(row)
}

func promotionRequestSerializationFailure(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "40001"
}

func (d *Database) GetPromotionAwaitingDispatch(ctx context.Context, at time.Time) (promotion.Promotion, error) {
	row, err := d.queries.GetPromotionAwaitingDispatch(ctx, databaseTime(at))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrNoPreparationWork
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get Promotion awaiting preparation dispatch: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) GetPromotionWithFinishedPreparation(ctx context.Context, at time.Time) (promotion.Promotion, error) {
	row, err := d.queries.GetPromotionWithFinishedPreparation(ctx, databaseTime(at))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrNoPreparationWork
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get Promotion with finished preparation: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) ReservePromotionPreparationRevision(
	ctx context.Context,
	promotionID uuid.UUID,
	repositoryID uuid.UUID,
	catalogRevisionID uuid.UUID,
	updatedAt time.Time,
) (promotion.Promotion, error) {
	row, err := d.queries.ReservePromotionPreparationRevision(
		ctx,
		storedb.ReservePromotionPreparationRevisionParams{
			PreparationRepositoryID:      databaseUUID(repositoryID),
			PreparationCatalogRevisionID: databaseUUID(catalogRevisionID),
			UpdatedAt:                    databaseTime(updatedAt),
			ID:                           databaseUUID(promotionID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrTransitionConflict
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("reserve Promotion preparation catalog revision: %w", err)
	}

	return promotionFromRow(row)
}
