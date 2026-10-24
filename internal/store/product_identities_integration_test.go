package store

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestProductIdentityPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	now := time.Now().UTC()
	userRecord := productidentity.Principal{
		ID:          uuid.New(),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
		Role:        productidentity.RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	email := "ada@example.com"
	identityRecord := productidentity.OIDCIdentity{
		ID:                  uuid.New(),
		PrincipalID:         userRecord.ID,
		Issuer:              "https://identity.example.com",
		Subject:             "00u-ada",
		Email:               &email,
		LastAuthenticatedAt: now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	user, identity, err := database.CreateOIDCUser(ctx, userRecord, identityRecord)
	if err != nil {
		t.Fatalf("CreateOIDCUser() error = %v", err)
	}
	loaded, err := database.GetPrincipal(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetPrincipal() error = %v", err)
	}
	if loaded.Kind != productidentity.KindUser ||
		loaded.DisplayName != "Ada Lovelace" ||
		loaded.Role != productidentity.RoleAdmin {
		t.Errorf("loaded principal = %#v", loaded)
	}

	if identity.PrincipalID != user.ID || identity.Email == nil ||
		*identity.Email != email {
		t.Errorf("created OIDC identity = %#v", identity)
	}

	serviceAccount, err := database.CreatePrincipal(ctx, productidentity.Principal{
		ID:          uuid.New(),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: "Deployment automation",
		Role:        productidentity.RoleOperator,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create service account: %v", err)
	}

	apiKeyDigest := productidentity.CredentialDigest(sha256.Sum256([]byte("API key secret")))
	expiresAt := now.Add(24 * time.Hour)
	key, err := database.CreateAPIKey(ctx, productidentity.APIKey{
		ID:                   uuid.New(),
		PrincipalID:          serviceAccount.ID,
		CreatedByPrincipalID: user.ID,
		Name:                 "deployments",
		TokenPrefix:          "slipramp_key_deployments",
		TokenDigest:          apiKeyDigest,
		Scopes: []productidentity.Scope{
			productidentity.ScopeRead,
			productidentity.ScopeRunsWrite,
			productidentity.ScopeInboxWrite,
			productidentity.ScopePromotionsSubmit,
			productidentity.ScopePromotionsWrite,
		},
		CreatedAt: now,
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if key.PrincipalID != serviceAccount.ID ||
		key.CreatedByPrincipalID != user.ID ||
		len(key.Scopes) != 5 ||
		key.Scopes[0] != productidentity.ScopeRead {
		t.Errorf("created API key = %#v", key)
	}
	loadedKey, keyPrincipal, err := database.GetAPIKeyPrincipal(ctx, apiKeyDigest)
	if err != nil {
		t.Fatalf("GetAPIKeyPrincipal() error = %v", err)
	}
	if loadedKey.ID != key.ID || keyPrincipal.ID != serviceAccount.ID {
		t.Errorf("loaded API key = %#v, principal = %#v", loadedKey, keyPrincipal)
	}
	loadedKey, err = database.GetAPIKey(ctx, key.ID)
	if err != nil || loadedKey.TokenDigest != key.TokenDigest {
		t.Errorf("GetAPIKey() key = %#v, error = %v", loadedKey, err)
	}
	keys, err := database.ListAPIKeys(ctx, serviceAccount.ID)
	if err != nil || len(keys) != 1 || keys[0].ID != key.ID {
		t.Errorf("ListAPIKeys() keys = %#v, error = %v", keys, err)
	}
	serviceAccounts, err := database.ListServiceAccounts(ctx)
	if err != nil || len(serviceAccounts) != 1 || serviceAccounts[0].ID != serviceAccount.ID {
		t.Errorf("ListServiceAccounts() principals = %#v, error = %v", serviceAccounts, err)
	}
	keyUsedAt := key.CreatedAt.Add(time.Minute)
	if err := database.TouchAPIKey(ctx, key.ID, keyUsedAt); err != nil {
		t.Fatalf("TouchAPIKey() error = %v", err)
	}
	loadedKey, _, err = database.GetAPIKeyPrincipal(ctx, apiKeyDigest)
	if err != nil {
		t.Fatalf("GetAPIKeyPrincipal() after touch error = %v", err)
	}
	if loadedKey.LastUsedAt == nil || !loadedKey.LastUsedAt.Equal(keyUsedAt) {
		t.Errorf("LastUsedAt = %v, want %s", loadedKey.LastUsedAt, keyUsedAt)
	}
	revokedAt := keyUsedAt.Add(time.Minute)
	if err := database.RevokeAPIKey(ctx, key.ID, revokedAt); err != nil {
		t.Fatalf("RevokeAPIKey() error = %v", err)
	}
	if err := database.RevokeAPIKey(ctx, key.ID, revokedAt.Add(time.Minute)); err != nil {
		t.Fatalf("second RevokeAPIKey() error = %v", err)
	}
	loadedKey, _, err = database.GetAPIKeyPrincipal(ctx, apiKeyDigest)
	if err != nil {
		t.Fatalf("GetAPIKeyPrincipal() after revocation error = %v", err)
	}
	if loadedKey.RevokedAt == nil || !loadedKey.RevokedAt.Equal(revokedAt) {
		t.Errorf("RevokedAt = %v, want %s", loadedKey.RevokedAt, revokedAt)
	}
	if err := database.TouchAPIKey(ctx, key.ID, revokedAt.Add(time.Minute)); !errors.Is(err, productidentity.ErrCredentialNotFound) {
		t.Errorf("TouchAPIKey() after revocation error = %v, want not found", err)
	}

	sessionDigest := productidentity.CredentialDigest(sha256.Sum256([]byte("web session secret")))
	session, err := database.CreateWebSession(ctx, productidentity.WebSession{
		ID:          uuid.New(),
		PrincipalID: user.ID,
		TokenDigest: sessionDigest,
		CreatedAt:   now,
		ExpiresAt:   now.Add(time.Hour),
		LastSeenAt:  now,
	})
	if err != nil {
		t.Fatalf("CreateWebSession() error = %v", err)
	}
	if session.PrincipalID != user.ID || session.TokenDigest != sessionDigest {
		t.Errorf("created web session = %#v", session)
	}

	loadedSession, sessionPrincipal, err := database.GetWebSessionPrincipal(ctx, sessionDigest)
	if err != nil {
		t.Fatalf("GetWebSessionPrincipal() error = %v", err)
	}
	if loadedSession.ID != session.ID || sessionPrincipal.ID != user.ID {
		t.Errorf("loaded session = %#v, principal = %#v", loadedSession, sessionPrincipal)
	}
	touchedAt := session.LastSeenAt.Add(time.Minute)
	if err := database.TouchWebSession(ctx, session.ID, touchedAt); err != nil {
		t.Fatalf("TouchWebSession() error = %v", err)
	}
	loadedSession, _, err = database.GetWebSessionPrincipal(ctx, sessionDigest)
	if err != nil {
		t.Fatalf("GetWebSessionPrincipal() after touch error = %v", err)
	}
	if !loadedSession.LastSeenAt.Equal(touchedAt) {
		t.Errorf("LastSeenAt = %s, want %s", loadedSession.LastSeenAt, touchedAt)
	}
	if err := database.DeleteWebSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteWebSession() error = %v", err)
	}
	_, _, err = database.GetWebSessionPrincipal(ctx, sessionDigest)
	if !errors.Is(err, productidentity.ErrCredentialNotFound) {
		t.Errorf("deleted session error = %v, want not found", err)
	}

	loadedUser, loadedIdentity, err := database.GetOIDCUser(ctx, identity.Issuer, identity.Subject)
	if err != nil {
		t.Fatalf("GetOIDCUser() error = %v", err)
	}
	loadedUser.DisplayName = "Ada King"
	loadedUser.Role = productidentity.RoleOperator
	loadedUser.UpdatedAt = loadedUser.UpdatedAt.Add(time.Minute)
	preferredUsername := "ada"
	loadedIdentity.PreferredUsername = &preferredUsername
	loadedIdentity.LastAuthenticatedAt = loadedIdentity.LastAuthenticatedAt.Add(time.Minute)
	loadedIdentity.UpdatedAt = loadedIdentity.UpdatedAt.Add(time.Minute)
	updatedUser, updatedIdentity, err := database.UpdateOIDCUser(ctx, loadedUser, loadedIdentity)
	if err != nil {
		t.Fatalf("UpdateOIDCUser() error = %v", err)
	}
	if updatedUser.DisplayName != "Ada King" ||
		updatedUser.Role != productidentity.RoleOperator ||
		updatedIdentity.PreferredUsername == nil ||
		*updatedIdentity.PreferredUsername != preferredUsername {
		t.Errorf("updated user = %#v, identity = %#v", updatedUser, updatedIdentity)
	}

	conflictingPrincipal := productidentity.Principal{
		ID: uuid.New(), Kind: productidentity.KindUser,
		DisplayName: "Grace Hopper", Role: productidentity.RoleViewer,
		CreatedAt: now, UpdatedAt: now,
	}
	conflictingIdentity := productidentity.OIDCIdentity{
		ID: uuid.New(), PrincipalID: conflictingPrincipal.ID,
		Issuer: identity.Issuer, Subject: identity.Subject,
		LastAuthenticatedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	_, _, err = database.CreateOIDCUser(ctx, conflictingPrincipal, conflictingIdentity)
	if !errors.Is(err, productidentity.ErrIdentityConflict) {
		t.Errorf("conflicting CreateOIDCUser() error = %v, want conflict", err)
	}
	_, err = database.GetPrincipal(ctx, conflictingPrincipal.ID)
	if !errors.Is(err, productidentity.ErrPrincipalNotFound) {
		t.Errorf("conflicting principal persisted: error = %v", err)
	}
}
