package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controlleridentity"
)

type ControllerAuthenticator interface {
	AuthenticateController(ctx context.Context, bearerToken string) (controlleridentity.Principal, error)
	AuthorizeControllerEnrollment(clusterName, bearerToken string) bool
}

type controllerPrincipalContextKey struct{}
type executionRequestContextKey struct{}

type executionRequestContext struct {
	BearerToken   string
	ContentLength int64
}

func ControllerAuthentication(authenticator ControllerAuthenticator) api.StrictMiddlewareFunc {
	return func(next api.StrictHandlerFunc, _ string) api.StrictHandlerFunc {
		return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
			switch controllerRequest := value.(type) {
			case api.EnrollControllerRequestObject:
				if controllerRequest.Body == nil {
					return next(ctx, writer, request, value)
				}

				token, ok := parseBearerToken(request.Header)
				if ok && authenticator.AuthorizeControllerEnrollment(controllerRequest.Body.ClusterName, token) {
					return next(ctx, writer, request, value)
				}

				return enrollmentUnauthorized(writer), nil
			case api.PollForControllerCommandRequestObject:
				return authenticateControllerRequest(
					ctx,
					writer,
					request,
					value,
					authenticator,
					next,
					func() any {
						return api.PollForControllerCommand401ApplicationProblemPlusJSONResponse(
							controllerUnauthorizedProblem(),
						)
					},
				)
			case api.AppendRunLogChunkRequestObject:
				return authenticateControllerRequest(
					ctx,
					writer,
					request,
					value,
					authenticator,
					next,
					func() any {
						return api.AppendRunLogChunk401ApplicationProblemPlusJSONResponse(
							controllerUnauthorizedProblem(),
						)
					},
				)
			case api.GetRunLogCursorRequestObject:
				return authenticateControllerRequest(
					ctx, writer, request, value, authenticator, next,
					func() any {
						return api.GetRunLogCursor401ApplicationProblemPlusJSONResponse(
							controllerUnauthorizedProblem(),
						)
					},
				)
			case api.SealRunLogRequestObject:
				return authenticateControllerRequest(
					ctx, writer, request, value, authenticator, next,
					func() any {
						return api.SealRunLog401ApplicationProblemPlusJSONResponse(controllerUnauthorizedProblem())
					},
				)
			case api.ReportRunObservationRequestObject:
				return authenticateControllerRequest(
					ctx,
					writer,
					request,
					value,
					authenticator,
					next,
					func() any {
						return api.ReportRunObservation401ApplicationProblemPlusJSONResponse(
							controllerUnauthorizedProblem(),
						)
					},
				)
			case api.ReportKargoPromotionDispatchRequestObject:
				return authenticateControllerRequest(
					ctx,
					writer,
					request,
					value,
					authenticator,
					next,
					func() any {
						return api.ReportKargoPromotionDispatch401ApplicationProblemPlusJSONResponse(
							controllerUnauthorizedProblem(),
						)
					},
				)
			case api.ReplaceKargoStageSnapshotRequestObject:
				return authenticateControllerRequest(
					ctx,
					writer,
					request,
					value,
					authenticator,
					next,
					func() any {
						return api.ReplaceKargoStageSnapshot401ApplicationProblemPlusJSONResponse{
							UnauthorizedApplicationProblemPlusJSONResponse: api.UnauthorizedApplicationProblemPlusJSONResponse(
								controllerUnauthorizedProblem(),
							),
						}
					},
				)
			case api.UploadRunArtifactRequestObject:
				return attachExecutionRequest(ctx, writer, request, value, next, func() any {
					return api.UploadRunArtifact401ApplicationProblemPlusJSONResponse(
						NewProblem(http.StatusUnauthorized, "The Run artifact credential is invalid."),
					)
				})
			case api.DownloadRunInputRequestObject:
				return attachExecutionRequest(ctx, writer, request, value, next, func() any {
					return api.DownloadRunInput401ApplicationProblemPlusJSONResponse(
						NewProblem(http.StatusUnauthorized, "The Run artifact credential is invalid."),
					)
				})
			default:
				return next(ctx, writer, request, value)
			}
		}
	}
}

func attachExecutionRequest(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	value any,
	next api.StrictHandlerFunc,
	unauthorizedResponse func() any,
) (any, error) {
	token, ok := parseBearerToken(request.Header)
	if !ok {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		return unauthorizedResponse(), nil
	}

	requestContext := context.WithValue(
		ctx,
		executionRequestContextKey{},
		executionRequestContext{
			BearerToken:   token,
			ContentLength: request.ContentLength,
		},
	)

	return next(requestContext, writer, request, value)
}

func currentExecutionRequest(ctx context.Context) (executionRequestContext, error) {
	value, ok := ctx.Value(executionRequestContextKey{}).(executionRequestContext)
	if !ok {
		return executionRequestContext{}, errors.New("run artifact request context is missing")
	}

	return value, nil
}

func authenticateControllerRequest(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	value any,
	authenticator ControllerAuthenticator,
	next api.StrictHandlerFunc,
	unauthorizedResponse func() any,
) (any, error) {
	token, ok := parseBearerToken(request.Header)
	if !ok {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		return unauthorizedResponse(), nil
	}

	principal, err := authenticator.AuthenticateController(ctx, token)
	if errors.Is(err, controlleridentity.ErrUnauthorized) {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		return unauthorizedResponse(), nil
	}
	if err != nil {
		return nil, err
	}

	principalContext := context.WithValue(ctx, controllerPrincipalContextKey{}, principal)

	return next(principalContext, writer, request, value)
}

func controllerPrincipal(ctx context.Context) (controlleridentity.Principal, error) {
	principal, ok := ctx.Value(controllerPrincipalContextKey{}).(controlleridentity.Principal)
	if !ok {
		return controlleridentity.Principal{}, errors.New("authenticated controller identity is missing")
	}

	return principal, nil
}

func enrollmentUnauthorized(writer http.ResponseWriter) any {
	writer.Header().Set("WWW-Authenticate", "Bearer")
	problem := NewProblem(http.StatusUnauthorized, "The controller enrollment credential is missing or invalid.")

	return api.EnrollController401ApplicationProblemPlusJSONResponse(problem)
}

func controllerUnauthorizedProblem() api.Problem {
	return NewProblem(http.StatusUnauthorized, "The controller credential is missing or invalid.")
}

func parseBearerToken(header http.Header) (string, bool) {
	values := header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}

	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	return parts[1], true
}
