package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const (
	promotionIdentityConstraint     = "promotions_external_identity_key"
	kargoSnapshotIdentityConstraint = "kargo_stage_snapshots_pkey"
)

func recordPromotionCommits(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID uuid.UUID,
	revisions ...string,
) error {
	for _, revision := range revisions {
		if revision == "" {
			continue
		}
		if err := upsertRepositoryCommit(ctx, queries, repository.Commit{
			RepositoryID: repositoryID,
			SHA:          revision,
		}); err != nil {
			return fmt.Errorf("record Promotion commit: %w", err)
		}
	}

	return nil
}

func (d *Database) GetPromotionStage(ctx context.Context, stageID uuid.UUID) (promotion.Stage, error) {
	row, err := d.queries.GetPromotionStage(ctx, databaseUUID(stageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Stage{}, promotion.ErrStageNotFound
	}
	if err != nil {
		return promotion.Stage{}, fmt.Errorf("get promotion Stage: %w", err)
	}

	return promotionStageFromRow(row)
}

func (d *Database) GetPromotionStageByIdentity(
	ctx context.Context,
	integration string,
	project string,
	name string,
) (promotion.Stage, error) {
	row, err := d.queries.GetPromotionStageByIdentity(
		ctx,
		storedb.GetPromotionStageByIdentityParams{
			Integration: integration,
			Project:     project,
			Name:        name,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Stage{}, promotion.ErrStageNotFound
	}
	if err != nil {
		return promotion.Stage{}, fmt.Errorf("get promotion Stage by identity: %w", err)
	}

	return promotionStageFromRow(row)
}

func (d *Database) ListPromotionStages(ctx context.Context) ([]promotion.Stage, error) {
	rows, err := d.queries.ListPromotionStages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list promotion Stages: %w", err)
	}

	records := make([]promotion.Stage, 0, len(rows))
	for _, row := range rows {
		record, decodeErr := promotionStageFromRow(row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		records = append(records, record)
	}

	return records, nil
}

func (d *Database) UpsertPromotionStagePolicy(
	ctx context.Context,
	record promotion.StagePolicy,
) (promotion.StagePolicy, error) {
	if err := record.Validate(); err != nil {
		return promotion.StagePolicy{}, err
	}

	actor, err := json.Marshal(record.UpdatedBySnapshot)
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("encode promotion policy actor: %w", err)
	}
	row, err := d.queries.UpsertPromotionStagePolicy(
		ctx,
		storedb.UpsertPromotionStagePolicyParams{
			StageID:              databaseUUID(record.StageID),
			Mode:                 string(record.Mode),
			ExpiresAt:            optionalDatabaseTime(record.ExpiresAt),
			Reason:               record.Reason,
			UpdatedByPrincipalID: databaseUUID(record.UpdatedByPrincipalID),
			UpdatedBySnapshot:    actor,
			UpdatedAt:            databaseTime(record.UpdatedAt),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.StagePolicy{}, promotion.ErrTransitionConflict
	}
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("upsert promotion Stage policy: %w", err)
	}

	return promotionStagePolicyFromRow(row)
}

func (d *Database) GetPromotionStagePolicy(
	ctx context.Context,
	stageID uuid.UUID,
) (promotion.StagePolicy, error) {
	row, err := d.queries.GetPromotionStagePolicy(ctx, databaseUUID(stageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.StagePolicy{}, promotion.ErrPolicyNotFound
	}
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("get promotion Stage policy: %w", err)
	}

	return promotionStagePolicyFromRow(row)
}

func (d *Database) ReplacePromotionStageSnapshot(
	ctx context.Context,
	snapshot promotion.StageSnapshot,
	stages []promotion.Stage,
) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Kargo Stage snapshot replacement: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	if err := queries.LockKargoStageSnapshots(ctx, snapshot.Integration); err != nil {
		return fmt.Errorf("lock Kargo Stage snapshots: %w", err)
	}

	existing, err := queries.GetKargoStageSnapshot(ctx, databaseUUID(snapshot.SnapshotID))
	if err == nil {
		stored := stageSnapshotFromRow(existing)
		if stored.Integration != snapshot.Integration || stored.ClusterName != snapshot.ClusterName ||
			!stored.ObservedAt.Equal(snapshot.ObservedAt) {
			return promotion.ErrSnapshotConflict
		}

		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit repeated Kargo Stage snapshot: %w", err)
		}

		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get existing Kargo Stage snapshot: %w", err)
	}

	latest, err := queries.GetLatestKargoStageSnapshot(
		ctx,
		storedb.GetLatestKargoStageSnapshotParams{
			Integration: snapshot.Integration,
			ClusterName: snapshot.ClusterName,
		},
	)
	if err == nil && !snapshot.ObservedAt.After(latest.ObservedAt.Time) {
		return promotion.ErrStaleSnapshot
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get latest Kargo Stage snapshot: %w", err)
	}

	identities := make(map[string]struct{}, len(stages))
	for _, stage := range stages {
		if err := stage.Validate(); err != nil {
			return err
		}
		if stage.Integration != snapshot.Integration || stage.ClusterName != snapshot.ClusterName ||
			!stage.FirstObservedAt.Equal(snapshot.ObservedAt) ||
			!stage.LastObservedAt.Equal(snapshot.ObservedAt) || stage.ArchivedAt != nil {
			return fmt.Errorf("%w: Stage does not belong to the complete snapshot", promotion.ErrInvalid)
		}

		identity := stage.Project + "\x00" + stage.Name
		if _, ok := identities[identity]; ok {
			return fmt.Errorf("%w: Stage %s/%s is duplicated", promotion.ErrInvalid, stage.Project, stage.Name)
		}
		identities[identity] = struct{}{}
	}

	for _, stage := range stages {
		params, err := stageDatabaseParams(stage)
		if err != nil {
			return err
		}
		if _, err := queries.UpsertPromotionStage(ctx, params); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return promotion.ErrStaleSnapshot
			}
			return fmt.Errorf("upsert promotion Stage in snapshot: %w", err)
		}
	}

	if err := queries.ArchiveMissingPromotionStages(
		ctx,
		storedb.ArchiveMissingPromotionStagesParams{
			ArchivedAt:  databaseTime(snapshot.ObservedAt),
			Integration: snapshot.Integration,
			ClusterName: snapshot.ClusterName,
		},
	); err != nil {
		return fmt.Errorf("archive missing promotion Stages: %w", err)
	}

	if _, err := queries.CreateKargoStageSnapshot(
		ctx,
		storedb.CreateKargoStageSnapshotParams{
			SnapshotID:  databaseUUID(snapshot.SnapshotID),
			Integration: snapshot.Integration,
			ClusterName: snapshot.ClusterName,
			ObservedAt:  databaseTime(snapshot.ObservedAt),
			AcceptedAt:  databaseTime(snapshot.AcceptedAt),
		},
	); constraintMatches(err, kargoSnapshotIdentityConstraint) {
		return promotion.ErrSnapshotConflict
	} else if err != nil {
		return fmt.Errorf("insert Kargo Stage snapshot: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Kargo Stage snapshot replacement: %w", err)
	}

	return nil
}

