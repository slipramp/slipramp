package placement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/cluster"
)

func TestInventoryServiceReturnsSanitizedEffectiveConfiguration(t *testing.T) {
	t.Parallel()

	configured, err := NewSet(map[string]Config{
		"preview": {
			ClusterName:        "development",
			JobNamespace:       "previews",
			ServiceAccountName: "preview-runner",
			Timeout:            20 * time.Minute,
			Resources: ResourceRequirements{
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"memory": "1Gi"},
			},
			Credentials: map[string]CredentialSource{
				"registry": {SecretRef: SecretReference{Name: "private-registry-secret"}},
				"cloud":    {SecretRef: SecretReference{Name: "cloud-secret"}},
			},
		},
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
		},
	})
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	lastSeenAt := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
	service := NewInventoryService(
		configured,
		staticClusterInventory{statuses: []cluster.Status{
			{
				Name:            "development",
				LastSeenAt:      lastSeenAt,
				ConnectionState: cluster.ConnectionStateConnected,
			},
		}},
		staticTargetUsage{usage: map[string]int64{"preview": 3}},
	)

	inventory, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if inventory.ConfigurationChecksum != configured.Checksum() {
		t.Errorf(
			"ConfigurationChecksum = %q, want %q",
			inventory.ConfigurationChecksum,
			configured.Checksum(),
		)
	}
	if len(inventory.Items) != 2 {
		t.Fatalf("item count = %d, want 2", len(inventory.Items))
	}

	infrastructure := inventory.Items[0]
	if infrastructure.Name != "infrastructure" || infrastructure.ClusterName != "colo" ||
		infrastructure.JobNamespace != "slipramp-runs" || infrastructure.ServiceAccountName != "default" {
		t.Errorf("infrastructure = %#v", infrastructure)
	}
	if infrastructure.ControllerConnectionState != cluster.ConnectionStateUnreachable ||
		infrastructure.ControllerLastSeenAt != nil || infrastructure.TargetCount != 0 {
		t.Errorf("infrastructure Controller state = %#v", infrastructure)
	}
	if infrastructure.Timeout != nil || len(infrastructure.Resources.Requests) != 0 ||
		len(infrastructure.Resources.Limits) != 0 || len(infrastructure.CredentialNames) != 0 {
		t.Errorf("infrastructure defaults = %#v", infrastructure)
	}

	preview := inventory.Items[1]
	if preview.ControllerConnectionState != cluster.ConnectionStateConnected ||
		preview.ControllerLastSeenAt == nil || !preview.ControllerLastSeenAt.Equal(lastSeenAt) ||
		preview.TargetCount != 3 {
		t.Errorf("preview Controller state = %#v", preview)
	}
	if preview.Timeout == nil || *preview.Timeout != 20*time.Minute ||
		preview.Resources.Requests["cpu"] != "100m" || preview.Resources.Limits["memory"] != "1Gi" {
		t.Errorf("preview defaults = %#v", preview)
	}
	if len(preview.CredentialNames) != 2 || preview.CredentialNames[0] != "cloud" ||
		preview.CredentialNames[1] != "registry" {
		t.Errorf("preview CredentialNames = %v", preview.CredentialNames)
	}

	preview.Resources.Requests["cpu"] = "2"
	again, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() again error = %v", err)
	}
	if again.Items[1].Resources.Requests["cpu"] != "100m" {
		t.Errorf("Inventory() retained mutated request = %q", again.Items[1].Resources.Requests["cpu"])
	}
}

func TestInventoryServiceReturnsClusterFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	service := NewInventoryService(Set{}, staticClusterInventory{err: want}, staticTargetUsage{})

	_, err := service.Inventory(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Inventory() error = %v, want %v", err, want)
	}
}

func TestInventoryServiceReturnsTargetUsageFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	service := NewInventoryService(Set{}, staticClusterInventory{}, staticTargetUsage{err: want})

	_, err := service.Inventory(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Inventory() error = %v, want %v", err, want)
	}
}

type staticClusterInventory struct {
	statuses []cluster.Status
	err      error
}

func (s staticClusterInventory) ListClusters(context.Context) ([]cluster.Status, error) {
	return s.statuses, s.err
}

type staticTargetUsage struct {
	usage map[string]int64
	err   error
}

func (s staticTargetUsage) ListPlacementTargetUsage(context.Context) (map[string]int64, error) {
	return s.usage, s.err
}
