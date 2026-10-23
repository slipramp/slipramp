package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const activeRepositoryNameConstraint = "repositories_active_name_key"

var _ repository.Store = (*Database)(nil)

func (d *Database) CreateRepository(ctx context.Context, record repository.Repository) (repository.Repository, error) {
	row, err := d.queries.CreateRepository(
		ctx,
		storedb.CreateRepositoryParams{
			ID: pgtype.UUID{
				Bytes: record.ID,
				Valid: true,
			},
			Name:                   record.Name,
			CloneUrl:               record.CloneURL,
			Branch:                 record.Branch,
			CommitIndexingMode:     string(record.CommitIndexing),
			CommitUrlTemplate:      record.CommitURLTemplate,
			RefreshCredentialName:  record.RefreshCredentialName,
			CheckoutCredentialName: record.CheckoutCredentialName,
			CreatedAt: pgtype.Timestamptz{
				Time:  record.CreatedAt,
				Valid: true,
			},
		},
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) &&
			postgresError.ConstraintName == activeRepositoryNameConstraint {
			return repository.Repository{}, repository.ErrNameConflict
		}

		return repository.Repository{}, fmt.Errorf("create repository: %w", err)
	}

	return repositoryFromCreateRow(row), nil
}

func (d *Database) ListRepositories(ctx context.Context) ([]repository.Repository, error) {
	rows, err := d.queries.ListRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}

	repositories := make([]repository.Repository, 0, len(rows))
	for _, row := range rows {
		repositories = append(repositories, repository.Repository{
			Name:                   row.Name,
			CloneURL:               row.CloneUrl,
			Branch:                 row.Branch,
			CommitIndexing:         repository.CommitIndexingMode(row.CommitIndexingMode),
			CommitURLTemplate:      row.CommitUrlTemplate,
			RefreshCredentialName:  row.RefreshCredentialName,
			CheckoutCredentialName: row.CheckoutCredentialName,
			ID:                     uuid.UUID(row.ID.Bytes),
			CreatedAt:              row.CreatedAt.Time.UTC(),
			Refresh: refreshState(
				row.RefreshStatus,
				row.ObservedCommitSha,
				row.LastRefreshAttemptedAt,
				row.LastRefreshSucceededAt,
				row.LastRefreshError,
			),
			ActiveCatalogCommitSHA: row.ActiveCatalogCommitSha,
		})
	}

	return repositories, nil
}

func (d *Database) GetRepository(ctx context.Context, repositoryID uuid.UUID) (repository.Repository, error) {
	row, err := d.queries.GetActiveRepository(ctx, pgtype.UUID{Bytes: repositoryID, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.Repository{}, repository.ErrNotFound
	}
	if err != nil {
		return repository.Repository{}, fmt.Errorf("get active repository: %w", err)
	}

	return repository.Repository{
		Name:                   row.Name,
		CloneURL:               row.CloneUrl,
		Branch:                 row.Branch,
		CommitIndexing:         repository.CommitIndexingMode(row.CommitIndexingMode),
		CommitURLTemplate:      row.CommitUrlTemplate,
		RefreshCredentialName:  row.RefreshCredentialName,
		CheckoutCredentialName: row.CheckoutCredentialName,
		ID:                     uuid.UUID(row.ID.Bytes),
		CreatedAt:              row.CreatedAt.Time.UTC(),
		Refresh: refreshState(
			row.RefreshStatus,
			row.ObservedCommitSha,
			row.LastRefreshAttemptedAt,
			row.LastRefreshSucceededAt,
			row.LastRefreshError,
		),
		ActiveCatalogCommitSHA: row.ActiveCatalogCommitSha,
	}, nil
}

func (d *Database) UpdateRepositoryCommitIndexing(
	ctx context.Context,
	repositoryID uuid.UUID,
	mode repository.CommitIndexingMode,
) (repository.Repository, error) {
	rowsAffected, err := d.queries.UpdateRepositoryCommitIndexing(
		ctx,
		storedb.UpdateRepositoryCommitIndexingParams{
			CommitIndexingMode: string(mode),
			ID:                 databaseUUID(repositoryID),
		},
	)
	if err != nil {
		return repository.Repository{}, fmt.Errorf("update repository commit indexing: %w", err)
	}
	if rowsAffected == 0 {
		return repository.Repository{}, repository.ErrNotFound
	}

	return d.GetRepository(ctx, repositoryID)
}

func (d *Database) RemoveRepository(ctx context.Context, repositoryID uuid.UUID, removedAt time.Time) error {
	rowsAffected, err := d.queries.RemoveRepository(
		ctx,
		storedb.RemoveRepositoryParams{
			RemovedAt: pgtype.Timestamptz{
				Time:  removedAt,
				Valid: true,
			},
			ID: pgtype.UUID{
				Bytes: repositoryID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("remove repository: %w", err)
	}

	if rowsAffected == 0 {
		return repository.ErrNotFound
	}

	return nil
}

func repositoryFromCreateRow(row storedb.CreateRepositoryRow) repository.Repository {
	return repository.Repository{
		Name:                   row.Name,
		CloneURL:               row.CloneUrl,
		Branch:                 row.Branch,
		CommitIndexing:         repository.CommitIndexingMode(row.CommitIndexingMode),
		CommitURLTemplate:      row.CommitUrlTemplate,
		RefreshCredentialName:  row.RefreshCredentialName,
		CheckoutCredentialName: row.CheckoutCredentialName,
		ID:                     uuid.UUID(row.ID.Bytes),
		CreatedAt:              row.CreatedAt.Time.UTC(),
		Refresh: refreshState(
			row.RefreshStatus,
			row.ObservedCommitSha,
			row.LastRefreshAttemptedAt,
			row.LastRefreshSucceededAt,
			row.LastRefreshError,
		),
	}
}

func refreshState(
	status string,
	commitSHA *string,
	attemptedAt pgtype.Timestamptz,
	succeededAt pgtype.Timestamptz,
	failureReason *string,
) repository.RefreshState {
	return repository.RefreshState{
		Status:        repository.RefreshStatus(status),
		CommitSHA:     commitSHA,
		AttemptedAt:   optionalTime(attemptedAt),
		SucceededAt:   optionalTime(succeededAt),
		FailureReason: failureReason,
	}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}

	timestamp := value.Time.UTC()

	return &timestamp
}
