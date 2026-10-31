package handler

import (
	"net/http"
	"strings"
)

const privateNoStoreCacheControl = "private, no-store"

func disablePrivateAPICaching(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if isPrivateAPIPath(request.URL.Path) {
			writer.Header().Set("Cache-Control", privateNoStoreCacheControl)
		}

		next.ServeHTTP(writer, request)
	})
}

func isPrivateAPIPath(path string) bool {
	return isPathWithin(path, "/api") ||
		isPathWithin(path, "/controller") ||
		isPathWithin(path, "/execution")
}

func isPathWithin(path, namespace string) bool {
	return path == namespace || strings.HasPrefix(path, namespace+"/")
}
