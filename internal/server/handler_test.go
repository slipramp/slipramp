package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api/handler"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/readiness"
)

// newAdminTestHandler supplies an administrator API key to product requests so
// endpoint tests outside authentication can stay focused on their feature.
func newAdminTestHandler(
	clusters handler.ClusterService,
	identities handler.ControllerIdentityService,
	repositories handler.RepositoryService,
	catalogs handler.CatalogService,
	runs handler.RunService,
	artifacts handler.ArtifactService,
	controllerEnrollmentTokens map[string]string,
) http.Handler {
	return newAdminTestHandlerWithLogger(
		clusters,
		identities,
		repositories,
		catalogs,
		runs,
		artifacts,
		controllerEnrollmentTokens,
		nil,
	)
}

func newAdminTestHandlerWithLogger(
	clusters handler.ClusterService,
	identities handler.ControllerIdentityService,
	repositories handler.RepositoryService,
	catalogs handler.CatalogService,
	runs handler.RunService,
	artifacts handler.ArtifactService,
	controllerEnrollmentTokens map[string]string,
	logger *slog.Logger,
) http.Handler {
	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	authentication := productidentity.Authentication{
		Principal: productidentity.Principal{
			ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1700"),
			Kind:        productidentity.KindUser,
			DisplayName: "Test administrator",
			Role:        productidentity.RoleAdmin,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Method:          productidentity.AuthenticationMethodAPIKey,
		CredentialID:    uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1701"),
		CredentialLabel: "test administrator key",
		Scopes:          []productidentity.Scope{productidentity.ScopeAdmin},
	}
	service := &recordingProductAuthenticationService{
		authentication: authentication,
	}
	httpHandler := newHTTPHandler(
		clusters,
		identities,
		repositories,
		catalogs,
		runs,
		artifacts,
		staticReadinessService{report: readiness.Report{
			Database:        readiness.DependencyAvailable,
			ArtifactStorage: readiness.DependencyAvailable,
		}},
		controllerEnrollmentTokens,
		service,
		logger,
	)

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/v1/") &&
			request.Header.Get("Authorization") == "" {
			request.Header.Set("Authorization", "Bearer test-api-key")
		}
		httpHandler.ServeHTTP(writer, request)
	})
}

type staticReadinessService struct {
	report readiness.Report
}

func (s staticReadinessService) Check(context.Context) readiness.Report {
	return s.report
}
