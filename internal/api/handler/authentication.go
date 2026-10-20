package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
)

const callbackPath = "/api/v1/auth/callback"

func (h *Handler) BeginOIDCLogin(
	context.Context,
	api.BeginOIDCLoginRequestObject,
) (api.BeginOIDCLoginResponseObject, error) {
	if h.productAuth == nil {
		problem := NewProblem(
			http.StatusServiceUnavailable,
			"Browser authentication is not configured.",
		)

		return api.BeginOIDCLogin503ApplicationProblemPlusJSONResponse(problem), nil
	}

	login, err := h.productAuth.BeginLogin()
	if errors.Is(err, productauth.ErrUnavailable) {
		problem := NewProblem(
			http.StatusServiceUnavailable,
			"Browser authentication is temporarily unavailable.",
		)

		return api.BeginOIDCLogin503ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	stateCookie := http.Cookie{
		Name:     productauth.StateCookieName,
		Value:    login.State,
		Path:     callbackPath,
		Expires:  login.ExpiresAt,
		HttpOnly: true,
		Secure:   h.productAuth.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	}

	return api.BeginOIDCLogin303Response{
		Headers: api.BeginOIDCLogin303ResponseHeaders{
			CacheControl: "no-store",
			Location:     login.AuthorizationURL,
			SetCookie:    stateCookie.String(),
		},
	}, nil
}

func (h *Handler) CompleteOIDCLogin(
	ctx context.Context,
	request api.CompleteOIDCLoginRequestObject,
) (api.CompleteOIDCLoginResponseObject, error) {
	if h.productAuth == nil {
		problem := NewProblem(
			http.StatusServiceUnavailable,
			"Browser authentication is not configured.",
		)

		return api.CompleteOIDCLogin503ApplicationProblemPlusJSONResponse(problem), nil
	}

	cookieState := ""
	if request.Params.SliprampOidcState != nil {
		cookieState = *request.Params.SliprampOidcState
	}
	credential, err := h.productAuth.CompleteLogin(
		ctx,
		request.Params.Code,
		request.Params.State,
		cookieState,
	)
	if err != nil {
		switch {
		case errors.Is(err, productauth.ErrInvalidLogin):
			problem := NewProblem(
				http.StatusBadRequest,
				"The OIDC authorization response is invalid or expired.",
			)

			return api.CompleteOIDCLogin400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, productauth.ErrEnrollmentDenied),
			errors.Is(err, productidentity.ErrUnauthenticated):
			problem := NewProblem(
				http.StatusForbidden,
				"The OIDC identity is not permitted to sign in.",
			)

			return api.CompleteOIDCLogin403ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}

	sessionCookie := http.Cookie{
		Name:     productauth.SessionCookieName,
		Value:    credential.Token,
		Path:     "/",
		Expires:  credential.ExpiresAt,
		HttpOnly: true,
		Secure:   h.productAuth.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	}

	return api.CompleteOIDCLogin303Response{
		Headers: api.CompleteOIDCLogin303ResponseHeaders{
			CacheControl: "no-store",
			Location:     "/",
			SetCookie:    sessionCookie.String(),
		},
	}, nil
}

func (h *Handler) GetCurrentPrincipal(
	ctx context.Context,
	request api.GetCurrentPrincipalRequestObject,
) (api.GetCurrentPrincipalResponseObject, error) {
	if h.productAuth == nil {
		problem := NewProblem(
			http.StatusServiceUnavailable,
			"Browser authentication is not configured.",
		)

		return api.GetCurrentPrincipal503ApplicationProblemPlusJSONResponse(problem), nil
	}

	token := ""
	if request.Params.SliprampSession != nil {
		token = *request.Params.SliprampSession
	}
	authentication, err := h.productAuth.AuthenticateSession(ctx, token)
	if errors.Is(err, productidentity.ErrUnauthenticated) {
		problem := NewProblem(
			http.StatusUnauthorized,
			"A valid product session is required.",
		)

		return api.GetCurrentPrincipal401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.GetCurrentPrincipal200JSONResponse{
		Body: productPrincipalResponse(authentication.Principal),
		Headers: api.GetCurrentPrincipal200ResponseHeaders{
			CacheControl: "no-store",
		},
	}, nil
}

func (h *Handler) Logout(
	ctx context.Context,
	request api.LogoutRequestObject,
) (api.LogoutResponseObject, error) {
	if h.productAuth == nil {
		problem := NewProblem(
			http.StatusServiceUnavailable,
			"Browser authentication is not configured.",
		)

		return api.Logout503ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Params.XSliprampCSRF != "1" {
		problem := NewProblem(
			http.StatusForbidden,
			"The CSRF protection header is invalid.",
		)

		return api.Logout403ApplicationProblemPlusJSONResponse(problem), nil
	}

	token := ""
	if request.Params.SliprampSession != nil {
		token = *request.Params.SliprampSession
	}
	if err := h.productAuth.RevokeSession(ctx, token); err != nil {
		if errors.Is(err, productidentity.ErrUnauthenticated) {
			problem := NewProblem(
				http.StatusUnauthorized,
				"A valid product session is required.",
			)

			return api.Logout401ApplicationProblemPlusJSONResponse(problem), nil
		}

		return nil, err
	}

	clearedCookie := http.Cookie{
		Name:     productauth.SessionCookieName,
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.productAuth.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	}

	return api.Logout204Response{
		Headers: api.Logout204ResponseHeaders{
			CacheControl: "no-store",
			SetCookie:    clearedCookie.String(),
		},
	}, nil
}

func productPrincipalResponse(principal productidentity.Principal) api.ProductPrincipal {
	return api.ProductPrincipal{
		PrincipalID: principal.ID,
		Kind:        string(principal.Kind),
		DisplayName: principal.DisplayName,
		Role:        string(principal.Role),
	}
}
