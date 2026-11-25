package run

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/placement"
)

func TestSchedulingDerivesQueuedState(t *testing.T) {
	t.Parallel()

	placements, err := placement.NewSetWithClusterLimits(
		map[string]placement.Config{
			"infrastructure": {
				ClusterName:       "colo",
				JobNamespace:      "slipramp-runs",
				MaximumActiveRuns: 2,
			},
		},
		map[string]int32{"colo": 3},
	)
	if err != nil {
		t.Fatalf("NewSetWithClusterLimits() error = %v", err)
	}

	now := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
	blockingRunID := uuid.New()
	tests := []struct {
		name         string
		review       ReviewState
		connected    bool
		snapshot     SchedulingSnapshot
		wantState    SchedulingState
		wantBlock    bool
		wantPosition bool
	}{
		{
			name: "awaiting review", review: ReviewStatePending, connected: true,
			snapshot:  SchedulingSnapshot{GatesReady: true, EligiblePosition: new(int32(1))},
			wantState: SchedulingStateAwaitingReview,
		},
		{
			name: "awaiting gates", review: ReviewStateApproved, connected: true,
			snapshot:  SchedulingSnapshot{},
			wantState: SchedulingStateAwaitingGates,
		},
		{
			name: "target busy", review: ReviewStateApproved, connected: true,
			snapshot:  SchedulingSnapshot{GatesReady: true, BlockingRunID: &blockingRunID},
			wantState: SchedulingStateTargetBusy, wantBlock: true,
		},
		{
			name: "cluster unreachable", review: ReviewStateApproved,
			snapshot:  SchedulingSnapshot{GatesReady: true, EligiblePosition: new(int32(1))},
			wantState: SchedulingStateClusterUnreachable, wantPosition: true,
		},
		{
			name: "cluster capacity", review: ReviewStateApproved, connected: true,
			snapshot:  SchedulingSnapshot{GatesReady: true, ClusterActiveRuns: 3},
			wantState: SchedulingStateClusterCapacity,
		},
		{
			name: "placement capacity", review: ReviewStateApproved, connected: true,
			snapshot:  SchedulingSnapshot{GatesReady: true, PlacementActiveRuns: 2},
			wantState: SchedulingStateClusterCapacity,
		},
		{
			name: "eligible", review: ReviewStateNotRequired, connected: true,
			snapshot:  SchedulingSnapshot{GatesReady: true, EligiblePosition: new(int32(2))},
			wantState: SchedulingStateEligible, wantPosition: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runID := uuid.New()
			test.snapshot.RunID = runID
			store := &stubSchedulingStore{snapshots: []SchedulingSnapshot{test.snapshot}}
			service := &Service{placements: placements, scheduling: store}
			details, err := service.Scheduling(
				context.Background(),
				[]Run{{
					ID: runID, Phase: PhaseQueued, Review: Review{State: test.review},
					ClusterName: "colo", PlacementName: "infrastructure", CreatedAt: now.Add(-time.Minute),
				}},
				map[string]bool{"colo": test.connected},
				now,
			)
			if err != nil {
				t.Fatalf("Scheduling() error = %v", err)
			}
			detail := details[runID]
			if detail.State != test.wantState {
				t.Errorf("state = %q, want %q", detail.State, test.wantState)
			}
			if (detail.BlockingRunID != nil) != test.wantBlock {
				t.Errorf("blocking Run present = %t, want %t", detail.BlockingRunID != nil, test.wantBlock)
			}
			if (detail.EligiblePosition != nil) != test.wantPosition {
				t.Errorf("eligible position present = %t, want %t", detail.EligiblePosition != nil, test.wantPosition)
			}
			if detail.QueueAge != time.Minute || !detail.SnapshotAt.Equal(now) {
				t.Errorf("queue snapshot = (%s, %s), want (1m, %s)", detail.QueueAge, detail.SnapshotAt, now)
			}
		})
	}
}

type stubSchedulingStore struct {
	snapshots []SchedulingSnapshot
	counts    map[string]RunCounts
}

func (s *stubSchedulingStore) ListClusterPlacementRunCounts(context.Context, string) (map[string]RunCounts, error) {
	return s.counts, nil
}

func TestClusterScheduling(t *testing.T) {
	t.Parallel()

	placements, err := placement.NewSetWithClusterLimits(map[string]placement.Config{
		"shared": {ClusterName: "colo", JobNamespace: "shared-jobs"},
		"capped": {ClusterName: "colo", JobNamespace: "batch-jobs", MaximumActiveRuns: 1},
		"remote": {ClusterName: "remote", JobNamespace: "jobs"},
	}, map[string]int32{"colo": 2})
	if err != nil {
		t.Fatal(err)
	}
	store := &stubSchedulingStore{counts: map[string]RunCounts{
		"capped":  {Active: 1, Queued: 2},
		"removed": {Active: 2, Queued: 1},
	}}
	service := &Service{placements: placements, scheduling: store}
	got, err := service.ClusterScheduling(t.Context(), "colo")
	if err != nil {
		t.Fatal(err)
	}
	want := ClusterScheduling{
		Active: 3, Queued: 3, MaximumActiveRuns: 2,
		Placements: []PlacementScheduling{
			{Name: "capped", Namespace: "batch-jobs", MaximumActiveRuns: 1, Active: 1, Queued: 2},
			{Name: "shared", Namespace: "shared-jobs"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClusterScheduling() = %#v, want %#v", got, want)
	}

	store.counts = nil
	empty, err := service.ClusterScheduling(t.Context(), "idle")
	if err != nil || empty.MaximumActiveRuns != DefaultMaximumActiveRuns || len(empty.Placements) != 0 {
		t.Errorf("ClusterScheduling(idle) = %#v, %v", empty, err)
	}
}

func (s *stubSchedulingStore) GetRunSchedulingSnapshots(
	context.Context,
	[]uuid.UUID,
	uuid.UUID,
	time.Time,
	[]byte,
	[]byte,
) ([]SchedulingSnapshot, error) {
	return s.snapshots, nil
}
