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
	"k8s.io/client-go/kubernetes"

	"github.com/slipramp/slipramp/internal/api"
)

const (
	pollRetryDelay             = 5 * time.Second
	jobObservationPeriod       = 2 * time.Second
	observationHeartbeatPeriod = 15 * time.Second
)

var (
	errControllerCredentialRejected = errors.New("controller credential was rejected")
	errRunObservationRejected       = errors.New("run observation was rejected")
)

type commandPoller interface {
	Poll(ctx context.Context) (*api.ControllerCommand, error)
}

type runObservationReporter interface {
	ReportObservation(
		ctx context.Context,
		runID uuid.UUID,
		observation api.RunObservation,
	) error
}

type activeObserver struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Controller struct {
	clusterName    string
	serverURL      string
	logger         *slog.Logger
	server         commandPoller
	reports        runObservationReporter
	jobs           runJobs
	retryDelay     time.Duration
	observeEvery   time.Duration
	heartbeatEvery time.Duration
	observerMutex  sync.Mutex
	observedRuns   map[uuid.UUID]*activeObserver
	observers      sync.WaitGroup
}

func New(
	cfg Config,
	logger *slog.Logger,
	kubeClient kubernetes.Interface,
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

	server, err := newServerClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Controller{
		clusterName:    cfg.ClusterName,
		serverURL:      cfg.ServerURL,
		logger:         logger.With("component", "controller"),
		server:         server,
		reports:        server,
		jobs:           newKubernetesRunJobs(cfg, kubeClient.BatchV1(), kubeClient.CoreV1()),
		retryDelay:     pollRetryDelay,
		observeEvery:   jobObservationPeriod,
		heartbeatEvery: observationHeartbeatPeriod,
		observedRuns:   make(map[uuid.UUID]*activeObserver),
	}, nil
}

