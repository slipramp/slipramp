package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/placement"
)

func (h *Handler) ListPlacementPreflights(
	ctx context.Context,
	request api.ListPlacementPreflightsRequestObject,
) (api.ListPlacementPreflightsResponseObject, error) {
	limitValue := request.Params.Limit
	if limitValue == nil {
		limitValue = new(placement.DefaultPreflightListLimit)
	}
	limit, offset, windowErr := listWindow(limitValue, request.Params.Offset)
	if windowErr != nil || limit > placement.MaximumPreflightListLimit {
		return api.ListPlacementPreflights400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, "The preflight list window is invalid."),
		), nil
	}

	records, err := h.placementPreflights.List(ctx, placement.PreflightListFilter{
		PlacementName: string(request.PlacementName),
		Limit:         int32(limit),
		Offset:        int32(offset),
	})
	if errors.Is(err, placement.ErrPreflightPlacementNotFound) {
		return api.ListPlacementPreflights404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The Placement is not configured."),
		), nil
	}
	if errors.Is(err, placement.ErrPreflightInvalid) {
		return api.ListPlacementPreflights400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]api.PlacementPreflight, 0, len(records.Items))
	for _, record := range records.Items {
		items = append(items, placementPreflightResponse(record))
	}

	return api.ListPlacementPreflights200JSONResponse{
		Items:  items,
		Total:  int(records.Total),
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (h *Handler) CreatePlacementPreflight(
	ctx context.Context,
	request api.CreatePlacementPreflightRequestObject,
) (api.CreatePlacementPreflightResponseObject, error) {
	created, err := h.placementPreflights.Create(
		ctx,
		string(request.PlacementName),
		currentProductPrincipal(ctx),
	)
	if err != nil {
		switch {
		case errors.Is(err, placement.ErrPreflightPlacementNotFound):
			return api.CreatePlacementPreflight404ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusNotFound, "The Placement is not configured."),
			), nil
		case errors.Is(err, placement.ErrPreflightControllerUnreachable):
			return api.CreatePlacementPreflight409ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusConflict, "The Placement Controller is unreachable."),
			), nil
		case errors.Is(err, placement.ErrPreflightActive):
			return api.CreatePlacementPreflight409ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusConflict, "The Placement already has an active preflight."),
			), nil
		case errors.Is(err, placement.ErrPreflightInvalid):
			return api.CreatePlacementPreflight409ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusConflict, err.Error()),
			), nil
		default:
			return nil, err
		}
	}

	return api.CreatePlacementPreflight201JSONResponse(placementPreflightResponse(created)), nil
}

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

func placementPreflightResponse(record placement.Preflight) api.PlacementPreflight {
	diagnostics := make([]api.PlacementPreflightDiagnostic, 0, len(record.Diagnostics))
	for _, diagnostic := range record.Diagnostics {
		diagnostics = append(diagnostics, api.PlacementPreflightDiagnostic{
			Reason:          diagnostic.Reason,
			Message:         diagnostic.Message,
			Count:           diagnostic.Count,
			FirstObservedAt: diagnostic.FirstObservedAt,
			LastObservedAt:  diagnostic.LastObservedAt,
		})
	}
	var responseDiagnostics *[]api.PlacementPreflightDiagnostic
	if len(diagnostics) > 0 {
		responseDiagnostics = &diagnostics
	}

	return api.PlacementPreflight{
		ID:                    record.ID,
		PlacementName:         api.CatalogName(record.Snapshot.PlacementName),
		Cluster:               api.CatalogName(record.Snapshot.ClusterName),
		ConfigurationChecksum: record.Snapshot.ConfigurationChecksum,
		State:                 api.PlacementPreflightState(record.State),
		CleanupState:          api.PlacementPreflightCleanupState(record.CleanupState),
		RequestedBy: api.RunActor{
			PrincipalID: record.RequestedBy.PrincipalID,
			Kind:        api.RunActorKind(record.RequestedBy.Kind),
			DisplayName: record.RequestedBy.DisplayName,
		},
		CreatedAt:   record.CreatedAt,
		StartedAt:   record.StartedAt,
		FinishedAt:  record.FinishedAt,
		JobUID:      record.JobUID,
		Reason:      record.Reason,
		Message:     record.Message,
		Diagnostics: responseDiagnostics,
	}
}
