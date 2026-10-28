package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/placement"
)

func TestListPlacements(t *testing.T) {
	t.Parallel()

	timeout := 30 * time.Minute
	lastSeenAt := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
	service := &staticPlacementService{inventory: placement.Inventory{
		ConfigurationChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Items: []placement.InventoryPlacement{
			{Name: "first"},
			{
				Name:                      "second",
				ClusterName:               "colo",
				ControllerConnectionState: cluster.ConnectionStateConnected,
				ControllerLastSeenAt:      &lastSeenAt,
				JobNamespace:              "slipramp-runs",
				ServiceAccountName:        "operations",
				Timeout:                   &timeout,
				Resources: placement.ResourceRequirements{
					Requests: map[string]string{"cpu": "100m"},
					Limits:   map[string]string{"memory": "1Gi"},
				},
				CredentialNames: []string{"cloud"},
			},
		},
	}}
	handler := newHandler(Dependencies{Placements: service})
	limit := 1
	offset := int32(1)

	response, err := handler.ListPlacements(context.Background(), api.ListPlacementsRequestObject{
		Params: api.ListPlacementsParams{Limit: &limit, Offset: &offset},
	})
	if err != nil {
		t.Fatalf("ListPlacements() error = %v", err)
	}
	result, ok := response.(api.ListPlacements200JSONResponse)
	if !ok {
		t.Fatalf("ListPlacements() response = %T", response)
	}
	if result.Total != 2 || result.Limit != 1 || result.Offset != 1 || len(result.Items) != 1 {
		t.Fatalf("ListPlacements() result = %#v", result)
	}
	if result.ConfigurationChecksum != service.inventory.ConfigurationChecksum {
		t.Errorf(
			"ListPlacements() ConfigurationChecksum = %q, want %q",
			result.ConfigurationChecksum,
			service.inventory.ConfigurationChecksum,
		)
	}
	item := result.Items[0]
	if item.Name != "second" || item.Cluster != "colo" || item.Namespace != "slipramp-runs" ||
		item.ServiceAccount != "operations" || item.Timeout == nil || *item.Timeout != "30m0s" ||
		item.ControllerConnectionState != api.PlacementControllerConnectionStateConnected ||
		item.ControllerLastSeenAt == nil || !item.ControllerLastSeenAt.Equal(lastSeenAt) ||
		item.Resources.Requests["cpu"] != "100m" || item.Resources.Limits["memory"] != "1Gi" ||
		len(item.CredentialNames) != 1 || item.CredentialNames[0] != "cloud" {
		t.Errorf("ListPlacements() item = %#v", item)
	}
}

func TestListPlacementsRejectsInvalidWindow(t *testing.T) {
	t.Parallel()

	handler := newHandler(Dependencies{Placements: &staticPlacementService{}})
	limit := 0

	response, err := handler.ListPlacements(context.Background(), api.ListPlacementsRequestObject{
		Params: api.ListPlacementsParams{Limit: &limit},
	})
	if err != nil {
		t.Fatalf("ListPlacements() error = %v", err)
	}
	if _, ok := response.(api.ListPlacements400ApplicationProblemPlusJSONResponse); !ok {
		t.Fatalf("ListPlacements() response = %T", response)
	}
}

func TestListPlacementsReturnsInventoryFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	handler := newHandler(Dependencies{Placements: &staticPlacementService{err: want}})

	response, err := handler.ListPlacements(context.Background(), api.ListPlacementsRequestObject{})
	if response != nil || !errors.Is(err, want) {
		t.Fatalf("ListPlacements() = (%T, %v), want (nil, %v)", response, err, want)
	}
}

type staticPlacementService struct {
	inventory placement.Inventory
	err       error
}

func (s *staticPlacementService) Inventory(context.Context) (placement.Inventory, error) {
	return s.inventory, s.err
}
