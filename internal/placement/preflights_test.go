package placement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestPreflightServiceCreatesSnapshotForConnectedPlacement(t *testing.T) {
	t.Parallel()

	placements, err := NewSet(map[string]Config{
		"infrastructure": {
			ClusterName:        "colo",
			JobNamespace:       "slipramp-runs",
			ServiceAccountName: "infrastructure-admin",
			Resources: ResourceRequirements{
				Requests: map[string]string{"cpu": "100m"},
			},
			NodeSelector: map[string]string{"workload": "infrastructure"},
		},
	})
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	store := &recordingPreflightStore{}
	var connected cluster.Status
	connected.Name = "colo"
	connected.ConnectionState = cluster.ConnectionStateConnected
	service := NewPreflightService(
		placements,
		staticClusterInventory{statuses: []cluster.Status{connected}},
		store,
	)
	now := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	principal := &productidentity.Principal{
		ID: uuid.New(), Kind: productidentity.KindUser, DisplayName: "Operator",
	}

	created, err := service.Create(context.Background(), "infrastructure", principal)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == uuid.Nil || created.State != PreflightStateQueued ||
		created.CleanupState != PreflightCleanupPending || !created.CreatedAt.Equal(now) {
		t.Errorf("created preflight = %#v", created)
	}
	if created.Snapshot.PlacementName != "infrastructure" || created.Snapshot.ClusterName != "colo" ||
		created.Snapshot.JobNamespace != "slipramp-runs" ||
		created.Snapshot.ServiceAccountName != "infrastructure-admin" ||
		created.Snapshot.ConfigurationChecksum != placements.Checksum() ||
		created.Snapshot.Resources.Requests["cpu"] != "100m" ||
		created.Snapshot.NodeSelector["workload"] != "infrastructure" {
		t.Errorf("created snapshot = %#v", created.Snapshot)
	}
	if created.RequestedBy.PrincipalID != principal.ID || created.RequestedBy.DisplayName != "Operator" {
		t.Errorf("requested by = %#v", created.RequestedBy)
	}
}

func TestPreflightServiceRejectsUnreachableController(t *testing.T) {
	t.Parallel()

	placements, err := NewSet(map[string]Config{
		"infrastructure": {ClusterName: "colo", JobNamespace: "slipramp-runs"},
	})
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	service := NewPreflightService(placements, staticClusterInventory{}, &recordingPreflightStore{})
	principal := &productidentity.Principal{
		ID: uuid.New(), Kind: productidentity.KindUser, DisplayName: "Operator",
	}

	_, err = service.Create(context.Background(), "infrastructure", principal)
	if !errors.Is(err, ErrPreflightControllerUnreachable) {
		t.Fatalf("Create() error = %v, want unreachable", err)
	}
}

func TestValidateReportTransitionPreservesJobAndResult(t *testing.T) {
	t.Parallel()

	jobUID := "job-uid"
	otherJobUID := "other-job-uid"
	reason := "ProbeSucceeded"
	otherReason := "ProbeFailed"
	message := "The probe completed."
	tests := []struct {
		name    string
		current Preflight
		report  PreflightReport
		wantErr error
	}{
		{
			name:    "repeat running report",
			current: Preflight{State: PreflightStateRunning, JobUID: &jobUID},
			report: PreflightReport{
				State: PreflightStateRunning, CleanupState: PreflightCleanupPending, JobUID: &jobUID,
			},
		},
		{
			name:    "replacement Job",
			current: Preflight{State: PreflightStateRunning, JobUID: &jobUID},
			report: PreflightReport{
				State: PreflightStateRunning, CleanupState: PreflightCleanupPending, JobUID: &otherJobUID,
			},
			wantErr: ErrPreflightConflict,
		},
		{
			name:    "running Job without cleanup",
			current: Preflight{State: PreflightStateRunning, JobUID: &jobUID},
			report: PreflightReport{
				State: PreflightStateFailed, CleanupState: PreflightCleanupNotRequired, JobUID: &jobUID,
			},
			wantErr: ErrPreflightConflict,
		},
		{
			name: "repeat terminal report",
			current: Preflight{
				State: PreflightStateSucceeded, CleanupState: PreflightCleanupSucceeded,
				JobUID: &jobUID, Reason: &reason, Message: &message,
			},
			report: PreflightReport{
				State: PreflightStateSucceeded, CleanupState: PreflightCleanupSucceeded,
				JobUID: &jobUID, Reason: &reason, Message: &message,
			},
		},
		{
			name: "changed terminal result",
			current: Preflight{
				State: PreflightStateSucceeded, CleanupState: PreflightCleanupSucceeded,
				JobUID: &jobUID, Reason: &reason, Message: &message,
			},
			report: PreflightReport{
				State: PreflightStateSucceeded, CleanupState: PreflightCleanupSucceeded,
				JobUID: &jobUID, Reason: &otherReason, Message: &message,
			},
			wantErr: ErrPreflightConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateReportTransition(test.current, test.report)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ValidateReportTransition() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

type recordingPreflightStore struct {
	record Preflight
	err    error
}

func (s *recordingPreflightStore) CreatePlacementPreflight(
	_ context.Context,
	record Preflight,
) (Preflight, error) {
	s.record = record
	return record, s.err
}

func (s *recordingPreflightStore) ListPlacementPreflights(
	context.Context,
	PreflightListFilter,
) (PreflightList, error) {
	return PreflightList{}, s.err
}

func (s *recordingPreflightStore) ClaimPlacementPreflight(
	context.Context,
	string,
	time.Time,
	time.Time,
) (Preflight, error) {
	return s.record, s.err
}

func (s *recordingPreflightStore) RecordPlacementPreflightReport(
	context.Context,
	PreflightReport,
) (Preflight, error) {
	return s.record, s.err
}
