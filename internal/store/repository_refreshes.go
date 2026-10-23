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
	lockedRepository, err := queries.LockActiveRepository(ctx, identifier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.ErrNotFound
		}

		return fmt.Errorf("lock repository for refresh: %w", err)
	}
	if result.Commit != nil &&
		repository.CommitIndexingMode(lockedRepository.CommitIndexingMode) != repository.CommitIndexingDisabled {
		commit := *result.Commit
		if commit.RepositoryID == uuid.Nil {
			commit.RepositoryID = repositoryID
		} else if commit.RepositoryID != repositoryID {
			return errors.New("fetched commit repository does not match refresh repository")
		}
		if err := upsertRepositoryCommit(ctx, queries, commit); err != nil {
			return err
		}
	}

	if result.Revision != nil {
		if err := recordCatalogRevision(
			ctx,
			queries,
			identifier,
			lockedRepository.ActiveCatalogRevisionID,
			result,
		); err != nil {
			return err
		}
	} else {
		if err := recordRefreshFailure(ctx, queries, identifier, result); err != nil {
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
	previousRevisionID pgtype.UUID,
	result repository.RefreshResult,
) error {
	revision := *result.Revision
	revisionID, err := findCatalogRevisionID(ctx, queries, repositoryID, revision.CommitSHA)
	if err != nil {
		return err
	}

	if !revisionID.Valid {
		revisionID = pgtype.UUID{Bytes: revision.ID, Valid: true}
		if err := createCatalogRevision(ctx, queries, repositoryID, revisionID, revision); err != nil {
			return err
		}
	}

	targetChanges, err := updateTargetStates(ctx, queries, repositoryID, revisionID, result)
	if err != nil {
		return err
	}
	if err := coalesceSupersededGitWork(ctx, queries, repositoryID, targetChanges, result.AttemptedAt); err != nil {
		return err
	}
	if previousRevisionID.Valid && previousRevisionID.Bytes != revisionID.Bytes {
		if err := createGitTriggerDeliveries(ctx, queries, repositoryID, revisionID, result); err != nil {
			return err
		}
	}

	err = queries.ActivateCatalogRevision(
		ctx,
		storedb.ActivateCatalogRevisionParams{
			CommitSha: &result.Commit.SHA,
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

func upsertRepositoryCommit(
	ctx context.Context,
	queries *storedb.Queries,
	commit repository.Commit,
) error {
	err := queries.UpsertRepositoryCommit(
		ctx,
		storedb.UpsertRepositoryCommitParams{
			RepositoryID: databaseUUID(commit.RepositoryID),
			Sha:          commit.SHA,
			Title:        commit.Title,
			AuthorName:   commit.AuthorName,
			AuthoredAt:   optionalDatabaseTime(commit.AuthoredAt),
		},
	)
	if err != nil {
		return fmt.Errorf("record repository commit: %w", err)
	}

	return nil
}

type targetStateChange struct {
	name             string
	desiredCommitSHA string
	archived         bool
}

func updateTargetStates(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	revisionID pgtype.UUID,
	result repository.RefreshResult,
) ([]targetStateChange, error) {
	existingRows, err := queries.ListRepositoryTargetStates(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository target states: %w", err)
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
	activeNameSet := make(map[string]struct{}, len(revision.Catalog.Targets))
	for _, target := range revision.Catalog.Targets {
		activeNames = append(activeNames, target.Metadata.Name)
		activeNameSet[target.Metadata.Name] = struct{}{}
	}

	changes := make([]targetStateChange, 0, len(revision.Catalog.Targets))
	for _, row := range existingRows {
		if _, active := activeNameSet[row.Name]; !active && !row.ArchivedAt.Valid {
			changes = append(changes, targetStateChange{
				name:     row.Name,
				archived: true,
			})
		}
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
		return nil, fmt.Errorf("archive missing targets: %w", err)
	}

	for _, target := range revision.Catalog.Targets {
		name := target.Metadata.Name
		state, found := existing[name]
		desiredCommitSHA := revision.CommitSHA
		if found && !state.archived &&
			!catalog.TargetAffected(target, result.ChangedPaths) {
			desiredCommitSHA = state.desiredCommitSHA
		}
		if !found || state.archived || state.desiredCommitSHA != desiredCommitSHA {
			changes = append(changes, targetStateChange{
				name:             name,
				desiredCommitSHA: desiredCommitSHA,
			})
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
			return nil, fmt.Errorf("upsert target state %q: %w", name, err)
		}
	}

	return changes, nil
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
			return fmt.Errorf("marshal catalog runtime %q: %w", runtime.Metadata.Name, err)
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
			return fmt.Errorf("create catalog runtime %q: %w", runtime.Metadata.Name, err)
		}
	}

	for _, target := range revision.Catalog.Targets {
		definition, err := json.Marshal(target)
		if err != nil {
			return fmt.Errorf("marshal catalog target %q: %w", target.Metadata.Name, err)
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
			return fmt.Errorf("create catalog target %q: %w", target.Metadata.Name, err)
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
	if result.Commit != nil {
		err := queries.RecordCatalogValidationFailure(
			ctx,
			storedb.RecordCatalogValidationFailureParams{
				RepositoryID:  repositoryID,
				CommitSha:     result.Commit.SHA,
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
			ObservedCommitSha: optionalCommitSHA(result.Commit),
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

func optionalCommitSHA(commit *repository.Commit) *string {
	if commit == nil {
		return nil
	}

	return &commit.SHA
}
