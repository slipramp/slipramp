package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestCatalogRevisionPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		Name:           "infrastructure",
		CloneURL:       "https://github.com/example/infrastructure.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		ID:             repositoryID,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	commitA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	revisionAID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	acceptedAt := createdAt.Add(time.Minute)
	accepted := successfulCatalogRefresh(commitA, acceptedAt, revisionAID)
	accepted.Revision.Catalog = persistedCatalog("1")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, accepted); err != nil {
		t.Fatalf("record accepted catalog: %v", err)
	}

	active, err := database.GetActiveCatalogRevision(ctx, repositoryID)
	if err != nil {
		t.Fatalf("GetActiveCatalogRevision() error = %v", err)
	}
	if active.ID != revisionAID || active.CommitSHA != commitA ||
		!active.CreatedAt.Equal(acceptedAt) {
		t.Errorf("active catalog revision = %#v", active)
	}
	if len(active.Catalog.Runtimes) != 1 ||
		active.Catalog.Runtimes[0].Spec.Image != "hashicorp/terraform:1" ||
		len(active.Catalog.Targets) != 1 ||
		active.Catalog.Targets[0].Metadata.Name != "network" {
		t.Errorf("decoded active catalog = %#v", active.Catalog)
	}

	assertActiveCatalog(t, database, repositoryID, revisionAID, commitA)
	assertCatalogCounts(t, database, repositoryID, 1, 1, 1)

	var targetDefinition []byte
	err = database.pool.QueryRow(
		ctx,
		`SELECT definition FROM catalog_targets WHERE catalog_revision_id = $1`,
		revisionAID,
	).Scan(&targetDefinition)
	if err != nil {
		t.Fatalf("load persisted target: %v", err)
	}

	var target catalog.Target
	if err := json.Unmarshal(targetDefinition, &target); err != nil {
		t.Fatalf("decode persisted target: %v", err)
	}
	if target.Metadata.Name != "network" ||
		target.Source.Path != ".slipramp/catalog.yaml" ||
		target.Source.Document != 2 {
		t.Errorf("persisted target = %#v", target)
	}

	commitB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	rejectionReason := "load catalog: placement \"missing\" is not defined"
	rejectedAt := acceptedAt.Add(time.Minute)
	rejected := repository.RefreshResult{
		Commit:        &repository.Commit{SHA: commitB},
		AttemptedAt:   rejectedAt,
		FailureReason: &rejectionReason,
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, rejected); err != nil {
		t.Fatalf("record rejected catalog: %v", err)
	}

	assertActiveCatalog(t, database, repositoryID, revisionAID, commitA)
	assertCatalogCounts(t, database, repositoryID, 1, 1, 1)
	activeAfterRejection, err := database.GetActiveCatalog(ctx, repositoryID)
	if err != nil {
		t.Fatalf("load catalog after rejection: %v", err)
	}
	assertDesiredCommit(t, activeAfterRejection, "network", commitA)

	repositories, err := database.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	refresh := repositories[0].Refresh
	if refresh.Status != repository.RefreshStatusFailed ||
		refresh.CommitSHA == nil || *refresh.CommitSHA != commitB ||
		refresh.AttemptedAt == nil || !refresh.AttemptedAt.Equal(rejectedAt) ||
		refresh.SucceededAt == nil || !refresh.SucceededAt.Equal(acceptedAt) ||
		refresh.FailureStartedAt == nil || !refresh.FailureStartedAt.Equal(rejectedAt) ||
		refresh.FailureReason == nil ||
		*refresh.FailureReason != rejectionReason {
		t.Errorf("refresh state after catalog rejection = %#v", refresh)
	}

	if err := database.RecordRepositoryRefresh(ctx, repositoryID, rejected); err != nil {
		t.Fatalf("repeat rejected catalog: %v", err)
	}
	assertValidationFailureCount(t, database, repositoryID, 1)

	revisionBID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	recoveredAt := rejectedAt.Add(time.Minute)
	recovered := successfulCatalogRefresh(commitB, recoveredAt, revisionBID)
	recovered.Revision.Catalog = persistedCatalog("2")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, recovered); err != nil {
		t.Fatalf("record recovered catalog: %v", err)
	}
	repositories, err = database.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("list repositories after recovery: %v", err)
	}
	if repositories[0].Refresh.FailureStartedAt != nil {
		t.Errorf("refresh failure episode remained after recovery = %#v", repositories[0].Refresh)
	}

	assertActiveCatalog(t, database, repositoryID, revisionBID, commitB)
	assertCatalogCounts(t, database, repositoryID, 2, 2, 2)
	historical, err := database.GetCatalogRevision(ctx, repositoryID, revisionAID)
	if err != nil {
		t.Fatalf("load historical catalog: %v", err)
	}
	if !reflect.DeepEqual(historical, active) {
		t.Errorf("historical catalog changed after refresh: got %#v, want %#v", historical, active)
	}

	duplicate := successfulCatalogRefresh(
		commitB,
		recoveredAt.Add(time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203"),
	)
	duplicate.Revision.Catalog = persistedCatalog("2")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, duplicate); err != nil {
		t.Fatalf("repeat accepted catalog: %v", err)
	}

	assertActiveCatalog(t, database, repositoryID, revisionBID, commitB)
	assertCatalogCounts(t, database, repositoryID, 2, 2, 2)
}

