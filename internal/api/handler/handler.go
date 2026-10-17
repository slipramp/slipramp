package handler

import "github.com/slipramp/slipramp/internal/api"

type Handler struct {
	clusters     ClusterService
	repositories RepositoryService
	catalogs     CatalogService
	runs         RunService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func New(
	clusters ClusterService,
	repositories RepositoryService,
	catalogs CatalogService,
	runs RunService,
) *Handler {
	return &Handler{
		clusters:     clusters,
		repositories: repositories,
		catalogs:     catalogs,
		runs:         runs,
	}
}
