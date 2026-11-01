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
		disabled   bool
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
			name:       "viewer session cannot review runs",
			role:       RoleViewer,
			method:     AuthenticationMethodWebSession,
			permission: PermissionRunsReview,
		},
		{
			name:       "operator session reviews runs",
			role:       RoleOperator,
			method:     AuthenticationMethodWebSession,
			permission: PermissionRunsReview,
			want:       true,
		},
		{
			name:       "viewer session cannot acknowledge Inbox problems",
			role:       RoleViewer,
			method:     AuthenticationMethodWebSession,
			permission: PermissionInboxWrite,
		},
		{
			name:       "operator session acknowledges Inbox problems",
			role:       RoleOperator,
			method:     AuthenticationMethodWebSession,
			permission: PermissionInboxWrite,
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
			name:       "read OAuth token reads",
			role:       RoleOperator,
			method:     AuthenticationMethodOAuthToken,
			scopes:     []Scope{ScopeRead},
			permission: PermissionRead,
			want:       true,
		},
		{
			name:       "read OAuth token cannot create runs",
			role:       RoleOperator,
			method:     AuthenticationMethodOAuthToken,
			scopes:     []Scope{ScopeRead},
			permission: PermissionRunsWrite,
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
			name:       "runs write key cannot review",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRunsWrite},
			permission: PermissionRunsReview,
		},
		{
			name:       "runs review key reviews",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRunsReview},
			permission: PermissionRunsReview,
			want:       true,
		},
		{
			name:       "runs review key cannot create runs",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRunsReview},
			permission: PermissionRunsWrite,
		},
		{
			name:       "runs review key grants revoke permission",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeRunsReview},
			permission: PermissionAPIKeyRevoke,
			want:       true,
		},
		{
			name:       "Inbox key acknowledges problems",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeInboxWrite},
			permission: PermissionInboxWrite,
			want:       true,
		},
		{
			name:       "Inbox key does not imply read",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeInboxWrite},
			permission: PermissionRead,
		},
		{
			name:       "Inbox key grants owner revoke permission",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeInboxWrite},
			permission: PermissionAPIKeyRevoke,
			want:       true,
		},
		{
			name:       "viewer Inbox scope cannot acknowledge problems",
			role:       RoleViewer,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeInboxWrite},
			permission: PermissionInboxWrite,
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
			name:       "admin key grants run review permission",
			role:       RoleAdmin,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeAdmin},
			permission: PermissionRunsReview,
			want:       true,
		},
		{
			name:       "promotion submit key submits",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopePromotionsSubmit},
			permission: PermissionPromotionsSubmit,
			want:       true,
		},
		{
			name:       "promotion submit key cannot decide",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopePromotionsSubmit},
			permission: PermissionPromotionsWrite,
		},
		{
			name:       "promotion submit key grants owner revoke permission",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopePromotionsSubmit},
			permission: PermissionAPIKeyRevoke,
			want:       true,
		},
		{
			name:       "promotion write key decides",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopePromotionsWrite},
			permission: PermissionPromotionsWrite,
			want:       true,
		},
		{
			name:       "promotion write key grants owner revoke permission",
			role:       RoleOperator,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopePromotionsWrite},
			permission: PermissionAPIKeyRevoke,
			want:       true,
		},
		{
			name:       "viewer promotion scope cannot submit",
			role:       RoleViewer,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopePromotionsSubmit},
			permission: PermissionPromotionsSubmit,
		},
		{
			name:       "scope cannot expand principal role",
			role:       RoleViewer,
			method:     AuthenticationMethodAPIKey,
			scopes:     []Scope{ScopeAdmin},
			permission: PermissionAdmin,
		},
		{
			name:       "disabled principal authorizes nothing",
			role:       RoleAdmin,
			method:     AuthenticationMethodWebSession,
			permission: PermissionRead,
			disabled:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authentication := testAuthentication(test.role, test.method, test.scopes)
			if test.disabled {
				disabledAt := time.Now().UTC()
				authentication.Principal.DisabledAt = &disabledAt
			}
			if got := authentication.Authorizes(test.permission); got != test.want {
				t.Errorf("Authorizes(%q) = %t, want %t", test.permission, got, test.want)
			}
		})
	}
}

func TestAuthenticationValidate(t *testing.T) {
	t.Parallel()

	allScopes := []Scope{
		ScopeRead,
		ScopeRunsWrite,
		ScopeRunsReview,
		ScopeInboxWrite,
		ScopePromotionsSubmit,
		ScopePromotionsWrite,
		ScopeAdmin,
	}
	validAPIKey := testAuthentication(RoleAdmin, AuthenticationMethodAPIKey, allScopes)
	if err := validAPIKey.Validate(); err != nil {
		t.Fatalf("Validate() all API key scopes error = %v", err)
	}
	validOAuthToken := testAuthentication(RoleAdmin, AuthenticationMethodOAuthToken, allScopes)
	if err := validOAuthToken.Validate(); err != nil {
		t.Fatalf("Validate() OAuth scopes error = %v", err)
	}

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
	if method == AuthenticationMethodAPIKey || method == AuthenticationMethodOAuthToken {
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
