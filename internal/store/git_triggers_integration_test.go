package store

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestGitTriggerDeliveryLifecycleIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1600")
	createdAt := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:        repositoryID,
		Name:      "git-triggers",
		CloneURL:  "https://github.com/example/git-triggers.git",
		Branch:    "main",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	commitA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	revisionAID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1601")
	initial := successfulCatalogRefresh(commitA, createdAt.Add(time.Minute), revisionAID)
	initial.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, initial); err != nil {
		t.Fatalf("record initial catalog: %v", err)
	}
	assertGitTriggerDeliveryCount(t, database, 0)

	unchanged := successfulCatalogRefresh(
		commitA,
		createdAt.Add(2*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1602"),
	)
	unchanged.ChangedPaths = []string{"infrastructure/network/main.tf"}
	unchanged.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, unchanged); err != nil {
		t.Fatalf("record unchanged catalog: %v", err)
	}
	assertGitTriggerDeliveryCount(t, database, 0)

	commitB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	rejectionReason := "catalog is invalid"
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, repository.RefreshResult{
		Commit:        &repository.Commit{SHA: commitB},
		AttemptedAt:   createdAt.Add(3 * time.Minute),
		FailureReason: &rejectionReason,
	}); err != nil {
		t.Fatalf("record rejected catalog: %v", err)
	}
	assertGitTriggerDeliveryCount(t, database, 0)

	revisionBID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1603")
	nonmatching := successfulCatalogRefresh(commitB, createdAt.Add(4*time.Minute), revisionBID)
	nonmatching.ChangedPaths = []string{"docs/README.md"}
	nonmatching.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, nonmatching); err != nil {
		t.Fatalf("record non-matching catalog: %v", err)
	}
	assertGitTriggerDeliveryCount(t, database, 0)

	commitC := "cccccccccccccccccccccccccccccccccccccccc"
	revisionCID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1604")
	matching := successfulCatalogRefresh(commitC, createdAt.Add(5*time.Minute), revisionCID)
	matching.ChangedPaths = []string{"infrastructure/network/main.tf"}
	matching.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, matching); err != nil {
		t.Fatalf("record matching catalog: %v", err)
	}
	assertActiveCatalog(t, database, repositoryID, revisionCID, commitC)
	assertGitTriggerDeliveryCount(t, database, 1)
	oldest, err := database.GetPendingGitTriggerDelivery(ctx)
	if err != nil {
		t.Fatalf("get first pending delivery: %v", err)
	}
	wantOldestParameters := gitTriggerParameters("production")
	if !reflect.DeepEqual(oldest.Parameters, wantOldestParameters) {
		t.Errorf("stored delivery parameters = %#v, want %#v", oldest.Parameters, wantOldestParameters)
	}

	// Accept another matching head before dispatching the first delivery. The
	// older pending delivery is retained as superseded and only the latest
	// revision remains eligible for dispatch.
	commitD := "dddddddddddddddddddddddddddddddddddddddd"
	revisionDID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1605")
	later := successfulCatalogRefresh(commitD, createdAt.Add(7*time.Minute), revisionDID)
	later.ChangedPaths = []string{"infrastructure/network/outputs.tf"}
	later.Revision.Catalog = gitTriggeredCatalogWithEnvironment("staging")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, later); err != nil {
		t.Fatalf("record later matching catalog: %v", err)
	}
	assertGitTriggerDeliveryCount(t, database, 2)
	assertGitTriggerDeliveryState(t, database, oldest.ID, true, false)
	pending, err := database.GetPendingGitTriggerDelivery(ctx)
	if err != nil {
		t.Fatalf("get latest delivery after later refresh: %v", err)
	}
	if pending.CatalogRevisionID != revisionDID {
		t.Errorf("pending revision = %s, want %s", pending.CatalogRevisionID, revisionDID)
	}
	wantLaterParameters := gitTriggerParameters("staging")
	if !reflect.DeepEqual(pending.Parameters, wantLaterParameters) {
		t.Errorf("pending parameters after refresh = %#v, want %#v", pending.Parameters, wantLaterParameters)
	}

	placements, err := placement.NewSet(map[string]placement.Config{
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
		},
	})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}
	repositories := repository.NewService(database, repository.RefreshCredentials{})
	catalogs := catalog.NewService(database)
	runs := run.NewService(run.Dependencies{
		Creation:      database,
		History:       database,
		Assignments:   database,
		Cancellations: database,
		Observations:  database,
		Repositories:  repositories,
		Catalogs:      catalogs,
		Placements:    placements,
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	failingStore := &failingGitTriggerCompletionStore{database: database}
	dispatcher := gittrigger.NewDispatcher(failingStore, runs, time.Second, logger)
	processed, err := dispatcher.ProcessNext(ctx)
	if err == nil || processed {
		t.Fatalf("first ProcessNext() = (%t, %v), want completion failure", processed, err)
	}

	// A fresh dispatcher retries after Run creation succeeded but before its ID
	// was recorded on the delivery. The real Run store must recover the same Run
	// by delivery idempotency rather than inserting another one.
	dispatcher = gittrigger.NewDispatcher(database, runs, time.Second, logger)
	processed, err = dispatcher.ProcessNext(ctx)
	if err != nil || !processed {
		t.Fatalf("retried ProcessNext() = (%t, %v), want success", processed, err)
	}

	createdRuns, err := database.ListRuns(ctx, run.RunListFilter{RepositoryID: &repositoryID, Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(createdRuns.Items) != 1 {
		t.Fatalf("created Runs = %d, want 1", len(createdRuns.Items))
	}
	createdRun := createdRuns.Items[0]
	if createdRun.CatalogRevisionID != revisionDID || createdRun.CommitSHA != commitD {
		t.Errorf("Run pin = (%s, %s), want (%s, %s)", createdRun.CatalogRevisionID, createdRun.CommitSHA, revisionDID, commitD)
	}
	if createdRun.CreatedBy != nil || createdRun.Source.Git == nil ||
		createdRun.Source.Git.TriggerName != "reconcile-on-change" {
		t.Errorf("Git-triggered Run source/actor = (%#v, %#v)", createdRun.Source, createdRun.CreatedBy)
	}
	if !reflect.DeepEqual(createdRun.Parameters, wantLaterParameters) {
		t.Errorf("Git-triggered Run parameters = %#v, want %#v", createdRun.Parameters, wantLaterParameters)
	}
	if _, err := database.GetPendingGitTriggerDelivery(ctx); !errors.Is(err, gittrigger.ErrNoPendingDelivery) {
		t.Fatalf("get pending delivery after dispatch error = %v, want no pending delivery", err)
	}
}

func TestGitTriggerLatestWinsBehindActiveRunIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1700")
	baseTime := time.Now().UTC().Truncate(time.Second)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:        repositoryID,
		Name:      "latest-wins",
		CloneURL:  "https://github.com/example/latest-wins.git",
		Branch:    "main",
		CreatedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	commitA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	initial := successfulCatalogRefresh(
		commitA,
		baseTime.Add(time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1701"),
	)
	initial.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, initial); err != nil {
		t.Fatalf("record initial catalog: %v", err)
	}

	runs, dispatcher := newGitTriggerTestServices(t, database)
	commitB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	revisionBID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1702")
	refreshB := successfulCatalogRefresh(commitB, baseTime.Add(2*time.Minute), revisionBID)
	refreshB.ChangedPaths = []string{"infrastructure/network/main.tf"}
	refreshB.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refreshB); err != nil {
		t.Fatalf("record revision B: %v", err)
	}
	if processed, err := dispatcher.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("dispatch revision B = (%t, %v), want success", processed, err)
	}

	listed, err := database.ListRuns(ctx, run.RunListFilter{RepositoryID: &repositoryID, Limit: 10})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("list revision B Run = (%d, %v), want one Run", len(listed.Items), err)
	}
	runB := listed.Items[0]
	assignedAt := baseTime.Add(3 * time.Minute)
	if _, err := database.ClaimQueuedRun(ctx, "colo", assignedAt); err != nil {
		t.Fatalf("claim revision B Run: %v", err)
	}
	jobUID := "62c9e83f-408e-4d2f-b0f4-c28a300f4c90"
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: runB.ID, ClusterName: "colo", Phase: run.PhasePreparing,
			JobUID: &jobUID, ObservedAt: baseTime.Add(4 * time.Minute),
		},
		[]run.Phase{run.PhaseAssigned},
	); err != nil {
		t.Fatalf("prepare revision B Run: %v", err)
	}
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: runB.ID, ClusterName: "colo", Phase: run.PhaseRunning,
			JobUID: &jobUID, ObservedAt: baseTime.Add(5 * time.Minute),
		},
		[]run.Phase{run.PhasePreparing},
	); err != nil {
		t.Fatalf("start revision B Run: %v", err)
	}

	commitC := "cccccccccccccccccccccccccccccccccccccccc"
	revisionCID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1703")
	refreshC := successfulCatalogRefresh(commitC, baseTime.Add(6*time.Minute), revisionCID)
	refreshC.ChangedPaths = []string{"infrastructure/network/main.tf"}
	refreshC.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refreshC); err != nil {
		t.Fatalf("record revision C: %v", err)
	}
	if processed, err := dispatcher.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("dispatch revision C = (%t, %v), want success", processed, err)
	}

	listed, err = database.ListRuns(ctx, run.RunListFilter{RepositoryID: &repositoryID, Limit: 10})
	if err != nil || len(listed.Items) != 2 {
		t.Fatalf("list revisions B and C Runs = (%d, %v), want two Runs", len(listed.Items), err)
	}
	runC := listed.Items[0]
	if runC.CommitSHA != commitC || runC.Phase != run.PhaseQueued {
		t.Fatalf("revision C Run = %#v", runC)
	}

	commitD := "dddddddddddddddddddddddddddddddddddddddd"
	revisionDID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1704")
	refreshD := successfulCatalogRefresh(commitD, baseTime.Add(7*time.Minute), revisionDID)
	refreshD.ChangedPaths = []string{"infrastructure/network/outputs.tf"}
	refreshD.Revision.Catalog = gitTriggeredCatalogWithEnvironment("staging")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refreshD); err != nil {
		t.Fatalf("record revision D: %v", err)
	}

	preservedB, err := runs.GetRun(ctx, runB.ID)
	if err != nil {
		t.Fatalf("get active revision B Run: %v", err)
	}
	if preservedB.Phase != run.PhaseRunning || preservedB.Outcome != nil {
		t.Errorf("active revision B Run after revision D = %#v", preservedB)
	}
	supersededC, err := runs.GetRun(ctx, runC.ID)
	if err != nil {
		t.Fatalf("get superseded revision C Run: %v", err)
	}
	if supersededC.Phase != run.PhaseFinished ||
		supersededC.Outcome == nil || *supersededC.Outcome != run.OutcomeCanceled ||
		supersededC.OutcomeReason == nil ||
		*supersededC.OutcomeReason != run.ReasonSupersededByGitChange {
		t.Errorf("superseded revision C Run = %#v", supersededC)
	}
	assertSupersededRunTransition(t, database, runC.ID)

	pendingD, err := database.GetPendingGitTriggerDelivery(ctx)
	if err != nil {
		t.Fatalf("get revision D delivery: %v", err)
	}
	if pendingD.CatalogRevisionID != revisionDID {
		t.Errorf("pending revision = %s, want %s", pendingD.CatalogRevisionID, revisionDID)
	}
	if processed, err := dispatcher.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("dispatch revision D = (%t, %v), want success", processed, err)
	}
	if _, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(8*time.Minute)); !errors.Is(err, run.ErrNoRunAssignment) {
		t.Fatalf("claim while revision B is active error = %v, want no assignment", err)
	}

	succeeded := run.OutcomeSucceeded
	completedReason := "Completed"
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: runB.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &succeeded, JobUID: &jobUID, Reason: &completedReason,
			ObservedAt: baseTime.Add(9 * time.Minute),
		},
		[]run.Phase{run.PhaseRunning},
	); err != nil {
		t.Fatalf("finish revision B Run: %v", err)
	}
	claimedD, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("claim revision D Run: %v", err)
	}
	if claimedD.CommitSHA != commitD || claimedD.Phase != run.PhaseAssigned {
		t.Errorf("claimed latest Run = %#v", claimedD)
	}
}

