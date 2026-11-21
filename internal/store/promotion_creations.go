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
	if target.ControlFlow {
		return promotion.Promotion{}, promotion.ErrStageControlFlow
	}
	stages := []promotion.Stage{target}
	var source *promotion.Stage
	if acceptance.SourceStageID != uuid.Nil {
		value := target
		if acceptance.SourceStageID != acceptance.TargetStageID {
			value, err = getOperatorPromotionStage(ctx, queries, acceptance.SourceStageID)
			if err != nil {
				return promotion.Promotion{}, err
			}
			stages = append(stages, value)
		}
		source = &value
	}
	for _, stage := range stages {
		if stage.ArchivedAt != nil {
			return promotion.Promotion{}, promotion.ErrStageArchived
		}
		if stage.LastObservedAt.Before(acceptance.AcceptedAt.Add(-acceptance.MaximumStageObservationAge)) {
			return promotion.Promotion{}, promotion.ErrStageStale
		}
	}

	var selectedFreight *promotion.FreightItem
	if source == nil {
		selectedFreight, err = promotion.SelectPromotionFreightCandidate(target, acceptance.FreightName)
	} else {
		selectedFreight, err = promotion.SelectPromotionFreight(target, *source, acceptance.FreightName)
	}
	if err != nil {
		return promotion.Promotion{}, err
	}

	reviewContext, err := resolvePromotionReviewContext(ctx, queries, target, acceptance.AllowedRepositories)
	if err != nil {
		return promotion.Promotion{}, err
	}
	gateRepositoryID, err := resolvePromotionGateRepository(ctx, queries, target, acceptance.AllowedRepositories)
	if err != nil {
		return promotion.Promotion{}, err
	}
	if acceptance.Policy != nil {
		if _, err := upsertPromotionStagePolicy(ctx, queries, *acceptance.Policy); err != nil {
			return promotion.Promotion{}, fmt.Errorf("update operator Promotion Stage policy: %w", err)
		}
	}

	policySnapshot, err := loadPromotionPolicySnapshot(ctx, queries, target.ID, acceptance.AcceptedAt)
	if err != nil {
		return promotion.Promotion{}, err
	}
	stageSnapshot := target.PromotionSnapshot()

	nextDispatchAt := acceptance.AcceptedAt
	reviewRequired := policySnapshot.EffectiveMode == promotion.PolicyModeReviewRequired
	if reviewRequired && reviewContext == nil {
		return promotion.Promotion{}, promotion.ErrReviewContextUnavailable
	}
	status := promotion.LifecycleApproved
	var expiresAt *time.Time
	if reviewRequired {
		if acceptance.PendingReviewRetention <= 0 {
			return promotion.Promotion{}, promotion.ErrReviewContextUnavailable
		}
		status = promotion.LifecycleCreating
		expiresAt = new(acceptance.AcceptedAt.Add(acceptance.PendingReviewRetention))
	}
	var sourceStageID *uuid.UUID
	if acceptance.SourceStageID != uuid.Nil {
		sourceStageID = new(acceptance.SourceStageID)
	}
	record := promotion.Promotion{
		ID:                      acceptance.ID,
		Source:                  promotion.SourceSlipramp,
		Integration:             target.Integration,
		ExternalPromotionID:     promotion.CreationExternalName(target.Name, acceptance.ID),
		StageID:                 target.ID,
		SourceStageID:           sourceStageID,
		StageSnapshot:           stageSnapshot,
		ReviewContext:           reviewContext,
		PolicySnapshot:          policySnapshot,
		SelectedFreightSnapshot: selectedFreight,
		ReviewRequired:          reviewRequired,
		Lifecycle: promotion.PromotionLifecycle{
			State:     status,
			ExpiresAt: expiresAt,
		},
		CreationDispatch:       promotion.CreationDispatchState{NextAttemptAt: &nextDispatchAt},
		RequestedByPrincipalID: acceptance.RequestedByPrincipalID,
		RequestedBySnapshot:    acceptance.RequestedBySnapshot,
		CreatedAt:              acceptance.AcceptedAt,
		UpdatedAt:              acceptance.AcceptedAt,
	}
	if err := record.Validate(); err != nil {
		return promotion.Promotion{}, err
	}

	params, err := promotionDatabaseParams(record)
	if err != nil {
		return promotion.Promotion{}, err
	}
	row, err := queries.CreatePromotion(ctx, params)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("insert operator Promotion: %w", err)
	}
	if err := createPromotionGates(ctx, queries, record, gateRepositoryID); err != nil {
		return promotion.Promotion{}, err
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

func resolvePromotionReviewContext(
	ctx context.Context,
	queries *storedb.Queries,
	stage promotion.Stage,
	allowedRepositories []string,
) (*promotion.PromotionReviewContext, error) {
	if !stage.Enrichment().ReviewReady() || !slices.Contains(allowedRepositories, stage.RepositoryName) {
		return nil, nil
	}
	repositoryRow, err := queries.GetActivePromotionRepositoryByName(ctx, stage.RepositoryName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get Promotion review repository: %w", err)
	}
	if repositoryRow.CloneUrl != stage.RepositoryURL {
		return nil, nil
	}

	return &promotion.PromotionReviewContext{
		RepositoryID: uuid.UUID(repositoryRow.ID.Bytes),
		RepositorySnapshot: promotion.PromotionRepositorySnapshot{
			Name:                   repositoryRow.Name,
			CloneURL:               repositoryRow.CloneUrl,
			Branch:                 repositoryRow.Branch,
			CheckoutCredentialName: repositoryRow.CheckoutCredentialName,
		},
		TargetBranch: stage.TargetBranch,
	}, nil
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
