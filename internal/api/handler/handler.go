package handler

import "github.com/slipramp/slipramp/internal/api"

type Handler struct {
	clusters     ClusterService
	repositories RepositoryService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func New(
	clusters ClusterService,
	repositories RepositoryService,
) *Handler {
	return &Handler{
		clusters:     clusters,
		repositories: repositories,
	}
}
