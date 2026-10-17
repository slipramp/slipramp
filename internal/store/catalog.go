package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ repository.RefreshStore = (*Database)(nil)
var _ catalog.Store = (*Database)(nil)

func (d *Database) GetActiveCatalogRevision(
	ctx context.Context,
	repositoryID uuid.UUID,
) (catalog.Revision, error) {
	repositoryIdentifier := pgtype.UUID{
		Bytes: repositoryID,
		Valid: true,
	}
	row, err := d.queries.GetActiveCatalogRevision(
		ctx,
		repositoryIdentifier,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Revision{}, catalog.ErrNotFound
	}
	if err != nil {
		return catalog.Revision{}, fmt.Errorf(
			"get active catalog revision: %w",
			err,
		)
	}

	return d.loadCatalogRevision(ctx, row.ID, row.CommitSha, row.CreatedAt)
}

func (d *Database) GetCatalogRevision(
	ctx context.Context,
	repositoryID uuid.UUID,
	revisionID uuid.UUID,
) (catalog.Revision, error) {
	row, err := d.queries.GetCatalogRevision(
		ctx,
		storedb.GetCatalogRevisionParams{
			ID:           databaseUUID(revisionID),
			RepositoryID: databaseUUID(repositoryID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Revision{}, catalog.ErrNotFound
	}
	if err != nil {
		return catalog.Revision{}, fmt.Errorf("get catalog revision: %w", err)
	}

	return d.loadCatalogRevision(ctx, row.ID, row.CommitSha, row.CreatedAt)
}

func (d *Database) loadCatalogRevision(
	ctx context.Context,
	revisionID pgtype.UUID,
	commitSHA string,
	createdAt pgtype.Timestamptz,
) (catalog.Revision, error) {
	runtimes, err := d.queries.ListCatalogRuntimes(ctx, revisionID)
	if err != nil {
		return catalog.Revision{}, fmt.Errorf("list catalog runtimes: %w", err)
	}

	targets, err := d.queries.ListCatalogTargets(ctx, revisionID)
	if err != nil {
		return catalog.Revision{}, fmt.Errorf("list catalog targets: %w", err)
	}
	targetStates, err := d.queries.ListCatalogTargetStates(ctx, revisionID)
	if err != nil {
		return catalog.Revision{}, fmt.Errorf("list catalog target states: %w", err)
	}

	decodedRuntimes, err := decodeCatalogDefinitions[catalog.Runtime](
		"runtime",
		runtimes,
	)
	if err != nil {
		return catalog.Revision{}, err
	}

	decodedTargets, err := decodeCatalogDefinitions[catalog.Target](
		"target",
		targets,
	)
	if err != nil {
		return catalog.Revision{}, err
	}

	decodedTargetStates := make(map[string]catalog.TargetState, len(targetStates))
	for _, state := range targetStates {
		decodedTargetStates[state.Name] = catalog.TargetState{
			DesiredCommitSHA: state.DesiredCommitSha,
		}
	}

	return catalog.Revision{
		ID:           uuid.UUID(revisionID.Bytes),
		CommitSHA:    commitSHA,
		CreatedAt:    createdAt.Time.UTC(),
		TargetStates: decodedTargetStates,
		Catalog: catalog.Catalog{
			Runtimes: decodedRuntimes,
			Targets:  decodedTargets,
		},
	}, nil
}

func decodeCatalogDefinitions[T any](
	kind string,
	definitions [][]byte,
) ([]T, error) {
	decoded := make([]T, 0, len(definitions))
	for _, definition := range definitions {
		var item T
		if err := json.Unmarshal(definition, &item); err != nil {
			return nil, fmt.Errorf("decode catalog %s: %w", kind, err)
		}

		decoded = append(decoded, item)
	}

	return decoded, nil
}

func (d *Database) RecordRepositoryRefresh(
	ctx context.Context,
	repositoryID repository.RepositoryID,
	result repository.RefreshResult,
) error {
	if err := repository.ValidateRefreshResult(result); err != nil {
		return fmt.Errorf("validate repository refresh result: %w", err)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin repository refresh transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	identifier := pgtype.UUID{Bytes: repositoryID, Valid: true}
	if _, err := queries.LockActiveRepository(ctx, identifier); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.ErrNotFound
		}

		return fmt.Errorf("lock repository for refresh: %w", err)
	}

	if result.Revision != nil {
		if err := recordCatalogRevision(
			ctx,
			queries,
			identifier,
			result,
		); err != nil {
			return err
		}
	} else {
		if err := recordRefreshFailure(
			ctx,
			queries,
			identifier,
			result,
		); err != nil {
			return err
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository refresh: %w", err)
	}

	return nil
}

func recordCatalogRevision(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	result repository.RefreshResult,
) error {
	revision := *result.Revision
	revisionID, err := findCatalogRevisionID(
		ctx,
		queries,
		repositoryID,
		revision.CommitSHA,
	)
	if err != nil {
		return err
	}

	if !revisionID.Valid {
		revisionID = pgtype.UUID{Bytes: revision.ID, Valid: true}
		if err := createCatalogRevision(
			ctx,
			queries,
			repositoryID,
			revisionID,
			revision,
		); err != nil {
			return err
		}
	}

	if err := updateTargetStates(
		ctx,
		queries,
		repositoryID,
		revisionID,
		result,
	); err != nil {
		return err
	}

	err = queries.ActivateCatalogRevision(
		ctx,
		storedb.ActivateCatalogRevisionParams{
			CommitSha: result.CommitSHA,
			AttemptedAt: pgtype.Timestamptz{
				Time:  result.AttemptedAt,
				Valid: true,
			},
			CatalogRevisionID: revisionID,
			RepositoryID:      repositoryID,
		},
	)
	if err != nil {
		return fmt.Errorf("activate catalog revision: %w", err)
	}

	return nil
}

func updateTargetStates(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	revisionID pgtype.UUID,
	result repository.RefreshResult,
) error {
	existingRows, err := queries.ListRepositoryTargetStates(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("list repository target states: %w", err)
	}

	type existingTargetState struct {
		desiredCommitSHA string
		archived         bool
	}
	existing := make(map[string]existingTargetState, len(existingRows))
	for _, row := range existingRows {
		existing[row.Name] = existingTargetState{
			desiredCommitSHA: row.DesiredCommitSha,
			archived:         row.ArchivedAt.Valid,
		}
	}

	revision := *result.Revision
	activeNames := make([]string, 0, len(revision.Catalog.Targets))
	for _, target := range revision.Catalog.Targets {
		activeNames = append(activeNames, target.Metadata.Name)
	}

	updatedAt := pgtype.Timestamptz{Time: result.AttemptedAt, Valid: true}
	if err := queries.ArchiveMissingTargets(
		ctx,
		storedb.ArchiveMissingTargetsParams{
			UpdatedAt:    updatedAt,
			RepositoryID: repositoryID,
			ActiveNames:  activeNames,
		},
	); err != nil {
		return fmt.Errorf("archive missing targets: %w", err)
	}

	for _, target := range revision.Catalog.Targets {
		name := target.Metadata.Name
		state, found := existing[name]
		desiredCommitSHA := revision.CommitSHA
		if found && !state.archived &&
			!catalog.TargetAffected(target, result.ChangedPaths) {
			desiredCommitSHA = state.desiredCommitSHA
		}

		err := queries.UpsertTargetState(
			ctx,
			storedb.UpsertTargetStateParams{
				RepositoryID:      repositoryID,
				Name:              name,
				CatalogRevisionID: revisionID,
				DesiredCommitSha:  desiredCommitSHA,
				UpdatedAt:         updatedAt,
			},
		)
		if err != nil {
			return fmt.Errorf("upsert target state %q: %w", name, err)
		}
	}

	return nil
}

func findCatalogRevisionID(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	commitSHA string,
) (pgtype.UUID, error) {
	revisionID, err := queries.FindCatalogRevisionID(
		ctx,
		storedb.FindCatalogRevisionIDParams{
			RepositoryID: repositoryID,
			CommitSha:    commitSHA,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("find catalog revision: %w", err)
	}

	return revisionID, nil
}

func createCatalogRevision(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	revisionID pgtype.UUID,
	revision catalog.Revision,
) error {
	err := queries.CreateCatalogRevision(
		ctx,
		storedb.CreateCatalogRevisionParams{
			ID:           revisionID,
			RepositoryID: repositoryID,
			CommitSha:    revision.CommitSHA,
			CreatedAt: pgtype.Timestamptz{
				Time:  revision.CreatedAt,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("create catalog revision: %w", err)
	}

	for _, runtime := range revision.Catalog.Runtimes {
		definition, err := json.Marshal(runtime)
		if err != nil {
			return fmt.Errorf(
				"marshal catalog runtime %q: %w",
				runtime.Metadata.Name,
				err,
			)
		}

		err = queries.CreateCatalogRuntime(
			ctx,
			storedb.CreateCatalogRuntimeParams{
				CatalogRevisionID: revisionID,
				Name:              runtime.Metadata.Name,
				SourcePath:        runtime.Source.Path,
				SourceDocument:    int32(runtime.Source.Document),
				Definition:        definition,
			},
		)
		if err != nil {
			return fmt.Errorf(
				"create catalog runtime %q: %w",
				runtime.Metadata.Name,
				err,
			)
		}
	}

	for _, target := range revision.Catalog.Targets {
		definition, err := json.Marshal(target)
		if err != nil {
			return fmt.Errorf(
				"marshal catalog target %q: %w",
				target.Metadata.Name,
				err,
			)
		}

		err = queries.CreateCatalogTarget(
			ctx,
			storedb.CreateCatalogTargetParams{
				CatalogRevisionID: revisionID,
				Name:              target.Metadata.Name,
				SourcePath:        target.Source.Path,
				SourceDocument:    int32(target.Source.Document),
				Definition:        definition,
			},
		)
		if err != nil {
			return fmt.Errorf(
				"create catalog target %q: %w",
				target.Metadata.Name,
				err,
			)
		}
	}

	return nil
}

func recordRefreshFailure(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	result repository.RefreshResult,
) error {
	attemptedAt := pgtype.Timestamptz{
		Time:  result.AttemptedAt,
		Valid: true,
	}
	if result.CommitSHA != nil {
		err := queries.RecordCatalogValidationFailure(
			ctx,
			storedb.RecordCatalogValidationFailureParams{
				RepositoryID:  repositoryID,
				CommitSha:     *result.CommitSHA,
				RecordedAt:    attemptedAt,
				FailureReason: *result.FailureReason,
			},
		)
		if err != nil {
			return fmt.Errorf("record catalog validation failure: %w", err)
		}
	}

	err := queries.RecordRepositoryRefreshFailure(
		ctx,
		storedb.RecordRepositoryRefreshFailureParams{
			ObservedCommitSha: result.CommitSHA,
			AttemptedAt:       attemptedAt,
			RefreshError:      result.FailureReason,
			ID:                repositoryID,
		},
	)
	if err != nil {
		return fmt.Errorf("record repository refresh failure: %w", err)
	}

	return nil
}
