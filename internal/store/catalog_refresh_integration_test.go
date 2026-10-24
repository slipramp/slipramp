package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func TestActiveCatalogReadIsConsistentDuringRefreshIntegration(t *testing.T) {
	ctx, cfg, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	createdAt := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:             repositoryID,
		Name:           "refresh-snapshot",
		Branch:         "main",
		CloneURL:       "https://github.com/example/infrastructure.git",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	first := successfulCatalogRefresh(strings.Repeat("a", 40), createdAt.Add(time.Minute), uuid.New())
	first.Revision.Catalog.Targets = []catalog.Target{testCatalogTarget("network", "network/**")}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, first); err != nil {
		t.Fatalf("record first catalog: %v", err)
	}

	second := successfulCatalogRefresh(strings.Repeat("b", 40), createdAt.Add(2*time.Minute), uuid.New())
	second.Revision.Catalog.Targets = first.Revision.Catalog.Targets
	second.ChangedPaths = []string{"network/main.tf"}
	refreshed := false
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		t.Fatalf("parse reader pool configuration: %v", err)
	}
	poolConfig.ConnConfig.Tracer = catalogRefreshTracer{afterMetadata: func(ctx context.Context) {
		if err := database.RecordRepositoryRefresh(ctx, repositoryID, second); err != nil {
			t.Fatalf("refresh during active catalog read: %v", err)
		}
		refreshed = true
	}}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("open reader pool: %v", err)
	}
	defer pool.Close()
	reader := &Database{pool: pool, queries: storedb.New(pool)}

	active, err := reader.GetActiveCatalog(ctx, repositoryID)
	if err != nil {
		t.Fatalf("read catalog during refresh: %v", err)
	}
	if !refreshed || active.Revision.ID != first.Revision.ID ||
		active.Revision.CommitSHA != first.Commit.SHA ||
		active.TargetStates["network"].DesiredCommitSHA != first.Commit.SHA {
		t.Fatalf("catalog read crossed refresh boundary: refreshed=%t, catalog=%#v", refreshed, active)
	}

	active, err = database.GetActiveCatalog(ctx, repositoryID)
	if err != nil {
		t.Fatalf("read catalog after refresh: %v", err)
	}
	if active.Revision.ID != second.Revision.ID || active.Revision.CommitSHA != second.Commit.SHA ||
		active.TargetStates["network"].DesiredCommitSHA != second.Commit.SHA {
		t.Errorf("catalog after refresh = %#v", active)
	}
}

type catalogMetadataQueryKey struct{}

type catalogRefreshTracer struct {
	afterMetadata func(context.Context)
}

func (tracer catalogRefreshTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	isMetadataQuery := strings.HasPrefix(data.SQL, "-- name: GetActiveCatalogRevision ")
	return context.WithValue(ctx, catalogMetadataQueryKey{}, isMetadataQuery)
}

func (tracer catalogRefreshTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if isMetadataQuery, _ := ctx.Value(catalogMetadataQueryKey{}).(bool); isMetadataQuery && data.Err == nil {
		tracer.afterMetadata(ctx)
	}
}