func TestGitTriggerTargetRemovalCancelsQueuedRunIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1750")
	baseTime := time.Now().UTC().Truncate(time.Second)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "target-removal", CloneURL: "https://github.com/example/target-removal.git",
		Branch: "main", CreatedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	initial := successfulCatalogRefresh(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		baseTime.Add(time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1751"),
	)
	initial.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, initial); err != nil {
		t.Fatalf("record initial catalog: %v", err)
	}

	changed := successfulCatalogRefresh(
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		baseTime.Add(2*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1752"),
	)
	changed.ChangedPaths = []string{"infrastructure/network/main.tf"}
	changed.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, changed); err != nil {
		t.Fatalf("record changed catalog: %v", err)
	}

	runs, dispatcher := newGitTriggerTestServices(t, database)
	if processed, err := dispatcher.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("dispatch changed catalog = (%t, %v), want success", processed, err)
	}
	listed, err := runs.ListRuns(ctx, run.RunListFilter{RepositoryID: &repositoryID, Limit: 10})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("list queued Git Run = (%d, %v), want one Run", len(listed.Items), err)
	}

	removed := successfulCatalogRefresh(
		"cccccccccccccccccccccccccccccccccccccccc",
		baseTime.Add(3*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1753"),
	)
	removed.ChangedPaths = []string{".slipramp/catalog.yaml"}
	removed.Revision.Catalog = gitTriggeredCatalog()
	removed.Revision.Catalog.Targets = nil
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, removed); err != nil {
		t.Fatalf("record target removal: %v", err)
	}

	stored, err := runs.GetRun(ctx, listed.Items[0].ID)
	if err != nil {
		t.Fatalf("get queued Git Run after target removal: %v", err)
	}
	if stored.Phase != run.PhaseFinished || stored.Outcome == nil ||
		*stored.Outcome != run.OutcomeCanceled || stored.OutcomeReason == nil ||
		*stored.OutcomeReason != run.ReasonSupersededByGitChange {
		t.Errorf("Git Run after target removal = %#v", stored)
	}
	assertSupersededRunTransition(t, database, stored.ID)
	if _, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(4*time.Minute)); !errors.Is(err, run.ErrNoRunAssignment) {
		t.Fatalf("claim after target removal error = %v, want no assignment", err)
	}
}

