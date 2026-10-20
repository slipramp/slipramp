package handler

import (
	"context"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
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
	productAuth  ProductAuthenticationService
	now          func() time.Time
}

type ProductAuthenticationService interface {
	BeginLogin() (productauth.LoginStart, error)
	CompleteLogin(
		ctx context.Context,
		code string,
		state string,
		cookieState string,
	) (productauth.SessionCredential, error)
	AuthenticateSession(
		ctx context.Context,
		token string,
	) (productidentity.Authentication, error)
	RevokeSession(context.Context, string) error
	SecureCookies() bool
}

var _ api.StrictServerInterface = (*Handler)(nil)

func New(
	clusters ClusterService,
	identities ControllerIdentityService,
	repositories RepositoryService,
	catalogs CatalogService,
	runs RunService,
	artifacts ArtifactService,
	productAuth ProductAuthenticationService,
) *Handler {
	return &Handler{
		clusters:     clusters,
		identities:   identities,
		repositories: repositories,
		catalogs:     catalogs,
		runs:         runs,
		artifacts:    artifacts,
		productAuth:  productAuth,
		now:          time.Now,
	}
}
