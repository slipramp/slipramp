package productauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	apiKeyTokenMarker         = "slipramp_key_"
	apiKeyDisplaySecretLength = 12
)

type APIKeyCredential struct {
	Key   productidentity.APIKey
	Token string
}

type accessStore interface {
	CreatePrincipal(context.Context, productidentity.Principal) (productidentity.Principal, error)
	GetPrincipal(context.Context, uuid.UUID) (productidentity.Principal, error)
	ListServiceAccounts(context.Context) ([]productidentity.Principal, error)
	CreateAPIKey(context.Context, productidentity.APIKey) (productidentity.APIKey, error)
	GetAPIKey(context.Context, uuid.UUID) (productidentity.APIKey, error)
	GetAPIKeyPrincipal(
		context.Context,
		productidentity.CredentialDigest,
	) (productidentity.APIKey, productidentity.Principal, error)
	ListAPIKeys(context.Context, uuid.UUID) ([]productidentity.APIKey, error)
	TouchAPIKey(context.Context, uuid.UUID, time.Time) error
	RevokeAPIKey(context.Context, uuid.UUID, time.Time) error
}

func (s *Service) ListOwnAPIKeys(
	ctx context.Context,
	authentication productidentity.Authentication,
) ([]productidentity.APIKey, error) {
	if err := authentication.Validate(); err != nil ||
		!authentication.Authorizes(productidentity.PermissionRead) {
		return nil, ErrForbidden
	}

	return s.listAPIKeys(ctx, authentication.Principal.ID)
}

func (s *Service) listAPIKeys(ctx context.Context, principalID uuid.UUID) ([]productidentity.APIKey, error) {
	if principalID == uuid.Nil {
		return nil, fmt.Errorf("%w: principal ID is required", productidentity.ErrInvalid)
	}

	keys, err := s.store.ListAPIKeys(ctx, principalID)
	if err != nil {
		return nil, fmt.Errorf("list product API keys: %w", err)
	}

	return keys, nil
}

func (s *Service) CreateUserAPIKey(
	ctx context.Context,
	authentication productidentity.Authentication,
	name string,
	scopes []productidentity.Scope,
	expiresAt *time.Time,
) (APIKeyCredential, error) {
	if err := authentication.Validate(); err != nil ||
		!authentication.Authorizes(productidentity.PermissionRead) ||
		authentication.Method != productidentity.AuthenticationMethodWebSession ||
		authentication.Principal.Kind != productidentity.KindUser {
		return APIKeyCredential{}, ErrForbidden
	}

	return s.createAPIKey(ctx, authentication.Principal.ID, authentication.Principal.ID, name, scopes, expiresAt)
}

func (s *Service) ListServiceAccounts(ctx context.Context) ([]productidentity.Principal, error) {
	principals, err := s.store.ListServiceAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list product service accounts: %w", err)
	}

	return principals, nil
}

func (s *Service) CreateServiceAccount(
	ctx context.Context,
	displayName string,
	role productidentity.Role,
) (productidentity.Principal, error) {
	now := s.now().UTC()
	principal := productidentity.Principal{
		ID:          s.newID(),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: displayName,
		Role:        role,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := principal.Validate(); err != nil {
		return productidentity.Principal{}, err
	}

	created, err := s.store.CreatePrincipal(ctx, principal)
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("create product service account: %w", err)
	}

	return created, nil
}

func (s *Service) ListServiceAccountAPIKeys(
	ctx context.Context,
	principalID uuid.UUID,
) ([]productidentity.APIKey, error) {
	if _, err := s.serviceAccount(ctx, principalID); err != nil {
		return nil, err
	}

	return s.listAPIKeys(ctx, principalID)
}

func (s *Service) CreateServiceAccountAPIKey(
	ctx context.Context,
	principalID uuid.UUID,
	createdByPrincipalID uuid.UUID,
	name string,
	scopes []productidentity.Scope,
	expiresAt *time.Time,
) (APIKeyCredential, error) {
	if _, err := s.serviceAccount(ctx, principalID); err != nil {
		return APIKeyCredential{}, err
	}

	return s.createAPIKey(ctx, principalID, createdByPrincipalID, name, scopes, expiresAt)
}

func (s *Service) RevokeAPIKey(
	ctx context.Context,
	authentication productidentity.Authentication,
	keyID uuid.UUID,
) error {
	if keyID == uuid.Nil {
		return fmt.Errorf("%w: API key ID is required", productidentity.ErrInvalid)
	}
	if err := authentication.Validate(); err != nil ||
		!authentication.Authorizes(productidentity.PermissionAPIKeyRevoke) {
		return ErrForbidden
	}

	key, err := s.store.GetAPIKey(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get product API key: %w", err)
	}
	if key.PrincipalID != authentication.Principal.ID &&
		!authentication.Authorizes(productidentity.PermissionAdmin) {
		return ErrForbidden
	}

	if err := s.store.RevokeAPIKey(ctx, keyID, s.now().UTC()); err != nil {
		return fmt.Errorf("revoke product API key: %w", err)
	}

	return nil
}

