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
	"github.com/slipramp/slipramp/internal/mcpserver"
	"github.com/slipramp/slipramp/internal/metricsserver"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/readiness"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/telemetry"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 1 * time.Minute
	maxHeaderBytes    = 1 << 20
)

type Server struct {
	address              string
	shutdownTimeout      time.Duration
	logger               *slog.Logger
	httpServer           *http.Server
	metricsServer        metricsRunner
	persistence          *persistence
	workers              []func(context.Context)
	promotionBranchDrift *promotion.BranchDriftService
	telemetry            *telemetry.Telemetry
}

type metricsRunner interface {
	Run(context.Context) error
}

func New(ctx context.Context, cfg Config, logger *slog.Logger, serviceVersion string) (*Server, error) {
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
	metrics, err := telemetry.New(ctx, telemetry.Config{
		ServiceVersion: serviceVersion,
		Source:         persistence.database,
		Logger:         logger,
	})
	if err != nil {
		persistence.Close()

		return nil, fmt.Errorf("initialize telemetry: %w", err)
	}
	closeInitialization := func() {
		_ = metrics.Shutdown(context.Background())
		persistence.Close()
	}

	clusters := cluster.NewService(persistence.database)
	identities := controlleridentity.NewService(persistence.database)
	catalogs := catalog.NewService(persistence.database, cfg.Placements)
	repositoryRefresher := repository.NewRefresher(
		persistence.database,
		repository.NewGitFetcher(cfg.RepositoryRefreshCredentials),
		cfg.Placements,
		cfg.RepositoryRefreshInterval,
		cfg.RepositoryFetchTimeout,
		logger,
		repository.WithRefreshObserver(metrics),
	)
	repositories := repository.NewService(
		persistence.database,
		cfg.RepositoryRefreshCredentials,
		repository.WithRefresher(repositoryRefresher),
		repository.WithConnectionTester(
			repository.NewGitConnectionTester(cfg.RepositoryRefreshCredentials),
			cfg.RepositoryFetchTimeout,
		),
	)
	notificationService, err := notification.NewService(cfg.Notifications, persistence.database)
	if err != nil {
		closeInitialization()

		return nil, fmt.Errorf("initialize notifications: %w", err)
	}
	runs := run.NewService(run.Dependencies{
		Creation:      persistence.database,
		History:       persistence.database,
		Scheduling:    persistence.database,
		Assignments:   persistence.database,
		Cancellations: persistence.database,
		Reviews:       persistence.database,
		Observations:  persistence.database,
		Repositories:  repositories,
		Catalogs:      catalogs,
		Placements:    cfg.Placements,
		Notifications: notificationService,
	})
	artifacts := artifact.NewService(artifact.Dependencies{
		Runs:           persistence.database,
		Artifacts:      persistence.database,
		LogChunks:      persistence.database,
		Contents:       persistence.artifactContents,
		ReportObserver: metrics,
		TokenKey:       []byte(cfg.ArtifactTokenSecret),
	})
	provider, err := productauth.DiscoverOIDCProvider(ctx, cfg.ProductAuthentication.OIDC)
	if err != nil {
		closeInitialization()

		return nil, err
	}

	productAuthenticationService, err := productauth.NewService(
		persistence.database,
		provider,
		cfg.ProductAuthentication,
	)
	if err != nil {
		closeInitialization()

		return nil, fmt.Errorf("initialize product authentication: %w", err)
	}
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
		gittrigger.WithDeliveryObserver(metrics),
	)
	artifactReconciler := artifact.NewReconciler(
		persistence.database,
		persistence.artifactContents,
		cfg.ArtifactRetentionPeriod,
		cfg.ArtifactReconcileInterval,
		cfg.ArtifactOrphanGracePeriod,
		logger,
		artifact.WithReconciliationObserver(metrics),
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
	promotionCandidateBranches := promotion.NewCandidateBranchService(
		cfg.KargoIntegrations,
		persistence.database,
		repository.NewGitCandidateBranchRemote(cfg.RepositoryRefreshCredentials),
		cfg.RepositoryRefreshInterval,
		cfg.RepositoryFetchTimeout,
		logger,
	)
	inboxService := inbox.NewService(inbox.Dependencies{
		State:            persistence.database,
		Acknowledgements: persistence.database,
		Promotions:       promotionReviews,
	})
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
		notification.WithDeliveryObserver(metrics),
	)
	workers := []func(context.Context){
		repositoryRefresher.Run,
		commitHistoryIndexer.Run,
		gitTriggerDispatcher.Run,
		artifactReconciler.Run,
		notificationDispatcher.Run,
		notificationRetention.Run,
		metrics.RunSnapshotWorker,
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
	if cfg.KargoIntegrations.CandidateBranchCleanupEnabled() {
		workers = append(workers, promotionCandidateBranches.Run)
	}
	workers = append(workers, promotionBranchDrift.Run)
	httpHandler := handler.NewHTTP(
		handler.Dependencies{
			ServiceVersion:           serviceVersion,
			Clusters:                 clusters,
			Placements:               placement.NewInventoryService(cfg.Placements, clusters, persistence.database),
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
			TrustedProxyHops:           cfg.TrustedProxyHops,
		},
	)
	mcpHandler, err := mcpserver.NewHTTP(
		mcpserver.Dependencies{
			Authentication: productAuthenticationService,
			Catalogs:       catalogs,
			Runs:           runs,
			Artifacts:      artifacts,
		},
		mcpserver.Options{
			ServiceVersion: serviceVersion,
			Logger:         logger,
		},
	)
	if err != nil {
		closeInitialization()

		return nil, fmt.Errorf("initialize MCP server: %w", err)
	}

	rootHandler := http.NewServeMux()
	rootHandler.Handle("/mcp", mcpHandler)
	rootHandler.Handle("/", httpHandler)

	server := newServer(cfg, logger, metrics.InstrumentHTTP(rootHandler))
	server.persistence = persistence
	server.workers = workers
	server.promotionBranchDrift = promotionBranchDrift
	server.telemetry = metrics
	server.metricsServer = metricsserver.New(
		cfg.MetricsAddress,
		metrics.Handler(),
		logger.With("component", "server_metrics"),
	)

	return server, nil
}

func newServer(cfg Config, logger *slog.Logger, handler http.Handler) *Server {
	return &Server{
		address:         cfg.Address,
		shutdownTimeout: cfg.ShutdownTimeout,
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
	if err := s.shutdownTelemetry(); err != nil {
		s.logger.Warn("shut down telemetry", "error", err)
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
	runContext, stop := context.WithCancelCause(ctx)
	defer stop(nil)

	var workers sync.WaitGroup
	for _, runWorker := range s.workers {
		workers.Go(func() {
			runWorker(runContext)
		})
	}
	if s.metricsServer != nil {
		workers.Go(func() {
			if err := s.metricsServer.Run(runContext); err != nil {
				stop(err)
			}
		})
	}

	serveErr := s.serve(runContext, listener)
	if serveErr != nil {
		stop(serveErr)
	} else {
		stop(nil)
	}
	workers.Wait()
	if serveErr == nil && ctx.Err() == nil {
		serveErr = context.Cause(runContext)
	}
	telemetryErr := s.shutdownTelemetry()

	return errors.Join(serveErr, telemetryErr)
}

func (s *Server) shutdownTelemetry() error {
	if s.telemetry == nil {
		return nil
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	if err := s.telemetry.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down telemetry: %w", err)
	}

	return nil
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
