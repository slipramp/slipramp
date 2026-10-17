package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
			State:             string(record.State),
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

	if err := queries.CreateRunStateTransition(
		ctx,
		storedb.CreateRunStateTransitionParams{
			RunID: databaseUUID(record.ID),
			State: string(record.State),
			RecordedAt: pgtype.Timestamptz{
				Time:  record.CreatedAt,
				Valid: true,
			},
		},
	); err != nil {
		return run.Run{}, fmt.Errorf("record initial run state: %w", err)
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

func (d *Database) GetPreparingRunForCluster(
	ctx context.Context,
	clusterName string,
) (run.Run, error) {
	row, err := d.queries.GetPreparingRunForCluster(ctx, clusterName)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get preparing Run for cluster: %w", err)
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
	row, err := queries.ClaimQueuedRun(ctx, clusterName)
	if errors.Is(err, pgx.ErrNoRows) || isTargetConcurrencyConflict(err) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("claim queued Run: %w", err)
	}

	if err := queries.CreateRunStateTransition(
		ctx,
		storedb.CreateRunStateTransitionParams{
			RunID: databaseUUID(uuid.UUID(row.ID.Bytes)),
			State: string(run.StatePreparing),
			RecordedAt: pgtype.Timestamptz{
				Time:  claimedAt,
				Valid: true,
			},
		},
	); err != nil {
		return run.Run{}, fmt.Errorf("record preparing Run state: %w", err)
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
		State:             run.State(row.State),
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
	}, nil
}

func databaseUUID(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: true}
}
