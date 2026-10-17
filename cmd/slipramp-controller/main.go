package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/controller"
)

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

	app, err := controller.New(cfg, logger, kubeClient)
	if err != nil {
		logger.Error("initialize controller", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := app.Run(ctx); err != nil {
		logger.Error("run controller", "error", err)
		os.Exit(1)
	}
}