func TestGitRefreshPreservesManualAndAPIFIFOIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1800")
	revisionAID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1801")
	baseTime := time.Now().UTC().Truncate(time.Second)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "fifo", CloneURL: "https://github.com/example/fifo.git",
		Branch: "main", CreatedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	commitA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	initial := successfulCatalogRefresh(commitA, baseTime.Add(time.Minute), revisionAID)
	initial.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, initial); err != nil {
		t.Fatalf("record initial catalog: %v", err)
	}

	manual := queuedTestRun(
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1802"),
		repositoryID,
		revisionAID,
		commitA,
		run.SourceTypeManual,
		baseTime.Add(2*time.Minute),
		"a",
	)
	apiRun := queuedTestRun(
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1803"),
		repositoryID,
		revisionAID,
		commitA,
		run.SourceTypeAPI,
		baseTime.Add(3*time.Minute),
		"b",
	)
	if _, err := database.CreateRun(ctx, manual); err != nil {
		t.Fatalf("create manual Run: %v", err)
	}
	if _, err := database.CreateRun(ctx, apiRun); err != nil {
		t.Fatalf("create API Run: %v", err)
	}

	refreshB := successfulCatalogRefresh(
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		baseTime.Add(4*time.Minute),
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1804"),
	)
	refreshB.ChangedPaths = []string{"infrastructure/network/main.tf"}
	refreshB.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refreshB); err != nil {
		t.Fatalf("record changed catalog: %v", err)
	}
	for _, record := range []run.Run{manual, apiRun} {
		stored, err := database.GetRun(ctx, record.ID)
		if err != nil {
			t.Fatalf("get %s Run: %v", record.Source.Type, err)
		}
		if stored.Phase != run.PhaseQueued || stored.Outcome != nil {
			t.Errorf("%s Run after Git refresh = %#v", record.Source.Type, stored)
		}
	}

	claimedManual, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("claim manual Run: %v", err)
	}
	if claimedManual.ID != manual.ID {
		t.Errorf("first FIFO Run = %s, want manual %s", claimedManual.ID, manual.ID)
	}
	succeeded := run.OutcomeSucceeded
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: manual.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &succeeded, ObservedAt: baseTime.Add(6 * time.Minute),
		},
		[]run.Phase{run.PhaseAssigned},
	); err != nil {
		t.Fatalf("finish manual Run: %v", err)
	}
	claimedAPI, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(7*time.Minute))
	if err != nil {
		t.Fatalf("claim API Run: %v", err)
	}
	if claimedAPI.ID != apiRun.ID {
		t.Errorf("second FIFO Run = %s, want API %s", claimedAPI.ID, apiRun.ID)
	}
}

