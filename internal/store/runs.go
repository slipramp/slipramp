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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ run.Store = (*Database)(nil)

func (d *Database) GetRunByIdempotencyKey(
	ctx context.Context,
	sourceType run.SourceType,
	idempotencyKey string,
) (run.Run, error) {
	row, err := d.queries.GetRunByIdempotencyKey(
		ctx,
		storedb.GetRunByIdempotencyKeyParams{
			SourceType:     string(sourceType),
			IdempotencyKey: &idempotencyKey,
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

func (d *Database) CreateRun(
	ctx context.Context,
	record run.Run,
) (run.Run, error) {
	parameters, err := json.Marshal(record.Parameters)
	if err != nil {
		return run.Run{}, fmt.Errorf("marshal run parameters: %w", err)
	}

	snapshot, err := json.Marshal(record.Snapshot)
	if err != nil {
		return run.Run{}, fmt.Errorf("marshal run snapshot: %w", err)
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
			ID:                databaseUUID(record.ID),
			Phase:             string(record.Phase),
			RepositoryID:      databaseUUID(record.RepositoryID),
			CatalogRevisionID: databaseUUID(record.CatalogRevisionID),
			CommitSha:         record.CommitSHA,
			TargetName:        record.TargetName,
			ActionName:        record.ActionName,
			Parameters:        parameters,
			PlacementName:     record.PlacementName,
			ClusterName:       record.ClusterName,
			SourceType:        string(record.Source.Type),
			IdempotencyKey:    record.IdempotencyKey,
			RequestDigest:     record.RequestDigest,
			Snapshot:          snapshot,
			CreatedAt: pgtype.Timestamptz{
				Time:  record.CreatedAt,
				Valid: true,
			},
		},
	)
	if errors.Is(err, pgx.ErrNoRows) && record.IdempotencyKey != nil {
		return existingIdempotentRun(
			ctx,
			queries,
			transaction,
			record,
		)
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("insert run: %w", err)
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

func (d *Database) GetAssignedRunForCluster(
	ctx context.Context,
	clusterName string,
) (run.Run, error) {
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

func (d *Database) ClaimQueuedRun(
	ctx context.Context,
	clusterName string,
	claimedAt time.Time,
) (run.Run, error) {
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
		!sameFinishedOutcome(currentPhase, current.Outcome, observation.Outcome) {
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

func sameJob(current, observed *string) bool {
	return current == nil || observed != nil && *current == *observed
}

func sameFinishedOutcome(
	currentPhase run.Phase,
	currentOutcome *string,
	observedOutcome *run.Outcome,
) bool {
	if currentPhase != run.PhaseFinished {
		return true
	}

	return currentOutcome != nil && observedOutcome != nil &&
		*currentOutcome == string(*observedOutcome)
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
			SourceType:     string(record.Source.Type),
			IdempotencyKey: record.IdempotencyKey,
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

	return run.Run{
		ID:                uuid.UUID(row.ID.Bytes),
		Phase:             run.Phase(row.Phase),
		Outcome:           runOutcome(row.Outcome),
		RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
		CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
		CommitSHA:         row.CommitSha,
		TargetName:        row.TargetName,
		ActionName:        row.ActionName,
		Parameters:        parameters,
		PlacementName:     row.PlacementName,
		ClusterName:       row.ClusterName,
		Source:            run.Source{Type: run.SourceType(row.SourceType)},
		IdempotencyKey:    row.IdempotencyKey,
		RequestDigest:     row.RequestDigest,
		Snapshot:          snapshot,
		CreatedAt:         row.CreatedAt.Time.UTC(),
		AssignedAt:        applicationTime(row.AssignedAt),
		PreparingAt:       applicationTime(row.PreparingAt),
		StartedAt:         applicationTime(row.StartedAt),
		FinishedAt:        applicationTime(row.FinishedAt),
		LastObservedAt:    applicationTime(row.LastObservedAt),
		CancellationRequestedAt: applicationTime(
			row.CancellationRequestedAt,
		),
		JobUID:         row.JobUid,
		OutcomeReason:  row.OutcomeReason,
		OutcomeMessage: row.OutcomeMessage,
		ExitCode:       row.ExitCode,
	}, nil
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
