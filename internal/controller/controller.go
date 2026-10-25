// Package controller runs the Slipramp component deployed in each execution
// cluster.
package controller

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/slipramp/slipramp/internal/api"
)

const (
	pollRetryDelay             = 5 * time.Second
	jobObservationPeriod       = 2 * time.Second
	observationHeartbeatPeriod = 15 * time.Second
)

var (
	errControllerCredentialRejected           = errors.New("controller credential was rejected")
	errControllerEnrollmentCredentialRejected = errors.New("controller enrollment credential was rejected")
	errControllerEnrollmentConflict           = errors.New("controller enrollment conflicts with an existing identity")
)

type controllerEnroller interface {
	Enroll(ctx context.Context) error
}

type commandPoller interface {
	Poll(ctx context.Context) (*api.ControllerCommand, error)
}

type runObservationReporter interface {
	ReportObservation(ctx context.Context, runID uuid.UUID, observation api.RunObservation) error
}

type kargoPromotionDispatchReporter interface {
	ReportKargoPromotionDispatch(
		context.Context,
		uuid.UUID,
		api.KargoPromotionDispatchReport,
	) error
}

type kargoPromotionReconciliationReporter interface {
	ReportKargoPromotionReconciliation(
		context.Context,
		uuid.UUID,
		api.KargoPromotionReconciliationReport,
	) error
}

type runLogReporter interface {
	GetLogCursor(context.Context, uuid.UUID) (logCursor, error)
	AppendLogChunk(context.Context, uuid.UUID, int64, []byte) error
	FailLog(context.Context, uuid.UUID) error
	SealLog(context.Context, uuid.UUID, int64, string) error
}

type logCursor struct {
	nextOffsetBytes int64
	sealed          bool
	failed          bool
	sha256          string
}

type runWorker struct {
	cancellations chan api.CancelRunCommand
	done          chan struct{}
	blocked       bool
}

type Controller struct {
	clusterName                    string
	serverURL                      string
	logger                         *slog.Logger
	server                         commandPoller
	enrollment                     controllerEnroller
	reports                        runObservationReporter
	promotionReports               kargoPromotionDispatchReporter
	promotionReconciliationReports kargoPromotionReconciliationReporter
	logs                           runLogReporter
	jobs                           runJobs
	retryDelay                     time.Duration
	observeEvery                   time.Duration
	heartbeatEvery                 time.Duration
	now                            func() time.Time
	runMutex                       sync.Mutex
	activeRuns                     map[uuid.UUID]*runWorker
	runWorkers                     sync.WaitGroup
	stageObservers                 []*kargoStageObserver
	promotions                     kargoPromotionCreator
	promotionReconciler            kargoPromotionReconciler
	workers                        sync.WaitGroup
	stop                           context.CancelCauseFunc
}

func New(
	cfg Config,
	logger *slog.Logger,
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
) (*Controller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if kubeClient == nil {
		return nil, errors.New("kubernetes client is required")
	}
	if len(cfg.KargoIntegrations) > 0 && dynamicClient == nil {
		return nil, errors.New("dynamic Kubernetes client is required for Kargo observation")
	}

	server, err := newServerClient(cfg)
	if err != nil {
		return nil, err
	}

	controller := &Controller{
		clusterName:                    cfg.ClusterName,
		serverURL:                      cfg.ServerURL,
		logger:                         logger.With("component", "controller"),
		server:                         server,
		enrollment:                     server,
		reports:                        server,
		promotionReports:               server,
		promotionReconciliationReports: server,
		logs:                           server,
		jobs:                           newKubernetesRunJobs(cfg, kubeClient.BatchV1(), kubeClient.CoreV1()),
		retryDelay:                     pollRetryDelay,
		observeEvery:                   jobObservationPeriod,
		heartbeatEvery:                 observationHeartbeatPeriod,
		now:                            time.Now,
		activeRuns:                     make(map[uuid.UUID]*runWorker),
	}
	if len(cfg.KargoIntegrations) > 0 {
		controller.stageObservers = newKargoStageObservers(
			cfg.KargoIntegrations,
			dynamicClient,
			server,
			logger,
		)
		promotionManager := newDynamicKargoPromotionCreator(cfg.KargoIntegrations, dynamicClient)
		controller.promotions = promotionManager
		controller.promotionReconciler = promotionManager
	}

	return controller, nil
}

