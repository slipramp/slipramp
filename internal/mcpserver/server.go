// Package mcpserver exposes a Model Context Protocol boundary backed by
// Slipramp's existing catalog, Run, and product identity services.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

const (
	authenticationExtraKey = "slipramp.authentication"
	privateNoStore         = "private, no-store"
)

type APIKeyAuthenticator interface {
	AuthenticateAPIKey(context.Context, string) (productidentity.Authentication, error)
}

type CatalogService interface {
	ExplainAction(context.Context, uuid.UUID, string) (catalog.Explanation, error)
	ListTargets(context.Context, catalog.TargetListFilter) (catalog.TargetList, error)
}

type RunService interface {
	CreateRun(context.Context, run.CreateRequest) (run.Run, error)
	GetRun(context.Context, uuid.UUID) (run.Run, error)
	CancelRun(context.Context, run.CancelRequest) (run.Run, error)
}

type Dependencies struct {
	Authentication APIKeyAuthenticator
	Catalogs       CatalogService
	Runs           RunService
}

type Options struct {
	ServiceVersion string
	Logger         *slog.Logger
}

func NewHTTP(dependencies Dependencies, options Options) (http.Handler, error) {
	if dependencies.Authentication == nil {
		return nil, errors.New("MCP API key authenticator is required")
	}
	if dependencies.Catalogs == nil {
		return nil, errors.New("MCP catalog service is required")
	}
	if dependencies.Runs == nil {
		return nil, errors.New("MCP Run service is required")
	}

	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	logger = logger.With("component", "mcp")
	serviceVersion := options.ServiceVersion
	if serviceVersion == "" {
		serviceVersion = "dev"
	}

	tools := &toolServer{
		catalogs: dependencies.Catalogs,
		runs:     dependencies.Runs,
		logger:   logger,
	}
	server := mcp.NewServer(
		&mcp.Implementation{Name: "slipramp", Version: serviceVersion},
		&mcp.ServerOptions{
			Instructions: "Use list_targets to resolve target IDs. Before create_run, call explain_target_action and review " +
				"the command, placement, parameters, credentials, inputs, and diagnostics. Never call create_run or " +
				"cancel_run unless the user clearly requested that change. create_run requires a unique idempotencyKey; " +
				"reuse it only for the same request. Runs may change external infrastructure.",
			Logger: logger,
		},
	)
	tools.register(server)

	transport := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
			Logger:       logger,
		},
	)
	verifier := apiKeyVerifier(dependencies.Authentication, logger)
	authenticated := auth.RequireBearerToken(
		verifier,
		&auth.RequireBearerTokenOptions{AllowMissingExpiration: true},
	)(transport)

	return disableCaching(addBearerChallenge(authenticated)), nil
}

func apiKeyVerifier(authenticator APIKeyAuthenticator, logger *slog.Logger) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		authentication, err := authenticator.AuthenticateAPIKey(ctx, token)
		if errors.Is(err, productidentity.ErrUnauthenticated) {
			return nil, fmt.Errorf("%w: invalid Slipramp API key", auth.ErrInvalidToken)
		}
		if err != nil {
			logger.Error("authenticate MCP API key", "error", err)

			return nil, errors.New("Slipramp API key authentication failed") //nolint:staticcheck // Product name.
		}
		if err := authentication.Validate(); err != nil {
			logger.Error("validate MCP API key authentication", "error", err)

			return nil, errors.New("Slipramp API key authentication failed") //nolint:staticcheck // Product name.
		}
		if authentication.Method != productidentity.AuthenticationMethodAPIKey {
			logger.Error(
				"validate MCP API key authentication",
				"error", "authentication method is not API key",
			)

			return nil, errors.New("Slipramp API key authentication failed") //nolint:staticcheck // Product name.
		}

		scopes := make([]string, 0, len(authentication.Scopes))
		for _, scope := range authentication.Scopes {
			scopes = append(scopes, string(scope))
		}

		return &auth.TokenInfo{
			Scopes: scopes,
			UserID: authentication.Principal.ID.String(),
			Extra: map[string]any{
				authenticationExtraKey: authentication,
			},
		}, nil
	}
}

func currentAuthentication(ctx context.Context) (productidentity.Authentication, error) {
	token := auth.TokenInfoFromContext(ctx)
	if token == nil {
		return productidentity.Authentication{}, errors.New("authenticated Slipramp identity is missing")
	}
	authentication, ok := token.Extra[authenticationExtraKey].(productidentity.Authentication)
	if !ok {
		return productidentity.Authentication{}, errors.New("authenticated Slipramp identity is invalid")
	}

	return authentication, nil
}

func authorize(ctx context.Context, permission productidentity.Permission) (productidentity.Authentication, error) {
	authentication, err := currentAuthentication(ctx)
	if err != nil {
		return productidentity.Authentication{}, err
	}
	if !authentication.Authorizes(permission) {
		return productidentity.Authentication{}, errors.New(
			"the authenticated API key is not authorized for this operation",
		)
	}

	return authentication, nil
}

func disableCaching(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", privateNoStore)
		next.ServeHTTP(writer, request)
	})
}

func addBearerChallenge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(&bearerChallengeWriter{ResponseWriter: writer}, request)
	})
}

type bearerChallengeWriter struct {
	http.ResponseWriter
}

func (w *bearerChallengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="slipramp"`)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *bearerChallengeWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
