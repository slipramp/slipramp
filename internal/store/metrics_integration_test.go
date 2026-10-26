package store

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestMetricsSnapshotUsesDispatchEligibilityIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	repositoryID := uuid.New()
	revisionID := uuid.New()
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:             repositoryID,
		Name:           "infrastructure",
		CloneURL:       "https://github.com/example/infrastructure.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("create Repository: %v", err)
	}

	catalogState := persistedCatalog("1.14.3")
	storageTarget := catalogState.Targets[0]
	storageTarget.Metadata.Name = "storage"
	storageTarget.Source.Document = 3
	storageTarget.Spec.Watch = []string{"infrastructure/storage/**"}
	catalogState.Targets = append(catalogState.Targets, storageTarget)
	refresh := successfulCatalogRefresh(strings.Repeat("a", 40), now.Add(-50*time.Minute), revisionID)
	refresh.Revision.Catalog = catalogState
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("record Repository refresh: %v", err)
	}

	networkTargetID := activeTargetID(t, database, repositoryID, "network")
	storageTargetID := activeTargetID(t, database, repositoryID, "storage")
	active := queuedTestRun(
		uuid.New(),
		networkTargetID,
		repositoryID,
		revisionID,
		refresh.Commit.SHA,
		run.SourceTypeManual,
		now.Add(-10*time.Minute),
		"b",
	)
	if _, err := database.CreateRun(ctx, active, nil); err != nil {
		t.Fatalf("create active candidate Run: %v", err)
	}
	if _, err := database.ClaimQueuedRun(ctx, "colo", now.Add(-9*time.Minute)); err != nil {
		t.Fatalf("claim active Run: %v", err)
	}

	blocked := queuedTestRun(
		uuid.New(),
		networkTargetID,
		repositoryID,
		revisionID,
		refresh.Commit.SHA,
		run.SourceTypeManual,
		now.Add(-8*time.Minute),
		"c",
	)
	if _, err := database.CreateRun(ctx, blocked, nil); err != nil {
		t.Fatalf("create blocked queued Run: %v", err)
	}

	eligible := queuedTestRun(
		uuid.New(),
		storageTargetID,
		repositoryID,
		revisionID,
		refresh.Commit.SHA,
		run.SourceTypeManual,
		now.Add(-6*time.Minute),
		"d",
	)
	eligible.TargetName = "storage"
	if _, err := database.CreateRun(ctx, eligible, nil); err != nil {
		t.Fatalf("create eligible queued Run: %v", err)
	}

	pendingReviewID := uuid.New()
	pendingDeliveryID := uuid.New()
	_, err = database.pool.Exec(ctx, `
        INSERT INTO runs (
            id, phase, target_id, repository_id, catalog_revision_id, commit_sha,
            target_name, action_name, parameters, placement_name, cluster_name,
            source, idempotency_key, request_digest, snapshot, review_state, created_at
        )
        SELECT
            $1, 'queued', target_id, repository_id, catalog_revision_id, commit_sha,
            target_name, action_name, parameters, placement_name, cluster_name,
            jsonb_build_object(
                'type', 'git',
                'git', jsonb_build_object('triggerName', 'reviewed', 'deliveryId', $2::text)
            ),
            $2::text, $3, snapshot, 'pending', $4
        FROM runs
        WHERE id = $5`,
		pendingReviewID,
		pendingDeliveryID,
		strings.Repeat("e", 64),
		now.Add(-7*time.Minute),
		eligible.ID,
	)
	if err != nil {
		t.Fatalf("create review-pending Run: %v", err)
	}

	if err := database.RecordClusterObservation(ctx, "fresh", now.Add(-30*time.Second)); err != nil {
		t.Fatalf("record fresh Controller: %v", err)
	}
	if err := database.RecordClusterObservation(ctx, "stale", now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("record stale Controller: %v", err)
	}
	notificationEventID := uuid.New()
	_, err = database.pool.Exec(ctx, `
        INSERT INTO notification_events (
            id, event_type, schema_version, subject_type, subject_id,
            occurred_at, payload_json, deduplication_key, created_at
        ) VALUES ($1, 'review.requested', 1, 'promotion', $2, $3, '{}', $4, $3)`,
		notificationEventID,
		uuid.New(),
		now.Add(-time.Minute),
		uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("create notification event: %v", err)
	}
	_, err = database.pool.Exec(ctx, `
        INSERT INTO notification_deliveries (
            id, event_id, destination_name, destination_type, state, next_attempt_at, created_at
        ) VALUES ($1, $2, 'operations', 'slack', 'pending', $3, $3)`,
		uuid.New(),
		notificationEventID,
		now,
	)
	if err != nil {
		t.Fatalf("create notification delivery: %v", err)
	}

	snapshot, err := database.LoadMetricsSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("LoadMetricsSnapshot() error = %v", err)
	}
	if snapshot.ActiveRuns[string(run.PhaseAssigned)] != 1 {
		t.Errorf("assigned Runs = %d, want 1", snapshot.ActiveRuns[string(run.PhaseAssigned)])
	}
	if snapshot.PendingRunReviews != 1 {
		t.Errorf("pending Run reviews = %d, want 1", snapshot.PendingRunReviews)
	}
	if snapshot.OldestEligibleRunCreatedAt == nil ||
		!snapshot.OldestEligibleRunCreatedAt.Equal(eligible.CreatedAt) {
		t.Errorf(
			"oldest eligible Run = %v, want %s",
			snapshot.OldestEligibleRunCreatedAt,
			eligible.CreatedAt,
		)
	}
	if snapshot.ConnectedControllers != 1 || snapshot.ControllerLastSeenAt == nil ||
		!snapshot.ControllerLastSeenAt.Equal(now.Add(-30*time.Second)) {
		t.Errorf(
			"Controller metrics = connected %d, last seen %v",
			snapshot.ConnectedControllers,
			snapshot.ControllerLastSeenAt,
		)
	}
	if snapshot.RepositoryRefreshStates[string(repository.RefreshStatusSucceeded)] != 1 {
		t.Errorf("Repository refresh states = %v", snapshot.RepositoryRefreshStates)
	}
	if snapshot.NotificationDeliveryStates["pending"] != 1 {
		t.Errorf("notification delivery states = %v", snapshot.NotificationDeliveryStates)
	}
}
