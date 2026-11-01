package mcpserver

import "net/http"

const privateNoStore = "private, no-store"

func disableCaching(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", privateNoStore)
		next.ServeHTTP(writer, request)
	})
}

func addBearerChallenge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(&bearerChallengeWriter{ResponseWriter: writer}, request)
	})
}

type bearerChallengeWriter struct {
	http.ResponseWriter
}

func (w *bearerChallengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="slipramp"`)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *bearerChallengeWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
