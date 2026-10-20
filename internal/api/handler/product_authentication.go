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
	AuthenticateProductRequest(
		ctx context.Context,
		request *http.Request,
	) (productidentity.Authentication, error)
}

type productAuthenticationContextKey struct{}

var productOperationPermissions = map[string]productidentity.Permission{
	"ListRepositories":     productidentity.PermissionRead,
	"GetRepositoryCatalog": productidentity.PermissionRead,
	"ListTargets":          productidentity.PermissionRead,
	"ListClusters":         productidentity.PermissionRead,
	"ListRuns":             productidentity.PermissionRead,
	"GetRun":               productidentity.PermissionRead,
	"ListRunArtifacts":     productidentity.PermissionRead,
	"DownloadArtifact":     productidentity.PermissionRead,
	"StreamRunLog":         productidentity.PermissionRead,
	"CreateRun":            productidentity.PermissionRunsWrite,
	"CancelRun":            productidentity.PermissionRunsWrite,
	"CreateRepository":     productidentity.PermissionAdmin,
	"RemoveRepository":     productidentity.PermissionAdmin,
}

var productAuthenticationOperations = map[string]struct{}{
	"BeginOIDCLogin":      {},
	"CompleteOIDCLogin":   {},
	"GetCurrentPrincipal": {},
	"Logout":              {},
}

// ProductAuthentication establishes and authorizes product principals for
// /api/v1 operations.
func ProductAuthentication(
	authenticator ProductAuthenticator,
) api.StrictMiddlewareFunc {
	return func(
		next api.StrictHandlerFunc,
		operationID string,
	) api.StrictHandlerFunc {
		if _, isAuthenticationOperation := productAuthenticationOperations[operationID]; isAuthenticationOperation {
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
					return nil, fmt.Errorf(
						"product authorization permission is missing for operation %q",
						operationID,
					)
				}

				return next(ctx, writer, request, value)
			}
		}

		return func(
			ctx context.Context,
			writer http.ResponseWriter,
			request *http.Request,
			value any,
		) (any, error) {
			authentication, err := authenticator.AuthenticateProductRequest(ctx, request)
			if errors.Is(err, productidentity.ErrUnauthenticated) {
				writeProductUnauthorized(writer)

				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("authenticate product request: %w", err)
			}
			if err := authentication.Validate(); err != nil {
				return nil, fmt.Errorf("validate product authentication result: %w", err)
			}
			if authentication.Principal.DisabledAt != nil {
				writeProductUnauthorized(writer)

				return nil, nil
			}
			if !authentication.Authorizes(permission) {
				WriteProblem(
					writer,
					http.StatusForbidden,
					"The authenticated principal is not authorized for this operation.",
				)

				return nil, nil
			}

			requestContext := context.WithValue(
				ctx,
				productAuthenticationContextKey{},
				authentication,
			)

			return next(requestContext, writer, request, value)
		}
	}
}

func isProductPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

func currentProductAuthentication(
	ctx context.Context,
) (productidentity.Authentication, error) {
	authentication, ok := productAuthenticationFromContext(ctx)
	if !ok {
		return productidentity.Authentication{}, errors.New(
			"authenticated product identity is missing",
		)
	}

	return authentication, nil
}

func productAuthenticationFromContext(
	ctx context.Context,
) (productidentity.Authentication, bool) {
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
	WriteProblem(
		writer,
		http.StatusUnauthorized,
		"A valid product session or bearer credential is required.",
	)
}