func TestGitRunCreationRejectsRefreshRaceAtDatabaseBoundaryIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1900")
	revisionAID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1901")
	revisionBID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1902")
	baseTime := time.Now().UTC().Truncate(time.Second)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "dispatch-race", CloneURL: "https://github.com/example/race.git",
		Branch: "main", CreatedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	initial := successfulCatalogRefresh(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		baseTime.Add(time.Minute),
		revisionAID,
	)
	initial.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, initial); err != nil {
		t.Fatalf("record initial catalog: %v", err)
	}
	commitB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	refreshB := successfulCatalogRefresh(commitB, baseTime.Add(2*time.Minute), revisionBID)
	refreshB.ChangedPaths = []string{"infrastructure/network/main.tf"}
	refreshB.Revision.Catalog = gitTriggeredCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refreshB); err != nil {
		t.Fatalf("record revision B: %v", err)
	}
	delivery, err := database.GetPendingGitTriggerDelivery(ctx)
	if err != nil {
		t.Fatalf("capture revision B delivery: %v", err)
	}

	refreshTransaction, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin simulated refresh transaction: %v", err)
	}
	defer func() { _ = refreshTransaction.Rollback(context.WithoutCancel(ctx)) }()
	var lockedRepositoryID uuid.UUID
	if err := refreshTransaction.QueryRow(
		ctx,
		`SELECT id FROM repositories WHERE id = $1 FOR UPDATE`,
		repositoryID,
	).Scan(&lockedRepositoryID); err != nil {
		t.Fatalf("lock repository for simulated refresh: %v", err)
	}
	commitC := "cccccccccccccccccccccccccccccccccccccccc"
	if _, err := refreshTransaction.Exec(
		ctx,
		`UPDATE targets SET desired_commit_sha = $1, updated_at = $2
         WHERE repository_id = $3 AND name = 'network'`,
		commitC,
		baseTime.Add(3*time.Minute),
		repositoryID,
	); err != nil {
		t.Fatalf("advance desired commit in simulated refresh: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, createErr := database.CreateRun(ctx, queuedGitTestRun(
			uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1903"),
			delivery,
			commitB,
			baseTime.Add(3*time.Minute),
		))
		result <- createErr
	}()
	if err := refreshTransaction.Commit(ctx); err != nil {
		t.Fatalf("commit simulated refresh: %v", err)
	}
	if err := <-result; !errors.Is(err, run.ErrGitRunSuperseded) {
		t.Fatalf("stale Git Run creation error = %v, want Git Run superseded", err)
	}
	assertGitTriggerDeliveryState(t, database, delivery.ID, true, false)
	assertRunCounts(t, database, 0, 0)
}

