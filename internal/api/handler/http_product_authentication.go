package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type productAuthenticationService interface {
	AuthenticateAPIKey(context.Context, string) (productidentity.Authentication, error)
	AuthenticateSession(context.Context, string) (productidentity.Authentication, error)
}

type productAuthenticator struct {
	authentication productAuthenticationService
}

func newProductAuthenticator(authentication productAuthenticationService) *productAuthenticator {
	return &productAuthenticator{authentication: authentication}
}

func (a *productAuthenticator) AuthenticateProductRequest(
	ctx context.Context,
	request *http.Request,
) (productidentity.Authentication, error) {
	authorizationValues := request.Header.Values("Authorization")
	if len(authorizationValues) != 0 {
		token, valid := productBearerToken(authorizationValues)
		if !valid {
			return productidentity.Authentication{}, productidentity.ErrUnauthenticated
		}

		return a.authentication.AuthenticateAPIKey(ctx, token)
	}

	cookie, err := request.Cookie(productauth.SessionCookieName)
	if err != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}
	if cookie.Value == "" {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}

	return a.authentication.AuthenticateSession(ctx, cookie.Value)
}

func productBearerToken(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}

	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	return parts[1], true
}
