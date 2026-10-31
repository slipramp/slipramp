package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type ProductAuthenticator interface {
	AuthenticateProductRequest(ctx context.Context, request *http.Request) (productidentity.Authentication, error)
}

type productAuthenticationContextKey struct{}

const (
	csrfProtectionHeaderName  = "X-Slipramp-CSRF"
	csrfProtectionHeaderValue = "1"
)

var productOperationPermissions = map[string]productidentity.Permission{
	"ListAPIKeys":                    productidentity.PermissionRead,
	"CreateAPIKey":                   productidentity.PermissionRead,
	"RevokeAPIKey":                   productidentity.PermissionAPIKeyRevoke,
	"ListPrincipals":                 productidentity.PermissionAdmin,
	"GetPrincipal":                   productidentity.PermissionAdmin,
	"UpdatePrincipal":                productidentity.PermissionAdmin,
	"ListPrincipalAPIKeys":           productidentity.PermissionAdmin,
	"CreatePrincipalAPIKey":          productidentity.PermissionAdmin,
	"RevokePrincipalAPIKeys":         productidentity.PermissionAdmin,
	"RevokePrincipalSessions":        productidentity.PermissionAdmin,
	"CreateServiceAccount":           productidentity.PermissionAdmin,
	"ListRepositories":               productidentity.PermissionRead,
	"ListRepositoryRefreshAttempts":  productidentity.PermissionRead,
	"ListFailedGitTriggerDeliveries": productidentity.PermissionRead,
	"ListCommits":                    productidentity.PermissionRead,
	"GetCommit":                      productidentity.PermissionRead,
	"GetRepositoryCatalog":           productidentity.PermissionRead,
	"ExplainTargetAction":            productidentity.PermissionRead,
	"ListTargets":                    productidentity.PermissionRead,
	"GetTarget":                      productidentity.PermissionRead,
	"ListClusters":                   productidentity.PermissionRead,
	"ListPlacements":                 productidentity.PermissionRead,
	"GetPlacement":                   productidentity.PermissionRead,
	"GetInbox":                       productidentity.PermissionRead,
	"ListNotificationDeliveries":     productidentity.PermissionRead,
	"AcknowledgeInboxProblems":       productidentity.PermissionInboxWrite,
	"ListRuns":                       productidentity.PermissionRead,
	"GetRun":                         productidentity.PermissionRead,
	"ListRunArtifacts":               productidentity.PermissionRead,
	"GetArtifactStorageUsage":        productidentity.PermissionRead,
	"DownloadArtifact":               productidentity.PermissionRead,
	"GetArtifactReport":              productidentity.PermissionRead,
	"StreamRunLog":                   productidentity.PermissionRead,
	"CreateRun":                      productidentity.PermissionRunsWrite,
	"RerunRun":                       productidentity.PermissionRunsWrite,
	"CancelRun":                      productidentity.PermissionRunsWrite,
	"DecideRunReview":                productidentity.PermissionRunsReview,
	"CreateRepository":               productidentity.PermissionAdmin,
	"TestRepositoryConnection":       productidentity.PermissionAdmin,
	"RefreshRepository":              productidentity.PermissionAdmin,
	"UpdateRepository":               productidentity.PermissionAdmin,
	"RemoveRepository":               productidentity.PermissionAdmin,
	"SubmitKargoPromotion":           productidentity.PermissionPromotionsSubmit,
	"CreatePromotion":                productidentity.PermissionPromotionsWrite,
	"ListPromotions":                 productidentity.PermissionRead,
	"GetPromotion":                   productidentity.PermissionRead,
	"DecidePromotion":                productidentity.PermissionPromotionsWrite,
	"ListPromotionProjects":          productidentity.PermissionRead,
	"ListPromotionStages":            productidentity.PermissionRead,
	"ListPromotionPipelines":         productidentity.PermissionRead,
	"ListPromotionBranchDrift":       productidentity.PermissionRead,
	"UpdatePromotionStagePolicy":     productidentity.PermissionPromotionsWrite,
}

