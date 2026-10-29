package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func recordRepositoryRefreshAttempt(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	result repository.RefreshResult,
) error {
	attempt := result.Attempt(uuid.UUID(repositoryID.Bytes))
	err := queries.CreateRepositoryRefreshAttempt(ctx, storedb.CreateRepositoryRefreshAttemptParams{
		ID:                   databaseUUID(attempt.ID),
		RepositoryID:         repositoryID,
		Source:               string(attempt.Source),
		Outcome:              string(attempt.Outcome),
		StartedAt:            databaseTime(attempt.StartedAt),
		DurationMilliseconds: attempt.Duration.Milliseconds(),
		CommitSha:            attempt.CommitSHA,
		Diagnostic:           attempt.Diagnostic,
	})
	if err != nil {
		return fmt.Errorf("record repository refresh attempt: %w", err)
	}

	err = queries.PruneRepositoryRefreshAttempts(ctx, storedb.PruneRepositoryRefreshAttemptsParams{
		RepositoryID: repositoryID,
		RetainCount:  repository.RefreshAttemptRetentionLimit,
	})
	if err != nil {
		return fmt.Errorf("prune repository refresh attempts: %w", err)
	}

	return nil
}

func (d *Database) ListRepositoryRefreshAttempts(
	ctx context.Context,
	repositoryID uuid.UUID,
	limit int,
	offset int,
) (repository.RefreshAttemptPage, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 2_147_483_647 {
		return repository.RefreshAttemptPage{}, errors.New("invalid repository refresh attempt list window")
	}

	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return repository.RefreshAttemptPage{}, fmt.Errorf("begin repository refresh attempt read: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)
	databaseRepositoryID := databaseUUID(repositoryID)

	total, err := queries.CountRepositoryRefreshAttempts(ctx, databaseRepositoryID)
	if err != nil {
		return repository.RefreshAttemptPage{}, fmt.Errorf("count repository refresh attempts: %w", err)
	}
	rows, err := queries.ListRepositoryRefreshAttempts(ctx, storedb.ListRepositoryRefreshAttemptsParams{
		RepositoryID: databaseRepositoryID,
		ResultLimit:  int32(limit),
		ResultOffset: int32(offset),
	})
	if err != nil {
		return repository.RefreshAttemptPage{}, fmt.Errorf("list repository refresh attempts: %w", err)
	}

	page := repository.RefreshAttemptPage{
		Items: make([]repository.RefreshAttempt, 0, len(rows)),
		Total: int(total),
	}
	for _, row := range rows {
		page.Items = append(page.Items, repository.RefreshAttempt{
			ID:           uuid.UUID(row.ID.Bytes),
			RepositoryID: uuid.UUID(row.RepositoryID.Bytes),
			Source:       repository.RefreshSource(row.Source),
			Outcome:      repository.RefreshOutcome(row.Outcome),
			StartedAt:    row.StartedAt.Time.UTC(),
			Duration:     time.Duration(row.DurationMilliseconds) * time.Millisecond,
			CommitSHA:    row.CommitSha,
			Diagnostic:   row.Diagnostic,
		})
	}

	if err := transaction.Commit(ctx); err != nil {
		return repository.RefreshAttemptPage{}, fmt.Errorf("commit repository refresh attempt read: %w", err)
	}

	return page, nil
}
