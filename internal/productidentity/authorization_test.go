package productidentity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthenticationAuthorizesRoleAndCredentialScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       Role
		method     AuthenticationMethod
		scopes     []Scope
		permission Permission
		want       bool
	}{
		{
			name:       "viewer session reads",
			role:       RoleViewer,
			method:     AuthenticationMethodWebSession,
			permission: PermissionRead,
			want:       true,
		},
		{
			name:       "viewer session cannot create runs",
			role:       RoleViewer,
			method:     AuthenticationMethodWebSession,
			permission: PermissionRunsWrite,
		},
		{
			name:       "operator session creates runs",
			role:       RoleOperator,
			method:     AuthenticationMethodWebSession,
			permission: PermissionRunsWrite,
			want:       true,
		},
		{
			name:       "operator session cannot administer",
			role:       RoleOperator,
			method:     AuthenticationMethodWebSession,
			permission: PermissionAdmin,
		},
		{
			name:       "viewer session cannot read sensitive artifacts",
			role:       RoleViewer,
			method:     AuthenticationMethodWebSession,
			permission: PermissionSensitiveArtifactsRead,
		},
		{
			name:       "operator session reads sensitive artifacts",
			role:       RoleOperator,
			method:     AuthenticationMethodWebSession,
			permission: PermissionSensitiveArtifactsRead,
			want:       true,
		},
		{
			name:       "admin session administers",
			role:       RoleAdmin,
			method:     AuthenticationMethodWebSession,
			permission: PermissionAdmin,
			want:       true,
		},
		{
			name:       "read key reads",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRead},
			permission: PermissionRead,
			want:       true,
		},
		{
			name:       "read key cannot create runs",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRead},
			permission: PermissionRunsWrite,
		},
		{
			name:       "operator read key reads sensitive artifacts",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRead},
			permission: PermissionSensitiveArtifactsRead,
			want:       true,
		},
		{
			name:       "runs key does not imply read",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRunsWrite},
			permission: PermissionRead,
		},
		{
			name:       "runs key grants revoke permission",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRunsWrite},
			permission: PermissionAPIKeyRevoke,
			want:       true,
		},
		{
			name:       "admin key grants run permission",
			role:       RoleAdmin,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeAdmin},
			permission: PermissionRunsWrite,
			want:       true,
		},
		{
			name:       "scope cannot expand principal role",
			role:       RoleViewer,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeAdmin},
			permission: PermissionAdmin,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authentication := testAuthentication(test.role, test.method, test.scopes)
			if got := authentication.Authorizes(test.permission); got != test.want {
				t.Errorf("Authorizes(%q) = %t, want %t", test.permission, got, test.want)
			}
		})
	}
}

func TestDisabledAuthenticationAuthorizesNothing(t *testing.T) {
	t.Parallel()

	authentication := testAuthentication(RoleAdmin, AuthenticationMethodWebSession, nil)
	disabledAt := time.Now().UTC()
	authentication.Principal.DisabledAt = &disabledAt

	if authentication.Authorizes(PermissionRead) {
		t.Error("disabled principal unexpectedly authorized")
	}
}

func TestAuthenticationValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Authentication)
	}{
		{
			name: "missing credential ID",
			mutate: func(authentication *Authentication) {
				authentication.CredentialID = uuid.Nil
			},
		},
		{
			name: "unknown method",
			mutate: func(authentication *Authentication) {
				authentication.Method = "unknown"
			},
		},
		{
			name: "session scopes",
			mutate: func(authentication *Authentication) {
				authentication.Scopes = []Scope{ScopeRead}
			},
		},
		{
			name: "API key without label",
			mutate: func(authentication *Authentication) {
				authentication.Method = AuthenticationMethodAPIKey
				authentication.Scopes = []Scope{ScopeRead}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authentication := testAuthentication(RoleAdmin, AuthenticationMethodWebSession, nil)
			test.mutate(&authentication)

			if err := authentication.Validate(); err == nil {
				t.Error("Validate() error = nil")
			}
		})
	}
}

func testAuthentication(role Role, method AuthenticationMethod, scopes []Scope) Authentication {
	now := time.Now().UTC()
	credentialLabel := ""
	if method == AuthenticationMethodAPIKey {
		credentialLabel = "automation"
	}

	return Authentication{
		Principal: Principal{
			ID:          uuid.New(),
			Kind:        KindUser,
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
