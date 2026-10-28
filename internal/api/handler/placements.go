package handler

import (
	"context"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
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
		item := api.Placement{
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
		}
		if configured.Timeout != nil {
			item.Timeout = new(configured.Timeout.String())
		}

		items = append(items, item)
	}

	return api.ListPlacements200JSONResponse{
		Items:                 items,
		ConfigurationChecksum: inventory.ConfigurationChecksum,
		Total:                 total,
		Limit:                 limit,
		Offset:                offset,
	}, nil
}
