// Package controller runs the Slipramp component deployed in each execution
// cluster.
package controller

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const pollRetryDelay = 5 * time.Second

var errControllerCredentialRejected = errors.New("controller credential was rejected")

type serverPoller interface {
	Poll(ctx context.Context) error
}

type Controller struct {
	clusterName string
	serverURL   string
	logger      *slog.Logger
	server      serverPoller
	retryDelay  time.Duration
}

func New(cfg Config, logger *slog.Logger) (*Controller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		return nil, errors.New("logger is required")
	}

	server, err := newServerClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Controller{
		clusterName: cfg.ClusterName,
		serverURL:   cfg.ServerURL,
		logger:      logger,
		server:      server,
		retryDelay:  pollRetryDelay,
	}, nil
}

func (c *Controller) Run(ctx context.Context) error {
	c.logger.Info(
		"controller started",
		"component",
		"controller",
		"cluster",
		c.clusterName,
		"server_url",
		c.serverURL,
	)

	defer c.logger.Info(
		"controller stopped",
		"component",
		"controller",
		"cluster",
		c.clusterName,
	)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		err := c.server.Poll(ctx)
		if err == nil {
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
			"component",
			"controller",
			"cluster",
			c.clusterName,
			"error",
			err,
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
