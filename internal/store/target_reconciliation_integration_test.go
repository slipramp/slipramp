package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestTargetReconciliationPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	repositoryID := uuid.New()
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:             repositoryID,
		Name:           "target-reconciliation",
		CloneURL:       "https://github.com/example/infrastructure.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	firstCommit := strings.Repeat("a", 40)
	first := successfulCatalogRefresh(firstCommit, createdAt.Add(time.Minute), uuid.New())
	first.Revision.Catalog = reconciliationCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, first); err != nil {
		t.Fatalf("record first catalog: %v", err)
	}
	targetID := activeTargetID(t, database, repositoryID, "network")

	// A non-sync action does not participate even when its intent looks like reconciliation.
	nonSync := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, first.Revision.ID, firstCommit,
		firstCommit, "preview", createdAt.Add(2*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, nonSync.ID, createdAt.Add(3*time.Minute))
	finishReconciliationRun(t, ctx, database, nonSync.ID, run.OutcomeSucceeded, createdAt.Add(4*time.Minute))
	assertTargetReconciliation(t, database, targetID, nil, nil, catalog.ReconciliationComparisonUnknown)

	// Canceling while queued is not an attempt.
	queued := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, first.Revision.ID, firstCommit,
		firstCommit, "sync", createdAt.Add(5*time.Minute),
	)
	if _, err := database.CancelRun(ctx, queued.ID, createdAt.Add(6*time.Minute), nil); err != nil {
		t.Fatalf("cancel queued sync: %v", err)
	}
	assertTargetReconciliation(t, database, targetID, nil, nil, catalog.ReconciliationComparisonUnknown)

	// Cancellation after assignment is an attempt but not a success.
	activeCanceled := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, first.Revision.ID, firstCommit,
		firstCommit, "sync", createdAt.Add(7*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, activeCanceled.ID, createdAt.Add(8*time.Minute))
	if _, err := database.CancelRun(ctx, activeCanceled.ID, createdAt.Add(9*time.Minute), nil); err != nil {
		t.Fatalf("request active sync cancellation: %v", err)
	}
	finishReconciliationRun(t, ctx, database, activeCanceled.ID, run.OutcomeCanceled, createdAt.Add(10*time.Minute))
	assertTargetReconciliation(
		t, database, targetID, &activeCanceled.ID, nil, catalog.ReconciliationComparisonUnknown,
	)

	for index, outcome := range []run.Outcome{run.OutcomeFailed, run.OutcomeTimedOut} {
		startedAt := createdAt.Add(time.Duration(11+index*3) * time.Minute)
		failed := createReconciliationRun(
			t, ctx, database, repositoryID, targetID, first.Revision.ID, firstCommit,
			firstCommit, "sync", startedAt,
		)
		claimReconciliationRun(t, ctx, database, failed.ID, startedAt.Add(time.Minute))
		finishReconciliationRun(t, ctx, database, failed.ID, outcome, startedAt.Add(2*time.Minute))
		assertTargetReconciliation(
			t, database, targetID, &failed.ID, nil, catalog.ReconciliationComparisonUnknown,
		)
	}

	firstSuccess := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, first.Revision.ID, firstCommit,
		firstCommit, "sync", createdAt.Add(18*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, firstSuccess.ID, createdAt.Add(19*time.Minute))
	finishReconciliationRun(t, ctx, database, firstSuccess.ID, run.OutcomeSucceeded, createdAt.Add(20*time.Minute))
	assertTargetReconciliation(
		t, database, targetID, &firstSuccess.ID, &firstSuccess.ID, catalog.ReconciliationComparisonMatched,
	)

	// A refresh can advance desired state during an active sync without rewriting its marker.
	refreshRace := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, first.Revision.ID, firstCommit,
		firstCommit, "sync", createdAt.Add(21*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, refreshRace.ID, createdAt.Add(22*time.Minute))
	secondCommit := strings.Repeat("b", 40)
	second := successfulCatalogRefresh(secondCommit, createdAt.Add(23*time.Minute), uuid.New())
	second.Revision.Catalog = reconciliationCatalog()
	second.ChangedPaths = []string{"infrastructure/network/main.tf"}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, second); err != nil {
		t.Fatalf("refresh during active sync: %v", err)
	}
	loadedRace, err := database.GetRun(ctx, refreshRace.ID)
	if err != nil {
		t.Fatalf("load active sync after refresh: %v", err)
	}
	if loadedRace.TargetRelevanceCommitSHA == nil || *loadedRace.TargetRelevanceCommitSHA != firstCommit {
		t.Errorf("active Run relevance marker = %v, want %q", loadedRace.TargetRelevanceCommitSHA, firstCommit)
	}
	finishReconciliationRun(t, ctx, database, refreshRace.ID, run.OutcomeSucceeded, createdAt.Add(24*time.Minute))
	assertTargetReconciliation(
		t, database, targetID, &refreshRace.ID, &refreshRace.ID, catalog.ReconciliationComparisonDifferent,
	)

	currentSuccess := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, second.Revision.ID, secondCommit,
		secondCommit, "sync", createdAt.Add(25*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, currentSuccess.ID, createdAt.Add(26*time.Minute))
	finishReconciliationRun(t, ctx, database, currentSuccess.ID, run.OutcomeSucceeded, createdAt.Add(27*time.Minute))
	assertTargetReconciliation(
		t, database, targetID, &currentSuccess.ID, &currentSuccess.ID, catalog.ReconciliationComparisonMatched,
	)

	// A later exact rerun of the old revision is a new factual checkpoint.
	exactOld := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, first.Revision.ID, firstCommit,
		firstCommit, "sync", createdAt.Add(28*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, exactOld.ID, createdAt.Add(29*time.Minute))
	finishReconciliationRun(t, ctx, database, exactOld.ID, run.OutcomeSucceeded, createdAt.Add(30*time.Minute))
	assertTargetReconciliation(
		t, database, targetID, &exactOld.ID, &exactOld.ID, catalog.ReconciliationComparisonDifferent,
	)

	// Restore a current success, then fail a later attempt. The failure remains visible while
	// the successful comparison remains matched.
	currentSuccess = createReconciliationRun(
		t, ctx, database, repositoryID, targetID, second.Revision.ID, secondCommit,
		secondCommit, "sync", createdAt.Add(31*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, currentSuccess.ID, createdAt.Add(32*time.Minute))
	finishReconciliationRun(t, ctx, database, currentSuccess.ID, run.OutcomeSucceeded, createdAt.Add(33*time.Minute))
	laterFailure := createReconciliationRun(
		t, ctx, database, repositoryID, targetID, second.Revision.ID, secondCommit,
		secondCommit, "sync", createdAt.Add(34*time.Minute),
	)
	claimReconciliationRun(t, ctx, database, laterFailure.ID, createdAt.Add(35*time.Minute))
	finishReconciliationRun(t, ctx, database, laterFailure.ID, run.OutcomeFailed, createdAt.Add(36*time.Minute))
	assertTargetReconciliation(
		t, database, targetID, &laterFailure.ID, &currentSuccess.ID, catalog.ReconciliationComparisonMatched,
	)

	// Repeating an older terminal observation cannot move either checkpoint backward.
	finishReconciliationRun(t, ctx, database, exactOld.ID, run.OutcomeSucceeded, createdAt.Add(37*time.Minute))
	assertTargetReconciliation(
		t, database, targetID, &laterFailure.ID, &currentSuccess.ID, catalog.ReconciliationComparisonMatched,
	)

	archived := successfulCatalogRefresh(strings.Repeat("c", 40), createdAt.Add(38*time.Minute), uuid.New())
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, archived); err != nil {
		t.Fatalf("archive Target: %v", err)
	}
	assertTargetReconciliation(
		t, database, targetID, &laterFailure.ID, &currentSuccess.ID, catalog.ReconciliationComparisonUnknown,
	)

	reactivatedCommit := strings.Repeat("d", 40)
	reactivated := successfulCatalogRefresh(reactivatedCommit, createdAt.Add(39*time.Minute), uuid.New())
	reactivated.Revision.Catalog = reconciliationCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, reactivated); err != nil {
		t.Fatalf("reactivate Target: %v", err)
	}
	assertTargetReconciliation(
		t, database, targetID, &laterFailure.ID, &currentSuccess.ID, catalog.ReconciliationComparisonDifferent,
	)
}

