package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	if !slices.Contains(acceptance.AllowedRepositories, stage.RepositoryName) {
		return promotion.Promotion{}, promotion.ErrRepositoryNotAllowed
	}

	repositoryRow, err := queries.GetActivePromotionRepositoryByName(ctx, stage.RepositoryName)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrRepositoryUnavailable
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get Kargo Promotion repository: %w", err)
	}
	if repositoryRow.CloneUrl != stage.RepositoryURL {
		return promotion.Promotion{}, promotion.ErrRepositoryNotAllowed
	}

	policySnapshot := promotion.PromotionPolicySnapshot{
		ConfiguredMode: promotion.PolicyModeAutomatic,
		EffectiveMode:  promotion.PolicyModeAutomatic,
	}
	policyRow, err := queries.GetPromotionStagePolicy(ctx, databaseUUID(stage.ID))
	if err == nil {
		policy, decodeErr := promotionStagePolicyFromRow(policyRow)
		if decodeErr != nil {
			return promotion.Promotion{}, decodeErr
		}
		updatedAt := policy.UpdatedAt
		updatedBy := policy.UpdatedBySnapshot
		policySnapshot = promotion.PromotionPolicySnapshot{
			ConfiguredMode: policy.Mode,
			EffectiveMode:  policy.Mode.Effective(policy.ExpiresAt, acceptance.AcceptedAt),
			ExpiresAt:      policy.ExpiresAt,
			Reason:         policy.Reason,
			UpdatedBy:      &updatedBy,
			UpdatedAt:      &updatedAt,
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, fmt.Errorf("get Kargo Promotion Stage policy: %w", err)
	}

	stageSnapshot := stage.PromotionSnapshot()
	repositorySnapshot := promotion.PromotionRepositorySnapshot{
		Name:                   repositoryRow.Name,
		CloneURL:               repositoryRow.CloneUrl,
		Branch:                 repositoryRow.Branch,
		CheckoutCredentialName: repositoryRow.CheckoutCredentialName,
	}
	requestedBy, err := json.Marshal(acceptance.RequestedBySnapshot)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode Kargo Promotion requesting actor: %w", err)
	}

	reviewRequired := policySnapshot.EffectiveMode == promotion.PolicyModeReviewRequired
	status := promotion.StatusApproved
	var expiresAt *time.Time
	var nextReconciliationAt *time.Time
	if reviewRequired {
		status = promotion.StatusPreparing
		expiresAt = new(acceptance.AcceptedAt.Add(acceptance.PendingReviewRetention))
	} else {
		nextReconciliationAt = new(acceptance.AcceptedAt)
	}
	record := promotion.Promotion{
		ID:                     acceptance.ID,
		Source:                 promotion.SourceKargo,
		Integration:            acceptance.Integration,
		ExternalPromotionID:    acceptance.Request.ExternalPromotionID,
		RequestDigest:          acceptance.RequestDigest,
		StageID:                stage.ID,
		StageSnapshot:          stageSnapshot,
		RepositoryID:           uuid.UUID(repositoryRow.ID.Bytes),
		RepositorySnapshot:     repositorySnapshot,
		BaseRevision:           acceptance.Request.BaseRevision,
		CandidateRevision:      acceptance.Request.CandidateRevision,
		CandidateBranch:        acceptance.Request.CandidateBranch,
		TargetBranch:           stage.TargetBranch,
		PolicySnapshot:         policySnapshot,
		ReviewRequired:         reviewRequired,
		Status:                 status,
		NextReconciliationAt:   nextReconciliationAt,
		RequestedByPrincipalID: acceptance.RequestedByPrincipalID,
		RequestedBySnapshot:    acceptance.RequestedBySnapshot,
		CreatedAt:              acceptance.AcceptedAt,
		ExpiresAt:              expiresAt,
		UpdatedAt:              acceptance.AcceptedAt,
	}
	if err := record.Validate(); err != nil {
		return promotion.Promotion{}, err
	}
	if err := recordPromotionCommits(
		ctx,
		queries,
		record.RepositoryID,
		record.BaseRevision,
		record.CandidateRevision,
	); err != nil {
		return promotion.Promotion{}, err
	}

	params, err := promotionDatabaseParams(record, requestedBy)
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
	if existing.Source != promotion.SourceSlipramp || existing.Status != promotion.StatusCreating {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}

	stage := existing.StageSnapshot
	if stage.Project != acceptance.Request.Project || stage.Name != acceptance.Request.Stage {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}
	if err := recordPromotionCommits(
		ctx,
		queries,
		existing.RepositoryID,
		acceptance.Request.BaseRevision,
		acceptance.Request.CandidateRevision,
	); err != nil {
		return promotion.Promotion{}, err
	}

	row, err = queries.HydrateOperatorPromotion(
		ctx,
		storedb.HydrateOperatorPromotionParams{
			RequestDigest:     acceptance.RequestDigest[:],
			BaseRevision:      nullableText(acceptance.Request.BaseRevision),
			CandidateRevision: nullableText(acceptance.Request.CandidateRevision),
			CandidateBranch:   nullableText(acceptance.Request.CandidateBranch),
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
