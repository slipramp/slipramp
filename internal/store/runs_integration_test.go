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
		Phase:             run.PhaseQueued,
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
	if created.ID != record.ID || created.Phase != run.PhaseQueued ||
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

	listed, err := database.ListRuns(ctx, run.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(listed) != 2 || listed[0].ID != second.ID || listed[1].ID != record.ID {
		t.Errorf("ListRuns() = %#v, want second then first", listed)
	}

	repositoryFilter := repositoryID
	listed, err = database.ListRuns(
		ctx,
		run.ListFilter{
			RepositoryID: &repositoryFilter,
			TargetName:   "network",
			Limit:        1,
		},
	)
	if err != nil {
		t.Fatalf("filtered ListRuns() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != second.ID {
		t.Errorf("filtered ListRuns() = %#v, want second", listed)
	}

	listed, err = database.ListRuns(
		ctx,
		run.ListFilter{TargetName: "missing", Limit: 10},
	)
	if err != nil {
		t.Fatalf("empty ListRuns() error = %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("empty ListRuns() = %#v, want no Runs", listed)
	}

	claimedAt := second.CreatedAt.Add(time.Minute)
	claimed, err := database.ClaimQueuedRun(ctx, "colo", claimedAt)
	if err != nil {
		t.Fatalf("ClaimQueuedRun() error = %v", err)
	}
	if claimed.ID != record.ID || claimed.Phase != run.PhaseAssigned ||
		claimed.AssignedAt == nil || !claimed.AssignedAt.Equal(claimedAt) {
		t.Errorf("claimed Run = %#v", claimed)
	}

	redelivered, err := database.GetAssignedRunForCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("GetAssignedRunForCluster() error = %v", err)
	}
	if redelivered.ID != record.ID {
		t.Errorf("redelivered Run ID = %s, want %s", redelivered.ID, record.ID)
	}

	jobUID := "f6ef8cb8-a838-4644-a8ec-b2e92bac0e80"
	preparingAt := claimedAt.Add(time.Minute)
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhasePreparing,
			JobUID: &jobUID, ObservedAt: preparingAt,
		},
		[]run.Phase{run.PhaseAssigned, run.PhasePreparing},
	); err != nil {
		t.Fatalf("observe Run preparing: %v", err)
	}
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhasePreparing,
			JobUID: &jobUID, ObservedAt: preparingAt.Add(time.Second),
		},
		[]run.Phase{run.PhaseAssigned, run.PhasePreparing},
	); err != nil {
		t.Fatalf("repeat preparing observation: %v", err)
	}
	otherJobUID := "152e047f-3665-4763-94fe-4a2b5494ee6d"
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseRunning,
			JobUID: &otherJobUID, ObservedAt: preparingAt.Add(time.Second),
		},
		[]run.Phase{run.PhasePreparing},
	); !errors.Is(err, run.ErrObservationConflict) {
		t.Fatalf("mismatched Job observation error = %v, want observation conflict", err)
	}

	runningAt := preparingAt.Add(time.Minute)
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseRunning,
			JobUID: &jobUID, ObservedAt: runningAt,
		},
		[]run.Phase{run.PhaseAssigned, run.PhasePreparing, run.PhaseRunning},
	); err != nil {
		t.Fatalf("observe Run running: %v", err)
	}

	succeeded := run.OutcomeSucceeded
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "edge", Phase: run.PhaseFinished,
			Outcome: &succeeded, JobUID: &jobUID, ObservedAt: runningAt.Add(time.Minute),
		},
		[]run.Phase{run.PhaseRunning},
	); !errors.Is(err, run.ErrRunNotFound) {
		t.Fatalf("wrong-cluster observation error = %v, want Run not found", err)
	}

	_, err = database.ClaimQueuedRun(ctx, "colo", claimedAt)
	if !errors.Is(err, run.ErrNoRunAssignment) {
		t.Errorf("second ClaimQueuedRun() error = %v, want no work", err)
	}

	completedAt := runningAt.Add(time.Minute)
	completedReason := "Completed"
	exitCode := int32(0)
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &succeeded, JobUID: &jobUID, Reason: &completedReason,
			ExitCode: &exitCode, ObservedAt: completedAt,
		},
		[]run.Phase{run.PhaseAssigned, run.PhasePreparing, run.PhaseRunning, run.PhaseFinished},
	); err != nil {
		t.Fatalf("complete first Run: %v", err)
	}
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseRunning,
			JobUID: &jobUID, ObservedAt: completedAt.Add(time.Minute),
		},
		[]run.Phase{run.PhaseAssigned, run.PhasePreparing, run.PhaseRunning},
	); !errors.Is(err, run.ErrObservationConflict) {
		t.Fatalf("backward transition error = %v, want observation conflict", err)
	}
	repeatedReason := "DifferentReason"
	repeatedExitCode := int32(1)
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &succeeded, JobUID: &jobUID, Reason: &repeatedReason,
			ExitCode:   &repeatedExitCode,
			ObservedAt: completedAt.Add(time.Second),
		},
		[]run.Phase{run.PhaseAssigned, run.PhasePreparing, run.PhaseRunning, run.PhaseFinished},
	); err != nil {
		t.Fatalf("repeat finished observation: %v", err)
	}
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &succeeded, JobUID: &jobUID,
			ObservedAt: completedAt.Add(-time.Second),
		},
		[]run.Phase{run.PhaseFinished},
	); err != nil {
		t.Fatalf("repeat stale finished observation: %v", err)
	}
	var phase string
	var outcome *string
	var storedJobUID *string
	var storedReason *string
	var storedExitCode *int32
	var lastObservedAt time.Time
	if err := database.pool.QueryRow(
		ctx,
		`SELECT phase, outcome, job_uid, outcome_reason, exit_code, last_observed_at
		 FROM runs WHERE id = $1`,
		record.ID,
	).Scan(
		&phase,
		&outcome,
		&storedJobUID,
		&storedReason,
		&storedExitCode,
		&lastObservedAt,
	); err != nil {
		t.Fatalf("load completed Run: %v", err)
	}
	if phase != "finished" || outcome == nil || *outcome != "succeeded" ||
		storedJobUID == nil || *storedJobUID != jobUID ||
		storedReason == nil || *storedReason != completedReason ||
		storedExitCode == nil || *storedExitCode != exitCode ||
		!lastObservedAt.Equal(completedAt.Add(time.Second)) {
		t.Errorf(
			"completed Run = (%q, %v, %v, %v, %v, %s)",
			phase,
			outcome,
			storedJobUID,
			storedReason,
			storedExitCode,
			lastObservedAt,
		)
	}

	claimed, err = database.ClaimQueuedRun(
		ctx,
		"colo",
		completedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("claim unblocked Run: %v", err)
	}
	if claimed.ID != second.ID || claimed.Phase != run.PhaseAssigned {
		t.Errorf("unblocked claimed Run = %#v", claimed)
	}

	canceled := run.OutcomeCanceled
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: second.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &canceled, ObservedAt: completedAt.Add(2 * time.Minute),
		},
		[]run.Phase{run.PhaseAssigned},
	); !errors.Is(err, run.ErrObservationConflict) {
		t.Fatalf("unrequested cancel error = %v, want observation conflict", err)
	}

	cancelAt := completedAt.Add(3 * time.Minute)
	canceling, err := database.CancelRun(ctx, second.ID, cancelAt)
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if canceling.Phase != run.PhaseAssigned ||
		canceling.CancellationRequestedAt == nil ||
		!canceling.CancellationRequestedAt.Equal(cancelAt) {
		t.Errorf("canceling Run = %#v", canceling)
	}
	pendingCancel, err := database.GetRunToCancelForCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("GetRunToCancelForCluster() error = %v", err)
	}
	if pendingCancel.ID != second.ID {
		t.Errorf("pending cancel ID = %s, want %s", pendingCancel.ID, second.ID)
	}
	repeated, err := database.CancelRun(
		ctx,
		second.ID,
		cancelAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("repeat CancelRun() error = %v", err)
	}
	if repeated.CancellationRequestedAt == nil ||
		!repeated.CancellationRequestedAt.Equal(cancelAt) {
		t.Errorf("repeated cancel = %#v", repeated)
	}

	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: second.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &canceled, ObservedAt: cancelAt.Add(time.Minute),
		},
		[]run.Phase{run.PhaseAssigned},
	); err != nil {
		t.Fatalf("complete canceled Run: %v", err)
	}
	if _, err := database.GetRunToCancelForCluster(ctx, "colo"); !errors.Is(err, run.ErrNoRunToCancel) {
		t.Errorf("completed cancel lookup error = %v, want no Run to cancel", err)
	}
	if _, err := database.CancelRun(
		ctx,
		record.ID,
		cancelAt,
	); !errors.Is(err, run.ErrRunFinished) {
		t.Errorf("finished Run cancel error = %v, want Run finished", err)
	}

	third := record
	third.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1205")
	third.IdempotencyKey = nil
	third.RequestDigest = strings.Repeat("d", 64)
	third.CreatedAt = cancelAt.Add(2 * time.Minute)
	if _, err := database.CreateRun(ctx, third); err != nil {
		t.Fatalf("create third Run: %v", err)
	}
	queuedCancelAt := third.CreatedAt.Add(time.Minute)
	canceledQueued, err := database.CancelRun(
		ctx,
		third.ID,
		queuedCancelAt,
	)
	if err != nil {
		t.Fatalf("cancel queued Run: %v", err)
	}
	if canceledQueued.Phase != run.PhaseFinished ||
		canceledQueued.Outcome == nil ||
		*canceledQueued.Outcome != run.OutcomeCanceled ||
		canceledQueued.FinishedAt == nil ||
		!canceledQueued.FinishedAt.Equal(queuedCancelAt) ||
		canceledQueued.CancellationRequestedAt == nil ||
		!canceledQueued.CancellationRequestedAt.Equal(queuedCancelAt) ||
		canceledQueued.OutcomeReason == nil ||
		*canceledQueued.OutcomeReason != run.ReasonCancelRequested {
		t.Errorf("canceled queued Run = %#v", canceledQueued)
	}
	if _, err := database.CancelRun(
		ctx,
		third.ID,
		queuedCancelAt.Add(time.Minute),
	); err != nil {
		t.Fatalf("repeat queued Run cancel: %v", err)
	}

	assertRunCounts(t, database, 3, 10)
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
			(SELECT count(*) FROM run_phase_transitions)`,
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
