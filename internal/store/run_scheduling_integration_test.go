package store

import (
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
			claimed, err := database.ClaimQueuedRun(ctx, "colo", test.capacity, claimedAt)
			if err != nil {
				t.Fatalf("claim first Run: %v", err)
			}
			if claimed.ID != first.ID {
				t.Fatalf("claimed Run = %s, want %s", claimed.ID, first.ID)
			}

			if _, err := database.ClaimQueuedRun(
				ctx,
				"colo",
				test.capacity,
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
				[]uuid.UUID{second.ID},
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
				Target: targetSnapshot, Action: catalog.EffectiveAction{Name: "preview"},
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
