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
	errRunLogRejected                         = errors.New("run log was rejected")
	errRunObservationRejected                 = errors.New("run observation was rejected")
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

type runLogReporter interface {
	GetLogCursor(context.Context, uuid.UUID) (logCursor, error)
	AppendLogChunk(context.Context, uuid.UUID, int64, []byte) error
	SealLog(context.Context, uuid.UUID, int64, string) error
}

type logCursor struct {
	nextOffsetBytes int64
	sealed          bool
	sha256          string
}

type runObserver struct {
	cancellations chan api.CancelRunCommand
	done          chan struct{}
}

type Controller struct {
	clusterName    string
	serverURL      string
	logger         *slog.Logger
	server         commandPoller
	enrollment     controllerEnroller
	reports        runObservationReporter
	logs           runLogReporter
	jobs           runJobs
	retryDelay     time.Duration
	observeEvery   time.Duration
	heartbeatEvery time.Duration
	now            func() time.Time
	observerMutex  sync.Mutex
	observedRuns   map[uuid.UUID]*runObserver
	observers      sync.WaitGroup
	stageObservers []*kargoStageObserver
	workers        sync.WaitGroup
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
		clusterName:    cfg.ClusterName,
		serverURL:      cfg.ServerURL,
		logger:         logger.With("component", "controller"),
		server:         server,
		enrollment:     server,
		reports:        server,
		logs:           server,
		jobs:           newKubernetesRunJobs(cfg, kubeClient.BatchV1(), kubeClient.CoreV1()),
		retryDelay:     pollRetryDelay,
		observeEvery:   jobObservationPeriod,
		heartbeatEvery: observationHeartbeatPeriod,
		now:            time.Now,
		observedRuns:   make(map[uuid.UUID]*runObserver),
	}
	if len(cfg.KargoIntegrations) > 0 {
		controller.stageObservers = newKargoStageObservers(
			cfg.KargoIntegrations,
			dynamicClient,
			server,
			logger,
		)
	}

	return controller, nil
}

func (c *Controller) Run(ctx context.Context) error {
	observeContext, stopObservers := context.WithCancel(ctx)
	defer func() {
		stopObservers()
		c.workers.Wait()
		c.observers.Wait()
	}()

	c.logger.Info(
		"controller started",
		"cluster", c.clusterName,
		"server_url", c.serverURL,
	)
	defer c.logger.Info("controller stopped", "cluster", c.clusterName)

	if err := c.enroll(ctx); err != nil {
		return err
	}
	c.startStageObservers(observeContext)

	if !c.recoverRuns(observeContext) {
		return nil
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		command, err := c.server.Poll(ctx)
		if err == nil && command == nil {
			continue
		}
		if err == nil {
			if err := c.handleCommand(observeContext, *command); err != nil {
				return err
			}

			if !waitForRetry(ctx, c.retryDelay) {
				return nil
			}

			continue
		}

		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errControllerCredentialRejected) {
			return err
		}

		c.logger.Warn(
			"controller poll failed",
			"cluster", c.clusterName,
			"error", err,
		)
		if !waitForRetry(ctx, c.retryDelay) {
			return nil
		}
	}
}

func (c *Controller) startStageObservers(ctx context.Context) {
	for _, observer := range c.stageObservers {
		c.workers.Go(func() { observer.Run(ctx) })
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
			errors.Is(err, errControllerEnrollmentConflict) {
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
				c.startRunObserver(ctx, handle, nil)
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
	switch command.Type {
	case api.ControllerCommandTypeRunAssignment:
		if command.Assignment == nil {
			c.logger.Warn("run assignment command has no assignment")

			return nil
		}

		return c.handleAssignment(ctx, *command.Assignment)
	case api.ControllerCommandTypeCancelRun:
		if command.Cancel == nil {
			c.logger.Warn("cancel-run command has no payload")

			return nil
		}

		return c.handleCancellation(ctx, *command.Cancel)
	default:
		c.logger.Warn("unsupported controller command", "type", command.Type)

		return nil
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
