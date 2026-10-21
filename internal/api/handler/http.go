package handler

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/api"
)

type HTTPOptions struct {
	Middlewares []api.StrictMiddlewareFunc
	Logger      *slog.Logger
}

func NewHTTP(server api.StrictServerInterface, options HTTPOptions) http.Handler {
	return newHTTP(server, options)
}

func newHTTP(server api.StrictServerInterface, options HTTPOptions) *operationRouter {
	strictHandler := api.NewStrictHandlerWithOptions(
		server,
		options.Middlewares,
		api.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(writer http.ResponseWriter, _ *http.Request, err error) {
				WriteProblem(writer, http.StatusBadRequest, err.Error())
			},
			ResponseErrorHandlerFunc: func(writer http.ResponseWriter, request *http.Request, err error) {
				if request.Context().Err() != nil {
					return
				}

				if options.Logger != nil {
					options.Logger.Error(
						"handle API request",
						"method", request.Method,
						"path", request.URL.Path,
						"error", err,
					)
				}

				WriteProblem(
					writer,
					http.StatusInternalServerError,
					"The server could not complete the request.",
				)
			},
		},
	)

	router := newOperationRouter()
	api.HandlerWithOptions(
		strictHandler,
		api.StdHTTPServerOptions{
			BaseRouter: router,
			ErrorHandlerFunc: func(writer http.ResponseWriter, _ *http.Request, err error) {
				WriteProblem(writer, http.StatusBadRequest, err.Error())
			},
		},
	)
	router.registerMethodNotAllowed()
	router.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		WriteProblem(writer, http.StatusNotFound, "The requested resource does not exist.")
	})

	return router
}

type operationRouter struct {
	*http.ServeMux
	allowedMethods map[string][]string
}

func newOperationRouter() *operationRouter {
	return &operationRouter{
		ServeMux:       http.NewServeMux(),
		allowedMethods: map[string][]string{},
	}
}

func (r *operationRouter) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	r.ServeMux.HandleFunc(pattern, handler)

	method, path, found := strings.Cut(pattern, " ")
	if !found || method == "" || path == "" || slices.Contains(r.allowedMethods[path], method) {
		return
	}

	r.allowedMethods[path] = append(r.allowedMethods[path], method)
}

func (r *operationRouter) registerMethodNotAllowed() {
	for path, registeredMethods := range r.allowedMethods {
		allowedMethods := slices.Clone(registeredMethods)
		slices.Sort(allowedMethods)

		r.ServeMux.HandleFunc(path, func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Allow", strings.Join(allowedMethods, ", "))
			WriteProblem(
				writer,
				http.StatusMethodNotAllowed,
				"The HTTP method is not supported for this resource.",
			)
		})
	}
}
