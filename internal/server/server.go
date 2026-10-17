package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/api/handler"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 1 * time.Minute
	maxHeaderBytes    = 1 << 20
)

type Server struct {
	address             string
	shutdownTimeout     time.Duration
	placements          placement.Set
	logger              *slog.Logger
	httpServer          *http.Server
	persistence         *persistence
	repositoryRefresher *repository.Refresher
}

func New(
	ctx context.Context,
	cfg Config,
	logger *slog.Logger,
) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		return nil, errors.New("logger is required")
	}

	persistence, err := openPersistence(ctx, cfg)
	if err != nil {
		return nil, err
	}

	clusters := cluster.NewService(persistence.database)
	identities := controlleridentity.NewService(persistence.database)
	catalogs := catalog.NewService(persistence.database)
	repositories := repository.NewService(
		persistence.database,
		cfg.RepositoryRefreshCredentials,
	)
	runs := run.NewService(
		persistence.database,
		repositories,
		catalogs,
		cfg.Placements,
	)
	artifacts := artifact.NewService(
		persistence.database,
		persistence.artifactContents,
		[]byte(cfg.ArtifactTokenSecret),
	)
	repositoryRefresher := repository.NewRefresher(
		persistence.database,
		repository.NewGitFetcher(cfg.RepositoryRefreshCredentials),
		cfg.Placements,
		cfg.RepositoryRefreshInterval,
		cfg.RepositoryFetchTimeout,
		logger,
	)
	httpHandler := newHandlerWithLogger(
		clusters,
		identities,
		repositories,
		catalogs,
		runs,
		artifacts,
		cfg.ControllerEnrollmentTokens,
		logger,
	)
	server := newServer(cfg, logger, httpHandler)
	server.persistence = persistence
	server.repositoryRefresher = repositoryRefresher

	return server, nil
}

func newServer(
	cfg Config,
	logger *slog.Logger,
	handler http.Handler,
) *Server {
	return &Server{
		address:         cfg.Address,
		shutdownTimeout: cfg.ShutdownTimeout,
		placements:      cfg.Placements,
		logger:          logger.With("component", "server"),
		httpServer: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
			MaxHeaderBytes:    maxHeaderBytes,
		},
	}
}

func (s *Server) Close() {
	s.persistence.Close()
}

func (s *Server) Run(ctx context.Context) error {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.address, err)
	}

	if s.repositoryRefresher == nil {
		return s.serve(ctx, listener)
	}

	runContext, stop := context.WithCancel(ctx)
	defer stop()

	refreshStopped := make(chan struct{})
	go func() {
		defer close(refreshStopped)
		s.repositoryRefresher.Run(runContext)
	}()

	serveErr := s.serve(runContext, listener)
	stop()
	<-refreshStopped

	return serveErr
}

func (s *Server) serve(ctx context.Context, listener net.Listener) error {
	lifecycleContext, stop := context.WithCancel(ctx)
	defer stop()
	s.httpServer.BaseContext = func(net.Listener) context.Context {
		return lifecycleContext
	}

	shutdownResult := make(chan error, 1)

	go func() {
		<-lifecycleContext.Done()

		if ctx.Err() == nil {
			shutdownResult <- nil
			return
		}

		shutdownResult <- s.shutdown(ctx)
	}()

	s.logger.Info(
		"server listening",
		"address", listener.Addr().String(),
	)

	serveError := s.httpServer.Serve(listener)
	stop()

	if err := <-shutdownResult; err != nil {
		return err
	}

	if err := normalizeServeError(serveError); err != nil {
		return err
	}

	s.logger.Info("server stopped")

	return nil
}

func (s *Server) shutdown(ctx context.Context) error {
	shutdownContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		s.shutdownTimeout,
	)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownContext); err != nil {
		if closeErr := s.httpServer.Close(); closeErr != nil {
			s.logger.Error("force server shutdown", "error", closeErr)
		}

		return fmt.Errorf("gracefully shut down server: %w", err)
	}

	return nil
}