func (c *Controller) Run(ctx context.Context) error {
	observeContext, stopObservers := context.WithCancel(ctx)
	defer func() {
		stopObservers()
		c.observers.Wait()
	}()

	c.logger.Info(
		"controller started",
		"cluster", c.clusterName,
		"server_url", c.serverURL,
	)
	defer c.logger.Info("controller stopped", "cluster", c.clusterName)

	if !c.recoverObservations(observeContext) {
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

func (c *Controller) recoverObservations(ctx context.Context) bool {
	for {
		handles, err := c.jobs.Recover(ctx)
		if err == nil {
			for _, handle := range handles {
				c.startObservation(ctx, handle)
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

func (c *Controller) handleCommand(
	ctx context.Context,
	command api.ControllerCommand,
) error {
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

		return c.handleCancel(ctx, *command.Cancel)
	default:
		c.logger.Warn("unsupported controller command", "type", command.Type)

		return nil
	}
}

func (c *Controller) handleAssignment(
	ctx context.Context,
	assignment api.RunAssignment,
) error {
	ensured, err := c.jobs.Ensure(ctx, assignment)
	if err != nil {
		c.logger.Warn(
			"run assignment failed",
			"run_id", assignment.ID,
			"error", err,
		)
		if !isPermanentMaterializationError(err) {
			return nil
		}

		reason := "MaterializationFailed"
		message := err.Error()
		outcome := api.RunObservationOutcomeFailed
		reportErr := c.reports.ReportObservation(
			ctx,
			assignment.ID,
			api.RunObservation{
				Phase:   api.RunObservationPhaseFinished,
				Outcome: &outcome,
				Reason:  &reason,
				Message: &message,
			},
		)
		if errors.Is(reportErr, errControllerCredentialRejected) {
			return reportErr
		}
		if reportErr != nil {
			c.logger.Warn(
				"report permanent materialization failure",
				"run_id", assignment.ID,
				"error", reportErr,
			)
		}

		return nil
	}

	c.logger.Info(
		"run assignment materialized",
		"run_id", assignment.ID,
		"job_created", ensured.created,
	)
	c.startObservation(ctx, ensured.handle)

	return nil
}

func (c *Controller) handleCancel(
	ctx context.Context,
	command api.CancelRunCommand,
) error {
	c.stopObservation(command.ID)

	jobUID, err := c.jobs.Cancel(ctx, command)
	if err != nil {
		c.logger.Warn(
			"cancel execution Job",
			"run_id", command.ID,
			"error", err,
		)

		return nil
	}

	outcome := api.RunObservationOutcomeCanceled
	reason := "CancelRequested"
	err = c.reports.ReportObservation(
		ctx,
		command.ID,
		api.RunObservation{
			Phase:   api.RunObservationPhaseFinished,
			Outcome: &outcome,
			JobUID:  jobUID,
			Reason:  &reason,
		},
	)
	if errors.Is(err, errControllerCredentialRejected) {
		return err
	}
	if errors.Is(err, errRunObservationRejected) {
		c.logger.Info(
			"Run cancel no longer applies",
			"run_id", command.ID,
		)

		return nil
	}
	if err != nil {
		c.logger.Warn(
			"report canceled Run",
			"run_id", command.ID,
			"error", err,
		)

		return nil
	}

	c.logger.Info("Run canceled", "run_id", command.ID)

	return nil
}

func (c *Controller) startObservation(ctx context.Context, handle jobHandle) {
	c.observerMutex.Lock()
	if _, observing := c.observedRuns[handle.runID]; observing {
		c.observerMutex.Unlock()

		return
	}
	observerContext, cancel := context.WithCancel(ctx)
	observer := &activeObserver{cancel: cancel, done: make(chan struct{})}
	c.observedRuns[handle.runID] = observer
	c.observers.Add(1)
	c.observerMutex.Unlock()

	go c.observeRun(observerContext, handle, observer)
}

func (c *Controller) stopObservation(runID uuid.UUID) {
	c.observerMutex.Lock()
	observer := c.observedRuns[runID]
	if observer != nil {
		observer.cancel()
	}
	c.observerMutex.Unlock()

	if observer != nil {
		<-observer.done
	}
}

func (c *Controller) observeRun(
	ctx context.Context,
	handle jobHandle,
	observer *activeObserver,
) {
	defer func() {
		c.observerMutex.Lock()
		if c.observedRuns[handle.runID] == observer {
			delete(c.observedRuns, handle.runID)
		}
		c.observerMutex.Unlock()
		close(observer.done)
		c.observers.Done()
	}()

	var reported *runObservation
	var reportedAt time.Time
	for {
		observation, err := c.jobs.Observe(ctx, handle)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.logger.Warn(
				"observe execution Job",
				"run_id", handle.runID,
				"error", err,
			)
		} else if shouldReport(observation, reported, reportedAt, c.heartbeatEvery) {
			err = c.reports.ReportObservation(
				ctx,
				handle.runID,
				observationRequest(observation),
			)
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, errRunObservationRejected) {
				c.logger.Warn(
					"Run observation rejected",
					"run_id", handle.runID,
					"error", err,
				)

				return
			}
			if err != nil {
				c.logger.Warn(
					"report Run observation",
					"run_id", handle.runID,
					"phase", observation.phase,
					"error", err,
				)
			} else {
				copy := observation
				reported = &copy
				reportedAt = time.Now()
				c.logger.Info(
					"Run observation reported",
					"run_id", handle.runID,
					"phase", observation.phase,
				)

				if observation.terminal() {
					return
				}
			}
		}

		if !waitForRetry(ctx, c.observeEvery) {
			return
		}
	}
}

func observationRequest(observation runObservation) api.RunObservation {
	return api.RunObservation{
		Phase:    observation.phase,
		Outcome:  observation.outcome,
		JobUID:   observation.jobUID,
		Reason:   observation.reason,
		Message:  observation.message,
		ExitCode: observation.exitCode,
	}
}

func shouldReport(
	observation runObservation,
	reported *runObservation,
	reportedAt time.Time,
	heartbeatEvery time.Duration,
) bool {
	if reported == nil || !sameObservation(observation, *reported) {
		return true
	}

	return time.Since(reportedAt) >= heartbeatEvery
}

func sameObservation(left, right runObservation) bool {
	return left.phase == right.phase &&
		equalPointers(left.outcome, right.outcome) &&
		equalPointers(left.jobUID, right.jobUID) &&
		equalPointers(left.reason, right.reason) &&
		equalPointers(left.message, right.message) &&
		equalPointers(left.exitCode, right.exitCode)
}

func equalPointers[T comparable](left, right *T) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
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
