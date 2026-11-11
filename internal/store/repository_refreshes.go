package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

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
	if result.Commit != nil &&
		repository.CommitIndexingMode(lockedRepository.CommitIndexingMode) ==
			repository.CommitIndexingTrackedBranchHistory {
		if err := queries.MarkRepositoryCommitHistoryPending(
			ctx,
			storedb.MarkRepositoryCommitHistoryPendingParams{
				RepositoryID:   identifier,
				DesiredHeadSha: &result.Commit.SHA,
			},
		); err != nil {
			return fmt.Errorf("mark repository commit history pending: %w", err)
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
	if err := recordRepositoryRefreshAttempt(ctx, queries, identifier, result); err != nil {
		return err
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
			CommittedAt:  optionalDatabaseTime(commit.CommittedAt),
		},
	)
	if err != nil {
		return fmt.Errorf("record repository commit: %w", err)
	}

	return nil
}

type targetStateChange struct {
	id               uuid.UUID
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
		id               uuid.UUID
		desiredCommitSHA string
		archived         bool
	}
	existing := make(map[string]existingTargetState, len(existingRows))
	for _, row := range existingRows {
		existing[row.Name] = existingTargetState{
			id:               uuid.UUID(row.ID.Bytes),
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
				id:       uuid.UUID(row.ID.Bytes),
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
		targetID := uuid.New()
		if found {
			targetID = state.id
		}
		desiredCommitSHA := revision.CommitSHA
		if found && !state.archived &&
			!catalog.TargetAffected(target, result.ChangedPaths) {
			desiredCommitSHA = state.desiredCommitSHA
		}
		if !found || state.archived || state.desiredCommitSHA != desiredCommitSHA {
			changes = append(changes, targetStateChange{
				id:               targetID,
				name:             name,
				desiredCommitSHA: desiredCommitSHA,
			})
		}

		err := queries.UpsertTargetState(
			ctx,
			storedb.UpsertTargetStateParams{
				ID:                databaseUUID(targetID),
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
	manifest, err := newCatalogSnapshotManifest(revision.Catalog)
	if err != nil {
		return err
	}
	contents, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal catalog snapshot contents: %w", err)
	}
	snapshotID, err := queries.CatalogSnapshotID(ctx, contents)
	if err != nil {
		return fmt.Errorf("compute catalog snapshot ID: %w", err)
	}

	created, err := queries.CreateCatalogSnapshot(
		ctx,
		storedb.CreateCatalogSnapshotParams{
			ID: snapshotID,
			CreatedAt: pgtype.Timestamptz{
				Time:  revision.CreatedAt,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("create catalog snapshot: %w", err)
	}

	if created == 1 {
		if err := createCatalogSnapshotDefinitions(ctx, queries, snapshotID, manifest); err != nil {
			return err
		}
	}

	err = queries.CreateCatalogRevision(
		ctx,
		storedb.CreateCatalogRevisionParams{
			ID:                revisionID,
			RepositoryID:      repositoryID,
			CommitSha:         revision.CommitSHA,
			CatalogSnapshotID: snapshotID,
			CreatedAt: pgtype.Timestamptz{
				Time:  revision.CreatedAt,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("create catalog revision: %w", err)
	}

	return nil
}

const catalogSnapshotManifestVersion = 1

// catalogSnapshotManifest is the durable content identity contract. Keep it in
// sync with the migration backfill before changing persisted catalog fields.
type catalogSnapshotDefinition struct {
	Name           string          `json:"name"`
	SourcePath     string          `json:"sourcePath"`
	SourceDocument int32           `json:"sourceDocument"`
	Definition     json.RawMessage `json:"definition"`
}

type catalogSnapshotManifest struct {
	Version  int                         `json:"version"`
	Runtimes []catalogSnapshotDefinition `json:"runtimes"`
	Targets  []catalogSnapshotDefinition `json:"targets"`
}

func newCatalogSnapshotManifest(contents catalog.Catalog) (catalogSnapshotManifest, error) {
	manifest := catalogSnapshotManifest{
		Version:  catalogSnapshotManifestVersion,
		Runtimes: make([]catalogSnapshotDefinition, 0, len(contents.Runtimes)),
		Targets:  make([]catalogSnapshotDefinition, 0, len(contents.Targets)),
	}

	for _, runtime := range contents.Runtimes {
		definition, err := json.Marshal(runtime)
		if err != nil {
			return catalogSnapshotManifest{}, fmt.Errorf(
				"marshal catalog runtime %q: %w",
				runtime.Metadata.Name,
				err,
			)
		}
		manifest.Runtimes = append(manifest.Runtimes, catalogSnapshotDefinition{
			Name:           runtime.Metadata.Name,
			SourcePath:     runtime.Source.Path,
			SourceDocument: int32(runtime.Source.Document),
			Definition:     definition,
		})
	}

	for _, target := range contents.Targets {
		definition, err := json.Marshal(target)
		if err != nil {
			return catalogSnapshotManifest{}, fmt.Errorf(
				"marshal catalog target %q: %w",
				target.Metadata.Name,
				err,
			)
		}
		manifest.Targets = append(manifest.Targets, catalogSnapshotDefinition{
			Name:           target.Metadata.Name,
			SourcePath:     target.Source.Path,
			SourceDocument: int32(target.Source.Document),
			Definition:     definition,
		})
	}

	sort.Slice(manifest.Runtimes, func(left, right int) bool {
		return manifest.Runtimes[left].Name < manifest.Runtimes[right].Name
	})
	sort.Slice(manifest.Targets, func(left, right int) bool {
		return manifest.Targets[left].Name < manifest.Targets[right].Name
	})

	return manifest, nil
}

func createCatalogSnapshotDefinitions(
	ctx context.Context,
	queries *storedb.Queries,
	snapshotID string,
	manifest catalogSnapshotManifest,
) error {
	for _, runtime := range manifest.Runtimes {
		err := queries.CreateCatalogRuntime(
			ctx,
			storedb.CreateCatalogRuntimeParams{
				CatalogSnapshotID: snapshotID,
				Name:              runtime.Name,
				SourcePath:        runtime.SourcePath,
				SourceDocument:    runtime.SourceDocument,
				Definition:        runtime.Definition,
			},
		)
		if err != nil {
			return fmt.Errorf("create catalog runtime %q: %w", runtime.Name, err)
		}
	}

	for _, target := range manifest.Targets {
		err := queries.CreateCatalogTarget(
			ctx,
			storedb.CreateCatalogTargetParams{
				CatalogSnapshotID: snapshotID,
				Name:              target.Name,
				SourcePath:        target.SourcePath,
				SourceDocument:    target.SourceDocument,
				Definition:        target.Definition,
			},
		)
		if err != nil {
			return fmt.Errorf("create catalog target %q: %w", target.Name, err)
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