func reconciliationCatalog() catalog.Catalog {
	result := persistedCatalog("1.14.3")
	target := &result.Targets[0]
	preview := target.Spec.Actions["preview"]
	preview.Intent = "reconcile"
	target.Spec.Actions["preview"] = preview
	target.Spec.Actions["sync"] = catalog.Action{Command: []string{"terraform", "apply"}}

	return result
}

func createReconciliationRun(
	t *testing.T,
	ctx context.Context,
	database *Database,
	repositoryID uuid.UUID,
	targetID uuid.UUID,
	revisionID uuid.UUID,
	checkoutCommitSHA string,
	relevanceCommitSHA string,
	actionName string,
	createdAt time.Time,
) run.Run {
	t.Helper()
	record := run.Run{
		ID:                       uuid.New(),
		Phase:                    run.PhaseQueued,
		Review:                   run.Review{State: run.ReviewStateNotRequired},
		TargetID:                 targetID,
		RepositoryID:             repositoryID,
		CatalogRevisionID:        revisionID,
		CommitSHA:                checkoutCommitSHA,
		TargetRelevanceCommitSHA: &relevanceCommitSHA,
		TargetName:               "network",
		ActionName:               actionName,
		Parameters:               map[string]any{},
		PlacementName:            "infrastructure",
		ClusterName:              "colo",
		Source:                   run.Source{Type: run.SourceTypeManual},
		RequestDigest:            strings.Repeat("e", 64),
		Snapshot:                 run.Snapshot{},
		CreatedAt:                createdAt,
	}
	created, err := database.CreateRun(ctx, record, nil)
	if err != nil {
		t.Fatalf("create %s Run: %v", actionName, err)
	}

	return created
}

