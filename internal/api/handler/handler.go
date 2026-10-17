package handler

import "github.com/slipramp/slipramp/internal/api"

type Handler struct {
	clusters     ClusterService
	repositories RepositoryService
	catalogs     CatalogService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func New(
	clusters ClusterService,
	repositories RepositoryService,
	catalogs CatalogService,
) *Handler {
	return &Handler{
		clusters:     clusters,
		repositories: repositories,
		catalogs:     catalogs,
	}
}
