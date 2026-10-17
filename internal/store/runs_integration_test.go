package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestRunPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
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

	refresh := successfulCatalogRefresh(
		"0123456789abcdef0123456789abcdef01234567",
		createdAt.Add(time.Minute),
		revisionID,
	)
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("record catalog refresh: %v", err)
	}

	idempotencyKey := "manual-network-preview"
	record := run.Run{
		ID:                runID,
		State:             run.StateQueued,
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         *refresh.CommitSHA,
		TargetName:        "network",
		ActionName:        "preview",
		Parameters:        map[string]any{"refresh": true},
		PlacementName:     "infrastructure",
		ClusterName:       "colo",
		Source:            run.Source{Type: run.SourceTypeManual},
		IdempotencyKey:    &idempotencyKey,
		RequestDigest:     strings.Repeat("a", 64),
		Snapshot: run.Snapshot{
			Repository: run.RepositorySnapshot{
				Name:     "infrastructure",
				CloneURL: "https://github.com/example/infrastructure.git",
				Branch:   "main",
			},
			Target:  refresh.Revision.Catalog.Targets[0],
			Action:  catalog.EffectiveAction{Name: "preview"},
			Runtime: refresh.Revision.Catalog.Runtimes[0],
			Placement: run.PlacementSnapshot{
				Name:         "infrastructure",
				ClusterName:  "colo",
				JobNamespace: "slipramp-jobs",
			},
		},
		CreatedAt: createdAt.Add(2 * time.Minute),
	}

	created, err := database.CreateRun(ctx, record)
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.ID != record.ID || created.State != run.StateQueued ||
		created.CatalogRevisionID != revisionID ||
		created.Parameters["refresh"] != true ||
		created.Snapshot.Repository.CloneURL != record.Snapshot.Repository.CloneURL {
		t.Errorf("created Run = %#v", created)
	}

	retry := record
	retry.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203")
	retried, err := database.CreateRun(ctx, retry)
	if err != nil {
		t.Fatalf("idempotent CreateRun() error = %v", err)
	}
	if retried.ID != runID {
		t.Errorf("idempotent Run ID = %s, want %s", retried.ID, runID)
	}
	assertRunCounts(t, database, 1, 1)

	conflict := retry
	conflict.RequestDigest = strings.Repeat("b", 64)
	_, err = database.CreateRun(ctx, conflict)
	if !errors.Is(err, run.ErrIdempotencyConflict) {
		t.Errorf("conflicting CreateRun() error = %v", err)
	}
	assertRunCounts(t, database, 1, 1)

	second := record
	second.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1204")
	second.IdempotencyKey = nil
	second.RequestDigest = strings.Repeat("c", 64)
	second.CreatedAt = record.CreatedAt.Add(time.Minute)
	if _, err := database.CreateRun(ctx, second); err != nil {
		t.Fatalf("create second Run: %v", err)
	}

	claimedAt := second.CreatedAt.Add(time.Minute)
	claimed, err := database.ClaimQueuedRun(ctx, "colo", claimedAt)
	if err != nil {
		t.Fatalf("ClaimQueuedRun() error = %v", err)
	}
	if claimed.ID != record.ID || claimed.State != run.StatePreparing {
		t.Errorf("claimed Run = %#v", claimed)
	}

	redelivered, err := database.GetPreparingRunForCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("GetPreparingRunForCluster() error = %v", err)
	}
	if redelivered.ID != record.ID {
		t.Errorf("redelivered Run ID = %s, want %s", redelivered.ID, record.ID)
	}

	_, err = database.ClaimQueuedRun(ctx, "colo", claimedAt)
	if !errors.Is(err, run.ErrNoRunAssignment) {
		t.Errorf("second ClaimQueuedRun() error = %v, want no work", err)
	}

	completedAt := claimedAt.Add(time.Minute)
	if _, err := database.pool.Exec(
		ctx,
		"UPDATE runs SET state = 'succeeded' WHERE id = $1",
		databaseUUID(record.ID),
	); err != nil {
		t.Fatalf("complete first Run: %v", err)
	}
	if _, err := database.pool.Exec(
		ctx,
		`INSERT INTO run_state_transitions (run_id, state, recorded_at)
         VALUES ($1, 'succeeded', $2)`,
		databaseUUID(record.ID),
		completedAt,
	); err != nil {
		t.Fatalf("record first Run completion: %v", err)
	}

	claimed, err = database.ClaimQueuedRun(
		ctx,
		"colo",
		completedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("claim unblocked Run: %v", err)
	}
	if claimed.ID != second.ID || claimed.State != run.StatePreparing {
		t.Errorf("unblocked claimed Run = %#v", claimed)
	}
	assertRunCounts(t, database, 2, 5)
}

func assertRunCounts(
	t *testing.T,
	database *Database,
	wantRuns int,
	wantTransitions int,
) {
	t.Helper()

	var runCount, transitionCount int
	err := database.pool.QueryRow(
		t.Context(),
		`SELECT
            (SELECT count(*) FROM runs),
            (SELECT count(*) FROM run_state_transitions)`,
	).Scan(&runCount, &transitionCount)
	if err != nil {
		t.Fatalf("count Runs: %v", err)
	}
	if runCount != wantRuns || transitionCount != wantTransitions {
		t.Errorf(
			"counts = (%d Runs, %d transitions), want (%d, %d)",
			runCount,
			transitionCount,
			wantRuns,
			wantTransitions,
		)
	}
}
