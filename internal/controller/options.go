package controller

import "net/http"

type controllerOptions struct {
	metricsHandler      http.Handler
	serverHTTPTransport http.RoundTripper
}

type Option func(*controllerOptions)

func WithServerHTTPTransport(transport http.RoundTripper) Option {
	return func(options *controllerOptions) {
		options.serverHTTPTransport = transport
	}
}

func WithMetricsHandler(handler http.Handler) Option {
	return func(options *controllerOptions) {
		options.metricsHandler = handler
	}
}
