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

	service := productidentity.NewService(database)
	user, err := service.CreatePrincipal(
		ctx,
		productidentity.KindUser,
		"Ada Lovelace",
		productidentity.RoleAdmin,
	)
	if err != nil {
		t.Fatalf("CreatePrincipal() error = %v", err)
	}
	loaded, err := service.GetPrincipal(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetPrincipal() error = %v", err)
	}
	if loaded.Kind != productidentity.KindUser ||
		loaded.DisplayName != "Ada Lovelace" ||
		loaded.Role != productidentity.RoleAdmin {
		t.Errorf("loaded principal = %#v", loaded)
	}

	email := "ada@example.com"
	identity, err := service.CreateOIDCIdentity(
		ctx,
		user.ID,
		"https://identity.example.com",
		"00u-ada",
		&email,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("CreateOIDCIdentity() error = %v", err)
	}
	if identity.PrincipalID != user.ID || identity.Email == nil ||
		*identity.Email != email {
		t.Errorf("created OIDC identity = %#v", identity)
	}
	_, err = service.CreateOIDCIdentity(
		ctx,
		user.ID,
		identity.Issuer,
		identity.Subject,
		nil,
		nil,
		nil,
	)
	if !errors.Is(err, productidentity.ErrIdentityConflict) {
		t.Errorf("duplicate OIDC identity error = %v, want conflict", err)
	}

	serviceAccount, err := service.CreatePrincipal(
		ctx,
		productidentity.KindServiceAccount,
		"Deployment automation",
		productidentity.RoleOperator,
	)
	if err != nil {
		t.Fatalf("create service account: %v", err)
	}

	apiKeyDigest := productidentity.CredentialDigest(
		sha256.Sum256([]byte("API key secret")),
	)
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	key, err := service.CreateAPIKey(
		ctx,
		serviceAccount.ID,
		user.ID,
		"deployments",
		"srk_deployments",
		apiKeyDigest,
		[]productidentity.Scope{
			productidentity.ScopeRunsWrite,
			productidentity.ScopeRead,
		},
		&expiresAt,
	)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if key.PrincipalID != serviceAccount.ID ||
		key.CreatedByPrincipalID != user.ID ||
		len(key.Scopes) != 2 ||
		key.Scopes[0] != productidentity.ScopeRead {
		t.Errorf("created API key = %#v", key)
	}

	sessionDigest := productidentity.CredentialDigest(
		sha256.Sum256([]byte("web session secret")),
	)
	session, err := service.CreateWebSession(
		ctx,
		user.ID,
		sessionDigest,
		time.Now().UTC().Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWebSession() error = %v", err)
	}
	if session.PrincipalID != user.ID || session.TokenDigest != sessionDigest {
		t.Errorf("created web session = %#v", session)
	}

	loadedSession, sessionPrincipal, err := database.GetWebSessionPrincipal(
		ctx,
		sessionDigest,
	)
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

	loadedUser, loadedIdentity, err := database.GetOIDCUser(
		ctx,
		identity.Issuer,
		identity.Subject,
	)
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
	updatedUser, updatedIdentity, err := database.UpdateOIDCUser(
		ctx,
		loadedUser,
		loadedIdentity,
	)
	if err != nil {
		t.Fatalf("UpdateOIDCUser() error = %v", err)
	}
	if updatedUser.DisplayName != "Ada King" ||
		updatedUser.Role != productidentity.RoleOperator ||
		updatedIdentity.PreferredUsername == nil ||
		*updatedIdentity.PreferredUsername != preferredUsername {
		t.Errorf("updated user = %#v, identity = %#v", updatedUser, updatedIdentity)
	}

	now := time.Now().UTC()
	newPrincipal := productidentity.Principal{
		ID: uuid.New(), Kind: productidentity.KindUser,
		DisplayName: "Grace Hopper", Role: productidentity.RoleViewer,
		CreatedAt: now, UpdatedAt: now,
	}
	newIdentity := productidentity.OIDCIdentity{
		ID: uuid.New(), PrincipalID: newPrincipal.ID,
		Issuer: identity.Issuer, Subject: "00u-grace",
		LastAuthenticatedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	createdPrincipal, createdIdentity, err := database.CreateOIDCUser(
		ctx,
		newPrincipal,
		newIdentity,
	)
	if err != nil {
		t.Fatalf("CreateOIDCUser() error = %v", err)
	}
	if createdPrincipal.ID != newPrincipal.ID || createdIdentity.ID != newIdentity.ID {
		t.Errorf("created principal = %#v, identity = %#v", createdPrincipal, createdIdentity)
	}

	conflictingPrincipal := newPrincipal
	conflictingPrincipal.ID = uuid.New()
	conflictingIdentity := newIdentity
	conflictingIdentity.ID = uuid.New()
	conflictingIdentity.PrincipalID = conflictingPrincipal.ID
	_, _, err = database.CreateOIDCUser(ctx, conflictingPrincipal, conflictingIdentity)
	if !errors.Is(err, productidentity.ErrIdentityConflict) {
		t.Errorf("conflicting CreateOIDCUser() error = %v, want conflict", err)
	}
	_, err = database.GetPrincipal(ctx, conflictingPrincipal.ID)
	if !errors.Is(err, productidentity.ErrPrincipalNotFound) {
		t.Errorf("conflicting principal persisted: error = %v", err)
	}
}
