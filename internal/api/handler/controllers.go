package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
)

type ClusterService interface {
	ListClusters(ctx context.Context) ([]cluster.Status, error)
	WaitForControllerWork(
		ctx context.Context,
		clusterName string,
		wait time.Duration,
	) error
}

const (
	defaultControllerPollWaitSeconds = 25
	maximumControllerPollWaitSeconds = 30
)

func (h *Handler) ListClusters(
	ctx context.Context,
	_ api.ListClustersRequestObject,
) (api.ListClustersResponseObject, error) {
	statuses, err := h.clusters.ListClusters(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]api.Cluster, 0, len(statuses))
	for _, status := range statuses {
		items = append(items, api.Cluster{
			Name:            status.Name,
			ConnectionState: api.ClusterConnectionState(status.ConnectionState),
			FirstSeenAt:     status.FirstSeenAt,
			LastSeenAt:      status.LastSeenAt,
		})
	}

	return api.ListClusters200JSONResponse{Items: items}, nil
}

func (h *Handler) PollForControllerWork(
	ctx context.Context,
	request api.PollForControllerWorkRequestObject,
) (api.PollForControllerWorkResponseObject, error) {
	timeoutSeconds := defaultControllerPollWaitSeconds
	if request.Params.TimeoutSeconds != nil {
		timeoutSeconds = *request.Params.TimeoutSeconds
	}

	if timeoutSeconds < 0 || timeoutSeconds > maximumControllerPollWaitSeconds {
		problem := NewProblem(
			http.StatusBadRequest,
			"timeoutSeconds must be between 0 and 30",
		)

		return api.PollForControllerWork400ApplicationProblemPlusJSONResponse(problem), nil
	}

	wait := time.Duration(timeoutSeconds) * time.Second
	if err := h.clusters.WaitForControllerWork(
		ctx,
		request.ClusterName,
		wait,
	); err != nil {
		return nil, err
	}

	return api.PollForControllerWork204Response{}, nil
}
