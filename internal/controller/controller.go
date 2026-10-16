// Package controller runs the Slipramp component deployed in each execution
// cluster.
package controller

import (
	"context"
	"errors"
	"log/slog"
)

type Controller struct {
	clusterName string
	serverURL   string
	bearerToken string
	logger      *slog.Logger
}

func New(cfg Config, logger *slog.Logger) (*Controller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		return nil, errors.New("logger is required")
	}

	return &Controller{
		clusterName: cfg.ClusterName,
		serverURL:   cfg.ServerURL,
		bearerToken: cfg.BearerToken,
		logger:      logger,
	}, nil
}

// Run owns the controller process lifecycle. Polling and Kubernetes
// reconciliation will be added behind this boundary with the generated API.
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

	<-ctx.Done()

	c.logger.Info(
		"controller stopped",
		"component",
		"controller",
		"cluster",
		c.clusterName,
	)

	return nil
}
