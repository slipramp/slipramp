package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ run.Store = (*Database)(nil)

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

func (d *Database) ListRuns(ctx context.Context, filter run.ListFilter) ([]run.Run, error) {
	var repositoryID pgtype.UUID
	if filter.RepositoryID != nil {
		repositoryID = databaseUUID(*filter.RepositoryID)
	}

	var targetName *string
	if filter.TargetName != "" {
		targetName = &filter.TargetName
	}

	rows, err := d.queries.ListRuns(
		ctx,
		storedb.ListRunsParams{
			RepositoryID: repositoryID,
			TargetName:   targetName,
			ResultLimit:  filter.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list Runs: %w", err)
	}

	records := make([]run.Run, 0, len(rows))
	for _, row := range rows {
		record, err := runFromRow(row)
		if err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
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
		ArtifactID:        uuid.UUID(row.ArtifactID.Bytes),
		ProducerRunID:     uuid.UUID(row.ProducerRunID.Bytes),
		Name:              row.Name,
		MediaType:         row.MediaType,
		SizeBytes:         row.SizeBytes,
		SHA256:            *row.Sha256,
		Sensitive:         row.Sensitive,
		RetentionState:    row.RetentionState,
		RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
		CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
		CommitSHA:         row.CommitSha,
		TargetName:        row.TargetName,
		ActionName:        row.ActionName,
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

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin run transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	row, err := queries.CreateRun(
		ctx,
		storedb.CreateRunParams{
			ID:                   databaseUUID(record.ID),
			Phase:                string(record.Phase),
			RepositoryID:         databaseUUID(record.RepositoryID),
			CatalogRevisionID:    databaseUUID(record.CatalogRevisionID),
			CommitSha:            record.CommitSHA,
			TargetName:           record.TargetName,
			ActionName:           record.ActionName,
			Parameters:           parameters,
			PlacementName:        record.PlacementName,
			ClusterName:          record.ClusterName,
			SourceType:           string(record.Source.Type),
			IdempotencyKey:       record.IdempotencyKey,
			RequestDigest:        record.RequestDigest,
			Snapshot:             snapshot,
			CreatedByPrincipalID: createdByPrincipalID,
			CreatedBySnapshot:    createdBySnapshot,
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

	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit run transaction: %w", err)
	}

	created, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return created, nil
}

func (d *Database) CancelRun(
	ctx context.Context,
	runID uuid.UUID,
	requestedAt time.Time,
	canceledBy *productidentity.ActorSnapshot,
) (run.Run, error) {
	canceledByPrincipalID, canceledBySnapshot, err := actorDatabaseFields(canceledBy)
	if err != nil {
		return run.Run{}, fmt.Errorf("encode Run canceler: %w", err)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin cancel Run transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	current, err := queries.GetRunForCancel(ctx, databaseUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrRunNotFound
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get Run to cancel: %w", err)
	}

	if current.Phase == string(run.PhaseFinished) {
		if !current.CancellationRequestedAt.Valid {
			return run.Run{}, run.ErrRunFinished
		}

		return commitCancelResult(ctx, transaction, current)
	}
	if current.CancellationRequestedAt.Valid {
		return commitCancelResult(ctx, transaction, current)
	}

	updated, err := queries.CancelRun(
		ctx,
		storedb.CancelRunParams{
			RequestedAt:           databaseTime(requestedAt),
			CanceledByPrincipalID: canceledByPrincipalID,
			CanceledBySnapshot:    canceledBySnapshot,
			ID:                    databaseUUID(runID),
		},
	)
	if err != nil {
		return run.Run{}, fmt.Errorf("cancel Run: %w", err)
	}

	if current.Phase == string(run.PhaseQueued) {
		outcome := string(run.OutcomeCanceled)
		reason := run.ReasonCancelRequested
		if err := queries.CreateRunPhaseTransition(
			ctx,
			storedb.CreateRunPhaseTransitionParams{
				RunID:      databaseUUID(runID),
				Phase:      string(run.PhaseFinished),
				Outcome:    &outcome,
				Reason:     &reason,
				RecordedAt: databaseTime(requestedAt),
			},
		); err != nil {
			return run.Run{}, fmt.Errorf("record canceled Run phase: %w", err)
		}
	}

	return commitCancelResult(ctx, transaction, updated)
}

func (d *Database) GetRunToCancelForCluster(ctx context.Context, clusterName string) (run.Run, error) {
	row, err := d.queries.GetRunToCancelForCluster(ctx, clusterName)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNoRunToCancel
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get Run to cancel for cluster: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func (d *Database) GetAssignedRunForCluster(ctx context.Context, clusterName string) (run.Run, error) {
	row, err := d.queries.GetAssignedRunForCluster(ctx, clusterName)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get assigned Run for cluster: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func (d *Database) ClaimQueuedRun(ctx context.Context, clusterName string, claimedAt time.Time) (run.Run, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin Run claim transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	row, err := queries.ClaimQueuedRun(
		ctx,
		storedb.ClaimQueuedRunParams{
			AssignedAt:  databaseTime(claimedAt),
			ClusterName: clusterName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) || isTargetConcurrencyConflict(err) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("claim queued Run: %w", err)
	}

	if err := queries.CreateRunPhaseTransition(
		ctx,
		storedb.CreateRunPhaseTransitionParams{
			RunID:      databaseUUID(uuid.UUID(row.ID.Bytes)),
			Phase:      string(run.PhaseAssigned),
			RecordedAt: databaseTime(claimedAt),
		},
	); err != nil {
		return run.Run{}, fmt.Errorf("record assigned Run phase: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit Run claim transaction: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func (d *Database) ApplyRunObservation(
	ctx context.Context,
	observation run.Observation,
	allowedCurrentPhases []run.Phase,
) error {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Run observation transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	current, err := queries.GetRunObservationStateForUpdate(
		ctx,
		storedb.GetRunObservationStateForUpdateParams{
			ID:          databaseUUID(observation.ID),
			ClusterName: observation.ClusterName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ErrRunNotFound
	}
	if err != nil {
		return fmt.Errorf("get current Run phase: %w", err)
	}

	currentPhase := run.Phase(current.Phase)
	if !slices.Contains(allowedCurrentPhases, currentPhase) ||
		!sameJob(current.JobUid, observation.JobUID) ||
		!sameFinishedOutcome(currentPhase, current.Outcome, observation.Outcome) ||
		!validCanceledObservation(current.CancellationRequestedAt.Valid, observation.Outcome) {
		return run.ErrObservationConflict
	}

	if err := queries.UpdateRunObservation(
		ctx,
		storedb.UpdateRunObservationParams{
			Phase:      string(observation.Phase),
			Outcome:    outcomeString(observation.Outcome),
			JobUid:     observation.JobUID,
			ObservedAt: databaseTime(observation.ObservedAt),
			Reason:     observation.Reason,
			Message:    observation.Message,
			ExitCode:   observation.ExitCode,
			ID:         databaseUUID(observation.ID),
		},
	); err != nil {
		return fmt.Errorf("update Run observation: %w", err)
	}

	phaseChanged := currentPhase != observation.Phase
	if phaseChanged {
		err = queries.CreateRunPhaseTransition(
			ctx,
			storedb.CreateRunPhaseTransitionParams{
				RunID:      databaseUUID(observation.ID),
				Phase:      string(observation.Phase),
				Outcome:    outcomeString(observation.Outcome),
				Reason:     observation.Reason,
				Message:    observation.Message,
				ExitCode:   observation.ExitCode,
				RecordedAt: databaseTime(observation.ObservedAt),
			},
		)
		if err != nil {
			return fmt.Errorf("record Run phase transition: %w", err)
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Run observation transaction: %w", err)
	}

	return nil
}

func commitCancelResult(ctx context.Context, transaction pgx.Tx, row storedb.Run) (run.Run, error) {
	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit cancel Run transaction: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func sameJob(current, observed *string) bool {
	return current == nil || observed != nil && *current == *observed
}

func sameFinishedOutcome(currentPhase run.Phase, currentOutcome *string, observedOutcome *run.Outcome) bool {
	if currentPhase != run.PhaseFinished {
		return true
	}

	return currentOutcome != nil && observedOutcome != nil &&
		*currentOutcome == string(*observedOutcome)
}

func validCanceledObservation(cancellationRequested bool, observedOutcome *run.Outcome) bool {
	return observedOutcome == nil || *observedOutcome != run.OutcomeCanceled ||
		cancellationRequested
}

func outcomeString(value *run.Outcome) *string {
	if value == nil {
		return nil
	}

	converted := string(*value)

	return &converted
}

func isTargetConcurrencyConflict(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) &&
		postgresError.ConstraintName == "runs_one_active_per_target"
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

	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit idempotent run transaction: %w", err)
	}

	existing, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return existing, nil
}

func runFromRow(row storedb.Run) (run.Run, error) {
	var parameters map[string]any
	if err := json.Unmarshal(row.Parameters, &parameters); err != nil {
		return run.Run{}, fmt.Errorf("decode run parameters: %w", err)
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

	return run.Run{
		ID:                      uuid.UUID(row.ID.Bytes),
		Phase:                   run.Phase(row.Phase),
		Outcome:                 runOutcome(row.Outcome),
		RepositoryID:            uuid.UUID(row.RepositoryID.Bytes),
		CatalogRevisionID:       uuid.UUID(row.CatalogRevisionID.Bytes),
		CommitSHA:               row.CommitSha,
		TargetName:              row.TargetName,
		ActionName:              row.ActionName,
		Parameters:              parameters,
		PlacementName:           row.PlacementName,
		ClusterName:             row.ClusterName,
		Source:                  run.Source{Type: run.SourceType(row.SourceType)},
		IdempotencyKey:          row.IdempotencyKey,
		RequestDigest:           row.RequestDigest,
		Snapshot:                snapshot,
		CreatedBy:               createdBy,
		CanceledBy:              canceledBy,
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

func databaseTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func applicationTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}

	converted := value.Time.UTC()

	return &converted
}

func databaseUUID(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: true}
}

func optionalDatabaseUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}

	return databaseUUID(*value)
}