func gitTriggeredCatalog() catalog.Catalog {
	return gitTriggeredCatalogWithEnvironment("production")
}

func gitTriggeredCatalogWithEnvironment(environment string) catalog.Catalog {
	return catalog.Catalog{
		Runtimes: []catalog.Runtime{{
			APIVersion: catalog.APIVersion,
			Kind:       catalog.RuntimeKind,
			Metadata:   catalog.Metadata{Name: "shell"},
			Spec:       catalog.RuntimeSpec{Image: "alpine:3.22"},
			Source:     catalog.Source{Path: ".slipramp/catalog.yaml", Document: 1},
		}},
		Targets: []catalog.Target{{
			APIVersion: catalog.APIVersion,
			Kind:       catalog.TargetKind,
			Metadata:   catalog.Metadata{Name: "network"},
			Spec: catalog.TargetSpec{
				Watch:     []string{"infrastructure/network/**"},
				Runtime:   "shell",
				Placement: "infrastructure",
				Actions: map[string]catalog.Action{
					"sync": {
						Command: []string{"./sync.sh"},
						Parameters: map[string]catalog.Parameter{
							"message": {
								Type:     catalog.ParameterTypeString,
								Argument: &catalog.ParameterArgument{Name: "--message"},
							},
							"auto-approve": {
								Type:     catalog.ParameterTypeBoolean,
								Argument: &catalog.ParameterArgument{Name: "--auto-approve"},
							},
							"retries": {
								Type:     catalog.ParameterTypeNumber,
								Argument: &catalog.ParameterArgument{Name: "--retries"},
							},
							"environment": {
								Type:     catalog.ParameterTypeEnum,
								Required: true,
								Values:   []string{"staging", "production"},
								Argument: &catalog.ParameterArgument{Name: "--environment"},
							},
						},
						Triggers: []catalog.Trigger{{
							Name:       "reconcile-on-change",
							Git:        &catalog.GitTrigger{},
							Parameters: gitTriggerCatalogParameters(environment),
						}},
					},
				},
			},
			Source: catalog.Source{Path: ".slipramp/catalog.yaml", Document: 2},
		}},
	}
}