func TestGetActiveCatalogRequiresActiveAcceptedCatalog(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		Name:           "infrastructure",
		CloneURL:       "https://github.com/example/infrastructure.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		ID:             repositoryID,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	_, err = database.GetActiveCatalog(ctx, repositoryID)
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("pending GetActiveCatalog() error = %v, want %v", err, catalog.ErrNotFound)
	}

	missingID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1299")
	_, err = database.GetActiveCatalog(ctx, missingID)
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("missing GetActiveCatalog() error = %v, want %v", err, catalog.ErrNotFound)
	}

	commitSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	acceptedAt := createdAt.Add(time.Minute)
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		successfulCatalogRefresh(commitSHA, acceptedAt, uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")),
	); err != nil {
		t.Fatalf("record empty catalog: %v", err)
	}

	active, err := database.GetActiveCatalog(ctx, repositoryID)
	if err != nil {
		t.Fatalf("get empty active catalog: %v", err)
	}
	if active.Revision.Catalog.Runtimes == nil || active.Revision.Catalog.Targets == nil ||
		len(active.Revision.Catalog.Runtimes) != 0 || len(active.Revision.Catalog.Targets) != 0 ||
		active.TargetStates == nil || len(active.TargetStates) != 0 {
		t.Errorf("empty active catalog = %#v", active)
	}

	if err := database.RemoveRepository(ctx, repositoryID, acceptedAt.Add(time.Minute)); err != nil {
		t.Fatalf("RemoveRepository() error = %v", err)
	}

	_, err = database.GetActiveCatalog(ctx, repositoryID)
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("removed GetActiveCatalog() error = %v, want %v", err, catalog.ErrNotFound)
	}
}