func (d *Database) GetPromotion(ctx context.Context, promotionID uuid.UUID) (promotion.Promotion, error) {
	row, err := d.queries.GetPromotion(ctx, databaseUUID(promotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get Promotion: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) GetPromotionByExternalIdentity(
	ctx context.Context,
	integration string,
	externalPromotionID string,
) (promotion.Promotion, error) {
	row, err := d.queries.GetPromotionByExternalIdentity(
		ctx,
		storedb.GetPromotionByExternalIdentityParams{
			Integration:         integration,
			ExternalPromotionID: externalPromotionID,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrPromotionNotFound
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get Promotion by external identity: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) GetPromotionDecision(
	ctx context.Context,
	promotionID uuid.UUID,
) (promotion.Decision, error) {
	row, err := d.queries.GetPromotionDecision(ctx, databaseUUID(promotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Decision{}, promotion.ErrDecisionNotFound
	}
	if err != nil {
		return promotion.Decision{}, fmt.Errorf("get Promotion decision: %w", err)
	}

	return promotionDecisionFromRow(row)
}

func (d *Database) ExpirePendingPromotions(
	ctx context.Context,
	expiredAt time.Time,
) ([]promotion.Promotion, error) {
	rows, err := d.queries.ExpirePendingPromotions(ctx, databaseTime(expiredAt))
	if err != nil {
		return nil, fmt.Errorf("expire pending Promotions: %w", err)
	}

	records := make([]promotion.Promotion, 0, len(rows))
	for _, row := range rows {
		record, decodeErr := promotionFromRow(row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		records = append(records, record)
	}

	return records, nil
}

func (d *Database) CompletePromotionPreparation(
	ctx context.Context,
	promotionID uuid.UUID,
	runID uuid.UUID,
	gitArtifactID uuid.UUID,
	argoArtifactID uuid.UUID,
	completedAt time.Time,
	notificationPlan *notification.EnqueuePlan,
) (promotion.Promotion, error) {
	if notificationPlan != nil {
		if err := notificationPlan.Validate(); err != nil {
			return promotion.Promotion{}, err
		}
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("begin Promotion preparation completion: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	row, err := queries.CompletePromotionPreparation(
		ctx,
		storedb.CompletePromotionPreparationParams{
			GitDiffArtifactID:      databaseUUID(gitArtifactID),
			ArgoDiffArtifactID:     databaseUUID(argoArtifactID),
			PreparationCompletedAt: databaseTime(completedAt),
			UpdatedAt:              databaseTime(completedAt),
			ID:                     databaseUUID(promotionID),
			PreparationRunID:       databaseUUID(runID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrTransitionConflict
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("complete Promotion preparation: %w", err)
	}
	if notificationPlan != nil {
		if _, err := enqueueNotification(
			ctx,
			queries,
			notificationPlan.Event,
			notificationPlan.Destinations,
		); err != nil {
			return promotion.Promotion{}, fmt.Errorf("enqueue Promotion review notification: %w", err)
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, fmt.Errorf("commit Promotion preparation completion: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) FailPromotionPreparation(
	ctx context.Context,
	promotionID uuid.UUID,
	runID uuid.UUID,
	message string,
	completedAt time.Time,
) (promotion.Promotion, error) {
	row, err := d.queries.FailPromotionPreparation(
		ctx,
		storedb.FailPromotionPreparationParams{
			Message:                &message,
			PreparationCompletedAt: databaseTime(completedAt),
			UpdatedAt:              databaseTime(completedAt),
			ID:                     databaseUUID(promotionID),
			PreparationRunID:       databaseUUID(runID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrTransitionConflict
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("fail Promotion preparation: %w", err)
	}

	return promotionFromRow(row)
}

func (d *Database) DecidePromotion(
	ctx context.Context,
	record promotion.Decision,
) (promotion.Promotion, promotion.Decision, error) {
	if err := record.Validate(); err != nil {
		return promotion.Promotion{}, promotion.Decision{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("begin Promotion decision: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	existing, err := queries.GetPromotionDecision(ctx, databaseUUID(record.PromotionID))
	if err == nil {
		return finishExistingDecision(ctx, queries, transaction, existing, record)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("get existing Promotion decision: %w", err)
	}

	if _, err := queries.ExpirePendingPromotion(
		ctx,
		storedb.ExpirePendingPromotionParams{
			ExpiredAt: databaseTime(record.DecidedAt),
			ID:        databaseUUID(record.PromotionID),
		},
	); err == nil {
		if err := transaction.Commit(ctx); err != nil {
			return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("commit Promotion expiry: %w", err)
		}

		return promotion.Promotion{}, promotion.Decision{}, promotion.ErrTransitionConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf(
			"expire Promotion before decision: %w",
			err,
		)
	}

	if _, err := queries.LockPendingPromotion(ctx, databaseUUID(record.PromotionID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, decisionErr := queries.GetPromotionDecision(ctx, databaseUUID(record.PromotionID))
			if decisionErr == nil {
				return finishExistingDecision(ctx, queries, transaction, existing, record)
			}
			return promotion.Promotion{}, promotion.Decision{}, promotion.ErrTransitionConflict
		}
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("lock pending Promotion: %w", err)
	}

	status := promotion.StatusApproved
	var message *string
	if record.Decision == promotion.DecisionRejected {
		status = promotion.StatusRejected
		message = record.Comment
	}
	promotionRow, err := queries.UpdatePromotionDecisionState(
		ctx,
		storedb.UpdatePromotionDecisionStateParams{
			Status:    string(status),
			Message:   message,
			DecidedAt: databaseTime(record.DecidedAt),
			ID:        databaseUUID(record.PromotionID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.Decision{}, promotion.ErrTransitionConflict
	}
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("update Promotion decision state: %w", err)
	}

	actor, err := json.Marshal(record.DecidedBySnapshot)
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("encode Promotion decision actor: %w", err)
	}
	decisionRow, err := queries.InsertPromotionDecision(
		ctx,
		storedb.InsertPromotionDecisionParams{
			PromotionID:          databaseUUID(record.PromotionID),
			Decision:             string(record.Decision),
			Comment:              record.Comment,
			DecidedByPrincipalID: databaseUUID(record.DecidedByPrincipalID),
			DecidedBySnapshot:    actor,
			DecidedAt:            databaseTime(record.DecidedAt),
		},
	)
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("insert Promotion decision: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("commit Promotion decision: %w", err)
	}

	updated, err := promotionFromRow(promotionRow)
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, err
	}
	decision, err := promotionDecisionFromRow(decisionRow)
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, err
	}

	return updated, decision, nil
}

func finishExistingDecision(
	ctx context.Context,
	queries *storedb.Queries,
	transaction pgx.Tx,
	row storedb.PromotionDecision,
	requested promotion.Decision,
) (promotion.Promotion, promotion.Decision, error) {
	existing, err := promotionDecisionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, err
	}
	if existing.Decision != requested.Decision || existing.DecidedByPrincipalID != requested.DecidedByPrincipalID ||
		!optionalStringsEqual(existing.Comment, requested.Comment) {
		return promotion.Promotion{}, promotion.Decision{}, promotion.ErrDecisionConflict
	}
	promotionRow, err := queries.GetPromotion(ctx, databaseUUID(requested.PromotionID))
	if err != nil {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("get decided Promotion: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.Promotion{}, promotion.Decision{}, fmt.Errorf("commit repeated Promotion decision: %w", err)
	}

	current, err := promotionFromRow(promotionRow)
	return current, existing, err
}

func stageDatabaseParams(record promotion.Stage) (storedb.UpsertPromotionStageParams, error) {
	params := storedb.UpsertPromotionStageParams{
		ID:              databaseUUID(record.ID),
		Integration:     record.Integration,
		ClusterName:     record.ClusterName,
		Project:         record.Project,
		Name:            record.Name,
		KubernetesUid:   record.KubernetesUID,
		ResourceVersion: record.ResourceVersion,
		Application:     record.Application,
		Environment:     record.Environment,
		RepositoryName:  record.RepositoryName,
		RepositoryUrl:   record.RepositoryURL,
		TargetBranch:    record.TargetBranch,
		SourcePaths:     record.SourcePaths,
		FirstObservedAt: databaseTime(record.FirstObservedAt),
		LastObservedAt:  databaseTime(record.LastObservedAt),
		ArchivedAt:      optionalDatabaseTime(record.ArchivedAt),
	}
	if err := encodePromotionStageJSON(record, &params); err != nil {
		return storedb.UpsertPromotionStageParams{}, err
	}

	return params, nil
}

func promotionDatabaseParams(
	record promotion.Promotion,
	requestedBy []byte,
) (storedb.CreatePromotionParams, error) {
	params := storedb.CreatePromotionParams{
		ID:                           databaseUUID(record.ID),
		Source:                       string(record.Source),
		Integration:                  record.Integration,
		ExternalPromotionID:          record.ExternalPromotionID,
		RequestDigest:                nullablePromotionRequestDigest(record.RequestDigest),
		StageID:                      databaseUUID(record.StageID),
		SourceStageID:                optionalDatabaseUUID(record.SourceStageID),
		RepositoryID:                 databaseUUID(record.RepositoryID),
		BaseRevision:                 optionalString(record.BaseRevision),
		CandidateRevision:            optionalString(record.CandidateRevision),
		CandidateBranch:              optionalString(record.CandidateBranch),
		TargetBranch:                 record.TargetBranch,
		ReviewRequired:               record.ReviewRequired,
		Status:                       string(record.Status),
		Message:                      record.Message,
		DispatchAttemptCount:         int32(record.DispatchAttemptCount),
		NextDispatchAt:               optionalDatabaseTime(record.NextDispatchAt),
		LastDispatchAt:               optionalDatabaseTime(record.LastDispatchAt),
		DispatchedAt:                 optionalDatabaseTime(record.DispatchedAt),
		KargoPromotionUid:            record.KargoPromotionUID,
		PreparationRepositoryID:      optionalDatabaseUUID(record.PreparationRepositoryID),
		PreparationCatalogRevisionID: optionalDatabaseUUID(record.PreparationCatalogRevisionID),
		PreparationRunID:             optionalDatabaseUUID(record.PreparationRunID),
		GitDiffArtifactID:            optionalDatabaseUUID(record.GitDiffArtifactID),
		ArgoDiffArtifactID:           optionalDatabaseUUID(record.ArgoDiffArtifactID),
		RequestedByPrincipalID:       databaseUUID(record.RequestedByPrincipalID),
		RequestedBySnapshot:          requestedBy,
		CreatedAt:                    databaseTime(record.CreatedAt),
		PreparationCompletedAt:       optionalDatabaseTime(record.PreparationCompletedAt),
		DecidedAt:                    optionalDatabaseTime(record.DecidedAt),
		ExpiresAt:                    optionalDatabaseTime(record.ExpiresAt),
		UpdatedAt:                    databaseTime(record.UpdatedAt),
	}
	if err := encodePromotionSnapshots(record, &params); err != nil {
		return storedb.CreatePromotionParams{}, err
	}

	return params, nil
}

func promotionStageFromRow(row storedb.PromotionStage) (promotion.Stage, error) {
	record := promotion.Stage{
		ID:              uuid.UUID(row.ID.Bytes),
		Integration:     row.Integration,
		ClusterName:     row.ClusterName,
		Project:         row.Project,
		Name:            row.Name,
		KubernetesUID:   row.KubernetesUid,
		ResourceVersion: row.ResourceVersion,
		Application:     row.Application,
		Environment:     row.Environment,
		RepositoryName:  row.RepositoryName,
		RepositoryURL:   row.RepositoryUrl,
		TargetBranch:    row.TargetBranch,
		SourcePaths:     row.SourcePaths,
		FirstObservedAt: row.FirstObservedAt.Time.UTC(),
		LastObservedAt:  row.LastObservedAt.Time.UTC(),
		ArchivedAt:      applicationTime(row.ArchivedAt),
	}
	if err := decodePromotionStageJSON(row, &record); err != nil {
		return promotion.Stage{}, err
	}

	return record, record.Validate()
}

func promotionStagePolicyFromRow(row storedb.PromotionStagePolicy) (promotion.StagePolicy, error) {
	actor, err := decodeActor(row.UpdatedBySnapshot)
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("decode promotion policy actor: %w", err)
	}
	record := promotion.StagePolicy{
		StageID:              uuid.UUID(row.StageID.Bytes),
		Mode:                 promotion.PolicyMode(row.Mode),
		ExpiresAt:            applicationTime(row.ExpiresAt),
		Reason:               row.Reason,
		UpdatedByPrincipalID: uuid.UUID(row.UpdatedByPrincipalID.Bytes),
		UpdatedBySnapshot:    actor,
		UpdatedAt:            row.UpdatedAt.Time.UTC(),
	}

	return record, record.Validate()
}

func stageSnapshotFromRow(row storedb.KargoStageSnapshot) promotion.StageSnapshot {
	return promotion.StageSnapshot{
		SnapshotID:  uuid.UUID(row.SnapshotID.Bytes),
		Integration: row.Integration,
		ClusterName: row.ClusterName,
		ObservedAt:  row.ObservedAt.Time.UTC(),
		AcceptedAt:  row.AcceptedAt.Time.UTC(),
	}
}

func promotionFromRow(row storedb.Promotion) (promotion.Promotion, error) {
	requestedBy, err := decodeActor(row.RequestedBySnapshot)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("decode Promotion requesting actor: %w", err)
	}
	digest, err := promotionRequestDigest(row.RequestDigest)
	if err != nil {
		return promotion.Promotion{}, err
	}
	record := promotion.Promotion{
		ID:                           uuid.UUID(row.ID.Bytes),
		Source:                       promotion.PromotionSource(row.Source),
		Integration:                  row.Integration,
		ExternalPromotionID:          row.ExternalPromotionID,
		RequestDigest:                digest,
		StageID:                      uuid.UUID(row.StageID.Bytes),
		SourceStageID:                applicationUUID(row.SourceStageID),
		RepositoryID:                 uuid.UUID(row.RepositoryID.Bytes),
		BaseRevision:                 stringValue(row.BaseRevision),
		CandidateRevision:            stringValue(row.CandidateRevision),
		CandidateBranch:              stringValue(row.CandidateBranch),
		TargetBranch:                 row.TargetBranch,
		ReviewRequired:               row.ReviewRequired,
		Status:                       promotion.PromotionStatus(row.Status),
		Message:                      row.Message,
		DispatchAttemptCount:         int(row.DispatchAttemptCount),
		NextDispatchAt:               applicationTime(row.NextDispatchAt),
		LastDispatchAt:               applicationTime(row.LastDispatchAt),
		DispatchedAt:                 applicationTime(row.DispatchedAt),
		KargoPromotionUID:            row.KargoPromotionUid,
		NextReconciliationAt:         applicationTime(row.NextReconciliationAt),
		LastReconciliationAt:         applicationTime(row.LastReconciliationAt),
		PreparationRepositoryID:      applicationUUID(row.PreparationRepositoryID),
		PreparationCatalogRevisionID: applicationUUID(row.PreparationCatalogRevisionID),
		PreparationRunID:             applicationUUID(row.PreparationRunID),
		PreparationFailureCount:      int(row.PreparationFailureCount),
		NextPreparationAttemptAt:     applicationTime(row.NextPreparationAttemptAt),
		GitDiffArtifactID:            applicationUUID(row.GitDiffArtifactID),
		ArgoDiffArtifactID:           applicationUUID(row.ArgoDiffArtifactID),
		RequestedByPrincipalID:       uuid.UUID(row.RequestedByPrincipalID.Bytes),
		RequestedBySnapshot:          requestedBy,
		CreatedAt:                    row.CreatedAt.Time.UTC(),
		PreparationCompletedAt:       applicationTime(row.PreparationCompletedAt),
		DecidedAt:                    applicationTime(row.DecidedAt),
		ExpiresAt:                    applicationTime(row.ExpiresAt),
		UpdatedAt:                    row.UpdatedAt.Time.UTC(),
	}
	if err := decodePromotionSnapshots(row, &record); err != nil {
		return promotion.Promotion{}, err
	}

	return record, record.Validate()
}

func promotionDecisionFromRow(row storedb.PromotionDecision) (promotion.Decision, error) {
	actor, err := decodeActor(row.DecidedBySnapshot)
	if err != nil {
		return promotion.Decision{}, fmt.Errorf("decode Promotion decision actor: %w", err)
	}
	record := promotion.Decision{
		PromotionID:          uuid.UUID(row.PromotionID.Bytes),
		Decision:             promotion.DecisionValue(row.Decision),
		Comment:              row.Comment,
		DecidedByPrincipalID: uuid.UUID(row.DecidedByPrincipalID.Bytes),
		DecidedBySnapshot:    actor,
		DecidedAt:            row.DecidedAt.Time.UTC(),
	}

	return record, record.Validate()
}

func decodeActor(encoded []byte) (productidentity.ActorSnapshot, error) {
	var actor productidentity.ActorSnapshot
	if err := json.Unmarshal(encoded, &actor); err != nil {
		return productidentity.ActorSnapshot{}, err
	}

	return actor, actor.Validate()
}

func promotionRequestDigest(value []byte) (promotion.RequestDigest, error) {
	var digest promotion.RequestDigest
	if len(value) == 0 {
		return digest, nil
	}
	if len(value) != len(digest) {
		return digest, fmt.Errorf("decode Promotion request digest: expected %d bytes, got %d", len(digest), len(value))
	}
	copy(digest[:], value)

	return digest, nil
}

func nullablePromotionRequestDigest(value promotion.RequestDigest) []byte {
	if value == (promotion.RequestDigest{}) {
		return nil
	}

	return value[:]
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func applicationUUID(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}

	converted := uuid.UUID(value.Bytes)
	return &converted
}

func optionalStringsEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
}
