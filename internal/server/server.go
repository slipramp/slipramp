package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/slipramp/slipramp/internal/api/handler"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/readiness"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 1 * time.Minute
	maxHeaderBytes    = 1 << 20
)

type Server struct {
	address               string
	shutdownTimeout       time.Duration
	placements            placement.Set
	logger                *slog.Logger
	httpServer            *http.Server
	persistence           *persistence
	repositoryRefresher   *repository.Refresher
	gitTriggerDispatcher  *gittrigger.Dispatcher
	artifactReconciler    *artifact.Reconciler
	promotionPreparations *promotion.PreparationCoordinator
	promotionExpirations  *promotion.ExpirationCoordinator
}

func New(ctx context.Context, cfg Config, logger *slog.Logger) (*Server, error) {
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
	repositories := repository.NewService(persistence.database, cfg.RepositoryRefreshCredentials)
	runs := run.NewService(run.Dependencies{
		Creation:      persistence.database,
		History:       persistence.database,
		Assignments:   persistence.database,
		Cancellations: persistence.database,
		Observations:  persistence.database,
		Repositories:  repositories,
		Catalogs:      catalogs,
		Placements:    cfg.Placements,
	})
	artifacts := artifact.NewService(artifact.Dependencies{
		Runs:      persistence.database,
		Artifacts: persistence.database,
		LogChunks: persistence.database,
		Contents:  persistence.artifactContents,
		TokenKey:  []byte(cfg.ArtifactTokenSecret),
	})
	provider, err := productauth.DiscoverOIDCProvider(ctx, cfg.ProductAuthentication.OIDC)
	if err != nil {
		persistence.Close()

		return nil, err
	}

	productAuthenticationService, err := productauth.NewService(
		persistence.database,
		provider,
		cfg.ProductAuthentication,
	)
	if err != nil {
		persistence.Close()

		return nil, fmt.Errorf("initialize product authentication: %w", err)
	}
	repositoryRefresher := repository.NewRefresher(
		persistence.database,
		repository.NewGitFetcher(cfg.RepositoryRefreshCredentials),
		cfg.Placements,
		cfg.RepositoryRefreshInterval,
		cfg.RepositoryFetchTimeout,
		logger,
	)
	gitTriggerDispatcher := gittrigger.NewDispatcher(
		persistence.database,
		runs,
		gittrigger.DefaultDispatchInterval,
		logger,
	)
	artifactReconciler := artifact.NewReconciler(
		persistence.database,
		persistence.artifactContents,
		cfg.ArtifactRetentionPeriod,
		cfg.ArtifactReconcileInterval,
		cfg.ArtifactOrphanGracePeriod,
		logger,
	)
	readinessService := readiness.NewService(persistence.database, persistence.artifactContents)
	promotionStages := promotion.NewStageService(
		cfg.KargoIntegrations,
		repositories,
		persistence.database,
	)
	promotionRequests := promotion.NewRequestService(cfg.KargoIntegrations, persistence.database)
	promotionReviews := promotion.NewReviewService(persistence.database)
	promotionProjects := promotion.NewProjectService(cfg.KargoIntegrations, persistence.database)
	promotionPipelines := promotion.NewFreightPipelineService(cfg.KargoIntegrations, persistence.database)
	promotionBranchDrift := promotion.NewBranchDriftService(
		cfg.KargoIntegrations,
		persistence.database,
		repository.NewGitBranchScanner(cfg.RepositoryRefreshCredentials),
		cfg.RepositoryRefreshInterval,
		cfg.RepositoryFetchTimeout,
	)
	var promotionPreparations *promotion.PreparationCoordinator
	var promotionExpirations *promotion.ExpirationCoordinator
	if len(cfg.KargoIntegrations) > 0 {
		promotionPreparations = promotion.NewPreparationCoordinator(
			cfg.KargoIntegrations,
			repositories,
			catalogs,
			runs,
			artifacts,
			persistence.database,
			promotion.DefaultPreparationInterval,
			logger,
		)
		promotionExpirations = promotion.NewExpirationCoordinator(
			persistence.database,
			promotion.DefaultExpirationInterval,
			logger,
		)
	}
	httpHandler := handler.NewHTTP(
		handler.Dependencies{
			Clusters:              clusters,
			ControllerIdentities:  identities,
			Repositories:          repositories,
			Commits:               persistence.database,
			Catalogs:              catalogs,
			Runs:                  runs,
			Artifacts:             artifacts,
			PromotionRequests:     promotionRequests,
			PromotionStages:       promotionStages,
			PromotionReviews:      promotionReviews,
			PromotionProjects:     promotionProjects,
			PromotionPipelines:    promotionPipelines,
			PromotionBranchDrift:  promotionBranchDrift,
			KargoIntegrations:     cfg.KargoIntegrations,
			Readiness:             readinessService,
			ProductAuthentication: productAuthenticationService,
		},
		handler.HTTPOptions{
			ControllerEnrollmentTokens: cfg.ControllerEnrollmentTokens,
			Logger:                     logger,
		},
	)
	server := newServer(cfg, logger, httpHandler)
	server.persistence = persistence
	server.repositoryRefresher = repositoryRefresher
	server.gitTriggerDispatcher = gitTriggerDispatcher
	server.artifactReconciler = artifactReconciler
	server.promotionPreparations = promotionPreparations
	server.promotionExpirations = promotionExpirations

	return server, nil
}

func newServer(cfg Config, logger *slog.Logger, handler http.Handler) *Server {
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

	workerCount := 0
	if s.repositoryRefresher != nil {
		workerCount++
	}
	if s.gitTriggerDispatcher != nil {
		workerCount++
	}
	if s.artifactReconciler != nil {
		workerCount++
	}
	if s.promotionPreparations != nil {
		workerCount++
	}
	if s.promotionExpirations != nil {
		workerCount++
	}
	if workerCount == 0 {
		return s.serve(ctx, listener)
	}

	runContext, stop := context.WithCancel(ctx)
	defer stop()

	workersStopped := make(chan struct{}, workerCount)
	if s.repositoryRefresher != nil {
		go func() {
			s.repositoryRefresher.Run(runContext)
			workersStopped <- struct{}{}
		}()
	}
	if s.gitTriggerDispatcher != nil {
		go func() {
			s.gitTriggerDispatcher.Run(runContext)
			workersStopped <- struct{}{}
		}()
	}
	if s.artifactReconciler != nil {
		go func() {
			s.artifactReconciler.Run(runContext)
			workersStopped <- struct{}{}
		}()
	}
	if s.promotionPreparations != nil {
		go func() {
			s.promotionPreparations.Run(runContext)
			workersStopped <- struct{}{}
		}()
	}
	if s.promotionExpirations != nil {
		go func() {
			s.promotionExpirations.Run(runContext)
			workersStopped <- struct{}{}
		}()
	}

	serveErr := s.serve(runContext, listener)
	stop()
	for range workerCount {
		<-workersStopped
	}

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

	s.logger.Info("server listening", "address", listener.Addr().String())

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
	shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownContext); err != nil {
		if closeErr := s.httpServer.Close(); closeErr != nil {
			s.logger.Error("force server shutdown", "error", closeErr)
		}

		return fmt.Errorf("gracefully shut down server: %w", err)
	}

	return nil
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve HTTP: %w", err)
}
