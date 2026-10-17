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
	"github.com/slipramp/slipramp/internal/placement"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 1 * time.Minute
	maxHeaderBytes    = 1 << 20
)

type Server struct {
	address         string
	shutdownTimeout time.Duration
	placements      placement.Set
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

	controllers := newControllerService(persistence.database)
	httpHandler := newHandlerWithLogger(
		controllers,
		cfg.ControllerTokens,
		logger,
	)
	server := newServer(cfg, logger, httpHandler)
	server.persistence = persistence

	return server, nil
}

func newServer(
	cfg Config,
	logger *slog.Logger,
	handler http.Handler,
) *Server {
	return &Server{
		address:         cfg.Address,
		shutdownTimeout: cfg.ShutdownTimeout,
		placements:      cfg.Placements,
		logger:          logger,
		httpServer: &http.Server{
			Handler:           handler,
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
	s.httpServer.BaseContext = func(net.Listener) context.Context {
		return lifecycleContext
	}

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

func newHandler(
	controllers controllerOperations,
	controllerTokens map[string]string,
) http.Handler {
	return newHandlerWithLogger(controllers, controllerTokens, nil)
}

func newHandlerWithLogger(
	controllers controllerOperations,
	controllerTokens map[string]string,
	logger *slog.Logger,
) http.Handler {
	authenticator := newControllerAuthenticator(controllerTokens)
	strictHandler := api.NewStrictHandlerWithOptions(
		handler.New(controllers, controllers),
		[]api.StrictMiddlewareFunc{
			handler.ControllerAuthentication(authenticator),
		},
		api.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(
				writer http.ResponseWriter,
				_ *http.Request,
				err error,
			) {
				handler.WriteProblem(
					writer,
					http.StatusBadRequest,
					err.Error(),
				)
			},
			ResponseErrorHandlerFunc: func(
				writer http.ResponseWriter,
				request *http.Request,
				err error,
			) {
				if request.Context().Err() != nil {
					return
				}

				if logger != nil {
					logger.Error(
						"handle API request",
						"method",
						request.Method,
						"path",
						request.URL.Path,
						"error",
						err,
					)
				}

				handler.WriteProblem(
					writer,
					http.StatusInternalServerError,
					"The server could not complete the request.",
				)
			},
		},
	)

	mux := http.NewServeMux()
	api.HandlerFromMux(strictHandler, mux)

	registerMethodNotAllowed(mux, "/healthz", http.MethodGet)
	registerMethodNotAllowed(mux, "/api/v1/clusters", http.MethodGet)
	registerMethodNotAllowed(
		mux,
		"/api/v1/controllers/{clusterName}/poll",
		http.MethodPost,
	)
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		handler.WriteProblem(
			writer,
			http.StatusNotFound,
			"The requested resource does not exist.",
		)
	})

	return mux
}

func registerMethodNotAllowed(
	mux *http.ServeMux,
	pattern string,
	allowedMethod string,
) {
	mux.HandleFunc(pattern, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Allow", allowedMethod)
		handler.WriteProblem(
			writer,
			http.StatusMethodNotAllowed,
			"The HTTP method is not supported for this resource.",
		)
	})
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve HTTP: %w", err)
}
