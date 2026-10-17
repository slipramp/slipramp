package handler

import "github.com/slipramp/slipramp/internal/api"

type Handler struct {
	controllerWork ControllerWorkWaiter
	clusters       ClusterLister
}

var _ api.StrictServerInterface = (*Handler)(nil)

func New(
	controllerWork ControllerWorkWaiter,
	clusters ClusterLister,
) *Handler {
	return &Handler{
		controllerWork: controllerWork,
		clusters:       clusters,
	}
}
