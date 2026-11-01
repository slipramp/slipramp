// Package mcpserver exposes a Model Context Protocol boundary backed by
// Slipramp's existing catalog, Run, and product identity services.
package mcpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Dependencies struct {
	Authentication    AuthenticationService
	Catalogs          CatalogService
	Runs              RunService
	Artifacts         ArtifactService
	OAuthAccessTokens OAuthAccessTokenVerifier
	ToolCallObserver  ToolCallObserver
}

type Options struct {
	ServiceVersion string
	Logger         *slog.Logger
	OAuth          OAuthConfig
}

func NewHTTP(dependencies Dependencies, options Options) (http.Handler, error) {
	if dependencies.Authentication == nil {
		return nil, errors.New("MCP authentication service is required")
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
	if options.OAuth.Enabled() && dependencies.OAuthAccessTokens == nil {
		return nil, errors.New("MCP OAuth access token verifier is required")
	}
	if !options.OAuth.Enabled() && dependencies.OAuthAccessTokens != nil {
		return nil, errors.New("MCP OAuth configuration is required for its access token verifier")
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

	tools := &toolHandler{
		catalogs:  dependencies.Catalogs,
		runs:      dependencies.Runs,
		artifacts: dependencies.Artifacts,
		logger:    logger,
		observer:  dependencies.ToolCallObserver,
	}
	if tools.observer == nil {
		tools.observer = noopToolCallObserver{}
	}
	protocolLogger := sdkLogger(logger)
	server := mcp.NewServer(
		&mcp.Implementation{Name: "slipramp", Version: serviceVersion},
		&mcp.ServerOptions{
			Instructions: "Use list_targets to resolve target IDs and list_runs to discover recent Run IDs. Before " +
				"create_run, call explain_target_action and review " +
				"the command, placement, parameters, credentials, inputs, and diagnostics. Never call create_run or " +
				"cancel_run unless the user clearly requested that change. create_run requires a unique idempotencyKey; " +
				"reuse it only for the same request. Bind every declared input to a compatible Artifact ID from " +
				"list_run_artifacts. Runs may change external infrastructure. Use read_run_log with " +
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
	verifier := bearerTokenVerifier(dependencies.Authentication, dependencies.OAuthAccessTokens, logger)
	bearerOptions := &auth.RequireBearerTokenOptions{AllowMissingExpiration: true}
	if options.OAuth.Enabled() {
		bearerOptions.ResourceMetadataURL = ProtectedResourceMetadataURL(options.OAuth.Resource)
	}
	authenticated := auth.RequireBearerToken(
		verifier,
		bearerOptions,
	)(transport)

	return disableCaching(addBearerChallenge(authenticated)), nil
}
