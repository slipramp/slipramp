package productidentity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreatePrincipal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	id := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500")
	store := &identityStore{}
	service := NewService(store)
	service.now = func() time.Time { return now }
	service.newID = func() uuid.UUID { return id }

	created, err := service.CreatePrincipal(
		context.Background(),
		KindUser,
		"Ada Lovelace",
		RoleAdmin,
	)
	if err != nil {
		t.Fatalf("CreatePrincipal() error = %v", err)
	}
	if created.ID != id || created.Kind != KindUser || created.Role != RoleAdmin ||
		created.DisplayName != "Ada Lovelace" || !created.CreatedAt.Equal(now) ||
		!created.UpdatedAt.Equal(now) {
		t.Errorf("created principal = %#v", created)
	}
}

func TestCreateOIDCIdentityRequiresUser(t *testing.T) {
	t.Parallel()

	principalID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500")
	service := NewService(&identityStore{principal: Principal{
		ID: principalID, Kind: KindServiceAccount,
	}})

	_, err := service.CreateOIDCIdentity(
		context.Background(),
		principalID,
		"https://identity.example.com",
		"subject",
		nil,
		nil,
		nil,
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("CreateOIDCIdentity() error = %v, want invalid identity", err)
	}
}

func TestCreateAPIKeyNormalizesScopes(t *testing.T) {
	t.Parallel()

	principalID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500")
	creatorID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501")
	store := &identityStore{}
	service := NewService(store)
	service.now = func() time.Time {
		return time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	}
	service.newID = func() uuid.UUID {
		return uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1502")
	}

	created, err := service.CreateAPIKey(
		context.Background(),
		principalID,
		creatorID,
		"deployment",
		"srk_12345678",
		CredentialDigest{1},
		[]Scope{ScopeRunsWrite, ScopeRead},
		nil,
	)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if len(created.Scopes) != 2 || created.Scopes[0] != ScopeRead ||
		created.Scopes[1] != ScopeRunsWrite {
		t.Errorf("API key scopes = %v", created.Scopes)
	}
}

func TestWebSessionRejectsServiceAccount(t *testing.T) {
	t.Parallel()

	principalID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500")
	service := NewService(&identityStore{principal: Principal{
		ID: principalID, Kind: KindServiceAccount,
	}})

	_, err := service.CreateWebSession(
		context.Background(),
		principalID,
		CredentialDigest{1},
		time.Now().Add(time.Hour),
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("CreateWebSession() error = %v, want invalid identity", err)
	}
}

type identityStore struct {
	principal Principal
}

func (s *identityStore) CreatePrincipal(
	_ context.Context,
	principal Principal,
) (Principal, error) {
	s.principal = principal

	return principal, nil
}

func (s *identityStore) GetPrincipal(
	_ context.Context,
	_ uuid.UUID,
) (Principal, error) {
	if s.principal.ID == uuid.Nil {
		return Principal{}, ErrPrincipalNotFound
	}

	return s.principal, nil
}

func (s *identityStore) CreateOIDCIdentity(
	_ context.Context,
	identity OIDCIdentity,
) (OIDCIdentity, error) {
	return identity, nil
}

func (s *identityStore) CreateAPIKey(
	_ context.Context,
	key APIKey,
) (APIKey, error) {
	return key, nil
}

func (s *identityStore) CreateWebSession(
	_ context.Context,
	session WebSession,
) (WebSession, error) {
	return session, nil
}
