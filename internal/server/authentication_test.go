package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestAuthenticationHTTPFlow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	authentication := productidentity.Authentication{
		Principal: productidentity.Principal{
			ID:   uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
			Kind: productidentity.KindUser, DisplayName: "Ada Lovelace",
			Role: productidentity.RoleAdmin, CreatedAt: now, UpdatedAt: now,
		},
		Method:       productidentity.AuthenticationMethodWebSession,
		CredentialID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501"),
	}
	service := &recordingProductAuthenticationService{
		login: productauth.LoginStart{
			AuthorizationURL: "https://identity.example.com/authorize",
			State:            "login-state",
			ExpiresAt:        now.Add(10 * time.Minute),
		},
		credential: productauth.SessionCredential{
			Token:     "session-token",
			ExpiresAt: now.Add(12 * time.Hour),
		},
		authentication: authentication,
		secure:         true,
	}
	httpHandler := newAuthenticationHandler(service)

	loginRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/auth/login",
		nil,
	)
	loginResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusSeeOther ||
		loginResponse.Header().Get("Location") != service.login.AuthorizationURL {
		t.Fatalf(
			"login status=%d location=%q",
			loginResponse.Code,
			loginResponse.Header().Get("Location"),
		)
	}
	if loginResponse.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("login Cache-Control = %q", loginResponse.Header().Get("Cache-Control"))
	}
	stateCookie := loginResponse.Result().Cookies()[0]
	if stateCookie.Name != productauth.StateCookieName ||
		stateCookie.Value != service.login.State ||
		stateCookie.Path != "/api/v1/auth/callback" ||
		!stateCookie.HttpOnly || !stateCookie.Secure || stateCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("state cookie = %#v", stateCookie)
	}

	callbackRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/auth/callback?code=authorization-code&state=login-state",
		nil,
	)
	callbackRequest.AddCookie(stateCookie)
	callbackResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusSeeOther ||
		callbackResponse.Header().Get("Location") != "/" {
		t.Fatalf(
			"callback status=%d location=%q body=%s",
			callbackResponse.Code,
			callbackResponse.Header().Get("Location"),
			callbackResponse.Body.String(),
		)
	}
	if service.completeCode != "authorization-code" ||
		service.completeState != "login-state" ||
		service.completeCookieState != "login-state" {
		t.Errorf("callback arguments = %#v", service)
	}
	sessionCookie := callbackResponse.Result().Cookies()[0]
	if sessionCookie.Name != productauth.SessionCookieName ||
		sessionCookie.Value != service.credential.Token || sessionCookie.Path != "/" ||
		!sessionCookie.HttpOnly || !sessionCookie.Secure {
		t.Errorf("session cookie = %#v", sessionCookie)
	}

	sessionRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/auth/session",
		nil,
	)
	sessionRequest.AddCookie(sessionCookie)
	sessionResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session status=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
	if sessionResponse.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("session Cache-Control = %q", sessionResponse.Header().Get("Cache-Control"))
	}
	var principal struct {
		ID          uuid.UUID `json:"principalId"`
		DisplayName string    `json:"displayName"`
		Role        string    `json:"role"`
	}
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &principal); err != nil {
		t.Fatalf("decode principal: %v", err)
	}
	if principal.ID != authentication.Principal.ID ||
		principal.DisplayName != "Ada Lovelace" || principal.Role != "admin" {
		t.Errorf("principal response = %#v", principal)
	}
	if service.authToken != service.credential.Token {
		t.Errorf("authenticated token = %q", service.authToken)
	}

	logoutRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/auth/logout",
		nil,
	)
	logoutRequest.Header.Set("X-Slipramp-CSRF", "1")
	logoutRequest.AddCookie(sessionCookie)
	logoutResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}
	if service.revokedToken != service.credential.Token {
		t.Errorf("revoked token = %q", service.revokedToken)
	}
	clearedCookie := logoutResponse.Result().Cookies()[0]
	if clearedCookie.Name != productauth.SessionCookieName || clearedCookie.MaxAge >= 0 {
		t.Errorf("cleared cookie = %#v", clearedCookie)
	}
}

func TestAuthenticationHTTPRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	service := &recordingProductAuthenticationService{
		authenticationErr: productidentity.ErrUnauthenticated,
		revokeErr:         productidentity.ErrUnauthenticated,
	}
	httpHandler := newAuthenticationHandler(service)

	tests := []struct {
		name       string
		method     string
		path       string
		configure  func(*http.Request)
		wantStatus int
	}{
		{
			name:       "invalid session",
			method:     http.MethodGet,
			path:       "/api/v1/auth/session",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "invalid CSRF header",
			method: http.MethodPost,
			path:   "/api/v1/auth/logout",
			configure: func(request *http.Request) {
				request.Header.Set("X-Slipramp-CSRF", "2")
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing CSRF header",
			method:     http.MethodPost,
			path:       "/api/v1/auth/logout",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "wrong method",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				context.Background(),
				test.method,
				test.path,
				strings.NewReader(""),
			)
			if test.configure != nil {
				test.configure(request)
			}
			response := httptest.NewRecorder()
			httpHandler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Errorf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestAuthenticationHTTPReportsUnconfiguredProvider(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/auth/login",
		nil,
	)
	response := httptest.NewRecorder()
	newAuthenticationHandler(nil).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want=%d", response.Code, http.StatusServiceUnavailable)
	}
}

func newAuthenticationHandler(service handlerProductAuthenticationService) http.Handler {
	return newHandlerWithProductAuthentication(
		&recordingClusterService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		service,
		nil,
	)
}

type handlerProductAuthenticationService interface {
	BeginLogin() (productauth.LoginStart, error)
	CompleteLogin(
		context.Context,
		string,
		string,
		string,
	) (productauth.SessionCredential, error)
	AuthenticateSession(
		context.Context,
		string,
	) (productidentity.Authentication, error)
	RevokeSession(context.Context, string) error
	SecureCookies() bool
}

type recordingProductAuthenticationService struct {
	login               productauth.LoginStart
	credential          productauth.SessionCredential
	authentication      productidentity.Authentication
	authenticationErr   error
	revokeErr           error
	secure              bool
	completeCode        string
	completeState       string
	completeCookieState string
	authToken           string
	revokedToken        string
}

func (s *recordingProductAuthenticationService) BeginLogin() (productauth.LoginStart, error) {
	return s.login, nil
}

func (s *recordingProductAuthenticationService) CompleteLogin(
	_ context.Context,
	code string,
	state string,
	cookieState string,
) (productauth.SessionCredential, error) {
	s.completeCode = code
	s.completeState = state
	s.completeCookieState = cookieState

	return s.credential, nil
}

func (s *recordingProductAuthenticationService) AuthenticateSession(
	_ context.Context,
	token string,
) (productidentity.Authentication, error) {
	s.authToken = token

	return s.authentication, s.authenticationErr
}

func (s *recordingProductAuthenticationService) RevokeSession(
	_ context.Context,
	token string,
) error {
	s.revokedToken = token

	return s.revokeErr
}

func (s *recordingProductAuthenticationService) SecureCookies() bool {
	return s.secure
}
