package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func BenchmarkGetRunSchedulingSnapshots(b *testing.B) {
	ctx, _, database := openIntegrationDatabase(b)
	defer database.Close()

	page := seedSchedulingBenchmark(b, ctx, database)
	clusterLimits := []byte(`{"cluster-0":32,"cluster-1":32}`)
	placementLimits := []byte(`{"placement-0":2}`)

	for _, scenario := range []struct {
		name string
		ids  []uuid.UUID
	}{
		// This Run is eligible and has eligible predecessors outside the page.
		{"single", page[20:21]},
		{"default-page", page[:run.DefaultRunListLimit]},
		{"maximum-page", page},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			read := func() {
				snapshots, err := database.GetRunSchedulingSnapshots(b.Context(), scenario.ids, uuid.Nil, time.Now(), clusterLimits, placementLimits)
				if err != nil || len(snapshots) != len(scenario.ids) {
					b.Fatalf("read %d snapshots: got %d, error %v", len(scenario.ids), len(snapshots), err)
				}
			}
			for range 10 {
				read()
			}
			for b.Loop() {
				read()
			}
		})
	}
}

func seedSchedulingBenchmark(t testing.TB, ctx context.Context, database *Database) []uuid.UUID {
	t.Helper()

	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	repositoryID := uuid.New()
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "bench", CloneURL: "https://example.com/bench.git",
		Branch: "main", CommitIndexing: repository.CommitIndexingObservedHeads, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	refresh := successfulCatalogRefresh(strings.Repeat("a", 40), at, uuid.New())
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	target := refresh.Revision.Catalog.Targets[0]
	refresh.Revision.Catalog.Targets = nil
	for i := 1; i <= 64; i++ {
		target.Metadata.Name = fmt.Sprintf("target-%d", i)
		refresh.Revision.Catalog.Targets = append(refresh.Revision.Catalog.Targets, target)
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatal(err)
	}

	fixture, err := os.ReadFile("testdata/run_scheduling_benchmark.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}

	rows, err := database.pool.Query(ctx, `
		SELECT id FROM runs WHERE phase = 'queued' ORDER BY created_at DESC, id DESC LIMIT $1
	`, run.MaximumRunListLimit)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(page) != run.MaximumRunListLimit {
		t.Fatalf("load benchmark page: got %d Runs, error %v", len(page), err)
	}
	return page
}
