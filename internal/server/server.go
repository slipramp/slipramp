package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/api/handler"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 1 * time.Minute
	maxHeaderBytes    = 1 << 20
)

type Server struct {
	address         string
	shutdownTimeout time.Duration
	logger          *slog.Logger
	httpServer      *http.Server
	persistence     *persistence
}

func New(
	ctx context.Context,
	cfg Config,
	logger *slog.Logger,
) (*Server, error) {
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

	server := newServer(cfg, logger)
	server.persistence = persistence

	return server, nil
}

func newServer(
	cfg Config,
	logger *slog.Logger,
) *Server {
	return &Server{
		address:         cfg.Address,
		shutdownTimeout: cfg.ShutdownTimeout,
		logger:          logger,
		httpServer: &http.Server{
			Handler:           newHandler(),
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
			MaxHeaderBytes:    maxHeaderBytes,
		},
	}
}

func (s *Server) Close() {
	s.persistence.Close()
}

func (s *Server) Run(ctx context.Context) error {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.address, err)
	}

	return s.serve(ctx, listener)
}

func (s *Server) serve(ctx context.Context, listener net.Listener) error {
	lifecycleContext, stop := context.WithCancel(ctx)
	defer stop()

	shutdownResult := make(chan error, 1)

	go func() {
		<-lifecycleContext.Done()

		if ctx.Err() == nil {
			shutdownResult <- nil
			return
		}

		shutdownResult <- s.shutdown(ctx)
	}()

	s.logger.Info(
		"server listening",
		"component",
		"server",
		"address",
		listener.Addr().String(),
	)

	serveError := s.httpServer.Serve(listener)
	stop()

	if err := <-shutdownResult; err != nil {
		return err
	}

	if err := normalizeServeError(serveError); err != nil {
		return err
	}

	s.logger.Info("server stopped", "component", "server")

	return nil
}

func (s *Server) shutdown(ctx context.Context) error {
	shutdownContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		s.shutdownTimeout,
	)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownContext); err != nil {
		if closeErr := s.httpServer.Close(); closeErr != nil {
			s.logger.Error("force server shutdown", "error", closeErr)
		}

		return fmt.Errorf("gracefully shut down server: %w", err)
	}

	return nil
}

func newHandler() http.Handler {
	strictHandler := api.NewStrictHandler(handler.New(), nil)

	return api.Handler(strictHandler)
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve HTTP: %w", err)
}
