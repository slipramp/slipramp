package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productidentity"
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
				TargetCount:     4,
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
		len(item.CredentialNames) != 1 || item.CredentialNames[0] != "cloud" || item.TargetCount != 4 {
		t.Errorf("ListPlacements() item = %#v", item)
	}
}

func TestPlacementPreflightEndpoints(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	actor := productidentity.ActorSnapshot{
		PrincipalID: uuid.New(), Kind: productidentity.KindUser, DisplayName: "Operator",
	}
	record := placement.Preflight{
		ID: uuid.New(),
		Snapshot: placement.PreflightSnapshot{
			PlacementName: "infrastructure", ClusterName: "colo",
			ConfigurationChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		State: placement.PreflightStateQueued, CleanupState: placement.PreflightCleanupPending,
		RequestedBy: actor, CreatedAt: now,
	}
	service := &recordingPlacementPreflightService{
		created: record,
		listed:  placement.PreflightList{Items: []placement.Preflight{record}, Total: 1},
	}
	handler := newHandler(Dependencies{PlacementPreflights: service})
	principal := productidentity.Principal{
		ID: actor.PrincipalID, Kind: actor.Kind, DisplayName: actor.DisplayName,
	}
	ctx := context.WithValue(context.Background(), productAuthenticationContextKey{}, productidentity.Authentication{
		Principal: principal,
	})

	created, err := handler.CreatePlacementPreflight(ctx, api.CreatePlacementPreflightRequestObject{
		PlacementName: "infrastructure",
	})
	if err != nil {
		t.Fatalf("CreatePlacementPreflight() error = %v", err)
	}
	createdResponse, ok := created.(api.CreatePlacementPreflight201JSONResponse)
	if !ok {
		t.Fatalf("CreatePlacementPreflight() response = %T", created)
	}
	if createdResponse.ID != record.ID || service.createdPlacementName != "infrastructure" ||
		service.createdBy == nil || service.createdBy.ID != principal.ID {
		t.Errorf("CreatePlacementPreflight() = %#v, service = %#v", createdResponse, service)
	}

	listed, err := handler.ListPlacementPreflights(ctx, api.ListPlacementPreflightsRequestObject{
		PlacementName: "infrastructure",
	})
	if err != nil {
		t.Fatalf("ListPlacementPreflights() error = %v", err)
	}
	listResponse, ok := listed.(api.ListPlacementPreflights200JSONResponse)
	if !ok {
		t.Fatalf("ListPlacementPreflights() response = %T", listed)
	}
	if listResponse.Total != 1 || listResponse.Limit != placement.PreflightHistoryLimit ||
		len(listResponse.Items) != 1 || listResponse.Items[0].RequestedBy.DisplayName != actor.DisplayName ||
		service.listFilter.PlacementName != "infrastructure" ||
		service.listFilter.Limit != placement.PreflightHistoryLimit {
		t.Errorf("ListPlacementPreflights() = %#v, filter = %#v", listResponse, service.listFilter)
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

func TestGetPlacement(t *testing.T) {
	t.Parallel()

	clusterMaximumActiveRuns := int32(4)
	service := &staticPlacementService{inventory: placement.Inventory{
		ConfigurationChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Items: []placement.InventoryPlacement{
			{
				Name:                     "infrastructure",
				ClusterName:              "colo",
				ClusterMaximumActiveRuns: &clusterMaximumActiveRuns,
				TargetCount:              3,
				Configuration: placement.Config{
					ClusterName:        "colo",
					JobNamespace:       "slipramp-runs",
					MaximumActiveRuns:  2,
					ServiceAccountName: "infrastructure-admin",
					Timeout:            30 * time.Minute,
					Resources: placement.ResourceRequirements{
						Requests: map[string]string{"cpu": "100m"},
						Limits:   map[string]string{"memory": "1Gi"},
					},
					NodeSelector: map[string]string{"topology.kubernetes.io/zone": "us-west-2a"},
					Credentials: map[string]placement.CredentialSource{
						"cloud": {SecretRef: placement.SecretReference{Name: "cloud-credentials"}},
					},
					PodTemplate: &corev1.PodTemplateSpec{
						Labels: map[string]string{"isolation": "strict"},
					},
					ActionVolumes: []corev1.Volume{{
						Name:     "tool-cache",
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					}},
					ActionVolumeMounts: []corev1.VolumeMount{{
						Name: "tool-cache", MountPath: "/cache",
					}},
				},
			},
		},
	}}
	handler := newHandler(Dependencies{Placements: service})

	response, err := handler.GetPlacement(context.Background(), api.GetPlacementRequestObject{
		PlacementName: "infrastructure",
	})
	if err != nil {
		t.Fatalf("GetPlacement() error = %v", err)
	}
	result, ok := response.(api.GetPlacement200JSONResponse)
	if !ok {
		t.Fatalf("GetPlacement() response = %T", response)
	}
	if result.ConfigurationChecksum != service.inventory.ConfigurationChecksum ||
		result.Placement.Name != "infrastructure" || result.Placement.Cluster != "colo" ||
		result.Placement.TargetCount != 3 {
		t.Errorf("GetPlacement() result = %#v", result)
	}
	definition := result.Definition.Placements["infrastructure"]
	if result.Definition.APIVersion != api.PlacementDefinitionAPIVersionV1Alpha1 ||
		result.Definition.Kind != api.PlacementDefinitionKindPlacementConfig ||
		result.Definition.Clusters == nil ||
		(*result.Definition.Clusters)["colo"].MaximumActiveRuns == nil ||
		*(*result.Definition.Clusters)["colo"].MaximumActiveRuns != 4 ||
		definition.Cluster != "colo" || definition.JobNamespace != "slipramp-runs" ||
		definition.MaximumActiveRuns == nil || *definition.MaximumActiveRuns != 2 ||
		definition.ServiceAccountName == nil || *definition.ServiceAccountName != "infrastructure-admin" ||
		definition.Timeout == nil || *definition.Timeout != "30m0s" ||
		definition.Resources == nil || definition.Resources.Requests == nil ||
		(*definition.Resources.Requests)["cpu"] != "100m" ||
		definition.NodeSelector == nil ||
		(*definition.NodeSelector)["topology.kubernetes.io/zone"] != "us-west-2a" ||
		definition.Credentials == nil ||
		(*definition.Credentials)["cloud"].SecretRef.Name != "cloud-credentials" ||
		definition.PodTemplate == nil || (*definition.PodTemplate)["metadata"] == nil ||
		definition.ActionVolumes == nil || len(*definition.ActionVolumes) != 1 ||
		definition.ActionVolumeMounts == nil || len(*definition.ActionVolumeMounts) != 1 {
		t.Errorf("GetPlacement() Definition = %#v", result.Definition)
	}
}

func TestGetPlacementReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler := newHandler(Dependencies{Placements: &staticPlacementService{}})
	response, err := handler.GetPlacement(context.Background(), api.GetPlacementRequestObject{
		PlacementName: "missing",
	})
	if err != nil {
		t.Fatalf("GetPlacement() error = %v", err)
	}
	if _, ok := response.(api.GetPlacement404ApplicationProblemPlusJSONResponse); !ok {
		t.Fatalf("GetPlacement() response = %T", response)
	}
}

type staticPlacementService struct {
	inventory placement.Inventory
	err       error
}

type recordingPlacementPreflightService struct {
	created              placement.Preflight
	listed               placement.PreflightList
	createdPlacementName string
	createdBy            *productidentity.Principal
	listFilter           placement.PreflightListFilter
	err                  error
}

func (s *recordingPlacementPreflightService) Create(
	_ context.Context,
	placementName string,
	principal *productidentity.Principal,
) (placement.Preflight, error) {
	s.createdPlacementName = placementName
	s.createdBy = principal
	return s.created, s.err
}

func (s *recordingPlacementPreflightService) List(
	_ context.Context,
	filter placement.PreflightListFilter,
) (placement.PreflightList, error) {
	s.listFilter = filter
	return s.listed, s.err
}

func (s *recordingPlacementPreflightService) Report(
	context.Context,
	placement.PreflightReport,
) (placement.Preflight, error) {
	return placement.Preflight{}, s.err
}

func (s *staticPlacementService) Inventory(context.Context) (placement.Inventory, error) {
	return s.inventory, s.err
}
