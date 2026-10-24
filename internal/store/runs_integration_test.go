package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
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
		CommitSHA:         refresh.Commit.SHA,
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
	second.CommitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	second.CreatedAt = record.CreatedAt.Add(time.Minute)
	rerunMode := run.RerunModeLatest
	second.RerunOfRunID = &record.ID
	second.RerunMode = &rerunMode
	createdSecond, err := database.CreateRun(ctx, second)
	if err != nil {
		t.Fatalf("create second Run: %v", err)
	}
	if createdSecond.RerunOfRunID == nil || *createdSecond.RerunOfRunID != record.ID ||
		createdSecond.RerunMode == nil || *createdSecond.RerunMode != run.RerunModeLatest {
		t.Errorf("rerun provenance = (%v, %v)", createdSecond.RerunOfRunID, createdSecond.RerunMode)
	}

	listed, err := database.ListRuns(ctx, run.RunListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(listed.Items) != 2 || listed.Items[0].ID != second.ID || listed.Items[1].ID != record.ID ||
		listed.Total != 2 {
		t.Errorf("ListRuns() = %#v, want second then first", listed)
	}

	repositoryFilter := repositoryID
	listed, err = database.ListRuns(
		ctx,
		run.RunListFilter{
			RepositoryID: &repositoryFilter,
			TargetName:   "network",
			Phase:        run.PhaseQueued,
			Limit:        1,
			Offset:       1,
		},
	)
	if err != nil {
		t.Fatalf("filtered ListRuns() error = %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != record.ID || listed.Total != 2 {
		t.Errorf("filtered ListRuns() = %#v, want first Run on second page", listed)
	}

	listed, err = database.ListRuns(
		ctx,
		run.RunListFilter{
			RepositoryID: &repositoryFilter,
			CommitSHA:    second.CommitSHA,
			Limit:        10,
		},
	)
	if err != nil {
		t.Fatalf("commit-filtered ListRuns() error = %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != second.ID || listed.Total != 1 {
		t.Errorf("commit-filtered ListRuns() = %#v, want second Run", listed)
	}

	listed, err = database.ListRuns(ctx, run.RunListFilter{ClusterName: "other", Limit: 10})
	if err != nil {
		t.Fatalf("cluster-filtered ListRuns() error = %v", err)
	}
	if len(listed.Items) != 0 || listed.Total != 0 {
		t.Errorf("cluster-filtered ListRuns() = %#v, want no Runs", listed)
	}

	listed, err = database.ListRuns(ctx, run.RunListFilter{TargetName: "missing", Limit: 10})
	if err != nil {
		t.Fatalf("empty ListRuns() error = %v", err)
	}
	if len(listed.Items) != 0 || listed.Total != 0 {
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
	); err != nil {
		t.Fatalf("observe Run preparing: %v", err)
	}
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhasePreparing,
			JobUID: &jobUID, ObservedAt: preparingAt.Add(time.Second),
		},
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
	); err != nil {
		t.Fatalf("complete first Run: %v", err)
	}
	if err := database.ApplyRunObservation(
		ctx,
		run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseRunning,
			JobUID: &jobUID, ObservedAt: completedAt.Add(time.Minute),
		},
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
	).Scan(&phase, &outcome, &storedJobUID, &storedReason, &storedExitCode, &lastObservedAt); err != nil {
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

	claimed, err = database.ClaimQueuedRun(ctx, "colo", completedAt.Add(time.Minute))
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
	); !errors.Is(err, run.ErrObservationConflict) {
		t.Fatalf("unrequested cancel error = %v, want observation conflict", err)
	}

	cancelAt := completedAt.Add(3 * time.Minute)
	canceling, err := database.CancelRun(ctx, second.ID, cancelAt, nil)
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
	repeated, err := database.CancelRun(ctx, second.ID, cancelAt.Add(time.Minute), nil)
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
	); err != nil {
		t.Fatalf("complete canceled Run: %v", err)
	}
	if _, err := database.GetRunToCancelForCluster(ctx, "colo"); !errors.Is(err, run.ErrNoRunToCancel) {
		t.Errorf("completed cancel lookup error = %v, want no Run to cancel", err)
	}
	if _, err := database.CancelRun(ctx, record.ID, cancelAt, nil); !errors.Is(err, run.ErrRunFinished) {
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
	canceledQueued, err := database.CancelRun(ctx, third.ID, queuedCancelAt, nil)
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
	if _, err := database.CancelRun(ctx, third.ID, queuedCancelAt.Add(time.Minute), nil); err != nil {
		t.Fatalf("repeat queued Run cancel: %v", err)
	}

	assertRunCounts(t, database, 3, 10)
}

func TestRunAttributionPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	creator := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
		Role:        productidentity.RoleOperator,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	creator, err := database.CreatePrincipal(ctx, creator)
	if err != nil {
		t.Fatalf("create Run creator: %v", err)
	}
	canceler := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501"),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: "Automation",
		Role:        productidentity.RoleOperator,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	canceler, err = database.CreatePrincipal(ctx, canceler)
	if err != nil {
		t.Fatalf("create Run canceler: %v", err)
	}

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1510")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1511")
	_, err = database.CreateRepository(ctx, repository.Repository{
		Name:           "attributed-infrastructure",
		CloneURL:       "https://github.com/example/infrastructure.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		ID:             repositoryID,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("create attributed repository: %v", err)
	}
	refresh := successfulCatalogRefresh(
		"0123456789abcdef0123456789abcdef01234567",
		createdAt.Add(time.Minute),
		revisionID,
	)
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("record attributed catalog: %v", err)
	}

	creatorSnapshot, err := creator.ActorSnapshot()
	if err != nil {
		t.Fatalf("snapshot Run creator: %v", err)
	}
	idempotencyKey := "shared-key"
	first := run.Run{
		ID:                uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1520"),
		Phase:             run.PhaseQueued,
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         refresh.Commit.SHA,
		TargetName:        "network",
		ActionName:        "preview",
		Parameters:        map[string]any{},
		PlacementName:     "infrastructure",
		ClusterName:       "colo",
		Source:            run.Source{Type: run.SourceTypeAPI},
		IdempotencyKey:    &idempotencyKey,
		RequestDigest:     strings.Repeat("a", 64),
		Snapshot: run.Snapshot{
			Repository: run.RepositorySnapshot{
				Name:     "attributed-infrastructure",
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
		CreatedBy: &creatorSnapshot,
		CreatedAt: createdAt.Add(2 * time.Minute),
	}
	created, err := database.CreateRun(ctx, first)
	if err != nil {
		t.Fatalf("create attributed Run: %v", err)
	}
	if created.CreatedBy == nil || created.CreatedBy.PrincipalID != creator.ID ||
		created.CreatedBy.DisplayName != creator.DisplayName {
		t.Errorf("created Run actor = %#v", created.CreatedBy)
	}

	retry := first
	retry.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1521")
	retried, err := database.CreateRun(ctx, retry)
	if err != nil {
		t.Fatalf("retry attributed Run: %v", err)
	}
	if retried.ID != first.ID {
		t.Errorf("retried Run ID = %s, want %s", retried.ID, first.ID)
	}

	cancelerSnapshot, err := canceler.ActorSnapshot()
	if err != nil {
		t.Fatalf("snapshot Run canceler: %v", err)
	}
	second := first
	second.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1522")
	second.CreatedBy = &cancelerSnapshot
	second.CreatedAt = first.CreatedAt.Add(time.Minute)
	createdSecond, err := database.CreateRun(ctx, second)
	if err != nil {
		t.Fatalf("reuse idempotency key for another principal: %v", err)
	}
	if createdSecond.ID != second.ID {
		t.Errorf("second principal Run ID = %s, want %s", createdSecond.ID, second.ID)
	}

	canceled, err := database.CancelRun(ctx, first.ID, createdAt.Add(4*time.Minute), &cancelerSnapshot)
	if err != nil {
		t.Fatalf("cancel attributed Run: %v", err)
	}
	if canceled.CanceledBy == nil || canceled.CanceledBy.PrincipalID != canceler.ID ||
		canceled.CanceledBy.DisplayName != canceler.DisplayName {
		t.Errorf("canceled Run actor = %#v", canceled.CanceledBy)
	}

	repeated, err := database.CancelRun(ctx, first.ID, createdAt.Add(5*time.Minute), &creatorSnapshot)
	if err != nil {
		t.Fatalf("repeat attributed cancellation: %v", err)
	}
	if repeated.CanceledBy == nil || repeated.CanceledBy.PrincipalID != canceler.ID {
		t.Errorf("repeat cancellation actor = %#v, want original", repeated.CanceledBy)
	}
}

func assertRunCounts(t *testing.T, database *Database, wantRuns int, wantTransitions int) {
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
