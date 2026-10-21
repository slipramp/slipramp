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

type Dependencies struct {
	Clusters              ClusterService
	ControllerIdentities  ControllerIdentityService
	Repositories          RepositoryService
	Catalogs              CatalogService
	Runs                  RunService
	Artifacts             ArtifactService
	Readiness             ReadinessService
	ProductAuthentication ProductAuthenticationService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func newHandler(dependencies Dependencies) *Handler {
	return &Handler{
		clusters:     dependencies.Clusters,
		identities:   dependencies.ControllerIdentities,
		repositories: dependencies.Repositories,
		catalogs:     dependencies.Catalogs,
		runs:         dependencies.Runs,
		artifacts:    dependencies.Artifacts,
		readiness:    dependencies.Readiness,
		productAuth:  dependencies.ProductAuthentication,
		now:          time.Now,
	}
}
