package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var (
	_ run.CreationStore     = (*Database)(nil)
	_ run.HistoryStore      = (*Database)(nil)
	_ run.AssignmentStore   = (*Database)(nil)
	_ run.CancellationStore = (*Database)(nil)
	_ run.ReviewStore       = (*Database)(nil)
	_ run.ObservationStore  = (*Database)(nil)
)

func (d *Database) GetRun(ctx context.Context, runID uuid.UUID) (run.Run, error) {
	row, err := d.queries.GetRun(ctx, databaseUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrRunNotFound
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get Run: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func (d *Database) ListRuns(ctx context.Context, filter run.RunListFilter) (run.RunList, error) {
	var repositoryID pgtype.UUID
	if filter.RepositoryID != nil {
		repositoryID = databaseUUID(*filter.RepositoryID)
	}

	var commitSHA *string
	if filter.CommitSHA != "" {
		commitSHA = &filter.CommitSHA
	}
	var clusterName *string
	if filter.ClusterName != "" {
		clusterName = &filter.ClusterName
	}
	var phase *string
	if filter.Phase != "" {
		value := string(filter.Phase)
		phase = &value
	}

	query := storedb.ListRunsParams{
		RepositoryID:    repositoryID,
		TargetID:        optionalDatabaseUUID(filter.TargetID),
		CommitSha:       commitSHA,
		InputArtifactID: optionalDatabaseUUID(filter.InputArtifactID),
		ClusterName:     clusterName,
		Phase:           phase,
		ResultLimit:     filter.Limit,
		ResultOffset:    filter.Offset,
	}
	rows, err := d.queries.ListRuns(
		ctx,
		query,
	)
	if err != nil {
		return run.RunList{}, fmt.Errorf("list Runs: %w", err)
	}
	total, err := d.queries.CountRuns(ctx, storedb.CountRunsParams{
		RepositoryID:    repositoryID,
		TargetID:        optionalDatabaseUUID(filter.TargetID),
		CommitSha:       commitSHA,
		InputArtifactID: optionalDatabaseUUID(filter.InputArtifactID),
		ClusterName:     clusterName,
		Phase:           phase,
	})
	if err != nil {
		return run.RunList{}, fmt.Errorf("count Runs: %w", err)
	}

	records := make([]run.Run, 0, len(rows))
	for _, row := range rows {
		record, err := runFromRow(row)
		if err != nil {
			return run.RunList{}, err
		}

		records = append(records, record)
	}

	return run.RunList{Items: records, Total: total}, nil
}

func (d *Database) GetArtifactInputCandidate(ctx context.Context, artifactID uuid.UUID) (run.ArtifactInput, error) {
	row, err := d.queries.GetArtifactInputCandidate(ctx, databaseUUID(artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ArtifactInput{}, run.ErrArtifactNotFound
	}
	if err != nil {
		return run.ArtifactInput{}, fmt.Errorf("get artifact input candidate: %w", err)
	}
	if row.State != "sealed" || row.Sha256 == nil {
		return run.ArtifactInput{}, fmt.Errorf("%w: artifact is not sealed", run.ErrInvalidInputs)
	}

	return run.ArtifactInput{
		ArtifactID:               uuid.UUID(row.ArtifactID.Bytes),
		ProducerRunID:            uuid.UUID(row.ProducerRunID.Bytes),
		Name:                     row.Name,
		MediaType:                row.MediaType,
		SizeBytes:                row.SizeBytes,
		SHA256:                   *row.Sha256,
		Sensitive:                row.Sensitive,
		RetentionState:           row.RetentionState,
		TargetID:                 uuid.UUID(row.TargetID.Bytes),
		RepositoryID:             uuid.UUID(row.RepositoryID.Bytes),
		CatalogRevisionID:        uuid.UUID(row.CatalogRevisionID.Bytes),
		CommitSHA:                row.CommitSha,
		TargetRelevanceCommitSHA: cloneOptionalString(row.TargetRelevanceCommitSha),
		TargetName:               row.TargetName,
		ActionName:               row.ActionName,
	}, nil
}

func (d *Database) GetRunByIdempotencyKey(
	ctx context.Context,
	principalID *uuid.UUID,
	sourceType run.SourceType,
	idempotencyKey string,
) (run.Run, error) {
	row, err := d.queries.GetRunByIdempotencyKey(
		ctx,
		storedb.GetRunByIdempotencyKeyParams{
			IdempotencyKey: &idempotencyKey,
			PrincipalID:    optionalDatabaseUUID(principalID),
			SourceType:     string(sourceType),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrRunNotFound
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get run by idempotency key: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func (d *Database) CreateRun(ctx context.Context, record run.Run) (run.Run, error) {
	source, err := json.Marshal(record.Source)
	if err != nil {
		return run.Run{}, fmt.Errorf("marshal run source: %w", err)
	}

	parameters, err := json.Marshal(record.Parameters)
	if err != nil {
		return run.Run{}, fmt.Errorf("marshal run parameters: %w", err)
	}

	snapshot, err := json.Marshal(record.Snapshot)
	if err != nil {
		return run.Run{}, fmt.Errorf("marshal run snapshot: %w", err)
	}

	createdByPrincipalID, createdBySnapshot, err := actorDatabaseFields(record.CreatedBy)
	if err != nil {
		return run.Run{}, fmt.Errorf("encode Run creator: %w", err)
	}
	if record.Review.State != run.ReviewStateNotRequired && record.Review.State != run.ReviewStatePending {
		return run.Run{}, fmt.Errorf("%w: new Run must have an initial review state", run.ErrInvalidRequest)
	}
	if record.Review.DecidedBy != nil || record.Review.DecidedAt != nil {
		return run.Run{}, fmt.Errorf("%w: new Run must not have a review decision", run.ErrInvalidRequest)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin run transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	if record.Source.Type == run.SourceTypeGit {
		superseded, err := supersedeStaleGitRun(ctx, queries, record)
		if err != nil {
			return run.Run{}, err
		}
		if superseded {
			if err := transaction.Commit(ctx); err != nil {
				return run.Run{}, fmt.Errorf("commit stale Git-trigger delivery: %w", err)
			}

			return run.Run{}, run.ErrGitRunSuperseded
		}
	}
	row, err := queries.CreateRun(
		ctx,
		storedb.CreateRunParams{
			ID:                       databaseUUID(record.ID),
			Phase:                    string(record.Phase),
			TargetID:                 databaseUUID(record.TargetID),
			RepositoryID:             databaseUUID(record.RepositoryID),
			CatalogRevisionID:        databaseUUID(record.CatalogRevisionID),
			CommitSha:                record.CommitSHA,
			TargetRelevanceCommitSha: cloneOptionalString(record.TargetRelevanceCommitSHA),
			TargetName:               record.TargetName,
			ActionName:               record.ActionName,
			Parameters:               parameters,
			PlacementName:            record.PlacementName,
			ClusterName:              record.ClusterName,
			Source:                   source,
			IdempotencyKey:           record.IdempotencyKey,
			RequestDigest:            record.RequestDigest,
			RerunOfRunID:             optionalDatabaseUUID(record.RerunOfRunID),
			RerunMode:                optionalRerunMode(record.RerunMode),
			Snapshot:                 snapshot,
			CreatedByPrincipalID:     createdByPrincipalID,
			CreatedBySnapshot:        createdBySnapshot,
			ReviewState:              string(record.Review.State),
			CreatedAt: pgtype.Timestamptz{
				Time:  record.CreatedAt,
				Valid: true,
			},
		},
	)
	if errors.Is(err, pgx.ErrNoRows) && record.IdempotencyKey != nil {
		return existingIdempotentRun(ctx, queries, transaction, record)
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("insert run: %w", err)
	}

	for _, inputName := range slices.Sorted(maps.Keys(record.Snapshot.Inputs)) {
		input := record.Snapshot.Inputs[inputName]
		rowsAffected, err := queries.CreateRunArtifactInput(
			ctx,
			storedb.CreateRunArtifactInputParams{
				RunID:      databaseUUID(record.ID),
				InputName:  inputName,
				ArtifactID: databaseUUID(input.ArtifactID),
			},
		)
		if err != nil {
			return run.Run{}, fmt.Errorf("record Run artifact input %q: %w", inputName, err)
		}
		if rowsAffected != 1 {
			return run.Run{}, fmt.Errorf(
				"%w: artifact for input %q is no longer retained",
				run.ErrInvalidInputs,
				inputName,
			)
		}
	}

	if err := queries.CreateRunPhaseTransition(
		ctx,
		storedb.CreateRunPhaseTransitionParams{
			RunID: databaseUUID(record.ID),
			Phase: string(record.Phase),
			RecordedAt: pgtype.Timestamptz{
				Time:  record.CreatedAt,
				Valid: true,
			},
		},
	); err != nil {
		return run.Run{}, fmt.Errorf("record initial Run phase: %w", err)
	}

	if err := linkGitTriggerRun(ctx, queries, record); err != nil {
		return run.Run{}, err
	}
	if err := linkPromotionPreparationRun(ctx, queries, record, createdBySnapshot); err != nil {
		return run.Run{}, err
	}

	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit run transaction: %w", err)
	}

	created, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return created, nil
}

func existingIdempotentRun(
	ctx context.Context,
	queries *storedb.Queries,
	transaction pgx.Tx,
	record run.Run,
) (run.Run, error) {
	row, err := queries.GetRunByIdempotencyKey(
		ctx,
		storedb.GetRunByIdempotencyKeyParams{
			IdempotencyKey: record.IdempotencyKey,
			PrincipalID:    actorDatabasePrincipalID(record.CreatedBy),
			SourceType:     string(record.Source.Type),
		},
	)
	if err != nil {
		return run.Run{}, fmt.Errorf("get idempotent run: %w", err)
	}

	if row.RequestDigest != record.RequestDigest {
		return run.Run{}, run.ErrIdempotencyConflict
	}

	existing, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit idempotent run transaction: %w", err)
	}

	return existing, nil
}

func runFromRow(row storedb.Run) (run.Run, error) {
	var parameters map[string]any
	if err := json.Unmarshal(row.Parameters, &parameters); err != nil {
		return run.Run{}, fmt.Errorf("decode run parameters: %w", err)
	}

	var source run.Source
	if err := json.Unmarshal(row.Source, &source); err != nil {
		return run.Run{}, fmt.Errorf("decode run source: %w", err)
	}

	var snapshot run.Snapshot
	if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil {
		return run.Run{}, fmt.Errorf("decode run snapshot: %w", err)
	}

	createdBy, err := actorFromDatabase(row.CreatedByPrincipalID, row.CreatedBySnapshot)
	if err != nil {
		return run.Run{}, fmt.Errorf("decode Run creator: %w", err)
	}
	canceledBy, err := actorFromDatabase(row.CanceledByPrincipalID, row.CanceledBySnapshot)
	if err != nil {
		return run.Run{}, fmt.Errorf("decode Run canceler: %w", err)
	}
	reviewDecidedBy, err := actorFromDatabase(row.ReviewDecidedByPrincipalID, row.ReviewDecidedBySnapshot)
	if err != nil {
		return run.Run{}, fmt.Errorf("decode Run review actor: %w", err)
	}

	return run.Run{
		ID:                       uuid.UUID(row.ID.Bytes),
		Phase:                    run.Phase(row.Phase),
		Outcome:                  runOutcome(row.Outcome),
		TargetID:                 uuid.UUID(row.TargetID.Bytes),
		RepositoryID:             uuid.UUID(row.RepositoryID.Bytes),
		CatalogRevisionID:        uuid.UUID(row.CatalogRevisionID.Bytes),
		CommitSHA:                row.CommitSha,
		TargetRelevanceCommitSHA: cloneOptionalString(row.TargetRelevanceCommitSha),
		TargetName:               row.TargetName,
		ActionName:               row.ActionName,
		Parameters:               parameters,
		PlacementName:            row.PlacementName,
		ClusterName:              row.ClusterName,
		Source:                   source,
		IdempotencyKey:           row.IdempotencyKey,
		RequestDigest:            row.RequestDigest,
		RerunOfRunID:             applicationUUID(row.RerunOfRunID),
		RerunMode:                applicationRerunMode(row.RerunMode),
		Snapshot:                 snapshot,
		CreatedBy:                createdBy,
		CanceledBy:               canceledBy,
		Review: run.Review{
			State:     run.ReviewState(row.ReviewState),
			DecidedBy: reviewDecidedBy,
			DecidedAt: applicationTime(row.ReviewDecidedAt),
		},
		CreatedAt:               row.CreatedAt.Time.UTC(),
		AssignedAt:              applicationTime(row.AssignedAt),
		PreparingAt:             applicationTime(row.PreparingAt),
		StartedAt:               applicationTime(row.StartedAt),
		FinishedAt:              applicationTime(row.FinishedAt),
		LastObservedAt:          applicationTime(row.LastObservedAt),
		CancellationRequestedAt: applicationTime(row.CancellationRequestedAt),
		JobUID:                  row.JobUid,
		OutcomeReason:           row.OutcomeReason,
		OutcomeMessage:          row.OutcomeMessage,
		ExitCode:                row.ExitCode,
	}, nil
}

func optionalRerunMode(value *run.RerunMode) *string {
	if value == nil {
		return nil
	}

	converted := string(*value)

	return &converted
}

func applicationRerunMode(value *string) *run.RerunMode {
	if value == nil {
		return nil
	}

	converted := run.RerunMode(*value)

	return &converted
}

func actorDatabaseFields(actor *productidentity.ActorSnapshot) (pgtype.UUID, []byte, error) {
	if actor == nil {
		return pgtype.UUID{}, nil, nil
	}
	if err := actor.Validate(); err != nil {
		return pgtype.UUID{}, nil, err
	}

	encoded, err := json.Marshal(actor)
	if err != nil {
		return pgtype.UUID{}, nil, err
	}

	return databaseUUID(actor.PrincipalID), encoded, nil
}

func actorDatabasePrincipalID(actor *productidentity.ActorSnapshot) pgtype.UUID {
	if actor == nil {
		return pgtype.UUID{}
	}

	return databaseUUID(actor.PrincipalID)
}

func actorFromDatabase(principalID pgtype.UUID, encoded []byte) (*productidentity.ActorSnapshot, error) {
	if !principalID.Valid && encoded == nil {
		return nil, nil
	}
	if !principalID.Valid || encoded == nil {
		return nil, errors.New("principal reference and snapshot must both be present")
	}

	var actor productidentity.ActorSnapshot
	if err := json.Unmarshal(encoded, &actor); err != nil {
		return nil, err
	}
	if actor.PrincipalID != uuid.UUID(principalID.Bytes) {
		return nil, errors.New("principal reference does not match actor snapshot")
	}
	if err := actor.Validate(); err != nil {
		return nil, err
	}

	return &actor, nil
}

func runOutcome(value *string) *run.Outcome {
	if value == nil {
		return nil
	}

	converted := run.Outcome(*value)

	return &converted
}
