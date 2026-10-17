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
			pollRequest, ok := value.(api.PollForControllerWorkRequestObject)
			if !ok {
				return next(ctx, writer, request, value)
			}

			token, ok := parseBearerToken(request.Header)
			if !ok || !authenticator.AuthenticateController(
				pollRequest.ClusterName,
				token,
			) {
				writer.Header().Set("WWW-Authenticate", "Bearer")
				problem := NewProblem(
					http.StatusUnauthorized,
					"The controller credential is missing or invalid.",
				)

				return api.PollForControllerWork401ApplicationProblemPlusJSONResponse(
					problem,
				), nil
			}

			return next(ctx, writer, request, value)
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
