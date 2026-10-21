package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api/handler"
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

	loginRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/login", nil)
	loginResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusSeeOther ||
		loginResponse.Header().Get("Location") != service.login.AuthorizationURL {
		t.Fatalf("login status=%d location=%q", loginResponse.Code, loginResponse.Header().Get("Location"))
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

	sessionRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/session", nil)
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

	logoutRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/logout", nil)
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

func TestProductAuthenticatorPrefersExplicitBearerCredential(t *testing.T) {
	t.Parallel()

	service := &recordingProductAuthenticationService{}
	authenticator := newProductAuthenticator(service)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/runs", nil)
	request.Header.Set("Authorization", "Bearer slipramp_key_secret")
	request.AddCookie(&http.Cookie{Name: productauth.SessionCookieName, Value: "session-token"})

	if _, err := authenticator.AuthenticateProductRequest(context.Background(), request); err != nil {
		t.Fatalf("AuthenticateProductRequest() error = %v", err)
	}
	if service.apiKeyToken != "slipramp_key_secret" || service.authToken != "" {
		t.Errorf("API key token = %q, session token = %q", service.apiKeyToken, service.authToken)
	}
}

func TestProductAuthenticatorRejectsMalformedBearerWithoutSessionFallback(t *testing.T) {
	t.Parallel()

	service := &recordingProductAuthenticationService{}
	authenticator := newProductAuthenticator(service)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/runs", nil)
	request.Header.Add("Authorization", "Bearer first")
	request.Header.Add("Authorization", "Bearer second")
	request.AddCookie(&http.Cookie{Name: productauth.SessionCookieName, Value: "session-token"})

	_, err := authenticator.AuthenticateProductRequest(context.Background(), request)
	if !errors.Is(err, productidentity.ErrUnauthenticated) {
		t.Fatalf("AuthenticateProductRequest() error = %v, want unauthenticated", err)
	}
	if service.apiKeyToken != "" || service.authToken != "" {
		t.Errorf("API key token = %q, session token = %q", service.apiKeyToken, service.authToken)
	}
}

func TestAPIKeyAndServiceAccountManagementHTTP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	principal := productidentity.Principal{
		ID:   uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1600"),
		Kind: productidentity.KindUser, DisplayName: "Ada Lovelace",
		Role: productidentity.RoleAdmin, CreatedAt: now, UpdatedAt: now,
	}
	authentication := productidentity.Authentication{
		Principal: principal, Method: productidentity.AuthenticationMethodWebSession,
		CredentialID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1601"),
	}
	key := productidentity.APIKey{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1602"),
		PrincipalID: principal.ID, CreatedByPrincipalID: principal.ID,
		Name: "automation", TokenPrefix: "slipramp_key_AQEBAQEBAQEB",
		TokenDigest: productidentity.CredentialDigest{1},
		Scopes:      []productidentity.Scope{productidentity.ScopeRead}, CreatedAt: now,
	}
	serviceAccount := productidentity.Principal{
		ID:   uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1603"),
		Kind: productidentity.KindServiceAccount, DisplayName: "Deployments",
		Role: productidentity.RoleOperator, CreatedAt: now, UpdatedAt: now,
	}
	service := &recordingProductAuthenticationService{
		authentication: authentication,
		apiKeys:        []productidentity.APIKey{key},
		createdAPIKey: productauth.APIKeyCredential{
			Key: key, Token: "slipramp_key_complete-secret",
		},
		serviceAccounts:       []productidentity.Principal{serviceAccount},
		createdServiceAccount: serviceAccount,
	}
	httpHandler := newAuthenticationHandler(service)

	listRequest := authenticatedProductRequest(http.MethodGet, "/api/v1/auth/api-keys", "")
	listResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), "complete-secret") {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	createRequest := authenticatedProductRequest(
		http.MethodPost,
		"/api/v1/auth/api-keys",
		`{"name":"new automation","scopes":["slipramp:read"]}`,
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated ||
		!strings.Contains(createResponse.Body.String(), "slipramp_key_complete-secret") {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	if service.createAPIKeyAuth.Principal.ID != principal.ID ||
		service.createAPIKeyName != "new automation" ||
		len(service.createAPIKeyScopes) != 1 ||
		service.createAPIKeyScopes[0] != productidentity.ScopeRead {
		t.Errorf("create API key request = %#v", service)
	}

	accountsRequest := authenticatedProductRequest(http.MethodGet, "/api/v1/service-accounts", "")
	accountsResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(accountsResponse, accountsRequest)
	if accountsResponse.Code != http.StatusOK ||
		!strings.Contains(accountsResponse.Body.String(), serviceAccount.DisplayName) {
		t.Fatalf("accounts status=%d body=%s", accountsResponse.Code, accountsResponse.Body.String())
	}

	createAccountRequest := authenticatedProductRequest(
		http.MethodPost,
		"/api/v1/service-accounts",
		`{"displayName":"Deployments","role":"operator"}`,
	)
	createAccountRequest.Header.Set("Content-Type", "application/json")
	createAccountResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(createAccountResponse, createAccountRequest)
	if createAccountResponse.Code != http.StatusCreated ||
		service.serviceAccountName != "Deployments" ||
		service.serviceAccountRole != productidentity.RoleOperator {
		t.Fatalf(
			"create account status=%d name=%q role=%q body=%s",
			createAccountResponse.Code,
			service.serviceAccountName,
			service.serviceAccountRole,
			createAccountResponse.Body.String(),
		)
	}

	createServiceKeyRequest := authenticatedProductRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/service-accounts/%s/api-keys", serviceAccount.ID),
		`{"name":"deployment key","scopes":["slipramp:runs:write"]}`,
	)
	createServiceKeyRequest.Header.Set("Content-Type", "application/json")
	createServiceKeyResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(createServiceKeyResponse, createServiceKeyRequest)
	if createServiceKeyResponse.Code != http.StatusCreated ||
		service.serviceAccountKeyPrincipalID != serviceAccount.ID ||
		service.serviceAccountKeyCreatorID != principal.ID {
		t.Fatalf(
			"create service key status=%d target=%s creator=%s body=%s",
			createServiceKeyResponse.Code,
			service.serviceAccountKeyPrincipalID,
			service.serviceAccountKeyCreatorID,
			createServiceKeyResponse.Body.String(),
		)
	}

	revokeRequest := authenticatedProductRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/auth/api-keys/%s", key.ID),
		"",
	)
	revokeResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent || service.revokedAPIKeyID != key.ID {
		t.Fatalf("revoke status=%d key=%s", revokeResponse.Code, service.revokedAPIKeyID)
	}
}

