package productauth

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestServiceCreatesAuthenticatesAndRevokesAPIKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	store := newProductAuthStore()
	service := newTestService(t, store, &recordingOIDCProvider{}, now)
	principal := productidentity.Principal{
		ID:          uuid.New(),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
		Role:        productidentity.RoleOperator,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	store.principals[principal.ID] = principal
	expiresAt := now.Add(24 * time.Hour)
	session := productidentity.Authentication{
		Principal: principal, Method: productidentity.AuthenticationMethodWebSession,
		CredentialID: uuid.New(),
	}

	credential, err := service.CreateUserAPIKey(
		context.Background(),
		session,
		"automation",
		[]productidentity.Scope{
			productidentity.ScopeRunsWrite,
			productidentity.ScopeRunsReview,
			productidentity.ScopeRead,
		},
		&expiresAt,
	)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if !strings.HasPrefix(credential.Token, apiKeyTokenMarker) {
		t.Errorf("API key token = %q", credential.Token)
	}
	if credential.Key.TokenPrefix != credential.Token[:len(apiKeyTokenMarker)+apiKeyDisplaySecretLength] {
		t.Errorf("API key display prefix = %q", credential.Key.TokenPrefix)
	}
	if credential.Key.TokenDigest != productidentity.CredentialDigest(sha256.Sum256([]byte(credential.Token))) {
		t.Error("API key digest does not match the returned token")
	}
	if credential.Key.Scopes[0] != productidentity.ScopeRead ||
		credential.Key.Scopes[1] != productidentity.ScopeRunsReview ||
		credential.Key.Scopes[2] != productidentity.ScopeRunsWrite {
		t.Errorf("API key scopes = %v", credential.Key.Scopes)
	}
	if !credential.Key.ExpiresAt.Equal(expiresAt) {
		t.Errorf("API key expiry = %v, want %v", credential.Key.ExpiresAt, expiresAt)
	}

	authentication, err := service.AuthenticateAPIKey(context.Background(), credential.Token)
	if err != nil {
		t.Fatalf("AuthenticateAPIKey() error = %v", err)
	}
	if authentication.Principal.ID != principal.ID ||
		authentication.Method != productidentity.AuthenticationMethodAPIKey ||
		authentication.CredentialID != credential.Key.ID ||
		authentication.CredentialLabel != "automation" ||
		len(authentication.Scopes) != 3 {
		t.Errorf("API key authentication = %#v", authentication)
	}
	if store.keyTouches != 1 {
		t.Errorf("API key touch count = %d, want 1", store.keyTouches)
	}

	if err := service.RevokeAPIKey(context.Background(), session, credential.Key.ID); err != nil {
		t.Fatalf("RevokeAPIKey() error = %v", err)
	}
	revokedAt := store.apiKeys[credential.Key.TokenDigest].RevokedAt
	if revokedAt == nil || !revokedAt.Equal(now) {
		t.Errorf("API key revoked at = %v, want %v", revokedAt, now)
	}
	if err := service.RevokeAPIKey(context.Background(), session, credential.Key.ID); err != nil {
		t.Fatalf("second RevokeAPIKey() error = %v", err)
	}
	if store.apiKeys[credential.Key.TokenDigest].RevokedAt != revokedAt {
		t.Error("idempotent revocation changed the original timestamp")
	}

	_, err = service.AuthenticateAPIKey(context.Background(), credential.Token)
	if !errors.Is(err, productidentity.ErrUnauthenticated) {
		t.Fatalf("authentication after revocation error = %v, want unauthenticated", err)
	}
	if store.keyTouches != 1 {
		t.Errorf("revoked API key touch count = %d, want 1", store.keyTouches)
	}
}

func TestServiceRejectsInvalidInactiveAndDisabledAPIKeys(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		token     string
		configure func(*productAuthStore, productidentity.CredentialDigest)
	}{
		{name: "foreign bearer token", token: encodedSecret(30)},
		{name: "malformed secret", token: apiKeyTokenMarker + "short"},
		{name: "unknown key", token: apiKeyTokenMarker + encodedSecret(31)},
		{
			name:  "expired key",
			token: apiKeyTokenMarker + encodedSecret(32),
			configure: func(store *productAuthStore, digest productidentity.CredentialDigest) {
				expiresAt := now
				storeAPIKeyFixture(store, digest, now, &expiresAt, nil, nil)
			},
		},
		{
			name:  "revoked key",
			token: apiKeyTokenMarker + encodedSecret(33),
			configure: func(store *productAuthStore, digest productidentity.CredentialDigest) {
				revokedAt := now.Add(-time.Minute)
				storeAPIKeyFixture(store, digest, now, nil, &revokedAt, nil)
			},
		},
		{
			name:  "disabled principal",
			token: apiKeyTokenMarker + encodedSecret(34),
			configure: func(store *productAuthStore, digest productidentity.CredentialDigest) {
				disabledAt := now.Add(-time.Minute)
				storeAPIKeyFixture(store, digest, now, nil, nil, &disabledAt)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := newProductAuthStore()
			service := newTestService(t, store, &recordingOIDCProvider{}, now)
			digest, digestErr := apiKeyDigest(test.token)
			if test.configure != nil {
				if digestErr != nil {
					t.Fatalf("apiKeyDigest() error = %v", digestErr)
				}
				test.configure(store, digest)
			}

			_, err := service.AuthenticateAPIKey(context.Background(), test.token)
			if !errors.Is(err, productidentity.ErrUnauthenticated) {
				t.Fatalf("AuthenticateAPIKey() error = %v, want unauthenticated", err)
			}
			if store.keyTouches != 0 {
				t.Errorf("API key touch count = %d, want 0", store.keyTouches)
			}
		})
	}
}

