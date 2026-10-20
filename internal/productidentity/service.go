package productidentity

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Store interface {
	CreatePrincipal(context.Context, Principal) (Principal, error)
	GetPrincipal(context.Context, uuid.UUID) (Principal, error)
	CreateOIDCIdentity(context.Context, OIDCIdentity) (OIDCIdentity, error)
	CreateAPIKey(context.Context, APIKey) (APIKey, error)
	CreateWebSession(context.Context, WebSession) (WebSession, error)
}

type Service struct {
	identities Store
	newID      func() uuid.UUID
	now        func() time.Time
}

func NewService(identities Store) *Service {
	return &Service{
		identities: identities,
		newID:      uuid.New,
		now:        time.Now,
	}
}

func (s *Service) CreatePrincipal(
	ctx context.Context,
	kind Kind,
	displayName string,
	role Role,
) (Principal, error) {
	now := s.now().UTC()
	record := Principal{
		ID:          s.newID(),
		Kind:        kind,
		DisplayName: displayName,
		Role:        role,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := record.Validate(); err != nil {
		return Principal{}, err
	}

	created, err := s.identities.CreatePrincipal(ctx, record)
	if err != nil {
		return Principal{}, fmt.Errorf("create product principal: %w", err)
	}

	return created, nil
}

func (s *Service) GetPrincipal(
	ctx context.Context,
	principalID uuid.UUID,
) (Principal, error) {
	if principalID == uuid.Nil {
		return Principal{}, fmt.Errorf("%w: principal ID is required", ErrInvalid)
	}

	return s.identities.GetPrincipal(ctx, principalID)
}

func (s *Service) CreateOIDCIdentity(
	ctx context.Context,
	principalID uuid.UUID,
	issuer string,
	subject string,
	email *string,
	preferredUsername *string,
	displayName *string,
) (OIDCIdentity, error) {
	principal, err := s.identities.GetPrincipal(ctx, principalID)
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("get OIDC principal: %w", err)
	}
	if principal.Kind != KindUser {
		return OIDCIdentity{}, fmt.Errorf("%w: OIDC identities require a user principal", ErrInvalid)
	}

	now := s.now().UTC()
	record := OIDCIdentity{
		ID:                  s.newID(),
		PrincipalID:         principalID,
		Issuer:              issuer,
		Subject:             subject,
		Email:               cloneString(email),
		PreferredUsername:   cloneString(preferredUsername),
		DisplayName:         cloneString(displayName),
		LastAuthenticatedAt: now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := record.Validate(); err != nil {
		return OIDCIdentity{}, err
	}

	created, err := s.identities.CreateOIDCIdentity(ctx, record)
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("create OIDC identity: %w", err)
	}

	return created, nil
}

func (s *Service) CreateAPIKey(
	ctx context.Context,
	principalID uuid.UUID,
	createdByPrincipalID uuid.UUID,
	name string,
	tokenPrefix string,
	tokenDigest CredentialDigest,
	scopes []Scope,
	expiresAt *time.Time,
) (APIKey, error) {
	now := s.now().UTC()
	record := APIKey{
		ID:                   s.newID(),
		PrincipalID:          principalID,
		CreatedByPrincipalID: createdByPrincipalID,
		Name:                 name,
		TokenPrefix:          tokenPrefix,
		TokenDigest:          tokenDigest,
		Scopes:               normalizedScopes(scopes),
		CreatedAt:            now,
		ExpiresAt:            cloneTime(expiresAt),
	}
	if err := record.Validate(); err != nil {
		return APIKey{}, err
	}

	created, err := s.identities.CreateAPIKey(ctx, record)
	if err != nil {
		return APIKey{}, fmt.Errorf("create API key metadata: %w", err)
	}

	return created, nil
}

func (s *Service) CreateWebSession(
	ctx context.Context,
	principalID uuid.UUID,
	tokenDigest CredentialDigest,
	expiresAt time.Time,
) (WebSession, error) {
	principal, err := s.identities.GetPrincipal(ctx, principalID)
	if err != nil {
		return WebSession{}, fmt.Errorf("get web session principal: %w", err)
	}
	if principal.Kind != KindUser {
		return WebSession{}, fmt.Errorf("%w: web sessions require a user principal", ErrInvalid)
	}

	now := s.now().UTC()
	record := WebSession{
		ID:          s.newID(),
		PrincipalID: principalID,
		TokenDigest: tokenDigest,
		CreatedAt:   now,
		ExpiresAt:   expiresAt.UTC(),
		LastSeenAt:  now,
	}
	if err := record.Validate(); err != nil {
		return WebSession{}, err
	}

	created, err := s.identities.CreateWebSession(ctx, record)
	if err != nil {
		return WebSession{}, fmt.Errorf("create web session metadata: %w", err)
	}

	return created, nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := value.UTC()

	return &cloned
}
