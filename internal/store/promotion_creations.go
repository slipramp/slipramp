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

func (d *Database) CreateOperatorPromotion(
	ctx context.Context,
	acceptance promotion.OperatorPromotionAcceptance,
) (promotion.Promotion, error) {
	if err := acceptance.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin operator Promotion creation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	target, err := getOperatorPromotionStage(ctx, queries, acceptance.TargetStageID)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if target.Integration != acceptance.Integration {
		return promotion.Promotion{}, promotion.ErrIntegrationNotFound
	}
	source := target
	if acceptance.SourceStageID != acceptance.TargetStageID {
		source, err = getOperatorPromotionStage(ctx, queries, acceptance.SourceStageID)
		if err != nil {
			return promotion.Promotion{}, err
		}
	}
	for _, stage := range []promotion.Stage{target, source} {
		if stage.ArchivedAt != nil {
			return promotion.Promotion{}, promotion.ErrStageArchived
		}
		if stage.LastObservedAt.Before(acceptance.AcceptedAt.Add(-acceptance.MaximumStageObservationAge)) {
			return promotion.Promotion{}, promotion.ErrStageStale
		}
	}

	selectedFreight, err := promotion.SelectPromotionFreight(target, source, acceptance.FreightName)
	if err != nil {
		return promotion.Promotion{}, err
	}

	repositoryRow, err := queries.GetActivePromotionRepositoryByName(ctx, target.RepositoryName)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrRepositoryUnavailable
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get operator Promotion repository: %w", err)
	}
	if repositoryRow.CloneUrl != target.RepositoryURL {
		return promotion.Promotion{}, promotion.ErrRepositoryNotAllowed
	}

	policySnapshot, err := operatorPromotionPolicySnapshot(ctx, queries, target.ID, acceptance.AcceptedAt)
	if err != nil {
		return promotion.Promotion{}, err
	}
	stageSnapshot, err := json.Marshal(promotion.PromotionStageSnapshot{
		ID:                              target.ID,
		Integration:                     target.Integration,
		ClusterName:                     target.ClusterName,
		Project:                         target.Project,
		Name:                            target.Name,
		KubernetesUID:                   target.KubernetesUID,
		ResourceVersion:                 target.ResourceVersion,
		Application:                     target.Application,
		Environment:                     target.Environment,
		RepositoryName:                  target.RepositoryName,
		RepositoryURL:                   target.RepositoryURL,
		TargetBranch:                    target.TargetBranch,
		SourcePaths:                     slices.Clone(target.SourcePaths),
		RequestedFreight:                slices.Clone(target.RequestedFreight),
		Freight:                         slices.Clone(target.Freight),
		SuccessfulRenderAcknowledgement: slices.Clone(target.SuccessfulRenderAcknowledgement),
		LastObservedAt:                  target.LastObservedAt,
	})
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode operator Promotion Stage snapshot: %w", err)
	}
	repositorySnapshot, err := json.Marshal(promotion.PromotionRepositorySnapshot{
		Name:                   repositoryRow.Name,
		CloneURL:               repositoryRow.CloneUrl,
		Branch:                 repositoryRow.Branch,
		CheckoutCredentialName: repositoryRow.CheckoutCredentialName,
	})
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode operator Promotion repository snapshot: %w", err)
	}
	policy, err := json.Marshal(policySnapshot)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode operator Promotion policy snapshot: %w", err)
	}
	requestedBy, err := json.Marshal(acceptance.RequestedBySnapshot)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("encode operator Promotion requesting actor: %w", err)
	}

	nextDispatchAt := acceptance.AcceptedAt
	reviewRequired := policySnapshot.EffectiveMode == promotion.PolicyModeReviewRequired
	var expiresAt *time.Time
	if reviewRequired {
		expiresAt = new(acceptance.AcceptedAt.Add(acceptance.PendingReviewRetention))
	}
	sourceStageID := acceptance.SourceStageID
	record := promotion.Promotion{
		ID:                      acceptance.ID,
		Source:                  promotion.SourceSlipramp,
		Integration:             target.Integration,
		ExternalPromotionID:     promotion.CreationExternalName(target.Name, acceptance.ID),
		StageID:                 target.ID,
		SourceStageID:           &sourceStageID,
		StageSnapshot:           stageSnapshot,
		RepositoryID:            uuid.UUID(repositoryRow.ID.Bytes),
		RepositorySnapshot:      repositorySnapshot,
		TargetBranch:            target.TargetBranch,
		PolicySnapshot:          policy,
		SelectedFreightSnapshot: selectedFreight,
		ReviewRequired:          reviewRequired,
		Status:                  promotion.StatusCreating,
		DispatchAttemptCount:    0,
		NextDispatchAt:          &nextDispatchAt,
		RequestedByPrincipalID:  acceptance.RequestedByPrincipalID,
		RequestedBySnapshot:     acceptance.RequestedBySnapshot,
		CreatedAt:               acceptance.AcceptedAt,
		ExpiresAt:               expiresAt,
		UpdatedAt:               acceptance.AcceptedAt,
	}
	if err := record.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	row, err := queries.CreatePromotion(ctx, promotionDatabaseParams(record, requestedBy))
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("insert operator Promotion: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit operator Promotion creation: %w", err)
	}

	return promotionFromRow(row)
}

func getOperatorPromotionStage(
	ctx context.Context,
	queries *storedb.Queries,
	stageID uuid.UUID,
) (promotion.Stage, error) {
	row, err := queries.GetPromotionStage(ctx, databaseUUID(stageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Stage{}, promotion.ErrStageNotFound
	}
	if err != nil {
		return promotion.Stage{}, fmt.Errorf("get operator Promotion Stage: %w", err)
	}

	return promotionStageFromRow(row)
}

func operatorPromotionPolicySnapshot(
	ctx context.Context,
	queries *storedb.Queries,
	stageID uuid.UUID,
	acceptedAt time.Time,
) (promotion.PromotionPolicySnapshot, error) {
	snapshot := promotion.PromotionPolicySnapshot{
		ConfiguredMode: promotion.PolicyModeAutomatic,
		EffectiveMode:  promotion.PolicyModeAutomatic,
	}
	row, err := queries.GetPromotionStagePolicy(ctx, databaseUUID(stageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return promotion.PromotionPolicySnapshot{}, fmt.Errorf("get operator Promotion Stage policy: %w", err)
	}
	policy, err := promotionStagePolicyFromRow(row)
	if err != nil {
		return promotion.PromotionPolicySnapshot{}, err
	}
	updatedAt := policy.UpdatedAt
	updatedBy := policy.UpdatedBySnapshot

	return promotion.PromotionPolicySnapshot{
		ConfiguredMode: policy.Mode,
		EffectiveMode:  policy.Mode.Effective(policy.ExpiresAt, acceptedAt),
		ExpiresAt:      policy.ExpiresAt,
		Reason:         policy.Reason,
		UpdatedBy:      &updatedBy,
		UpdatedAt:      &updatedAt,
	}, nil
}