func TestServiceManagesUserAndServiceAccountAPIKeys(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	store := newProductAuthStore()
	service := newTestService(t, store, &recordingOIDCProvider{}, now)
	user := productidentity.Principal{
		ID: uuid.New(), Kind: productidentity.KindUser, DisplayName: "Ada Lovelace",
		Role: productidentity.RoleAdmin, CreatedAt: now, UpdatedAt: now,
	}
	store.principals[user.ID] = user
	session := productidentity.Authentication{
		Principal: user, Method: productidentity.AuthenticationMethodWebSession,
		CredentialID: uuid.New(),
	}

	userCredential, err := service.CreateUserAPIKey(
		context.Background(),
		session,
		"personal automation",
		[]productidentity.Scope{productidentity.ScopeRunsWrite},
		nil,
	)
	if err != nil {
		t.Fatalf("CreateUserAPIKey() error = %v", err)
	}
	userKeys, err := service.ListOwnAPIKeys(context.Background(), session)
	if err != nil || len(userKeys) != 1 || userKeys[0].ID != userCredential.Key.ID {
		t.Fatalf("ListAPIKeys() keys=%#v error=%v", userKeys, err)
	}

	serviceAccount, err := service.CreateServiceAccount(
		context.Background(),
		"Deployment automation",
		productidentity.RoleOperator,
	)
	if err != nil {
		t.Fatalf("CreateServiceAccount() error = %v", err)
	}
	serviceCredential, err := service.CreateServiceAccountAPIKey(
		context.Background(),
		serviceAccount.ID,
		user.ID,
		"deployer",
		[]productidentity.Scope{productidentity.ScopeRunsWrite},
		nil,
	)
	if err != nil {
		t.Fatalf("CreateServiceAccountAPIKey() error = %v", err)
	}
	serviceKeys, err := service.ListServiceAccountAPIKeys(context.Background(), serviceAccount.ID)
	if err != nil || len(serviceKeys) != 1 || serviceKeys[0].ID != serviceCredential.Key.ID {
		t.Fatalf("ListServiceAccountAPIKeys() keys=%#v error=%v", serviceKeys, err)
	}

	inboxOnlyUserKey := productidentity.Authentication{
		Principal: user, Method: productidentity.AuthenticationMethodAPIKey,
		CredentialID: userCredential.Key.ID, CredentialLabel: userCredential.Key.Name,
		Scopes: []productidentity.Scope{productidentity.ScopeInboxWrite},
	}
	err = service.RevokeAPIKey(context.Background(), inboxOnlyUserKey, serviceCredential.Key.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-principal revoke error = %v, want forbidden", err)
	}
	if err := service.RevokeAPIKey(context.Background(), inboxOnlyUserKey, userCredential.Key.ID); err != nil {
		t.Fatalf("owner revoke error = %v", err)
	}
	if err := service.RevokeAPIKey(context.Background(), session, serviceCredential.Key.ID); err != nil {
		t.Fatalf("administrator revoke error = %v", err)
	}
}

func TestServiceRejectsAPIKeyCredentialMintingAnotherUserKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, newProductAuthStore(), &recordingOIDCProvider{}, now)
	authentication := productidentity.Authentication{
		Principal: productidentity.Principal{
			ID: uuid.New(), Kind: productidentity.KindUser, DisplayName: "Ada Lovelace",
			Role: productidentity.RoleAdmin, CreatedAt: now, UpdatedAt: now,
		},
		Method: productidentity.AuthenticationMethodAPIKey, CredentialID: uuid.New(),
		CredentialLabel: "existing", Scopes: []productidentity.Scope{productidentity.ScopeAdmin},
	}

	_, err := service.CreateUserAPIKey(
		context.Background(),
		authentication,
		"replacement",
		[]productidentity.Scope{productidentity.ScopeRead},
		nil,
	)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateUserAPIKey() error = %v, want forbidden", err)
	}
}

func storeAPIKeyFixture(
	store *productAuthStore,
	digest productidentity.CredentialDigest,
	now time.Time,
	expiresAt *time.Time,
	revokedAt *time.Time,
	disabledAt *time.Time,
) {
	principal := productidentity.Principal{
		ID:          uuid.New(),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: "Automation",
		Role:        productidentity.RoleOperator,
		DisabledAt:  disabledAt,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	key := productidentity.APIKey{
		ID:                   uuid.New(),
		PrincipalID:          principal.ID,
		CreatedByPrincipalID: uuid.New(),
		Name:                 "automation",
		TokenPrefix:          "slipramp_key_fixture",
		TokenDigest:          digest,
		Scopes:               []productidentity.Scope{productidentity.ScopeRunsWrite},
		CreatedAt:            now.Add(-time.Hour),
		ExpiresAt:            expiresAt,
		RevokedAt:            revokedAt,
	}
	store.principals[principal.ID] = principal
	store.apiKeys[digest] = key
}
