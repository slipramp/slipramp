// Package metricsserver serves an operational Prometheus scrape endpoint.
package metricsserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAddress  = ":9091"
	shutdownTimeout = 5 * time.Second
	readTimeout     = 5 * time.Second
	idleTimeout     = time.Minute
	maxHeaderBytes  = 1 << 20
)

type Server struct {
	address string
	server  *http.Server
	logger  *slog.Logger
}

func New(address string, handler http.Handler, logger *slog.Logger) *Server {
	router := http.NewServeMux()
	router.Handle("GET /metrics", handler)

	return &Server{
		address: address,
		server: &http.Server{
			Handler:           router,
			ReadHeaderTimeout: readTimeout,
			IdleTimeout:       idleTimeout,
			MaxHeaderBytes:    maxHeaderBytes,
		},
		logger: logger,
	}
}

func ValidateAddress(address string) error {
	if strings.TrimSpace(address) == "" {
		return errors.New("metrics address must not be empty")
	}

	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("validate metrics address: %w", err)
	}

	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return errors.New("metrics address must contain a numeric port between 1 and 65535")
	}

	return nil
}

func (s *Server) Run(ctx context.Context) error {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen for metrics on %s: %w", s.address, err)
	}

	return s.serve(ctx, listener)
}

func (s *Server) serve(ctx context.Context, listener net.Listener) error {
	s.server.BaseContext = func(net.Listener) context.Context {
		return ctx
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- s.server.Serve(listener)
	}()

	s.logger.Info("metrics server listening", "address", listener.Addr().String())
	defer s.logger.Info("metrics server stopped")

	select {
	case serveErr := <-serveResult:
		return normalizeServeError(serveErr)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		shutdownErr := s.server.Shutdown(shutdownContext)
		if shutdownErr != nil {
			_ = s.server.Close()
			shutdownErr = fmt.Errorf("shut down metrics server: %w", shutdownErr)
		}

		return errors.Join(shutdownErr, normalizeServeError(<-serveResult))
	}
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve metrics: %w", err)
}
