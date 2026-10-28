package handler

import (
	"context"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/placement"
)

func (h *Handler) ListPlacements(
	ctx context.Context,
	request api.ListPlacementsRequestObject,
) (api.ListPlacementsResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListPlacements400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}

	inventory, err := h.placements.Inventory(ctx)
	if err != nil {
		return nil, err
	}

	total := len(inventory.Items)
	page := listPage(inventory.Items, offset, limit)
	items := make([]api.Placement, 0, len(page))
	for _, configured := range page {
		items = append(items, placementResponse(configured))
	}

	return api.ListPlacements200JSONResponse{
		Items:                 items,
		ConfigurationChecksum: inventory.ConfigurationChecksum,
		Total:                 total,
		Limit:                 limit,
		Offset:                offset,
	}, nil
}

func (h *Handler) GetPlacement(
	ctx context.Context,
	request api.GetPlacementRequestObject,
) (api.GetPlacementResponseObject, error) {
	inventory, err := h.placements.Inventory(ctx)
	if err != nil {
		return nil, err
	}

	for _, configured := range inventory.Items {
		if configured.Name != string(request.PlacementName) {
			continue
		}

		return api.GetPlacement200JSONResponse{
			Placement:             placementResponse(configured),
			ConfigurationChecksum: inventory.ConfigurationChecksum,
		}, nil
	}

	return api.GetPlacement404ApplicationProblemPlusJSONResponse(
		NewProblem(http.StatusNotFound, "The Placement is not configured."),
	), nil
}

func placementResponse(configured placement.InventoryPlacement) api.Placement {
	response := api.Placement{
		Name:    configured.Name,
		Cluster: configured.ClusterName,
		ControllerConnectionState: api.PlacementControllerConnectionState(
			configured.ControllerConnectionState,
		),
		ControllerLastSeenAt: configured.ControllerLastSeenAt,
		Namespace:            configured.JobNamespace,
		ServiceAccount:       configured.ServiceAccountName,
		Resources: api.PlacementResourceRequirements{
			Requests: configured.Resources.Requests,
			Limits:   configured.Resources.Limits,
		},
		CredentialNames: configured.CredentialNames,
		TargetCount:     configured.TargetCount,
	}
	if configured.Timeout != nil {
		response.Timeout = new(configured.Timeout.String())
	}

	return response
}
