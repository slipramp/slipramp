package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestProductAuthenticationRejectsMissingAndInvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "missing", err: productidentity.ErrUnauthenticated},
		{
			name: "invalid",
			err:  fmt.Errorf("validate bearer token: %w", productidentity.ErrUnauthenticated),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authenticator := &recordingProductAuthenticator{
				err: test.err,
			}
			called := false
			response := runProductMiddleware(t, authenticator, "ListRuns", func(context.Context) { called = true })

			if response.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if response.Header().Get("WWW-Authenticate") != `Bearer realm="slipramp"` {
				t.Errorf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
			}
			if called {
				t.Error("next handler was called")
			}
		})
	}
}

func TestProductAuthenticationRejectsDisabledPrincipal(t *testing.T) {
	t.Parallel()

	authentication := productAuthentication(
		productidentity.RoleAdmin,
		productidentity.AuthenticationMethodWebSession,
		nil,
	)
	disabledAt := time.Now().UTC()
	authentication.Principal.DisabledAt = &disabledAt

	called := false
	response := runProductMiddleware(
		t,
		&recordingProductAuthenticator{authentication: authentication},
		"CreateRepository",
		func(context.Context) {
			called = true
		},
	)

	if response.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if called {
		t.Error("next handler was called")
	}
}

func TestProductAuthenticationRejectsInsufficientRoleOrScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		authentication productidentity.Authentication
		operationID    string
	}{
		{
			name: "viewer role",
			authentication: productAuthentication(
				productidentity.RoleViewer,
				productidentity.AuthenticationMethodWebSession,
				nil,
			),
			operationID: "CreateRun",
		},
		{
			name: "viewer cannot acknowledge Inbox problems",
			authentication: productAuthentication(
				productidentity.RoleViewer,
				productidentity.AuthenticationMethodWebSession,
				nil,
			),
			operationID: "AcknowledgeInboxProblems",
		},
		{
			name: "read key cannot acknowledge Inbox problems",
			authentication: productAuthentication(
				productidentity.RoleOperator,
				productidentity.AuthenticationMethodAPIKey,
				[]productidentity.Scope{productidentity.ScopeRead},
			),
			operationID: "AcknowledgeInboxProblems",
		},
		{
			name: "restricted API key",
			authentication: productAuthentication(
				productidentity.RoleAdmin,
				productidentity.AuthenticationMethodAPIKey,
				[]productidentity.Scope{productidentity.ScopeRead},
			),
			operationID: "RemoveRepository",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			called := false
			response := runProductMiddleware(
				t,
				&recordingProductAuthenticator{authentication: test.authentication},
				test.operationID,
				func(context.Context) {
					called = true
				},
			)

			if response.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
			if called {
				t.Error("next handler was called")
			}
		})
	}
}

func TestProductAuthenticationAttachesAuthorizedIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		operationID string
		scope       productidentity.Scope
	}{
		{name: "Run cancellation", operationID: "CancelRun", scope: productidentity.ScopeRunsWrite},
		{name: "Run rerun", operationID: "RerunRun", scope: productidentity.ScopeRunsWrite},
		{name: "API key revocation", operationID: "RevokeAPIKey", scope: productidentity.ScopeRunsWrite},
		{
			name:        "Inbox acknowledgement",
			operationID: "AcknowledgeInboxProblems",
			scope:       productidentity.ScopeInboxWrite,
		},
		{
			name:        "Promotion creation",
			operationID: "CreatePromotion",
			scope:       productidentity.ScopePromotionsWrite,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authentication := productAuthentication(
				productidentity.RoleOperator,
				productidentity.AuthenticationMethodAPIKey,
				[]productidentity.Scope{test.scope},
			)
			var attached productidentity.Authentication
			response := runProductMiddleware(
				t,
				&recordingProductAuthenticator{authentication: authentication},
				test.operationID,
				func(ctx context.Context) {
					var err error
					attached, err = currentProductAuthentication(ctx)
					if err != nil {
						t.Errorf("currentProductAuthentication() error = %v", err)
					}
				},
			)

			if response.Code != http.StatusNoContent {
				t.Errorf("status = %d, want %d", response.Code, http.StatusNoContent)
			}
			if attached.Principal.ID != authentication.Principal.ID ||
				attached.CredentialID != authentication.CredentialID {
				t.Errorf("attached authentication = %#v, want %#v", attached, authentication)
			}
		})
	}
}

func TestKargoSubmissionRequiresServiceAccountAPIKey(t *testing.T) {
	t.Parallel()

	userAuthentication := productAuthentication(
		productidentity.RoleOperator,
		productidentity.AuthenticationMethodAPIKey,
		[]productidentity.Scope{productidentity.ScopePromotionsSubmit},
	)
	called := false
	response := runProductMiddleware(
		t,
		&recordingProductAuthenticator{authentication: userAuthentication},
		"SubmitKargoPromotion",
		func(context.Context) { called = true },
	)
	if response.Code != http.StatusForbidden || called {
		t.Errorf("user API key status = %d, called = %t", response.Code, called)
	}

	serviceAccountAuthentication := userAuthentication
	serviceAccountAuthentication.Principal.Kind = productidentity.KindServiceAccount
	response = runProductMiddleware(
		t,
		&recordingProductAuthenticator{authentication: serviceAccountAuthentication},
		"SubmitKargoPromotion",
		func(context.Context) { called = true },
	)
	if response.Code != http.StatusNoContent || !called {
		t.Errorf("service-account API key status = %d, called = %t", response.Code, called)
	}
}

func TestSensitiveArtifactAuthorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		authentication *productidentity.Authentication
		want           bool
	}{
		{name: "unauthenticated"},
		{
			name: "viewer session",
			authentication: new(productAuthentication(
				productidentity.RoleViewer,
				productidentity.AuthenticationMethodWebSession,
				nil,
			)),
		},
		{
			name: "operator session",
			authentication: new(productAuthentication(
				productidentity.RoleOperator,
				productidentity.AuthenticationMethodWebSession,
				nil,
			)),
			want: true,
		},
		{
			name: "operator read key",
			authentication: new(productAuthentication(
				productidentity.RoleOperator,
				productidentity.AuthenticationMethodAPIKey,
				[]productidentity.Scope{productidentity.ScopeRead},
			)),
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if test.authentication != nil {
				ctx = context.WithValue(ctx, productAuthenticationContextKey{}, *test.authentication)
			}
			if got := canDownloadSensitiveArtifact(ctx); got != test.want {
				t.Errorf("canDownloadSensitiveArtifact() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestProductAuthenticationLeavesOtherTrustBoundariesAlone(t *testing.T) {
	t.Parallel()

	authenticator := &recordingProductAuthenticator{
		err: errors.New("must not be called"),
	}
	called := false
	response := runProductMiddleware(
		t,
		authenticator,
		"PollForControllerCommand",
		func(context.Context) {
			called = true
		},
	)

	if response.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if !called {
		t.Error("next handler was not called")
	}
	if authenticator.calls != 0 {
		t.Errorf("authentication calls = %d, want 0", authenticator.calls)
	}
}

func TestProductAuthenticationLeavesAuthenticationOperationsAlone(t *testing.T) {
	t.Parallel()

	authenticator := &recordingProductAuthenticator{
		err: errors.New("must not be called"),
	}
	for _, operationID := range []string{
		"BeginOIDCLogin",
		"CompleteOIDCLogin",
		"GetCurrentPrincipal",
		"Logout",
	} {
		called := false
		response := runProductMiddleware(t, authenticator, operationID, func(context.Context) { called = true })

		if response.Code != http.StatusNoContent {
			t.Errorf("%s status = %d, want %d", operationID, response.Code, http.StatusNoContent)
		}
		if !called {
			t.Errorf("%s next handler was not called", operationID)
		}
	}
	if authenticator.calls != 0 {
		t.Errorf("authentication calls = %d, want 0", authenticator.calls)
	}
}

func TestProductAuthenticationFailsClosedForUnmappedProductOperation(t *testing.T) {
	t.Parallel()

	authenticator := &recordingProductAuthenticator{}
	next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
		t.Fatal("next handler was called")

		return nil, nil
	}
	handler := ProductAuthentication(authenticator)(api.StrictHandlerFunc(next), "FutureProductOperation")
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/future", nil)
	response := httptest.NewRecorder()

	if _, err := handler(request.Context(), response, request, struct{}{}); err == nil {
		t.Fatal("middleware error = nil")
	}
	if authenticator.calls != 0 {
		t.Errorf("authentication calls = %d, want 0", authenticator.calls)
	}
}

type recordingProductAuthenticator struct {
	authentication productidentity.Authentication
	err            error
	calls          int
}

func (a *recordingProductAuthenticator) AuthenticateProductRequest(
	context.Context,
	*http.Request,
) (productidentity.Authentication, error) {
	a.calls++

	return a.authentication, a.err
}

func runProductMiddleware(
	t *testing.T,
	authenticator ProductAuthenticator,
	operationID string,
	inspectContext func(context.Context),
) *httptest.ResponseRecorder {
	t.Helper()

	next := func(ctx context.Context, writer http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		inspectContext(ctx)
		writer.WriteHeader(http.StatusNoContent)

		return nil, nil
	}
	middleware := ProductAuthentication(authenticator)
	handler := middleware(api.StrictHandlerFunc(next), operationID)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	if _, err := handler(request.Context(), response, request, struct{}{}); err != nil {
		t.Fatalf("middleware error = %v", err)
	}

	return response
}

func productAuthentication(
	role productidentity.Role,
	method productidentity.AuthenticationMethod,
	scopes []productidentity.Scope,
) productidentity.Authentication {
	now := time.Now().UTC()
	credentialLabel := ""
	if method == productidentity.AuthenticationMethodAPIKey {
		credentialLabel = "automation"
	}

	return productidentity.Authentication{
		Principal: productidentity.Principal{
			ID:          uuid.New(),
			Kind:        productidentity.KindUser,
			DisplayName: "Test User",
			Role:        role,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Method:          method,
		CredentialID:    uuid.New(),
		CredentialLabel: credentialLabel,
		Scopes:          scopes,
	}
}
