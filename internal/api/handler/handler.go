package handler

import (
	"context"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/readiness"
)

type ControllerIdentityService interface {
	EnrollController(ctx context.Context, clusterName string, credentialSHA256 string) error
	AuthenticateController(ctx context.Context, bearerToken string) (controlleridentity.Principal, error)
}

type Handler struct {
	clusters     ClusterService
	identities   ControllerIdentityService
	repositories RepositoryService
	catalogs     CatalogService
	runs         RunService
	artifacts    ArtifactService
	readiness    ReadinessService
	productAuth  ProductAuthenticationService
	now          func() time.Time
}

type ReadinessService interface {
	Check(ctx context.Context) readiness.Report
}

type ProductAuthenticationService interface {
	BrowserAuthenticationService
	AccessService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func New(
	clusters ClusterService,
	identities ControllerIdentityService,
	repositories RepositoryService,
	catalogs CatalogService,
	runs RunService,
	artifacts ArtifactService,
	readinessService ReadinessService,
	productAuth ProductAuthenticationService,
) *Handler {
	return &Handler{
		clusters:     clusters,
		identities:   identities,
		repositories: repositories,
		catalogs:     catalogs,
		runs:         runs,
		artifacts:    artifacts,
		readiness:    readinessService,
		productAuth:  productAuth,
		now:          time.Now,
	}
}
