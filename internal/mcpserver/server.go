// Package mcpserver exposes a Model Context Protocol boundary backed by
// Slipramp's existing catalog, Run, and product identity services.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

const (
	authenticationExtraKey = "slipramp.authentication"
	privateNoStore         = "private, no-store"
)

var errInternalToolCall = errors.New("Slipramp could not complete the request") //nolint:staticcheck // Product name.

type CredentialAuthenticator interface {
	AuthenticateAPIKey(context.Context, string) (productidentity.Authentication, error)
	AuthenticateOAuthAccessToken(
		context.Context,
		productauth.OAuthAccessToken,
	) (productidentity.Authentication, error)
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

type ArtifactService interface {
	List(context.Context, uuid.UUID) ([]artifact.Record, error)
	OpenArtifact(context.Context, uuid.UUID) (artifact.Content, error)
	StreamLog(context.Context, uuid.UUID, int64) (artifact.LogStream, error)
}

type Dependencies struct {
	Authentication CredentialAuthenticator
	Catalogs       CatalogService
	Runs           RunService
	Artifacts      ArtifactService
	OAuthTokens    OAuthAccessTokenVerifier
	ToolCalls      ToolCallObserver
}

type Options struct {
	ServiceVersion string
	Logger         *slog.Logger
	OAuth          OAuthConfig
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
	if dependencies.Artifacts == nil {
		return nil, errors.New("MCP artifact service is required")
	}
	if err := options.OAuth.Validate(); err != nil {
		return nil, err
	}
	if options.OAuth.Enabled() && dependencies.OAuthTokens == nil {
		return nil, errors.New("MCP OAuth access-token verifier is required")
	}
	if !options.OAuth.Enabled() && dependencies.OAuthTokens != nil {
		return nil, errors.New("MCP OAuth configuration is required for its access-token verifier")
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
		catalogs:  dependencies.Catalogs,
		runs:      dependencies.Runs,
		artifacts: dependencies.Artifacts,
		logger:    logger,
		observer:  dependencies.ToolCalls,
	}
	if tools.observer == nil {
		tools.observer = noopToolCallObserver{}
	}
	protocolLogger := sdkLogger(logger)
	server := mcp.NewServer(
		&mcp.Implementation{Name: "slipramp", Version: serviceVersion},
		&mcp.ServerOptions{
			Instructions: "Use list_targets to resolve target IDs. Before create_run, call explain_target_action and review " +
				"the command, placement, parameters, credentials, inputs, and diagnostics. Never call create_run or " +
				"cancel_run unless the user clearly requested that change. create_run requires a unique idempotencyKey; " +
				"reuse it only for the same request. Runs may change external infrastructure. Use read_run_log with " +
				"nextOffsetBytes to page durable logs, and list_run_artifacts before reading Artifact contents.",
			Logger: protocolLogger,
		},
	)
	tools.register(server)

	transport := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
			Logger:       protocolLogger,
		},
	)
	verifier := bearerTokenVerifier(dependencies.Authentication, dependencies.OAuthTokens, logger)
	bearerOptions := &auth.RequireBearerTokenOptions{AllowMissingExpiration: true}
	var metadataHandler http.Handler
	if options.OAuth.Enabled() {
		metadataURL := OAuthMetadataURL(options.OAuth.Resource)
		bearerOptions.ResourceMetadataURL = metadataURL
		metadataHandler = auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
			Resource:               options.OAuth.Resource,
			AuthorizationServers:   []string{options.OAuth.Issuer},
			ScopesSupported:        OAuthScopeNames(),
			BearerMethodsSupported: []string{"header"},
			ResourceName:           "Slipramp MCP",
		})
	}
	authenticated := auth.RequireBearerToken(
		verifier,
		bearerOptions,
	)(transport)

	return routeOAuthMetadata(
		options.OAuth,
		metadataHandler,
		disableCaching(addBearerChallenge(authenticated)),
	), nil
}

func bearerTokenVerifier(
	authenticator CredentialAuthenticator,
	oauthTokens OAuthAccessTokenVerifier,
	logger *slog.Logger,
) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if !strings.HasPrefix(token, "slipramp_key_") {
			return verifyOAuthAccessToken(ctx, authenticator, oauthTokens, token, logger)
		}

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

		return tokenInfo(authentication, time.Time{}), nil
	}
}

func verifyOAuthAccessToken(
	ctx context.Context,
	authenticator CredentialAuthenticator,
	verifier OAuthAccessTokenVerifier,
	rawToken string,
	logger *slog.Logger,
) (*auth.TokenInfo, error) {
	if verifier == nil {
		return nil, fmt.Errorf("%w: invalid Slipramp API key", auth.ErrInvalidToken)
	}

	verified, err := verifier.VerifyAccessToken(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid OAuth access token", auth.ErrInvalidToken)
	}
	authentication, err := authenticator.AuthenticateOAuthAccessToken(ctx, verified.Credential)
	if errors.Is(err, productauth.ErrEnrollmentDenied) || errors.Is(err, productidentity.ErrUnauthenticated) {
		return nil, fmt.Errorf("%w: OAuth identity is not authorized", auth.ErrInvalidToken)
	}
	if err != nil {
		logger.Error("authenticate MCP OAuth access token", "error", err)

		return nil, errors.New("Slipramp OAuth authentication failed") //nolint:staticcheck // Product name.
	}
	if err := authentication.Validate(); err != nil {
		logger.Error("validate MCP OAuth authentication", "error", err)

		return nil, errors.New("Slipramp OAuth authentication failed") //nolint:staticcheck // Product name.
	}
	if authentication.Method != productidentity.AuthenticationMethodOAuthToken {
		logger.Error(
			"validate MCP OAuth authentication",
			"error", "authentication method is not OAuth access token",
		)

		return nil, errors.New("Slipramp OAuth authentication failed") //nolint:staticcheck // Product name.
	}

	return tokenInfo(authentication, verified.Expiration), nil
}

func tokenInfo(authentication productidentity.Authentication, expiration time.Time) *auth.TokenInfo {
	scopes := make([]string, 0, len(authentication.Scopes))
	for _, scope := range authentication.Scopes {
		scopes = append(scopes, string(scope))
	}

	return &auth.TokenInfo{
		Scopes:     scopes,
		Expiration: expiration,
		UserID:     authentication.Principal.ID.String(),
		Extra: map[string]any{
			authenticationExtraKey: authentication,
		},
	}
}

func routeOAuthMetadata(cfg OAuthConfig, metadata http.Handler, mcp http.Handler) http.Handler {
	if !cfg.Enabled() {
		return mcp
	}

	metadataPath := OAuthMetadataURL(cfg.Resource)
	parsed, _ := url.Parse(metadataPath)

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == parsed.Path {
			metadata.ServeHTTP(writer, request)

			return
		}
		mcp.ServeHTTP(writer, request)
	})
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
			"the authenticated credential is not authorized for this operation",
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
