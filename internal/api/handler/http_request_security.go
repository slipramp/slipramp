package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/controlleridentity"
)

const (
	publicRequestBodyLimit        = 64 << 10
	authenticatedRequestBodyLimit = 32 << 20
	publicRequestReadTimeout      = 15 * time.Second
	authenticatedReadTimeout      = 2 * time.Minute
)

const (
	appendRunLogChunkPattern = "POST /controller/v1/runs/{runId}/logs"
	uploadRunArtifactPattern = "PUT /execution/v1/runs/{runId}/artifacts/{artifactName}"
)

type requestAuthenticationErrorHandler func(http.ResponseWriter, *http.Request, error)

func limitRequestBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !requestHasBody(request) || request.Pattern == uploadRunArtifactPattern {
			next.ServeHTTP(writer, request)
			return
		}

		limit := int64(authenticatedRequestBodyLimit)
		switch {
		case request.Pattern == appendRunLogChunkPattern:
			limit = artifact.MaxLogChunkSizeBytes + 1
		case isPublicRequestPath(request.URL.Path):
			limit = publicRequestBodyLimit
		}

		if request.ContentLength > limit {
			writeRequestTooLarge(writer)
			return
		}

		request.Body = http.MaxBytesReader(writer, request.Body, limit)
		next.ServeHTTP(writer, request)
	})
}

func authenticateBeforeDecoding(
	productAuthenticator ProductAuthenticator,
	controllerAuthenticator ControllerAuthenticator,
	artifacts ArtifactService,
	errorHandler requestAuthenticationErrorHandler,
) api.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx := request.Context()

			switch {
			case isProtectedProductPath(request.URL.Path):
				authenticatedContext, ok, err := establishProductAuthentication(
					ctx,
					writer,
					request,
					productAuthenticator,
				)
				if err != nil {
					errorHandler(writer, request, err)
					return
				}
				if !ok {
					return
				}

				ctx = authenticatedContext
				extendRequestReadDeadline(writer, request, authenticatedReadTimeout)
			case request.URL.Path == "/controller/v1/enroll":
				token, ok := parseBearerToken(request.Header)
				if !ok || !controllerAuthenticator.AuthorizeAnyControllerEnrollment(token) {
					writeEnrollmentUnauthorized(writer)
					return
				}
			case isPathWithin(request.URL.Path, "/controller/v1"):
				authenticatedContext, ok, err := establishControllerAuthentication(
					ctx,
					writer,
					request,
					controllerAuthenticator,
				)
				if err != nil {
					errorHandler(writer, request, err)
					return
				}
				if !ok {
					return
				}

				ctx = authenticatedContext
				extendRequestReadDeadline(writer, request, authenticatedReadTimeout)
			case isPathWithin(request.URL.Path, "/execution/v1"):
				authenticatedContext, ok, err := establishExecutionAuthentication(
					ctx,
					writer,
					request,
					artifacts,
				)
				if err != nil {
					errorHandler(writer, request, err)
					return
				}
				if !ok {
					return
				}

				ctx = authenticatedContext
				if request.Pattern == uploadRunArtifactPattern && request.ContentLength >= 0 {
					clearRequestReadDeadline(writer, request)
				} else {
					extendRequestReadDeadline(writer, request, authenticatedReadTimeout)
				}
			}

			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func establishControllerAuthentication(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	authenticator ControllerAuthenticator,
) (context.Context, bool, error) {
	if _, ok := ctx.Value(controllerPrincipalContextKey{}).(controlleridentity.Principal); ok {
		return ctx, true, nil
	}

	token, ok := parseBearerToken(request.Header)
	if !ok {
		writeControllerUnauthorized(writer)
		return ctx, false, nil
	}

	principal, err := authenticator.AuthenticateController(ctx, token)
	if errors.Is(err, controlleridentity.ErrUnauthorized) {
		writeControllerUnauthorized(writer)
		return ctx, false, nil
	}
	if err != nil {
		return ctx, false, err
	}

	return context.WithValue(ctx, controllerPrincipalContextKey{}, principal), true, nil
}

func establishExecutionAuthentication(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	artifacts ArtifactService,
) (context.Context, bool, error) {
	if _, ok := ctx.Value(executionRequestContextKey{}).(executionRequestContext); ok {
		return ctx, true, nil
	}

	token, ok := parseBearerToken(request.Header)
	if !ok {
		writeExecutionUnauthorized(writer)
		return ctx, false, nil
	}

	runID, err := uuid.Parse(request.PathValue("runId"))
	if err != nil {
		return ctx, false, err
	}
	if artifacts == nil || !artifacts.AuthenticateRunToken(runID, token) {
		writeExecutionUnauthorized(writer)
		return ctx, false, nil
	}

	execution := executionRequestContext{
		BearerToken:   token,
		ContentLength: request.ContentLength,
	}

	return context.WithValue(ctx, executionRequestContextKey{}, execution), true, nil
}

func boundRequestReadTime(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !requestHasBody(request) {
			next.ServeHTTP(writer, request)
			return
		}

		setRequestReadDeadline(writer, time.Now().Add(publicRequestReadTimeout))
		defer clearRequestReadDeadline(writer, request)
		defer func() {
			_ = request.Body.Close()
		}()

		next.ServeHTTP(writer, request)
	})
}

func extendRequestReadDeadline(writer http.ResponseWriter, request *http.Request, timeout time.Duration) {
	if requestHasBody(request) {
		setRequestReadDeadline(writer, time.Now().Add(timeout))
	}
}

func clearRequestReadDeadline(writer http.ResponseWriter, request *http.Request) {
	if requestHasBody(request) {
		setRequestReadDeadline(writer, time.Time{})
	}
}

func setRequestReadDeadline(writer http.ResponseWriter, deadline time.Time) {
	_ = http.NewResponseController(writer).SetReadDeadline(deadline)
}

func requestHasBody(request *http.Request) bool {
	return request.Body != nil && request.Body != http.NoBody
}

func isPublicRequestPath(path string) bool {
	return path == "/controller/v1/enroll" ||
		!isPrivateAPIPath(path) ||
		isProductAuthenticationPath(path) ||
		isClientCompatibilityPath(path)
}

func isProtectedProductPath(path string) bool {
	return isProductPath(path) &&
		!isProductAuthenticationPath(path) &&
		!isClientCompatibilityPath(path)
}

func isClientCompatibilityPath(path string) bool {
	return path == "/api/v1/client-compatibility"
}

func isProductAuthenticationPath(path string) bool {
	switch path {
	case "/api/v1/auth/login", "/api/v1/auth/native/login", "/api/v1/auth/native/exchange",
		"/api/v1/auth/callback", "/api/v1/auth/session", "/api/v1/auth/logout":
		return true
	default:
		return false
	}
}

func writeRequestTooLarge(writer http.ResponseWriter) {
	WriteProblem(writer, http.StatusRequestEntityTooLarge, "The request body exceeds the accepted size limit.")
}

func writeEnrollmentUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", "Bearer")
	WriteProblem(writer, http.StatusUnauthorized, "The controller enrollment credential is missing or invalid.")
}

func writeControllerUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", "Bearer")
	WriteProblem(writer, http.StatusUnauthorized, "The controller credential is missing or invalid.")
}

func writeExecutionUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", "Bearer")
	WriteProblem(writer, http.StatusUnauthorized, "The Run artifact credential is invalid.")
}