var productAuthenticationExemptOperations = map[string]struct{}{
	"BeginNativeOIDCLogin":    {},
	"BeginOIDCLogin":          {},
	"CompleteOIDCLogin":       {},
	"ExchangeNativeOIDCLogin": {},
	"GetClientCompatibility":  {},
	"GetCurrentPrincipal":     {},
	"Logout":                  {},
}

// ProductAuthentication establishes and authorizes product principals for
// /api/v1 operations.
func ProductAuthentication(authenticator ProductAuthenticator) api.StrictMiddlewareFunc {
	return func(next api.StrictHandlerFunc, operationID string) api.StrictHandlerFunc {
		if _, isExemptOperation := productAuthenticationExemptOperations[operationID]; isExemptOperation {
			return next
		}

		permission, isProductOperation := productOperationPermissions[operationID]
		if !isProductOperation {
			return func(
				ctx context.Context,
				writer http.ResponseWriter,
				request *http.Request,
				value any,
			) (any, error) {
				if isProductPath(request.URL.Path) {
					return nil, fmt.Errorf("product authorization permission is missing for operation %q", operationID)
				}

				return next(ctx, writer, request, value)
			}
		}

		return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
			authenticatedContext, ok, err := establishProductAuthentication(
				ctx,
				writer,
				request,
				authenticator,
			)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil
			}

			authentication, _ := productAuthenticationFromContext(authenticatedContext)
			if !authentication.Authorizes(permission) {
				WriteProblem(
					writer,
					http.StatusForbidden,
					"The authenticated principal is not authorized for this operation.",
				)

				return nil, nil
			}
			if operationID == "SubmitKargoPromotion" &&
				(authentication.Method != productidentity.AuthenticationMethodAPIKey ||
					authentication.Principal.Kind != productidentity.KindServiceAccount) {
				WriteProblem(
					writer,
					http.StatusForbidden,
					"Kargo Promotions must be submitted by a service-account API key.",
				)

				return nil, nil
			}

			return next(authenticatedContext, writer, request, value)
		}
	}
}

func establishProductAuthentication(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	authenticator ProductAuthenticator,
) (context.Context, bool, error) {
	authentication, err := authenticator.AuthenticateProductRequest(ctx, request)
	if errors.Is(err, productidentity.ErrUnauthenticated) {
		writeProductUnauthorized(writer)
		return ctx, false, nil
	}
	if err != nil {
		return ctx, false, fmt.Errorf("authenticate product request: %w", err)
	}
	if err := authentication.Validate(); err != nil {
		return ctx, false, fmt.Errorf("validate product authentication result: %w", err)
	}
	if authentication.Principal.DisabledAt != nil {
		writeProductUnauthorized(writer)
		return ctx, false, nil
	}
	if authentication.Method == productidentity.AuthenticationMethodWebSession &&
		request.Header.Get("Authorization") == "" &&
		requiresCSRFProtection(request.Method) &&
		request.Header.Get(csrfProtectionHeaderName) != csrfProtectionHeaderValue {
		WriteProblem(writer, http.StatusForbidden, "The CSRF protection header is invalid.")
		return ctx, false, nil
	}

	return context.WithValue(ctx, productAuthenticationContextKey{}, authentication), true, nil
}

func requiresCSRFProtection(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func isProductPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

func currentProductAuthentication(ctx context.Context) (productidentity.Authentication, error) {
	authentication, ok := productAuthenticationFromContext(ctx)
	if !ok {
		return productidentity.Authentication{}, errors.New("authenticated product identity is missing")
	}

	return authentication, nil
}

func productAuthenticationFromContext(ctx context.Context) (productidentity.Authentication, bool) {
	authentication, ok := ctx.Value(productAuthenticationContextKey{}).(productidentity.Authentication)

	return authentication, ok
}

func currentProductPrincipal(ctx context.Context) *productidentity.Principal {
	authentication, ok := productAuthenticationFromContext(ctx)
	if !ok {
		return nil
	}

	return &authentication.Principal
}

func writeProductUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", `Bearer realm="slipramp"`)
	WriteProblem(writer, http.StatusUnauthorized, "A valid product session or bearer credential is required.")
}
