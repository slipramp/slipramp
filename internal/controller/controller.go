// Package controller runs the Slipramp component deployed in each execution
// cluster.
package controller

import (
	"context"
	"errors"
	"log/slog"
	"time"

	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"

	"github.com/slipramp/slipramp/internal/api"
)

const pollRetryDelay = 5 * time.Second

var errControllerCredentialRejected = errors.New("controller credential was rejected")

type assignmentPoller interface {
	Poll(ctx context.Context) (*api.RunAssignment, error)
}

type Controller struct {
	clusterName string
	serverURL   string
	logger      *slog.Logger
	server      assignmentPoller
	assignments assignmentMaterializer
	retryDelay  time.Duration
}

func New(
	cfg Config,
	logger *slog.Logger,
	jobs batchv1client.BatchV1Interface,
) (*Controller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if jobs == nil {
		return nil, errors.New("kubernetes Job client is required")
	}

	server, err := newServerClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Controller{
		clusterName: cfg.ClusterName,
		serverURL:   cfg.ServerURL,
		logger:      logger.With("component", "controller"),
		server:      server,
		assignments: newJobMaterializer(cfg, jobs),
		retryDelay:  pollRetryDelay,
	}, nil
}

func (c *Controller) Run(ctx context.Context) error {
	c.logger.Info(
		"controller started",
		"cluster", c.clusterName,
		"server_url", c.serverURL,
	)

	defer c.logger.Info(
		"controller stopped",
		"cluster", c.clusterName,
	)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		assignment, err := c.server.Poll(ctx)
		if err == nil && assignment == nil {
			continue
		}
		if err == nil {
			created, ensureErr := c.assignments.Ensure(ctx, *assignment)
			if ensureErr != nil {
				c.logger.Warn(
					"run assignment failed",
					"run_id", assignment.ID,
					"error", ensureErr,
				)

				if !waitForRetry(ctx, c.retryDelay) {
					return nil
				}

				continue
			}

			c.logger.Info(
				"run assignment materialized",
				"run_id", assignment.ID,
				"job_created", created,
			)

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
