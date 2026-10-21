package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/server"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadServer(os.LookupEnv)
	if err != nil {
		logger.Error("invalid server configuration", "error", err)

		return 1
	}

	app, err := server.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("initialize server", "error", err)

		return 1
	}
	defer app.Close()

	if err := app.Run(ctx); err != nil {
		logger.Error("run server", "error", err)

		return 1
	}

	return 0
}
