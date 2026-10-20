package server

import (
	"context"
	"net/http"

	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type productSessionService interface {
	AuthenticateSession(
		context.Context,
		string,
	) (productidentity.Authentication, error)
}

type productSessionAuthenticator struct {
	sessions productSessionService
}

func newProductSessionAuthenticator(
	sessions productSessionService,
) *productSessionAuthenticator {
	return &productSessionAuthenticator{sessions: sessions}
}

func (a *productSessionAuthenticator) AuthenticateProductRequest(
	ctx context.Context,
	request *http.Request,
) (productidentity.Authentication, error) {
	cookie, err := request.Cookie(productauth.SessionCookieName)
	if err != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}
	if cookie.Value == "" {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}

	return a.sessions.AuthenticateSession(ctx, cookie.Value)
}
