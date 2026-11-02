package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestPlacementPreflightPersistenceLifecycleIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	principal, err := database.CreatePrincipal(ctx, productidentity.Principal{
		ID: uuid.New(), Kind: productidentity.KindUser, DisplayName: "Preflight operator",
		Role: productidentity.RoleOperator, CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create principal: %v", err)
	}
	actor, err := principal.ActorSnapshot()
	if err != nil {
		t.Fatalf("snapshot principal: %v", err)
	}
	record := placement.Preflight{
		ID: uuid.New(),
		Snapshot: placement.PreflightSnapshot{
			PlacementName: "infrastructure", ConfigurationChecksum: strings.Repeat("a", 64),
			ClusterName: "colo", JobNamespace: "slipramp-runs",
			Resources: placement.ResourceRequirements{},
		},
		State: placement.PreflightStateQueued, CleanupState: placement.PreflightCleanupPending,
		RequestedBy: actor, CreatedAt: createdAt,
	}

	created, err := database.CreatePlacementPreflight(ctx, record)
	if err != nil {
		t.Fatalf("CreatePlacementPreflight() error = %v", err)
	}
	if created.ID != record.ID || created.RequestedBy.PrincipalID != principal.ID {
		t.Errorf("created preflight = %#v", created)
	}
	if _, err := database.CreatePlacementPreflight(ctx, placement.Preflight{
		ID: uuid.New(), Snapshot: record.Snapshot, State: placement.PreflightStateQueued,
		CleanupState: placement.PreflightCleanupPending, RequestedBy: actor, CreatedAt: createdAt.Add(time.Second),
	}); !errors.Is(err, placement.ErrPreflightActive) {
		t.Fatalf("second active preflight error = %v, want active", err)
	}

	dispatchedAt := createdAt.Add(time.Minute)
	dispatchBefore := dispatchedAt.Add(-placement.PreflightDispatchRetryDelay)
	claimed, err := database.ClaimPlacementPreflight(ctx, "colo", dispatchBefore, dispatchedAt)
	if err != nil {
		t.Fatalf("ClaimPlacementPreflight() error = %v", err)
	}
	if claimed.ID != record.ID || claimed.LastDispatchedAt == nil || !claimed.LastDispatchedAt.Equal(dispatchedAt) {
		t.Errorf("claimed preflight = %#v", claimed)
	}
	earlyRetryAt := dispatchedAt.Add(placement.PreflightDispatchRetryDelay - time.Second)
	if _, err := database.ClaimPlacementPreflight(
		ctx,
		"colo",
		earlyRetryAt.Add(-placement.PreflightDispatchRetryDelay),
		earlyRetryAt,
	); !errors.Is(
		err,
		placement.ErrNoPreflightCommand,
	) {
		t.Fatalf("early redispatch error = %v, want no command", err)
	}

	jobUID := "probe-job-uid"
	runningAt := dispatchedAt.Add(time.Second)
	running, err := database.RecordPlacementPreflightReport(ctx, placement.PreflightReport{
		ID: record.ID, ClusterName: "colo", State: placement.PreflightStateRunning,
		CleanupState: placement.PreflightCleanupPending, JobUID: &jobUID, ObservedAt: runningAt,
	})
	if err != nil {
		t.Fatalf("running report error = %v", err)
	}
	if running.State != placement.PreflightStateRunning || running.JobUID == nil || *running.JobUID != jobUID {
		t.Errorf("running preflight = %#v", running)
	}
	replacementJobUID := "replacement-job-uid"
	if _, err := database.RecordPlacementPreflightReport(ctx, placement.PreflightReport{
		ID: record.ID, ClusterName: "colo", State: placement.PreflightStateRunning,
		CleanupState: placement.PreflightCleanupPending, JobUID: &replacementJobUID,
		ObservedAt: runningAt.Add(time.Second),
	}); !errors.Is(err, placement.ErrPreflightConflict) {
		t.Fatalf("replacement Job report error = %v, want conflict", err)
	}

	reason := "ProbeSucceeded"
	message := "The probe completed."
	finishedAt := runningAt.Add(2 * time.Second)
	finished, err := database.RecordPlacementPreflightReport(ctx, placement.PreflightReport{
		ID: record.ID, ClusterName: "colo", State: placement.PreflightStateSucceeded,
		CleanupState: placement.PreflightCleanupSucceeded, JobUID: &jobUID,
		Reason: &reason, Message: &message, ObservedAt: finishedAt,
	})
	if err != nil {
		t.Fatalf("terminal report error = %v", err)
	}
	if finished.State != placement.PreflightStateSucceeded || finished.FinishedAt == nil ||
		!finished.FinishedAt.Equal(finishedAt) {
		t.Errorf("finished preflight = %#v", finished)
	}

	var newestID uuid.UUID
	retentionReason := "MaterializationFailed"
	retentionMessage := "The probe Job could not be created."
	for index := 1; index <= placement.PreflightHistoryLimit; index++ {
		next := record
		next.ID = uuid.New()
		next.CreatedAt = finishedAt.Add(time.Duration(index) * time.Minute)
		if _, err := database.CreatePlacementPreflight(ctx, next); err != nil {
			t.Fatalf("create retained preflight %d: %v", index, err)
		}
		if _, err := database.RecordPlacementPreflightReport(ctx, placement.PreflightReport{
			ID:           next.ID,
			ClusterName:  "colo",
			State:        placement.PreflightStateFailed,
			CleanupState: placement.PreflightCleanupNotRequired,
			Reason:       &retentionReason,
			Message:      &retentionMessage,
			ObservedAt:   next.CreatedAt.Add(time.Second),
		}); err != nil {
			t.Fatalf("finish retained preflight %d: %v", index, err)
		}
		newestID = next.ID
	}

	history, err := database.ListPlacementPreflights(ctx, placement.PreflightListFilter{
		PlacementName: "infrastructure", Limit: placement.PreflightHistoryLimit,
	})
	if err != nil {
		t.Fatalf("ListPlacementPreflights() error = %v", err)
	}
	if history.Total != placement.PreflightHistoryLimit || len(history.Items) != placement.PreflightHistoryLimit ||
		history.Items[0].ID != newestID {
		t.Errorf("history = %#v", history)
	}
	for _, retained := range history.Items {
		if retained.ID == record.ID {
			t.Errorf("oldest preflight %s was retained", record.ID)
		}
	}
}
