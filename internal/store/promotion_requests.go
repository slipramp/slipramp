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

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) AcceptPromotionRequest(
	ctx context.Context,
	acceptance promotion.PromotionAcceptance,
) (promotion.Promotion, error) {
	if err := acceptance.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Kargo Promotion request: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	existing, err := queries.GetPromotionByExternalIdentity(
		ctx,
		storedb.GetPromotionByExternalIdentityParams{
			Integration:         acceptance.Integration,
			ExternalPromotionID: acceptance.Request.ExternalPromotionID,
		},
	)
	if err == nil {
		return finishExistingPromotionRequest(ctx, transaction, existing, acceptance.RequestDigest)
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

	stageSnapshot, err := json.Marshal(promotion.PromotionStageSnapshot{
		ID:                              stage.ID,
		Integration:                     stage.Integration,
		ClusterName:                     stage.ClusterName,
		Project:                         stage.Project,
		Name:                            stage.Name,
		KubernetesUID:                   stage.KubernetesUID,
		ResourceVersion:                 stage.ResourceVersion,
		Application:                     stage.Application,
		Environment:                     stage.Environment,
		RepositoryName:                  stage.RepositoryName,
		RepositoryURL:                   stage.RepositoryURL,
		TargetBranch:                    stage.TargetBranch,
		SourcePaths:                     slices.Clone(stage.SourcePaths),
		RequestedFreight:                slices.Clone(stage.RequestedFreight),
		Freight:                         slices.Clone(stage.Freight),
		SuccessfulRenderAcknowledgement: slices.Clone(stage.SuccessfulRenderAcknowledgement),
		LastObservedAt:                  stage.LastObservedAt,
	})
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode Kargo Promotion Stage snapshot: %w", err)
	}
	repositorySnapshot, err := json.Marshal(promotion.PromotionRepositorySnapshot{
		Name:                   repositoryRow.Name,
		CloneURL:               repositoryRow.CloneUrl,
		Branch:                 repositoryRow.Branch,
		CheckoutCredentialName: repositoryRow.CheckoutCredentialName,
	})
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode Kargo Promotion repository snapshot: %w", err)
	}
	policy, err := json.Marshal(policySnapshot)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode Kargo Promotion policy snapshot: %w", err)
	}
	requestedBy, err := json.Marshal(acceptance.RequestedBySnapshot)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode Kargo Promotion requesting actor: %w", err)
	}

	reviewRequired := policySnapshot.EffectiveMode == promotion.PolicyModeReviewRequired
	status := promotion.StatusApproved
	var expiresAt *time.Time
	if reviewRequired {
		status = promotion.StatusPreparing
		expiresAt = new(acceptance.AcceptedAt.Add(acceptance.PendingReviewRetention))
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
		PolicySnapshot:         policy,
		ReviewRequired:         reviewRequired,
		Status:                 status,
		RequestedByPrincipalID: acceptance.RequestedByPrincipalID,
		RequestedBySnapshot:    acceptance.RequestedBySnapshot,
		CreatedAt:              acceptance.AcceptedAt,
		ExpiresAt:              expiresAt,
		UpdatedAt:              acceptance.AcceptedAt,
	}
	if err := record.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	row, err := queries.CreatePromotion(ctx, promotionDatabaseParams(record, requestedBy))
	if constraintMatches(err, promotionIdentityConstraint) {
		_ = transaction.Rollback(ctx)

		return d.existingPromotionRequest(ctx, acceptance)
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("insert Kargo Promotion request: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Kargo Promotion request: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) existingPromotionRequest(
	ctx context.Context,
	acceptance promotion.PromotionAcceptance,
) (promotion.Promotion, error) {
	existing, err := d.GetPromotionByExternalIdentity(
		ctx,
		acceptance.Integration,
		acceptance.Request.ExternalPromotionID,
	)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if existing.RequestDigest != acceptance.RequestDigest {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}

	return existing, nil
}

func finishExistingPromotionRequest(
	ctx context.Context,
	transaction pgx.Tx,
	row storedb.Promotion,
	digest promotion.RequestDigest,
) (promotion.Promotion, error) {
	existing, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if existing.RequestDigest != digest {
		return promotion.Promotion{}, promotion.ErrIdentityConflict
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit repeated Kargo Promotion request: %w", err)
	}

	return existing, nil
}

func (d *Database) GetPromotionAwaitingDispatch(ctx context.Context) (promotion.Promotion, error) {
	row, err := d.queries.GetPromotionAwaitingDispatch(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrNoPreparationWork
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get Promotion awaiting preparation dispatch: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) GetPromotionWithFinishedPreparation(ctx context.Context) (promotion.Promotion, error) {
	row, err := d.queries.GetPromotionWithFinishedPreparation(ctx)
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
