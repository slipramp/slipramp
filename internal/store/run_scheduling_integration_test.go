package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestRunCapacityIntegration(t *testing.T) {
	tests := []struct {
		name     string
		capacity run.SchedulingCapacity
	}{
		{
			name: "cluster maximum",
			capacity: run.SchedulingCapacity{
				ClusterMaximumActiveRuns:   1,
				PlacementMaximumActiveRuns: map[string]int32{},
			},
		},
		{
			name: "Placement maximum",
			capacity: run.SchedulingCapacity{
				ClusterMaximumActiveRuns:   4,
				PlacementMaximumActiveRuns: map[string]int32{"infrastructure": 1},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _, database := openIntegrationDatabase(t)
			defer database.Close()

			first, second := createCapacityTestRuns(t, ctx, database)
			claimedAt := first.CreatedAt.Add(time.Minute)
			claimed, err := database.claimQueuedRunAt(ctx, "colo", test.capacity, uuid.Nil, claimedAt)
			if err != nil {
				t.Fatalf("claim first Run: %v", err)
			}
			if claimed.ID != first.ID {
				t.Fatalf("claimed Run = %s, want %s", claimed.ID, first.ID)
			}

			if _, err := database.claimQueuedRunAt(
				ctx,
				"colo",
				test.capacity,
				uuid.Nil,
				claimedAt.Add(time.Minute),
			); !errors.Is(err, run.ErrNoRunAssignment) {
				t.Fatalf("capacity-limited claim error = %v, want no assignment", err)
			}

			clusterLimits, err := json.Marshal(map[string]int32{
				"colo": test.capacity.ClusterMaximumActiveRuns,
			})
			if err != nil {
				t.Fatalf("encode cluster limits: %v", err)
			}
			placementLimits, err := json.Marshal(test.capacity.PlacementMaximumActiveRuns)
			if err != nil {
				t.Fatalf("encode Placement limits: %v", err)
			}
			snapshots, err := database.GetRunSchedulingSnapshots(
				ctx,
				[]uuid.UUID{second.ID}, uuid.Nil, time.Now(),
				clusterLimits,
				placementLimits,
			)
			if err != nil {
				t.Fatalf("get scheduling snapshots: %v", err)
			}
			if len(snapshots) != 1 || snapshots[0].ClusterActiveRuns != 1 ||
				snapshots[0].PlacementActiveRuns != 1 || snapshots[0].EligiblePosition != nil {
				t.Errorf("scheduling snapshots = %#v", snapshots)
			}
		})
	}
}

