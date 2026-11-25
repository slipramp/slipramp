package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
)

func (h *Handler) GetClusterScheduling(
	ctx context.Context,
	request api.GetClusterSchedulingRequestObject,
) (api.GetClusterSchedulingResponseObject, error) {
	scheduling, err := h.runs.ClusterScheduling(ctx, string(request.ClusterName))
	if errors.Is(err, cluster.ErrNotFound) {
		return api.GetClusterScheduling404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The Server has never observed this Cluster."),
		), nil
	}
	if err != nil {
		return nil, err
	}

	placements := make([]api.ClusterPlacementScheduling, 0, len(scheduling.Placements))
	for _, placement := range scheduling.Placements {
		item := api.ClusterPlacementScheduling{
			Name:       placement.Name,
			Namespace:  placement.Namespace,
			ActiveRuns: placement.Active,
			QueuedRuns: placement.Queued,
		}
		if placement.MaximumActiveRuns > 0 {
			item.MaximumActiveRuns = new(placement.MaximumActiveRuns)
		}
		placements = append(placements, item)
	}

	return api.GetClusterScheduling200JSONResponse{
		MaximumActiveRuns: scheduling.MaximumActiveRuns,
		ActiveRuns:        scheduling.Active,
		QueuedRuns:        scheduling.Queued,
		Placements:        placements,
	}, nil
}