func authenticatedProductRequest(method, path, body string) *http.Request {
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: productauth.SessionCookieName, Value: "session-token"})

	return request
}

func newAuthenticationHandler(service handler.ProductAuthenticationService) http.Handler {
	return newHTTPHandler(&recordingClusterService{}, nil, nil, nil, nil, nil, nil, nil, service, nil)
}

type recordingProductAuthenticationService struct {
	login                        productauth.LoginStart
	credential                   productauth.SessionCredential
	authentication               productidentity.Authentication
	authenticationErr            error
	revokeErr                    error
	secure                       bool
	completeCode                 string
	completeState                string
	completeCookieState          string
	authToken                    string
	apiKeyToken                  string
	revokedToken                 string
	apiKeys                      []productidentity.APIKey
	createdAPIKey                productauth.APIKeyCredential
	createAPIKeyAuth             productidentity.Authentication
	createAPIKeyName             string
	createAPIKeyScopes           []productidentity.Scope
	revokedAPIKeyID              uuid.UUID
	serviceAccounts              []productidentity.Principal
	createdServiceAccount        productidentity.Principal
	serviceAccountName           string
	serviceAccountRole           productidentity.Role
	serviceAccountKeyPrincipalID uuid.UUID
	serviceAccountKeyCreatorID   uuid.UUID
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

func (s *recordingProductAuthenticationService) AuthenticateAPIKey(
	_ context.Context,
	token string,
) (productidentity.Authentication, error) {
	s.apiKeyToken = token

	return s.authentication, s.authenticationErr
}

func (s *recordingProductAuthenticationService) ListOwnAPIKeys(
	context.Context,
	productidentity.Authentication,
) ([]productidentity.APIKey, error) {
	return s.apiKeys, nil
}

func (s *recordingProductAuthenticationService) CreateUserAPIKey(
	_ context.Context,
	authentication productidentity.Authentication,
	name string,
	scopes []productidentity.Scope,
	_ *time.Time,
) (productauth.APIKeyCredential, error) {
	s.createAPIKeyAuth = authentication
	s.createAPIKeyName = name
	s.createAPIKeyScopes = scopes

	return s.createdAPIKey, nil
}

func (s *recordingProductAuthenticationService) RevokeAPIKey(
	_ context.Context,
	_ productidentity.Authentication,
	keyID uuid.UUID,
) error {
	s.revokedAPIKeyID = keyID

	return nil
}

func (s *recordingProductAuthenticationService) ListServiceAccounts(
	context.Context,
) ([]productidentity.Principal, error) {
	return s.serviceAccounts, nil
}

func (s *recordingProductAuthenticationService) CreateServiceAccount(
	_ context.Context,
	displayName string,
	role productidentity.Role,
) (productidentity.Principal, error) {
	s.serviceAccountName = displayName
	s.serviceAccountRole = role

	return s.createdServiceAccount, nil
}

func (s *recordingProductAuthenticationService) ListServiceAccountAPIKeys(
	context.Context,
	uuid.UUID,
) ([]productidentity.APIKey, error) {
	return s.apiKeys, nil
}

func (s *recordingProductAuthenticationService) CreateServiceAccountAPIKey(
	_ context.Context,
	principalID uuid.UUID,
	createdByPrincipalID uuid.UUID,
	name string,
	scopes []productidentity.Scope,
	_ *time.Time,
) (productauth.APIKeyCredential, error) {
	s.serviceAccountKeyPrincipalID = principalID
	s.serviceAccountKeyCreatorID = createdByPrincipalID
	s.createAPIKeyName = name
	s.createAPIKeyScopes = scopes

	return s.createdAPIKey, nil
}

func (s *recordingProductAuthenticationService) RevokeSession(_ context.Context, token string) error {
	s.revokedToken = token

	return s.revokeErr
}

func (s *recordingProductAuthenticationService) SecureCookies() bool {
	return s.secure
}
