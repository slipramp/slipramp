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
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ catalog.Store = (*Database)(nil)

func (d *Database) GetActiveCatalogRevision(ctx context.Context, repositoryID uuid.UUID) (catalog.Revision, error) {
	return loadActiveCatalogRevision(ctx, d.queries, repositoryID)
}

func (d *Database) GetActiveCatalog(ctx context.Context, repositoryID uuid.UUID) (catalog.ActiveCatalog, error) {
	// Refresh atomically replaces the active revision and mutable Target state.
	// Keep every query on one snapshot even if a refresh commits between reads.
	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return catalog.ActiveCatalog{}, fmt.Errorf("begin active catalog read: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	revision, err := loadActiveCatalogRevision(ctx, queries, repositoryID)
	if err != nil {
		return catalog.ActiveCatalog{}, err
	}
	states, err := queries.ListCatalogTargetStates(ctx, databaseUUID(revision.ID))
	if err != nil {
		return catalog.ActiveCatalog{}, fmt.Errorf("list catalog target states: %w", err)
	}

	targetStates := make(map[string]catalog.TargetState, len(states))
	for _, state := range states {
		targetStates[state.Name] = catalog.TargetState{DesiredCommitSHA: state.DesiredCommitSha}
	}

	if err := transaction.Commit(ctx); err != nil {
		return catalog.ActiveCatalog{}, fmt.Errorf("commit active catalog read: %w", err)
	}

	return catalog.ActiveCatalog{Revision: revision, TargetStates: targetStates}, nil
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

	return loadCatalogRevision(ctx, d.queries, row.ID, row.CommitSha, row.CreatedAt)
}

func (d *Database) ListActiveTargets(
	ctx context.Context,
	filter catalog.TargetListFilter,
) (catalog.TargetList, error) {
	var repositoryID pgtype.UUID
	if filter.RepositoryID != nil {
		repositoryID = databaseUUID(*filter.RepositoryID)
	}

	var searchQuery *string
	if filter.Query != "" {
		searchQuery = &filter.Query
	}
	var runtime *string
	if filter.Runtime != "" {
		runtime = &filter.Runtime
	}
	var placement *string
	if filter.Placement != "" {
		placement = &filter.Placement
	}

	countParams := storedb.CountActiveTargetsParams{
		RepositoryID: repositoryID,
		SearchQuery:  searchQuery,
		Runtime:      runtime,
		Placement:    placement,
	}
	total, err := d.queries.CountActiveTargets(ctx, countParams)
	if err != nil {
		return catalog.TargetList{}, fmt.Errorf("count active targets: %w", err)
	}

	rows, err := d.queries.ListActiveTargets(
		ctx,
		storedb.ListActiveTargetsParams{
			RepositoryID: repositoryID,
			SearchQuery:  searchQuery,
			Runtime:      runtime,
			Placement:    placement,
			ResultLimit:  filter.Limit,
			ResultOffset: filter.Offset,
		},
	)
	if err != nil {
		return catalog.TargetList{}, fmt.Errorf("list active targets: %w", err)
	}

	items := make([]catalog.TargetSummary, 0, len(rows))
	for _, row := range rows {
		var target catalog.Target
		if err := json.Unmarshal(row.Definition, &target); err != nil {
			return catalog.TargetList{}, fmt.Errorf("decode active target: %w", err)
		}

		items = append(items, catalog.TargetSummary{
			ID:                uuid.UUID(row.ID.Bytes),
			RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
			RepositoryName:    row.RepositoryName,
			CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
			CatalogCommitSHA:  row.CatalogCommitSha,
			DesiredCommitSHA:  row.DesiredCommitSha,
			Target:            target,
		})
	}

	return catalog.TargetList{Items: items, Total: total}, nil
}

func (d *Database) GetTarget(ctx context.Context, targetID uuid.UUID) (catalog.TargetSummary, error) {
	row, err := d.queries.GetTarget(ctx, databaseUUID(targetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.TargetSummary{}, catalog.ErrTargetNotFound
	}
	if err != nil {
		return catalog.TargetSummary{}, fmt.Errorf("get Target: %w", err)
	}

	return targetSummaryFromRow(
		row.ID,
		row.RepositoryID,
		row.RepositoryName,
		row.CatalogRevisionID,
		row.CatalogCommitSha,
		row.DesiredCommitSha,
		row.ArchivedAt,
		row.Definition,
	)
}

func (d *Database) GetActiveTargetByName(
	ctx context.Context,
	repositoryID uuid.UUID,
	targetName string,
) (catalog.TargetSummary, error) {
	row, err := d.queries.GetActiveTargetByName(ctx, storedb.GetActiveTargetByNameParams{
		RepositoryID: databaseUUID(repositoryID),
		Name:         targetName,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.TargetSummary{}, catalog.ErrTargetNotFound
	}
	if err != nil {
		return catalog.TargetSummary{}, fmt.Errorf("get active Target by name: %w", err)
	}

	return targetSummaryFromRow(
		row.ID,
		row.RepositoryID,
		row.RepositoryName,
		row.CatalogRevisionID,
		row.CatalogCommitSha,
		row.DesiredCommitSha,
		row.ArchivedAt,
		row.Definition,
	)
}

func targetSummaryFromRow(
	id pgtype.UUID,
	repositoryID pgtype.UUID,
	repositoryName string,
	catalogRevisionID pgtype.UUID,
	catalogCommitSHA string,
	desiredCommitSHA string,
	archivedAt pgtype.Timestamptz,
	definition []byte,
) (catalog.TargetSummary, error) {
	var target catalog.Target
	if err := json.Unmarshal(definition, &target); err != nil {
		return catalog.TargetSummary{}, fmt.Errorf("decode Target definition: %w", err)
	}

	return catalog.TargetSummary{
		ID:                uuid.UUID(id.Bytes),
		RepositoryID:      uuid.UUID(repositoryID.Bytes),
		RepositoryName:    repositoryName,
		CatalogRevisionID: uuid.UUID(catalogRevisionID.Bytes),
		CatalogCommitSHA:  catalogCommitSHA,
		DesiredCommitSHA:  desiredCommitSHA,
		ArchivedAt:        applicationTime(archivedAt),
		Target:            target,
	}, nil
}

func loadActiveCatalogRevision(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID uuid.UUID,
) (catalog.Revision, error) {
	row, err := queries.GetActiveCatalogRevision(ctx, databaseUUID(repositoryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Revision{}, catalog.ErrNotFound
	}
	if err != nil {
		return catalog.Revision{}, fmt.Errorf("get active catalog revision: %w", err)
	}

	return loadCatalogRevision(ctx, queries, row.ID, row.CommitSha, row.CreatedAt)
}

func loadCatalogRevision(
	ctx context.Context,
	queries *storedb.Queries,
	revisionID pgtype.UUID,
	commitSHA string,
	createdAt pgtype.Timestamptz,
) (catalog.Revision, error) {
	runtimes, err := queries.ListCatalogRuntimes(ctx, revisionID)
	if err != nil {
		return catalog.Revision{}, fmt.Errorf("list catalog runtimes: %w", err)
	}

	targets, err := queries.ListCatalogTargets(ctx, revisionID)
	if err != nil {
		return catalog.Revision{}, fmt.Errorf("list catalog targets: %w", err)
	}

	decodedRuntimes, err := decodeCatalogDefinitions[catalog.Runtime]("runtime", runtimes)
	if err != nil {
		return catalog.Revision{}, err
	}

	decodedTargets, err := decodeCatalogDefinitions[catalog.Target]("target", targets)
	if err != nil {
		return catalog.Revision{}, err
	}

	return catalog.Revision{
		ID:        uuid.UUID(revisionID.Bytes),
		CommitSHA: commitSHA,
		CreatedAt: createdAt.Time.UTC(),
		Catalog: catalog.Catalog{
			Runtimes: decodedRuntimes,
			Targets:  decodedTargets,
		},
	}, nil
}

func decodeCatalogDefinitions[T any](kind string, definitions [][]byte) ([]T, error) {
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