func TestSchedulingSnapshotRanksUnrequestedRunsInFIFOOrder(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	first, second := createCapacityTestRuns(t, ctx, database)
	_, err := database.pool.Exec(ctx, "UPDATE runs SET created_at = $1 WHERE id = $2", first.CreatedAt, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(first.ID[:], second.ID[:]) > 0 {
		first, second = second, first
	}

	snapshot := func() run.SchedulingSnapshot {
		t.Helper()
		rows, err := database.GetRunSchedulingSnapshots(ctx, []uuid.UUID{second.ID}, uuid.Nil, time.Now(), []byte(`{"colo":2}`), []byte(`{}`))
		if err != nil || len(rows) != 1 {
			t.Fatalf("load scheduling snapshot: rows=%v error=%v", rows, err)
		}
		return rows[0]
	}
	if position := snapshot().EligiblePosition; position == nil || *position != 2 {
		t.Fatalf("later Run in timestamp tie: position = %v, want 2", position)
	}

	// An active Run in another Cluster consumes Target capacity but no local Cluster or Placement slots.
	remote := second
	remote.ID = uuid.New()
	remote.ClusterName = "remote"
	remote.Snapshot.Placement.ClusterName = remote.ClusterName
	if _, err := database.CreateRun(ctx, remote, nil); err != nil {
		t.Fatal(err)
	}
	_, err = database.claimQueuedRunAt(ctx, remote.ClusterName, testSchedulingCapacity(), uuid.Nil, remote.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	blocked := snapshot()
	if blocked.BlockingRunID == nil || *blocked.BlockingRunID != remote.ID || blocked.EligiblePosition != nil ||
		blocked.ClusterActiveRuns != 0 || blocked.PlacementActiveRuns != 0 {
		t.Fatalf("cross-Cluster Target blocker = %#v", blocked)
	}
}

func TestTargetRunCapacityIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	repositoryID := uuid.New()
	if _, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "target-capacity", CloneURL: "https://example.com/infrastructure.git",
		Branch: "main", CommitIndexing: repository.CommitIndexingObservedHeads, CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("create repository: %v", err)
	}

	revisionID := uuid.New()
	refresh := successfulCatalogRefresh(strings.Repeat("a", 40), createdAt.Add(time.Minute), revisionID)
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	maximumActiveRuns := int32(2)
	refresh.Revision.Catalog.Targets[0].Spec.MaximumActiveRuns = &maximumActiveRuns
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("record catalog refresh: %v", err)
	}

	targetID := activeTargetID(t, database, repositoryID, "network")
	for index := range 3 {
		record := queuedTestRun(
			uuid.New(),
			targetID,
			repositoryID,
			revisionID,
			refresh.Commit.SHA,
			run.SourceTypeManual,
			createdAt.Add(time.Duration(index+2)*time.Minute),
			string(rune('b'+index)),
		)
		record.ActionName = "preview"
		if _, err := database.CreateRun(ctx, record, nil); err != nil {
			t.Fatalf("create queued Run %d: %v", index+1, err)
		}
	}

	capacity := testSchedulingCapacity()
	if _, err := database.claimQueuedRunAt(ctx, "colo", capacity, uuid.Nil, createdAt.Add(6*time.Minute)); err != nil {
		t.Fatalf("claim first Run: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := database.claimQueuedRunAt(ctx, "colo", capacity, uuid.Nil, createdAt.Add(7*time.Minute))
			results <- err
		}()
	}
	close(start)

	claimed := 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			claimed++
		case errors.Is(err, run.ErrNoRunAssignment):
		default:
			t.Fatalf("concurrent claim: %v", err)
		}
	}
	if claimed != 1 {
		t.Fatalf("concurrent successful claims = %d, want 1", claimed)
	}

	var activeRuns int32
	if err := database.pool.QueryRow(
		ctx,
		`SELECT count(*)::integer
		 FROM runs
		 WHERE target_id = $1
		   AND phase IN ('assigned', 'preparing', 'running')`,
		targetID,
	).Scan(&activeRuns); err != nil {
		t.Fatalf("count active Runs: %v", err)
	}
	if activeRuns != maximumActiveRuns {
		t.Fatalf("active Runs = %d, want %d", activeRuns, maximumActiveRuns)
	}

	queued, err := database.ListRuns(ctx, run.RunListFilter{
		TargetID: &targetID,
		Phase:    run.PhaseQueued,
		Limit:    10,
	})
	if err != nil || len(queued.Items) != 1 {
		t.Fatalf("list capacity-blocked Runs = (%d, %v), want 1", len(queued.Items), err)
	}
	snapshots, err := database.GetRunSchedulingSnapshots(
		ctx,
		[]uuid.UUID{queued.Items[0].ID}, uuid.Nil, time.Now(),
		[]byte(`{"colo":4}`),
		[]byte(`{}`),
	)
	if err != nil {
		t.Fatalf("get target scheduling snapshot: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].BlockingRunID == nil ||
		snapshots[0].EligiblePosition != nil {
		t.Errorf("target scheduling snapshot = %#v", snapshots)
	}
}

func createCapacityTestRuns(t *testing.T, ctx context.Context, database *Database) (run.Run, run.Run) {
	t.Helper()

	createdAt := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
	repositoryID := uuid.New()
	if _, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "infrastructure", CloneURL: "https://example.com/infrastructure.git",
		Branch: "main", CommitIndexing: repository.CommitIndexingObservedHeads, CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("create repository: %v", err)
	}

	revisionID := uuid.New()
	refresh := successfulCatalogRefresh(strings.Repeat("a", 40), createdAt.Add(time.Minute), revisionID)
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	secondTarget := refresh.Revision.Catalog.Targets[0]
	secondTarget.Metadata.Name = "database"
	secondTarget.Spec.Watch = []string{"infrastructure/database/**"}
	refresh.Revision.Catalog.Targets = append(refresh.Revision.Catalog.Targets, secondTarget)
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("record catalog refresh: %v", err)
	}

	newRun := func(name string, offset time.Duration) run.Run {
		targetID := activeTargetID(t, database, repositoryID, name)
		targetSnapshot := refresh.Revision.Catalog.Targets[0]
		requestDigest := strings.Repeat("a", 64)
		if name == "database" {
			targetSnapshot = secondTarget
			requestDigest = strings.Repeat("b", 64)
		}
		return run.Run{
			ID: uuid.New(), Phase: run.PhaseQueued,
			Review:   run.Review{State: run.ReviewStateNotRequired},
			TargetID: targetID, RepositoryID: repositoryID, CatalogRevisionID: revisionID,
			CommitSHA: refresh.Commit.SHA, TargetName: name, ActionName: "preview",
			Parameters: map[string]any{}, PlacementName: "infrastructure", ClusterName: "colo",
			Source: run.Source{Type: run.SourceTypeManual}, RequestDigest: requestDigest,
			Snapshot: run.Snapshot{
				Repository: run.RepositorySnapshot{
					Name: "infrastructure", CloneURL: "https://example.com/infrastructure.git", Branch: "main",
				},
				Target: targetSnapshot, Action: catalog.EffectiveAction{Name: "preview", Intent: "preview"},
				Runtime: refresh.Revision.Catalog.Runtimes[0],
				Placement: run.PlacementSnapshot{
					Name: "infrastructure", ClusterName: "colo", JobNamespace: "slipramp-runs",
				},
			},
			CreatedAt: createdAt.Add(offset),
		}
	}

	first, err := database.CreateRun(ctx, newRun("network", 2*time.Minute), nil)
	if err != nil {
		t.Fatalf("create first Run: %v", err)
	}
	second, err := database.CreateRun(ctx, newRun("database", 3*time.Minute), nil)
	if err != nil {
		t.Fatalf("create second Run: %v", err)
	}

	return first, second
}