func TestCatalogRefreshTracksTargetDesiredCommitsIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300")
	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		Name:           "desired-commits",
		CloneURL:       "https://github.com/example/desired-commits.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		ID:             repositoryID,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	commitA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	first := successfulCatalogRefresh(
		commitA,
		createdAt.Add(time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1301"),
	)
	first.Revision.Catalog.Targets = []catalog.Target{
		testCatalogTarget("network", "infrastructure/network/**"),
		testCatalogTarget("operations", "operations/**"),
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, first); err != nil {
		t.Fatalf("record first catalog: %v", err)
	}

	active, err := database.GetActiveCatalog(ctx, repositoryID)
	if err != nil {
		t.Fatalf("load first catalog: %v", err)
	}
	assertDesiredCommit(t, active, "network", commitA)
	assertDesiredCommit(t, active, "operations", commitA)

	commitB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	second := successfulCatalogRefresh(
		commitB,
		createdAt.Add(2*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1302"),
	)
	second.ChangedPaths = []string{"docs/README.md"}
	second.Revision.Catalog.Targets = []catalog.Target{
		testCatalogTarget("network", "infrastructure/network/**"),
		testCatalogTarget("operations", "operations/**"),
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, second); err != nil {
		t.Fatalf("record unrelated catalog change: %v", err)
	}

	active, err = database.GetActiveCatalog(ctx, repositoryID)
	if err != nil {
		t.Fatalf("load second catalog: %v", err)
	}
	assertDesiredCommit(t, active, "network", commitA)
	assertDesiredCommit(t, active, "operations", commitA)

	commitC := "cccccccccccccccccccccccccccccccccccccccc"
	third := successfulCatalogRefresh(
		commitC,
		createdAt.Add(3*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1303"),
	)
	third.ChangedPaths = []string{"infrastructure/network/main.tf"}
	third.Revision.Catalog.Targets = []catalog.Target{
		testCatalogTarget("network", "infrastructure/network/**"),
		testCatalogTarget("operations", "operations/**"),
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, third); err != nil {
		t.Fatalf("record matching catalog change: %v", err)
	}

	active, err = database.GetActiveCatalog(ctx, repositoryID)
	if err != nil {
		t.Fatalf("load third catalog: %v", err)
	}
	assertDesiredCommit(t, active, "network", commitC)
	assertDesiredCommit(t, active, "operations", commitA)

	commitD := "dddddddddddddddddddddddddddddddddddddddd"
	fourth := successfulCatalogRefresh(
		commitD,
		createdAt.Add(4*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1304"),
	)
	fourth.Revision.Catalog.Targets = []catalog.Target{
		testCatalogTarget("operations", "operations/**"),
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, fourth); err != nil {
		t.Fatalf("record removed target catalog: %v", err)
	}

	var (
		archivedRevision pgtype.UUID
		archivedAt       pgtype.Timestamptz
		desiredCommit    string
	)
	err = database.pool.QueryRow(
		ctx,
		`SELECT catalog_revision_id, archived_at, desired_commit_sha
         FROM targets
         WHERE repository_id = $1 AND name = 'network'`,
		repositoryID,
	).Scan(&archivedRevision, &archivedAt, &desiredCommit)
	if err != nil {
		t.Fatalf("load archived target: %v", err)
	}
	if archivedRevision.Valid || !archivedAt.Valid || desiredCommit != commitC {
		t.Errorf(
			"archived target = (%#v, %#v, %q), want archived at %q",
			archivedRevision,
			archivedAt,
			desiredCommit,
			commitC,
		)
	}
}

func TestListActiveTargetsIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	firstRepositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1400")
	secondRepositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1401")
	repositories := []repository.Repository{
		{
			ID:             firstRepositoryID,
			Name:           "infrastructure",
			CloneURL:       "https://github.com/example/infrastructure.git",
			Branch:         "main",
			CommitIndexing: repository.CommitIndexingObservedHeads,
			CreatedAt:      createdAt,
		},
		{
			ID:             secondRepositoryID,
			Name:           "operations",
			CloneURL:       "https://github.com/example/operations.git",
			Branch:         "main",
			CommitIndexing: repository.CommitIndexingObservedHeads,
			CreatedAt:      createdAt,
		},
	}
	for _, record := range repositories {
		if _, err := database.CreateRepository(ctx, record); err != nil {
			t.Fatalf("CreateRepository(%q) error = %v", record.Name, err)
		}
	}

	firstCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	firstRefresh := successfulCatalogRefresh(
		firstCommit,
		createdAt.Add(time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1402"),
	)
	firstTarget := testCatalogTarget("network", "network/**")
	firstTarget.Metadata.Title = "Home network"
	firstTarget.Spec.Runtime = "terraform"
	firstTarget.Spec.Placement = "infrastructure"
	firstTarget.Spec.Actions["deploy"] = catalog.Action{Command: []string{"deploy"}}
	firstRefresh.Revision.Catalog.Targets = []catalog.Target{firstTarget}
	if err := database.RecordRepositoryRefresh(ctx, firstRepositoryID, firstRefresh); err != nil {
		t.Fatalf("record first catalog: %v", err)
	}

	secondCommit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	secondRefresh := successfulCatalogRefresh(
		secondCommit,
		createdAt.Add(2*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1403"),
	)
	secondTarget := testCatalogTarget("backups", "backups/**")
	secondTarget.Metadata.Title = "Backups"
	secondRefresh.Revision.Catalog.Targets = []catalog.Target{secondTarget}
	if err := database.RecordRepositoryRefresh(ctx, secondRepositoryID, secondRefresh); err != nil {
		t.Fatalf("record second catalog: %v", err)
	}

	page, err := database.ListActiveTargets(ctx, catalog.TargetListFilter{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("ListActiveTargets() error = %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 ||
		page.Items[0].Target.Metadata.Name != "backups" {
		t.Errorf("first target page = %#v", page)
	}

	search, err := database.ListActiveTargets(ctx, catalog.TargetListFilter{Query: "DEPLOY", Limit: 25})
	if err != nil {
		t.Fatalf("search active targets: %v", err)
	}
	if search.Total != 1 || len(search.Items) != 1 ||
		search.Items[0].RepositoryID != firstRepositoryID ||
		search.Items[0].CatalogCommitSHA != firstCommit ||
		search.Items[0].DesiredCommitSHA != firstCommit {
		t.Errorf("target search = %#v", search)
	}

	filtered, err := database.ListActiveTargets(ctx, catalog.TargetListFilter{
		RepositoryID: &secondRepositoryID,
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("filter active targets: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 ||
		filtered.Items[0].RepositoryName != "operations" {
		t.Errorf("repository target filter = %#v", filtered)
	}
}

func testCatalogTarget(name, watch string) catalog.Target {
	return catalog.Target{
		APIVersion: catalog.APIVersion,
		Kind:       catalog.TargetKind,
		Metadata:   catalog.Metadata{Name: name},
		Spec: catalog.TargetSpec{
			Watch: []string{watch},
			Actions: map[string]catalog.Action{
				"inspect": {Command: []string{"inspect"}},
			},
		},
		Source: catalog.Source{Path: ".slipramp/targets.yaml", Document: 1},
	}
}

func assertDesiredCommit(t *testing.T, active catalog.ActiveCatalog, targetName string, want string) {
	t.Helper()

	state, found := active.TargetStates[targetName]
	if !found {
		t.Fatalf("target state %q was not loaded", targetName)
	}
	if state.DesiredCommitSHA != want {
		t.Errorf("target %q desired commit = %q, want %q", targetName, state.DesiredCommitSHA, want)
	}
}

func assertCatalogCounts(
	t *testing.T,
	database *Database,
	repositoryID uuid.UUID,
	wantRevisions int,
	wantRuntimes int,
	wantTargets int,
) {
	t.Helper()

	var revisionCount, runtimeCount, targetCount int
	err := database.pool.QueryRow(
		t.Context(),
		`SELECT
            (SELECT count(*)
             FROM catalog_revisions
             WHERE repository_id = $1),
            (SELECT count(*)
             FROM catalog_runtimes
             WHERE catalog_revision_id IN (
                 SELECT id FROM catalog_revisions WHERE repository_id = $1
             )),
            (SELECT count(*)
             FROM catalog_targets
             WHERE catalog_revision_id IN (
                 SELECT id FROM catalog_revisions WHERE repository_id = $1
             ))`,
		repositoryID,
	).Scan(&revisionCount, &runtimeCount, &targetCount)
	if err != nil {
		t.Fatalf("count catalog records: %v", err)
	}

	if revisionCount != wantRevisions || runtimeCount != wantRuntimes ||
		targetCount != wantTargets {
		t.Errorf(
			"catalog counts = (%d revisions, %d runtimes, %d targets), want (%d, %d, %d)",
			revisionCount,
			runtimeCount,
			targetCount,
			wantRevisions,
			wantRuntimes,
			wantTargets,
		)
	}
}

func assertValidationFailureCount(t *testing.T, database *Database, repositoryID uuid.UUID, want int) {
	t.Helper()

	var count int
	err := database.pool.QueryRow(
		t.Context(),
		`SELECT count(*)
         FROM catalog_validation_failures
         WHERE repository_id = $1`,
		repositoryID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count catalog validation failures: %v", err)
	}
	if count != want {
		t.Errorf("catalog validation failure count = %d, want %d", count, want)
	}
}
