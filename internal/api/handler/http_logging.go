package handler

import (
	"log/slog"
	"net/http"
	"time"
)

func logHTTPRequests(next http.Handler, logger *slog.Logger, trustedProxyHops int) http.Handler {
	if logger == nil {
		return next
	}
	logger = logger.With("component", "http")

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		loggedWriter := &requestLogResponseWriter{ResponseWriter: writer}
		next.ServeHTTP(loggedWriter, request)

		status := loggedWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info(
			"http request",
			"client_ip", requestClientAddress(request, trustedProxyHops),
			"method", request.Method,
			"path", request.URL.Path,
			"status", status,
			"duration", time.Since(startedAt),
		)
	})
}

type requestLogResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *requestLogResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *requestLogResponseWriter) Write(contents []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	return w.ResponseWriter.Write(contents)
}

func (w *requestLogResponseWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *requestLogResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