func newHandler(
	clusters handler.ClusterService,
	identities handler.ControllerIdentityService,
	repositories handler.RepositoryService,
	catalogs handler.CatalogService,
	runs handler.RunService,
	artifacts handler.ArtifactService,
	controllerEnrollmentTokens map[string]string,
) http.Handler {
	return newHandlerWithLogger(
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

func newHandlerWithLogger(
	clusters handler.ClusterService,
	identities handler.ControllerIdentityService,
	repositories handler.RepositoryService,
	catalogs handler.CatalogService,
	runs handler.RunService,
	artifacts handler.ArtifactService,
	controllerEnrollmentTokens map[string]string,
	logger *slog.Logger,
) http.Handler {
	authenticator := newControllerAuthenticator(
		identities,
		controllerEnrollmentTokens,
	)
	strictHandler := api.NewStrictHandlerWithOptions(
		handler.New(clusters, identities, repositories, catalogs, runs, artifacts),
		[]api.StrictMiddlewareFunc{
			handler.ControllerAuthentication(authenticator),
		},
		api.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(
				writer http.ResponseWriter,
				_ *http.Request,
				err error,
			) {
				handler.WriteProblem(
					writer,
					http.StatusBadRequest,
					err.Error(),
				)
			},
			ResponseErrorHandlerFunc: func(
				writer http.ResponseWriter,
				request *http.Request,
				err error,
			) {
				if request.Context().Err() != nil {
					return
				}

				if logger != nil {
					logger.Error(
						"handle API request",
						"method", request.Method,
						"path", request.URL.Path,
						"error", err,
					)
				}

				handler.WriteProblem(
					writer,
					http.StatusInternalServerError,
					"The server could not complete the request.",
				)
			},
		},
	)

	mux := http.NewServeMux()
	api.HandlerWithOptions(
		strictHandler,
		api.StdHTTPServerOptions{
			BaseRouter: mux,
			ErrorHandlerFunc: func(
				writer http.ResponseWriter,
				_ *http.Request,
				err error,
			) {
				handler.WriteProblem(
					writer,
					http.StatusBadRequest,
					err.Error(),
				)
			},
		},
	)

	registerMethodNotAllowed(mux, "/healthz", http.MethodGet)
	registerMethodNotAllowed(
		mux,
		"/api/v1/repositories",
		http.MethodGet,
		http.MethodPost,
	)
	registerMethodNotAllowed(
		mux,
		"/api/v1/repositories/{repositoryId}",
		http.MethodDelete,
	)
	registerMethodNotAllowed(
		mux,
		"/api/v1/repositories/{repositoryId}/catalog",
		http.MethodGet,
	)
	registerMethodNotAllowed(mux, "/api/v1/clusters", http.MethodGet)
	registerMethodNotAllowed(
		mux,
		"/controller/v1/enroll",
		http.MethodPost,
	)
	registerMethodNotAllowed(
		mux,
		"/controller/v1/poll",
		http.MethodPost,
	)
	registerMethodNotAllowed(
		mux,
		"/controller/v1/runs/{runId}/log-chunks",
		http.MethodPost,
	)
	registerMethodNotAllowed(
		mux,
		"/controller/v1/runs/{runId}/observations",
		http.MethodPost,
	)
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		handler.WriteProblem(
			writer,
			http.StatusNotFound,
			"The requested resource does not exist.",
		)
	})

	return mux
}

func registerMethodNotAllowed(
	mux *http.ServeMux,
	pattern string,
	allowedMethods ...string,
) {
	mux.HandleFunc(pattern, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Allow", strings.Join(allowedMethods, ", "))
		handler.WriteProblem(
			writer,
			http.StatusMethodNotAllowed,
			"The HTTP method is not supported for this resource.",
		)
	})
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve HTTP: %w", err)
}
