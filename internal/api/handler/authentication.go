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

type BrowserAuthenticationService interface {
	BeginLogin() (productauth.LoginStart, error)
	BeginNativeLogin(codeChallenge string) (productauth.LoginStart, error)
	CompleteLogin(
		ctx context.Context,
		code string,
		state string,
		cookieState string,
	) (productauth.LoginCompletion, error)
	ExchangeNativeLogin(
		ctx context.Context,
		code string,
		codeVerifier string,
	) (productauth.SessionCredential, error)
	AuthenticateSession(ctx context.Context, token string) (productidentity.Authentication, error)
	RevokeSession(context.Context, string) error
	SecureCookies() bool
}

func (h *Handler) BeginNativeOIDCLogin(
	_ context.Context,
	request api.BeginNativeOIDCLoginRequestObject,
) (api.BeginNativeOIDCLoginResponseObject, error) {
	login, err := h.productAuth.BeginNativeLogin(request.Params.CodeChallenge)
	if errors.Is(err, productauth.ErrInvalidLogin) {
		problem := NewProblem(http.StatusBadRequest, "The native PKCE challenge is invalid.")

		return api.BeginNativeOIDCLogin400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, productauth.ErrUnavailable) {
		problem := NewProblem(
			http.StatusServiceUnavailable,
			"Native authentication is temporarily unavailable.",
		)

		return api.BeginNativeOIDCLogin503ApplicationProblemPlusJSONResponse(problem), nil
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

	return api.BeginNativeOIDCLogin303Response{
		Headers: api.BeginNativeOIDCLogin303ResponseHeaders{
			CacheControl: privateNoStoreCacheControl,
			Location:     login.AuthorizationURL,
			SetCookie:    stateCookie.String(),
		},
	}, nil
}

func (h *Handler) BeginOIDCLogin(
	context.Context,
	api.BeginOIDCLoginRequestObject,
) (api.BeginOIDCLoginResponseObject, error) {
	login, err := h.productAuth.BeginLogin()
	if errors.Is(err, productauth.ErrUnavailable) {
		problem := NewProblem(http.StatusServiceUnavailable, "Browser authentication is temporarily unavailable.")

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
			CacheControl: privateNoStoreCacheControl,
			Location:     login.AuthorizationURL,
			SetCookie:    stateCookie.String(),
		},
	}, nil
}

func (h *Handler) CompleteOIDCLogin(
	ctx context.Context,
	request api.CompleteOIDCLoginRequestObject,
) (api.CompleteOIDCLoginResponseObject, error) {
	cookieState := ""
	if request.Params.SliprampOidcState != nil {
		cookieState = *request.Params.SliprampOidcState
	}
	code := ""
	if request.Params.Code != nil {
		code = *request.Params.Code
	}
	completion, err := h.productAuth.CompleteLogin(ctx, code, request.Params.State, cookieState)
	if err != nil {
		switch {
		case errors.Is(err, productauth.ErrInvalidLogin):
			problem := NewProblem(http.StatusBadRequest, "The OIDC authorization response is invalid or expired.")

			return api.CompleteOIDCLogin400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, productauth.ErrEnrollmentDenied),
			errors.Is(err, productidentity.ErrUnauthenticated):
			problem := NewProblem(http.StatusForbidden, "The OIDC identity is not permitted to sign in.")

			return api.CompleteOIDCLogin403ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}
	if completion.RedirectURL != "" {
		return api.CompleteOIDCLogin303Response{
			Headers: api.CompleteOIDCLogin303ResponseHeaders{
				CacheControl: privateNoStoreCacheControl,
				Location:     completion.RedirectURL,
			},
		}, nil
	}
	if completion.Session == nil {
		return nil, errors.New("OIDC login completion is missing a destination")
	}
	credential := *completion.Session

	sessionCookie := http.Cookie{
		Name:     productauth.SessionCookieName,
		Value:    credential.Token,
		Path:     "/",
		Expires:  credential.ExpiresAt,
		HttpOnly: true,
		Secure:   h.productAuth.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	}
	setCookie := sessionCookie.String()

	return api.CompleteOIDCLogin303Response{
		Headers: api.CompleteOIDCLogin303ResponseHeaders{
			CacheControl: privateNoStoreCacheControl,
			Location:     "/",
			SetCookie:    &setCookie,
		},
	}, nil
}

func (h *Handler) ExchangeNativeOIDCLogin(
	ctx context.Context,
	request api.ExchangeNativeOIDCLoginRequestObject,
) (api.ExchangeNativeOIDCLoginResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "The native login grant is required.")

		return api.ExchangeNativeOIDCLogin400ApplicationProblemPlusJSONResponse(problem), nil
	}
	credential, err := h.productAuth.ExchangeNativeLogin(
		ctx,
		request.Body.Code,
		request.Body.CodeVerifier,
	)
	if errors.Is(err, productauth.ErrInvalidLogin) {
		problem := NewProblem(http.StatusBadRequest, "The native login grant is invalid or expired.")

		return api.ExchangeNativeOIDCLogin400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.ExchangeNativeOIDCLogin200JSONResponse{
		Body: api.NativeOIDCCredential{
			Token:     credential.Token,
			ExpiresAt: credential.ExpiresAt,
		},
		Headers: api.ExchangeNativeOIDCLogin200ResponseHeaders{
			CacheControl: privateNoStoreCacheControl,
		},
	}, nil
}

func (h *Handler) GetCurrentPrincipal(
	ctx context.Context,
	request api.GetCurrentPrincipalRequestObject,
) (api.GetCurrentPrincipalResponseObject, error) {
	token := ""
	if request.Params.SliprampSession != nil {
		token = *request.Params.SliprampSession
	}
	authentication, err := h.productAuth.AuthenticateSession(ctx, token)
	if errors.Is(err, productidentity.ErrUnauthenticated) {
		problem := NewProblem(http.StatusUnauthorized, "A valid product session is required.")

		return api.GetCurrentPrincipal401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.GetCurrentPrincipal200JSONResponse{
		Body: productPrincipalResponse(authentication.Principal),
		Headers: api.GetCurrentPrincipal200ResponseHeaders{
			CacheControl: privateNoStoreCacheControl,
		},
	}, nil
}

func (h *Handler) Logout(ctx context.Context, request api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	if request.Params.XSliprampCSRF != csrfProtectionHeaderValue {
		problem := NewProblem(http.StatusForbidden, "The CSRF protection header is invalid.")

		return api.Logout403ApplicationProblemPlusJSONResponse(problem), nil
	}

	token := ""
	if request.Params.SliprampSession != nil {
		token = *request.Params.SliprampSession
	}
	if err := h.productAuth.RevokeSession(ctx, token); err != nil {
		if errors.Is(err, productidentity.ErrUnauthenticated) {
			problem := NewProblem(http.StatusUnauthorized, "A valid product session is required.")

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
			CacheControl: privateNoStoreCacheControl,
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
