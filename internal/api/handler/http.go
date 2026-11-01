package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/api"
)

type HTTPOptions struct {
	ControllerEnrollmentTokens map[string]string
	Logger                     *slog.Logger
	TrustedProxyHops           int
}

func NewHTTP(dependencies Dependencies, options HTTPOptions) http.Handler {
	handler := limitOIDCLoginStarts(
		newHTTP(dependencies, options),
		newOIDCLoginStartLimiter(),
		options.TrustedProxyHops,
	)
	handler = logHTTPRequests(handler, options.Logger, options.TrustedProxyHops)

	return disablePrivateAPICaching(boundRequestReadTime(handler))
}

func newHTTP(dependencies Dependencies, options HTTPOptions) *operationRouter {
	controllerAuthenticator := newControllerAuthenticator(
		dependencies.ControllerIdentities,
		options.ControllerEnrollmentTokens,
	)
	productAuthenticator := newProductAuthenticator(dependencies.ProductAuthentication)
	middlewares := []api.StrictMiddlewareFunc{
		ControllerAuthentication(controllerAuthenticator),
		ProductAuthentication(productAuthenticator),
	}
	responseErrorHandler := func(writer http.ResponseWriter, request *http.Request, err error) {
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
	}
	strictHandler := api.NewStrictHandlerWithOptions(
		newHandler(dependencies),
		middlewares,
		api.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(writer http.ResponseWriter, _ *http.Request, err error) {
				if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
					writeRequestTooLarge(writer)
					return
				}

				WriteProblem(writer, http.StatusBadRequest, err.Error())
			},
			ResponseErrorHandlerFunc: responseErrorHandler,
		},
	)

	router := newOperationRouter()
	api.HandlerWithOptions(
		strictHandler,
		api.StdHTTPServerOptions{
			BaseRouter: router,
			Middlewares: []api.MiddlewareFunc{
				limitRequestBodies,
				authenticateBeforeDecoding(
					productAuthenticator,
					controllerAuthenticator,
					dependencies.Artifacts,
					func(writer http.ResponseWriter, request *http.Request, err error) {
						responseErrorHandler(writer, request, fmt.Errorf("authenticate request before decoding: %w", err))
					},
				),
			},
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