func claimReconciliationRun(
	t *testing.T,
	ctx context.Context,
	database *Database,
	wantRunID uuid.UUID,
	assignedAt time.Time,
) {
	t.Helper()
	claimed, err := database.ClaimQueuedRun(ctx, "colo", testSchedulingCapacity(), assignedAt)
	if err != nil {
		t.Fatalf("claim Run %s: %v", wantRunID, err)
	}
	if claimed.ID != wantRunID {
		t.Fatalf("claimed Run = %s, want %s", claimed.ID, wantRunID)
	}
}

func finishReconciliationRun(
	t *testing.T,
	ctx context.Context,
	database *Database,
	runID uuid.UUID,
	outcome run.Outcome,
	finishedAt time.Time,
) {
	t.Helper()
	if err := database.ApplyRunObservation(ctx, run.Observation{
		ID:          runID,
		ClusterName: "colo",
		Phase:       run.PhaseFinished,
		Outcome:     &outcome,
		ObservedAt:  finishedAt,
	}); err != nil {
		t.Fatalf("finish Run %s as %s: %v", runID, outcome, err)
	}
}

func assertTargetReconciliation(
	t *testing.T,
	database *Database,
	targetID uuid.UUID,
	wantAttempted *uuid.UUID,
	wantSuccessful *uuid.UUID,
	wantComparison catalog.ReconciliationComparison,
) {
	t.Helper()
	record, err := database.GetTarget(t.Context(), targetID)
	if err != nil {
		t.Fatalf("get Target reconciliation: %v", err)
	}
	if checkpointRunID(record.LastAttemptedSync) != optionalUUID(wantAttempted) ||
		checkpointRunID(record.LastSuccessfulSync) != optionalUUID(wantSuccessful) ||
		record.ReconciliationComparison() != wantComparison {
		t.Errorf(
			"Target reconciliation = (attempted %s, successful %s, comparison %s), want (%s, %s, %s)",
			checkpointRunID(record.LastAttemptedSync),
			checkpointRunID(record.LastSuccessfulSync),
			record.ReconciliationComparison(),
			optionalUUID(wantAttempted),
			optionalUUID(wantSuccessful),
			wantComparison,
		)
	}
}

func checkpointRunID(checkpoint *catalog.SyncRunCheckpoint) uuid.UUID {
	if checkpoint == nil {
		return uuid.Nil
	}

	return checkpoint.RunID
}

func optionalUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}

	return *value
}