func (s *Service) createAPIKey(
	ctx context.Context,
	principalID uuid.UUID,
	createdByPrincipalID uuid.UUID,
	name string,
	scopes []productidentity.Scope,
	expiresAt *time.Time,
) (APIKeyCredential, error) {
	for range maximumCreateTries {
		secret, err := s.randomSecret()
		if err != nil {
			return APIKeyCredential{}, fmt.Errorf("generate product API key: %w", err)
		}
		token := apiKeyTokenMarker + secret
		digest, err := apiKeyDigest(token)
		if err != nil {
			return APIKeyCredential{}, fmt.Errorf("digest product API key: %w", err)
		}

		now := s.now().UTC()
		record := productidentity.APIKey{
			ID:                   s.newID(),
			PrincipalID:          principalID,
			CreatedByPrincipalID: createdByPrincipalID,
			Name:                 name,
			TokenPrefix:          apiKeyTokenMarker + secret[:apiKeyDisplaySecretLength],
			TokenDigest:          digest,
			Scopes:               normalizedAPIKeyScopes(scopes),
			CreatedAt:            now,
			ExpiresAt:            cloneTime(expiresAt),
		}
		if err := record.Validate(); err != nil {
			return APIKeyCredential{}, err
		}

		created, err := s.store.CreateAPIKey(ctx, record)
		if errors.Is(err, productidentity.ErrCredentialConflict) {
			continue
		}
		if err != nil {
			return APIKeyCredential{}, fmt.Errorf("create product API key: %w", err)
		}

		return APIKeyCredential{Key: created, Token: token}, nil
	}

	return APIKeyCredential{}, productidentity.ErrCredentialConflict
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, token string) (productidentity.Authentication, error) {
	digest, err := apiKeyDigest(token)
	if err != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}

	key, principal, err := s.store.GetAPIKeyPrincipal(ctx, digest)
	if errors.Is(err, productidentity.ErrCredentialNotFound) {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}
	if err != nil {
		return productidentity.Authentication{}, fmt.Errorf("get product API key: %w", err)
	}

	now := s.now().UTC()
	if key.RevokedAt != nil ||
		(key.ExpiresAt != nil && !now.Before(*key.ExpiresAt)) ||
		principal.DisabledAt != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}
	if err := s.store.TouchAPIKey(ctx, key.ID, now); err != nil {
		if errors.Is(err, productidentity.ErrCredentialNotFound) {
			return productidentity.Authentication{}, productidentity.ErrUnauthenticated
		}

		return productidentity.Authentication{}, fmt.Errorf("touch product API key: %w", err)
	}

	authentication := productidentity.Authentication{
		Principal:       principal,
		Method:          productidentity.AuthenticationMethodAPIKey,
		CredentialID:    key.ID,
		CredentialLabel: key.Name,
		Scopes:          slices.Clone(key.Scopes),
	}
	if err := authentication.Validate(); err != nil {
		return productidentity.Authentication{}, fmt.Errorf("validate product API key authentication: %w", err)
	}

	return authentication, nil
}

func (s *Service) serviceAccount(ctx context.Context, principalID uuid.UUID) (productidentity.Principal, error) {
	if principalID == uuid.Nil {
		return productidentity.Principal{}, fmt.Errorf("%w: principal ID is required", productidentity.ErrInvalid)
	}

	principal, err := s.store.GetPrincipal(ctx, principalID)
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("get product service account: %w", err)
	}
	if principal.Kind != productidentity.KindServiceAccount {
		return productidentity.Principal{}, productidentity.ErrPrincipalNotFound
	}

	return principal, nil
}

func apiKeyDigest(token string) (productidentity.CredentialDigest, error) {
	secret, found := strings.CutPrefix(token, apiKeyTokenMarker)
	if !found {
		return productidentity.CredentialDigest{}, errors.New("invalid API key prefix")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(decoded) != randomSecretBytes {
		return productidentity.CredentialDigest{}, errors.New("invalid API key secret")
	}

	return productidentity.CredentialDigest(sha256.Sum256([]byte(token))), nil
}

func normalizedAPIKeyScopes(scopes []productidentity.Scope) []productidentity.Scope {
	result := slices.Clone(scopes)
	slices.Sort(result)

	return result
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := value.UTC()

	return &cloned
}
