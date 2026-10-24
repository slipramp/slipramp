package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/slipramp/slipramp/internal/api/handler"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/controllercommand"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/inbox"
	"github.com/slipramp/slipramp/internal/notification"
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
	address              string
	shutdownTimeout      time.Duration
	placements           placement.Set
	logger               *slog.Logger
	httpServer           *http.Server
	persistence          *persistence
	workers              []func(context.Context)
	promotionBranchDrift *promotion.BranchDriftService
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
	commitHistoryIndexer := repository.NewCommitHistoryIndexer(
		persistence.database,
		repository.NewGitCommitHistoryFetcher(cfg.RepositoryRefreshCredentials),
		repository.DefaultCommitHistoryIndexInterval,
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
	promotionCreations := promotion.NewCreationService(cfg.KargoIntegrations, persistence.database)
	promotionDispatches := promotion.NewCreationDispatchService(persistence.database)
	promotionReconciliations := promotion.NewCreationReconciliationService(persistence.database)
	controllerCommands := controllercommand.NewScheduler(runs, promotionDispatches, promotionReconciliations)
	promotionReviews := promotion.NewReviewService(persistence.database)
	promotionProjects := promotion.NewProjectService(cfg.KargoIntegrations, persistence.database)
	promotionPipelines := promotion.NewFreightPipelineService(cfg.KargoIntegrations, persistence.database)
	promotionBranchDrift := promotion.NewBranchDriftService(
		cfg.KargoIntegrations,
		persistence.database,
		repository.NewGitBranchScanner(cfg.RepositoryRefreshCredentials),
		cfg.RepositoryRefreshInterval,
		cfg.RepositoryFetchTimeout,
		logger,
	)
	inboxService := inbox.NewService(inbox.Dependencies{
		State:            persistence.database,
		Acknowledgements: persistence.database,
		Promotions:       promotionReviews,
	})
	notificationService, err := notification.NewService(cfg.Notifications, persistence.database)
	if err != nil {
		persistence.Close()

		return nil, fmt.Errorf("initialize notifications: %w", err)
	}
	notificationRetention := notification.NewRetentionReconciler(
		persistence.database,
		cfg.Notifications.RetentionPeriod,
		notification.DefaultRetentionInterval,
		logger,
	)
	notificationDispatcher := notification.NewDispatcher(
		cfg.Notifications,
		persistence.database,
		notification.DefaultDispatchInterval,
		logger,
	)
	workers := []func(context.Context){
		repositoryRefresher.Run,
		commitHistoryIndexer.Run,
		gitTriggerDispatcher.Run,
		artifactReconciler.Run,
		notificationDispatcher.Run,
		notificationRetention.Run,
	}
	if len(cfg.KargoIntegrations) > 0 {
		promotionPreparations := promotion.NewPreparationCoordinator(
			cfg.KargoIntegrations,
			repositories,
			catalogs,
			runs,
			artifacts,
			notificationService,
			persistence.database,
			promotion.DefaultPreparationInterval,
			logger,
		)
		promotionExpirations := promotion.NewExpirationCoordinator(
			persistence.database,
			promotion.DefaultExpirationInterval,
			logger,
		)
		workers = append(workers, promotionPreparations.Run, promotionExpirations.Run)
	}
	workers = append(workers, promotionBranchDrift.Run)
	httpHandler := handler.NewHTTP(
		handler.Dependencies{
			Clusters:                 clusters,
			ControllerIdentities:     identities,
			ControllerCommands:       controllerCommands,
			Repositories:             repositories,
			GitTriggerDeliveries:     persistence.database,
			Commits:                  persistence.database,
			Catalogs:                 catalogs,
			Inbox:                    inboxService,
			Notifications:            notificationService,
			Runs:                     runs,
			Artifacts:                artifacts,
			PromotionRequests:        promotionRequests,
			PromotionCreations:       promotionCreations,
			PromotionDispatches:      promotionDispatches,
			PromotionReconciliations: promotionReconciliations,
			PromotionStages:          promotionStages,
			PromotionReviews:         promotionReviews,
			PromotionProjects:        promotionProjects,
			PromotionPipelines:       promotionPipelines,
			PromotionBranchDrift:     promotionBranchDrift,
			KargoIntegrations:        cfg.KargoIntegrations,
			Readiness:                readinessService,
			ProductAuthentication:    productAuthenticationService,
		},
		handler.HTTPOptions{
			ControllerEnrollmentTokens: cfg.ControllerEnrollmentTokens,
			Logger:                     logger,
		},
	)
	server := newServer(cfg, logger, httpHandler)
	server.persistence = persistence
	server.workers = workers
	server.promotionBranchDrift = promotionBranchDrift

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
	if s.promotionBranchDrift != nil {
		if err := s.promotionBranchDrift.Close(); err != nil {
			s.logger.Warn("clean branch drift cache", "error", err)
		}
	}
	s.persistence.Close()
}

func (s *Server) Run(ctx context.Context) error {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.address, err)
	}

	return s.serveWithWorkers(ctx, listener)
}

func (s *Server) serveWithWorkers(ctx context.Context, listener net.Listener) error {
	runContext, stop := context.WithCancel(ctx)
	defer stop()

	var workers sync.WaitGroup
	for _, runWorker := range s.workers {
		workers.Go(func() {
			runWorker(runContext)
		})
	}

	serveErr := s.serve(runContext, listener)
	stop()
	workers.Wait()

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