func gitTriggerParameters(environment string) map[string]any {
	return map[string]any{
		"message":      "reconcile",
		"auto-approve": true,
		"retries":      float64(3),
		"environment":  environment,
	}
}

func gitTriggerCatalogParameters(environment string) map[string]any {
	parameters := gitTriggerParameters(environment)
	parameters["retries"] = 3

	return parameters
}

func newGitTriggerTestServices(
	t *testing.T,
	database *Database,
) (*run.Service, *gittrigger.Dispatcher) {
	t.Helper()

	placements, err := placement.NewSet(map[string]placement.Config{
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
		},
	})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}
	repositories := repository.NewService(database, repository.RefreshCredentials{})
	catalogs := catalog.NewService(database)
	runs := run.NewService(run.Dependencies{
		Creation:      database,
		History:       database,
		Assignments:   database,
		Cancellations: database,
		Observations:  database,
		Repositories:  repositories,
		Catalogs:      catalogs,
		Placements:    placements,
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return runs, gittrigger.NewDispatcher(database, runs, time.Second, logger)
}

func queuedTestRun(
	id uuid.UUID,
	repositoryID uuid.UUID,
	revisionID uuid.UUID,
	commitSHA string,
	sourceType run.SourceType,
	createdAt time.Time,
	digestCharacter string,
) run.Run {
	return run.Run{
		ID:                id,
		Phase:             run.PhaseQueued,
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         commitSHA,
		TargetName:        "network",
		ActionName:        "sync",
		Parameters:        map[string]any{},
		PlacementName:     "infrastructure",
		ClusterName:       "colo",
		Source:            run.Source{Type: sourceType},
		RequestDigest:     strings.Repeat(digestCharacter, 64),
		Snapshot:          run.Snapshot{},
		CreatedAt:         createdAt,
	}
}

func queuedGitTestRun(
	id uuid.UUID,
	delivery gittrigger.Delivery,
	commitSHA string,
	createdAt time.Time,
) run.Run {
	idempotencyKey := delivery.ID.String()

	return run.Run{
		ID:                id,
		Phase:             run.PhaseQueued,
		RepositoryID:      delivery.RepositoryID,
		CatalogRevisionID: delivery.CatalogRevisionID,
		CommitSHA:         commitSHA,
		TargetName:        delivery.TargetName,
		ActionName:        delivery.ActionName,
		Parameters:        delivery.Parameters,
		PlacementName:     "infrastructure",
		ClusterName:       "colo",
		Source: run.Source{
			Type: run.SourceTypeGit,
			Git: &run.GitRunSource{
				TriggerName: delivery.TriggerName,
				DeliveryID:  delivery.ID,
			},
		},
		IdempotencyKey: &idempotencyKey,
		RequestDigest:  strings.Repeat("c", 64),
		Snapshot:       run.Snapshot{},
		CreatedAt:      createdAt,
	}
}

func assertSupersededRunTransition(t *testing.T, database *Database, runID uuid.UUID) {
	t.Helper()

	var count int
	if err := database.pool.QueryRow(
		t.Context(),
		`SELECT count(*) FROM run_phase_transitions
         WHERE run_id = $1
           AND phase = 'finished'
           AND outcome = 'canceled'
           AND reason = 'SupersededByGitChange'`,
		runID,
	).Scan(&count); err != nil {
		t.Fatalf("count superseded Run transitions: %v", err)
	}
	if count != 1 {
		t.Errorf("superseded Run transitions = %d, want 1", count)
	}
}

func assertGitTriggerDeliveryCount(t *testing.T, database *Database, want int) {
	t.Helper()

	var count int
	if err := database.pool.QueryRow(t.Context(), `SELECT count(*) FROM git_trigger_deliveries`).Scan(&count); err != nil {
		t.Fatalf("count Git-trigger deliveries: %v", err)
	}
	if count != want {
		t.Errorf("Git-trigger delivery count = %d, want %d", count, want)
	}
}

func assertGitTriggerDeliveryState(
	t *testing.T,
	database *Database,
	deliveryID uuid.UUID,
	wantSuperseded bool,
	wantRun bool,
) {
	t.Helper()

	var storedID, storedRunID pgtype.UUID
	var supersededTime pgtype.Timestamptz
	err := database.pool.QueryRow(
		t.Context(),
		`SELECT id, superseded_at, run_id FROM git_trigger_deliveries WHERE id = $1`,
		deliveryID,
	).Scan(&storedID, &supersededTime, &storedRunID)
	if err != nil {
		t.Fatalf("load Git-trigger delivery state: %v", err)
	}
	if supersededTime.Valid != wantSuperseded || storedRunID.Valid != wantRun {
		t.Errorf(
			"Git-trigger delivery state = (superseded %t, Run %t), want (%t, %t)",
			supersededTime.Valid,
			storedRunID.Valid,
			wantSuperseded,
			wantRun,
		)
	}
}

type failingGitTriggerCompletionStore struct {
	database *Database
}

func (s *failingGitTriggerCompletionStore) GetPendingGitTriggerDelivery(
	ctx context.Context,
) (gittrigger.Delivery, error) {
	return s.database.GetPendingGitTriggerDelivery(ctx)
}

func (*failingGitTriggerCompletionStore) CompleteGitTriggerDelivery(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) error {
	return errors.New("simulated delivery completion failure")
}