func (c *Controller) Run(ctx context.Context) (result error) {
	observeContext, stopObservers := context.WithCancelCause(ctx)
	c.stop = stopObservers
	defer func() {
		cause := context.Cause(observeContext)
		stopObservers(nil)
		c.workers.Wait()
		c.runWorkers.Wait()
		if result == nil && ctx.Err() == nil {
			result = cause
		}
	}()

	c.logger.Info(
		"controller started",
		"cluster", c.clusterName,
		"server_url", c.serverURL,
	)
	defer c.logger.Info("controller stopped", "cluster", c.clusterName)

	if err := c.enroll(observeContext); err != nil {
		return err
	}
	c.startStageObservers(observeContext)

	if !c.recoverRuns(observeContext) {
		return nil
	}

	for {
		if observeContext.Err() != nil {
			return nil
		}

		c.reapRunWorkers()
		command, err := c.server.Poll(observeContext)
		if err == nil && command == nil {
			continue
		}
		if err == nil {
			if err := c.handleCommand(observeContext, *command); err != nil {
				return err
			}

			if !waitForRetry(observeContext, c.retryDelay) {
				return nil
			}

			continue
		}

		if observeContext.Err() != nil {
			return nil
		}
		if permanentServerError(err) {
			return err
		}

		c.logger.Warn(
			"controller poll failed",
			"cluster", c.clusterName,
			"error", err,
		)
		if !waitForRetry(observeContext, c.retryDelay) {
			return nil
		}
	}
}

func (c *Controller) startStageObservers(ctx context.Context) {
	for _, observer := range c.stageObservers {
		c.workers.Go(func() {
			if err := observer.Run(ctx); err != nil && c.stop != nil {
				c.stop(err)
			}
		})
	}
}

func (c *Controller) enroll(ctx context.Context) error {
	for {
		err := c.enrollment.Enroll(ctx)
		if err == nil {
			c.logger.Info("controller enrolled", "cluster", c.clusterName)

			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errControllerEnrollmentCredentialRejected) ||
			errors.Is(err, errControllerEnrollmentConflict) || permanentServerError(err) {
			return err
		}

		c.logger.Warn(
			"controller enrollment failed",
			"cluster", c.clusterName,
			"error", err,
		)
		if !waitForRetry(ctx, c.retryDelay) {
			return nil
		}
	}
}

func (c *Controller) recoverRuns(ctx context.Context) bool {
	for {
		handles, err := c.jobs.Recover(ctx)
		if err == nil {
			for _, handle := range handles {
				c.startRunWorker(ctx, handle, nil, nil)
			}

			return true
		}
		if ctx.Err() != nil {
			return false
		}

		c.logger.Warn("recover execution Jobs", "error", err)
		if !waitForRetry(ctx, c.retryDelay) {
			return false
		}
	}
}

func (c *Controller) handleCommand(ctx context.Context, command api.ControllerCommand) error {
	payloads := 0
	for _, present := range [...]bool{
		command.Assignment != nil,
		command.Cancel != nil,
		command.Promotion != nil,
		command.PromotionReconciliation != nil,
	} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return invalidServerResponse("poll Controller command", "command must contain exactly one matching payload")
	}

	switch command.Type {
	case api.ControllerCommandTypeRunAssignment:
		if command.Assignment == nil {
			return invalidServerResponse("poll Controller command", "run assignment payload is missing")
		}

		return c.handleAssignment(ctx, *command.Assignment)
	case api.ControllerCommandTypeCancelRun:
		if command.Cancel == nil {
			return invalidServerResponse("poll Controller command", "cancellation payload is missing")
		}

		return c.handleCancellation(ctx, *command.Cancel)
	case api.ControllerCommandTypeCreateKargoPromotion:
		if command.Promotion == nil {
			return invalidServerResponse("poll Controller command", "Promotion creation payload is missing")
		}

		return c.handleKargoPromotionCreation(ctx, *command.Promotion)
	case api.ControllerCommandTypeReconcileKargoPromotion:
		if command.PromotionReconciliation == nil {
			return invalidServerResponse("poll Controller command", "Promotion reconciliation payload is missing")
		}

		return c.handleKargoPromotionReconciliation(ctx, *command.PromotionReconciliation)
	default:
		return invalidServerResponse("poll Controller command", "command type is unsupported")
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
