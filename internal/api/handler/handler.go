package handler

import (
	"context"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controlleridentity"
)

type ControllerIdentityService interface {
	EnrollController(
		ctx context.Context,
		clusterName string,
		credentialSHA256 string,
	) error
	AuthenticateController(
		ctx context.Context,
		bearerToken string,
	) (controlleridentity.Principal, error)
}

type Handler struct {
	clusters     ClusterService
	identities   ControllerIdentityService
	repositories RepositoryService
	catalogs     CatalogService
	runs         RunService
	artifacts    ArtifactService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func New(
	clusters ClusterService,
	identities ControllerIdentityService,
	repositories RepositoryService,
	catalogs CatalogService,
	runs RunService,
	artifacts ArtifactService,
) *Handler {
	return &Handler{
		clusters:     clusters,
		identities:   identities,
		repositories: repositories,
		catalogs:     catalogs,
		runs:         runs,
		artifacts:    artifacts,
	}
}
