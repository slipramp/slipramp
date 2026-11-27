package store

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestSearchIntegration(t *testing.T) {
	ctx, _, db := openIntegrationDatabase(t)
	defer db.Close()

	t.Run("Targets", func(t *testing.T) { testTargetSearch(t, ctx, db) })
	t.Run("Stages and Pipelines", func(t *testing.T) { testPromotionSearch(t, ctx, db) })
}

func testTargetSearch(t *testing.T, ctx context.Context, database *Database) {
	t.Helper()
	now := time.Now().UTC()
	repositoryID := uuid.New()
	createGateTestRepository(t, database, repositoryID, uuid.New(), now)
	refresh := successfulCatalogRefresh(strings.Repeat("b", 40), now.Add(time.Minute), uuid.New())
	contents := persistedCatalog("1.14.3")
	template := contents.Targets[0]
	contents.Targets = nil
	for _, name := range []string{"edge-1-prod", "edge-2-prod", "edge-3-prod", "edge-4-prod", "edge-5-prod", "prod-edge"} {
		target := template
		target.Metadata = catalog.Metadata{Name: name}
		contents.Targets = append(contents.Targets, target)
	}
	title := template
	title.Metadata = catalog.Metadata{Name: "production", Title: "Prod"}
	contents.Targets = append(contents.Targets, title)
	refresh.Revision.Catalog = contents
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatal(err)
	}
	items, err := database.SearchTargets(ctx, []string{"prod"}, 5)
	if err != nil || len(items) != 5 || items[0].Name != "production" || items[1].Name != "prod-edge" {
		t.Fatalf("ranked target matches = %+v, err = %v", items, err)
	}
	items, err = database.SearchTargets(ctx, []string{"network"}, 5)
	if err != nil || len(items) != 0 {
		t.Fatalf("archived Target matched: %+v, err = %v", items, err)
	}

	other, err := database.CreateRepository(ctx, repository.Repository{
		ID: uuid.New(), Name: "backup", CloneURL: "https://example.com/backup.git", Branch: "main",
		CommitIndexing: repository.CommitIndexingObservedHeads, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	second := successfulCatalogRefresh(strings.Repeat("c", 40), now.Add(time.Minute), uuid.New())
	second.Revision.Catalog = contents
	if err := database.RecordRepositoryRefresh(ctx, other.ID, second); err != nil {
		t.Fatal(err)
	}
	items, err = database.SearchTargets(ctx, []string{"edge-5"}, 5)
	if err != nil || len(items) != 2 || items[0].Repository != "backup" ||
		items[1].Repository != "infrastructure" || items[0].ID == items[1].ID {
		t.Fatalf("same Target name across repositories = %+v, err = %v", items, err)
	}
	items, err = database.SearchTargets(ctx, []string{"infra", "edge-5"}, 5)
	if err != nil || len(items) != 1 || items[0].Repository != "infrastructure" || items[0].Title != "edge-5-prod" {
		t.Fatalf("cross-field target matches = %+v, err = %v", items, err)
	}
}

func testPromotionSearch(t *testing.T, ctx context.Context, db *Database) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	makeStage := func(project, name, application string, sources ...string) promotion.Stage {
		stage := promotionTestStage("kargo", "west", project, name, now)
		stage.Application = application
		stage.RequestedFreight = []promotion.FreightRequest{{Origin: "app", Direct: len(sources) == 0, SourceStages: sources}}
		return stage
	}
	dev := makeStage("checkout", "dev", "Accounts", "prod")
	prod := makeStage("checkout", "prod", "Payments", "dev")
	release := makeStage("checkout", "release", "Payments")
	other := makeStage("other", "prod", "Payments", "dev")
	numbered := makeStage("other", "test", "", "verify")
	verify := makeStage("other", "verify", "")
	unlabeled := makeStage("other", "alone", "")
	stages := []promotion.Stage{prod, release, dev, other, numbered, verify, unlabeled}
	snapshot := promotionTestSnapshot(uuid.New(), "kargo", "west", now)
	if err := db.ReplacePromotionStageSnapshot(ctx, snapshot, stages); err != nil {
		t.Fatal(err)
	}
	east := makeStage("checkout", "dev", "Payments")
	east.Integration, east.ClusterName = "east-kargo", "east"
	eastSnapshot := promotionTestSnapshot(uuid.New(), east.Integration, east.ClusterName, now)
	if err := db.ReplacePromotionStageSnapshot(ctx, eastSnapshot, []promotion.Stage{east}); err != nil {
		t.Fatal(err)
	}

	search := func(config promotion.IntegrationSet, limit int32, query string) promotion.InventorySearchResults {
		t.Helper()
		results, err := db.SearchPromotionInventory(ctx, config, strings.Fields(query), limit)
		if err != nil {
			t.Fatal(err)
		}
		return results
	}
	config := promotion.IntegrationSet{"kargo": {ClusterName: "west"}, "east-kargo": {ClusterName: "east"}}
	all := search(config, 20, "kargo")
	if len(all.Stages) != 8 || len(all.Pipelines) != 6 {
		t.Fatalf("inventory = %+v", all)
	}
	anchors := map[string]uuid.UUID{"accounts + 1 more": dev.ID, "pipeline 3": numbered.ID, "alone": unlabeled.ID}
	for name, id := range anchors {
		found := search(config, 20, name).Pipelines
		if len(found) != 1 || found[0].StageID != id {
			t.Fatalf("search %q = %+v", name, found)
		}
	}
	crossField := search(config, 20, "check west pro")
	if len(crossField.Stages) != 1 || crossField.Stages[0].StageID != prod.ID || len(crossField.Pipelines) != 0 {
		t.Fatalf("cross-field search = %+v", crossField)
	}
	if found := search(config, 20, "%"); len(found.Stages)+len(found.Pipelines) != 0 {
		t.Fatalf("literal percent = %+v", found)
	}
	swapped := promotion.IntegrationSet{"kargo": {ClusterName: "east"}, "east-kargo": {ClusterName: "west"}}
	if found := search(swapped, 20, "kargo"); len(found.Stages)+len(found.Pipelines) != 0 {
		t.Fatalf("mismatched integration/cluster pairs = %+v", found)
	}

	// An upgrade must produce the same names and durable anchors before another snapshot arrives.
	files, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	adapter := stdlib.OpenDBFromPool(db.pool)
	defer func() { _ = adapter.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, adapter, files, goose.WithTableName(migrationTable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 83); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	backfilled := search(config, 20, "kargo")
	if !slices.Equal(backfilled.Stages, all.Stages) || !slices.Equal(backfilled.Pipelines, all.Pipelines) {
		t.Fatalf("backfill = %+v, want %+v", backfilled, all)
	}

	// Split the cycle, merge prod with release, rename its application and archive the other Project.
	next := now.Add(time.Minute)
	devID, prodID := dev.ID, prod.ID
	for _, s := range []*promotion.Stage{&dev, &prod, &release} {
		s.ID = uuid.New()
		s.FirstObservedAt, s.LastObservedAt = next, next
		s.Application = "New"
	}
	dev.RequestedFreight = nil
	prod.RequestedFreight = nil
	release.RequestedFreight[0] = promotion.FreightRequest{Origin: "app", SourceStages: []string{"prod"}}
	update := promotionTestSnapshot(uuid.New(), "kargo", "west", next)
	if err := db.ReplacePromotionStageSnapshot(ctx, update, []promotion.Stage{release, dev, prod}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplacePromotionStageSnapshot(ctx, update, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplacePromotionStageSnapshot(ctx, snapshot, stages); !errors.Is(err, promotion.ErrStaleSnapshot) {
		t.Fatalf("stale snapshot error = %v", err)
	}

	found := search(config, 20, "new checkout").Pipelines
	if len(found) != 2 || found[0].StageID != devID || found[1].StageID != prodID {
		t.Fatalf("updated Pipeline anchors = %+v", found)
	}
	if found := search(config, 20, "west"); len(found.Stages) != 3 || len(found.Pipelines) != 2 {
		t.Fatalf("archived Project still visible: %+v", found)
	}
	empty := promotionTestSnapshot(uuid.New(), "kargo", "west", next.Add(time.Minute))
	if err := db.ReplacePromotionStageSnapshot(ctx, empty, nil); err != nil {
		t.Fatal(err)
	}
	remaining := search(config, 20, "kargo")
	if len(remaining.Stages) != 1 || len(remaining.Pipelines) != 1 || remaining.Pipelines[0].StageID != east.ID {
		t.Fatalf("empty snapshot changed unrelated scope: %+v", remaining)
	}

	rankedAt := next.Add(2 * time.Minute)
	var rankedStages []promotion.Stage
	for _, name := range []string{"a-prod-1", "a-prod-2", "a-prod-3", "a-prod-4", "prod-east", "prod"} {
		rankedStages = append(rankedStages, promotionTestStage("kargo", "west", "checkout", name, rankedAt))
	}
	ranked := promotionTestSnapshot(uuid.New(), "kargo", "west", rankedAt)
	if err := db.ReplacePromotionStageSnapshot(ctx, ranked, rankedStages); err != nil {
		t.Fatal(err)
	}
	rankedResults := search(config, 5, "prod")
	for _, group := range [][]promotion.InventorySearchResult{rankedResults.Stages, rankedResults.Pipelines} {
		var names []string
		for _, item := range group {
			names = append(names, item.Name)
		}
		if !slices.Equal(names, []string{"prod", "prod-east", "a-prod-1", "a-prod-2", "a-prod-3"}) {
			t.Fatalf("ranked results = %v", names)
		}
	}
}
