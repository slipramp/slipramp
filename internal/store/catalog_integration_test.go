package store

import (
	"encoding/json"
	"errors"
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
		Name:      "infrastructure",
		CloneURL:  "https://github.com/example/infrastructure.git",
		Branch:    "main",
		ID:        repositoryID,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	commitA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	revisionAID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	acceptedAt := createdAt.Add(time.Minute)
	accepted := successfulCatalogRefresh(commitA, acceptedAt, revisionAID)
	accepted.Revision.Catalog = persistedCatalog("1")
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		accepted,
	); err != nil {
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
		CommitSHA:     &commitB,
		AttemptedAt:   rejectedAt,
		FailureReason: &rejectionReason,
	}
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		rejected,
	); err != nil {
		t.Fatalf("record rejected catalog: %v", err)
	}

	assertActiveCatalog(t, database, repositoryID, revisionAID, commitA)
	assertCatalogCounts(t, database, repositoryID, 1, 1, 1)

	repositories, err := database.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	refresh := repositories[0].Refresh
	if refresh.Status != repository.RefreshStatusFailed ||
		refresh.CommitSHA == nil || *refresh.CommitSHA != commitB ||
		refresh.AttemptedAt == nil || !refresh.AttemptedAt.Equal(rejectedAt) ||
		refresh.SucceededAt == nil || !refresh.SucceededAt.Equal(acceptedAt) ||
		refresh.FailureReason == nil ||
		*refresh.FailureReason != rejectionReason {
		t.Errorf("refresh state after catalog rejection = %#v", refresh)
	}

	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		rejected,
	); err != nil {
		t.Fatalf("repeat rejected catalog: %v", err)
	}
	assertValidationFailureCount(t, database, repositoryID, 1)

	revisionBID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	recoveredAt := rejectedAt.Add(time.Minute)
	recovered := successfulCatalogRefresh(commitB, recoveredAt, revisionBID)
	recovered.Revision.Catalog = persistedCatalog("2")
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		recovered,
	); err != nil {
		t.Fatalf("record recovered catalog: %v", err)
	}

	assertActiveCatalog(t, database, repositoryID, revisionBID, commitB)
	assertCatalogCounts(t, database, repositoryID, 2, 2, 2)

	duplicate := successfulCatalogRefresh(
		commitB,
		recoveredAt.Add(time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203"),
	)
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		duplicate,
	); err != nil {
		t.Fatalf("repeat accepted catalog: %v", err)
	}

	assertActiveCatalog(t, database, repositoryID, revisionBID, commitB)
	assertCatalogCounts(t, database, repositoryID, 2, 2, 2)
}

func TestGetActiveCatalogRevisionRequiresActiveAcceptedCatalog(
	t *testing.T,
) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		Name:      "infrastructure",
		CloneURL:  "https://github.com/example/infrastructure.git",
		Branch:    "main",
		ID:        repositoryID,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	_, err = database.GetActiveCatalogRevision(ctx, repositoryID)
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf(
			"pending GetActiveCatalogRevision() error = %v, want %v",
			err,
			catalog.ErrNotFound,
		)
	}

	missingID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1299")
	_, err = database.GetActiveCatalogRevision(ctx, missingID)
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf(
			"missing GetActiveCatalogRevision() error = %v, want %v",
			err,
			catalog.ErrNotFound,
		)
	}

	commitSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	acceptedAt := createdAt.Add(time.Minute)
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		successfulCatalogRefresh(
			commitSHA,
			acceptedAt,
			uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201"),
		),
	); err != nil {
		t.Fatalf("record empty catalog: %v", err)
	}

	active, err := database.GetActiveCatalogRevision(ctx, repositoryID)
	if err != nil {
		t.Fatalf("get empty active catalog: %v", err)
	}
	if active.Catalog.Runtimes == nil || active.Catalog.Targets == nil ||
		len(active.Catalog.Runtimes) != 0 || len(active.Catalog.Targets) != 0 {
		t.Errorf("empty active catalog = %#v", active.Catalog)
	}

	if err := database.RemoveRepository(
		ctx,
		repositoryID,
		acceptedAt.Add(time.Minute),
	); err != nil {
		t.Fatalf("RemoveRepository() error = %v", err)
	}

	_, err = database.GetActiveCatalogRevision(ctx, repositoryID)
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf(
			"removed GetActiveCatalogRevision() error = %v, want %v",
			err,
			catalog.ErrNotFound,
		)
	}
}

func successfulCatalogRefresh(
	commitSHA string,
	attemptedAt time.Time,
	revisionID uuid.UUID,
) repository.RefreshResult {
	return repository.RefreshResult{
		CommitSHA:   &commitSHA,
		AttemptedAt: attemptedAt,
		Revision: &catalog.Revision{
			ID:        revisionID,
			CommitSHA: commitSHA,
			CreatedAt: attemptedAt,
		},
	}
}

func persistedCatalog(imageVersion string) catalog.Catalog {
	return catalog.Catalog{
		Runtimes: []catalog.Runtime{{
			APIVersion: catalog.APIVersion,
			Kind:       catalog.RuntimeKind,
			Metadata:   catalog.Metadata{Name: "terraform"},
			Spec: catalog.RuntimeSpec{
				Image: "hashicorp/terraform:" + imageVersion,
			},
			Source: catalog.Source{
				Path:     ".slipramp/catalog.yaml",
				Document: 1,
			},
		}},
		Targets: []catalog.Target{{
			APIVersion: catalog.APIVersion,
			Kind:       catalog.TargetKind,
			Metadata:   catalog.Metadata{Name: "network"},
			Spec: catalog.TargetSpec{
				Runtime:   "terraform",
				Placement: "infrastructure",
				Actions: map[string]catalog.Action{
					"preview": {Command: []string{"terraform", "plan"}},
				},
			},
			Source: catalog.Source{
				Path:     ".slipramp/catalog.yaml",
				Document: 2,
			},
		}},
	}
}

func assertActiveCatalog(
	t *testing.T,
	database *Database,
	repositoryID uuid.UUID,
	wantRevisionID uuid.UUID,
	wantCommitSHA string,
) {
	t.Helper()

	var (
		revisionID pgtype.UUID
		commitSHA  string
	)
	err := database.pool.QueryRow(
		t.Context(),
		`SELECT r.active_catalog_revision_id, c.commit_sha
         FROM repositories r
         JOIN catalog_revisions c ON c.id = r.active_catalog_revision_id
         WHERE r.id = $1`,
		repositoryID,
	).Scan(&revisionID, &commitSHA)
	if err != nil {
		t.Fatalf("load active catalog: %v", err)
	}
	if uuid.UUID(revisionID.Bytes) != wantRevisionID || commitSHA != wantCommitSHA {
		t.Errorf(
			"active catalog = (%s, %q), want (%s, %q)",
			uuid.UUID(revisionID.Bytes),
			commitSHA,
			wantRevisionID,
			wantCommitSHA,
		)
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

func assertValidationFailureCount(
	t *testing.T,
	database *Database,
	repositoryID uuid.UUID,
	want int,
) {
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
