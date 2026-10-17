package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/slipramp/slipramp/internal/api"
)

type ControllerAuthenticator interface {
	AuthenticateController(clusterName, bearerToken string) bool
}

func ControllerAuthentication(
	authenticator ControllerAuthenticator,
) api.StrictMiddlewareFunc {
	return func(
		next api.StrictHandlerFunc,
		_ string,
	) api.StrictHandlerFunc {
		return func(
			ctx context.Context,
			writer http.ResponseWriter,
			request *http.Request,
			value any,
		) (any, error) {
			switch controllerRequest := value.(type) {
			case api.PollForControllerCommandRequestObject:
				token, ok := parseBearerToken(request.Header)
				if ok && authenticator.AuthenticateController(
					controllerRequest.ClusterName,
					token,
				) {
					return next(ctx, writer, request, value)
				}

				writer.Header().Set("WWW-Authenticate", "Bearer")
				problem := NewProblem(
					http.StatusUnauthorized,
					"The controller credential is missing or invalid.",
				)

				return api.PollForControllerCommand401ApplicationProblemPlusJSONResponse(
					problem,
				), nil
			case api.ReportRunObservationRequestObject:
				token, ok := parseBearerToken(request.Header)
				if ok && authenticator.AuthenticateController(
					controllerRequest.ClusterName,
					token,
				) {
					return next(ctx, writer, request, value)
				}

				writer.Header().Set("WWW-Authenticate", "Bearer")
				problem := NewProblem(
					http.StatusUnauthorized,
					"The controller credential is missing or invalid.",
				)

				return api.ReportRunObservation401ApplicationProblemPlusJSONResponse(
					problem,
				), nil
			default:
				return next(ctx, writer, request, value)
			}
		}
	}
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
