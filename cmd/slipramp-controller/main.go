package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/controller"
	"github.com/slipramp/slipramp/internal/telemetry"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.LoadController(os.LookupEnv)
	if err != nil {
		logger.Error("invalid controller configuration", "error", err)
		os.Exit(1)
	}

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Error("load in-cluster Kubernetes configuration", "error", err)
		os.Exit(1)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		logger.Error("initialize Kubernetes client", "error", err)
		os.Exit(1)
	}
	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		logger.Error("initialize dynamic Kubernetes client", "error", err)
		os.Exit(1)
	}
	metrics, err := telemetry.NewRuntime(context.Background(), telemetry.RuntimeConfig{
		ServiceName:    "slipramp-controller",
		ServiceVersion: version,
	})
	if err != nil {
		logger.Error("initialize controller telemetry", "error", err)
		os.Exit(1)
	}
	shutdownTelemetry := func() error {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return metrics.Shutdown(shutdownContext)
	}

	app, err := controller.New(
		cfg,
		logger,
		kubeClient,
		dynamicClient,
		controller.WithMetricsHandler(metrics.Handler()),
		controller.WithServerHTTPTransport(metrics.InstrumentHTTPTransport(nil)),
	)
	if err != nil {
		if shutdownErr := shutdownTelemetry(); shutdownErr != nil {
			logger.Warn("shut down controller telemetry", "error", shutdownErr)
		}
		logger.Error("initialize controller", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := errors.Join(app.Run(ctx), shutdownTelemetry()); err != nil {
		logger.Error("run controller", "error", err)
		os.Exit(1)
	}
}
