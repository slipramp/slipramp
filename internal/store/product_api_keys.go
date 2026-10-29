package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const (
	apiKeyTokenPrefixConstraint = "api_keys_token_prefix_key"
	apiKeyTokenDigestConstraint = "api_keys_token_digest_key"
)

func (d *Database) CreateAPIKey(ctx context.Context, record productidentity.APIKey) (productidentity.APIKey, error) {
	row, err := d.queries.CreateAPIKey(
		ctx,
		storedb.CreateAPIKeyParams{
			ID:                   databaseUUID(record.ID),
			PrincipalID:          databaseUUID(record.PrincipalID),
			CreatedByPrincipalID: databaseUUID(record.CreatedByPrincipalID),
			Name:                 record.Name,
			TokenPrefix:          record.TokenPrefix,
			TokenDigest:          record.TokenDigest[:],
			Scopes:               apiKeyScopes(record.Scopes),
			CreatedAt:            databaseTime(record.CreatedAt),
			ExpiresAt:            optionalDatabaseTime(record.ExpiresAt),
		},
	)
	if constraintMatches(err, apiKeyTokenPrefixConstraint, apiKeyTokenDigestConstraint) {
		return productidentity.APIKey{}, productidentity.ErrCredentialConflict
	}
	if err != nil {
		return productidentity.APIKey{}, fmt.Errorf("insert API key: %w", err)
	}

	return apiKeyFromRow(row)
}

func (d *Database) GetAPIKey(ctx context.Context, keyID uuid.UUID) (productidentity.APIKey, error) {
	row, err := d.queries.GetAPIKey(ctx, databaseUUID(keyID))
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.APIKey{}, productidentity.ErrCredentialNotFound
	}
	if err != nil {
		return productidentity.APIKey{}, fmt.Errorf("get API key: %w", err)
	}

	return apiKeyFromRow(row)
}

func (d *Database) ListAPIKeys(ctx context.Context, principalID uuid.UUID) ([]productidentity.APIKey, error) {
	rows, err := d.queries.ListAPIKeysByPrincipal(ctx, databaseUUID(principalID))
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}

	keys := make([]productidentity.APIKey, 0, len(rows))
	for _, row := range rows {
		key, decodeErr := apiKeyFromRow(row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		keys = append(keys, key)
	}

	return keys, nil
}

func (d *Database) GetAPIKeyPrincipal(
	ctx context.Context,
	tokenDigest productidentity.CredentialDigest,
) (productidentity.APIKey, productidentity.Principal, error) {
	row, err := d.queries.GetAPIKeyByDigest(ctx, tokenDigest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.APIKey{}, productidentity.Principal{},
			productidentity.ErrCredentialNotFound
	}
	if err != nil {
		return productidentity.APIKey{}, productidentity.Principal{},
			fmt.Errorf("get API key: %w", err)
	}

	key, err := apiKeyFromRow(row)
	if err != nil {
		return productidentity.APIKey{}, productidentity.Principal{}, err
	}
	principal, err := d.GetPrincipal(ctx, key.PrincipalID)
	if err != nil {
		return productidentity.APIKey{}, productidentity.Principal{}, err
	}

	return key, principal, nil
}

func (d *Database) TouchAPIKey(ctx context.Context, keyID uuid.UUID, lastUsedAt time.Time) error {
	rowsAffected, err := d.queries.TouchAPIKey(
		ctx,
		storedb.TouchAPIKeyParams{
			LastUsedAt: databaseTime(lastUsedAt),
			ID:         databaseUUID(keyID),
		},
	)
	if err != nil {
		return fmt.Errorf("touch API key: %w", err)
	}
	if rowsAffected != 1 {
		return productidentity.ErrCredentialNotFound
	}

	return nil
}

func (d *Database) RevokeAPIKey(ctx context.Context, keyID uuid.UUID, revokedAt time.Time) error {
	rowsAffected, err := d.queries.RevokeAPIKey(
		ctx,
		storedb.RevokeAPIKeyParams{
			RevokedAt: databaseTime(revokedAt),
			ID:        databaseUUID(keyID),
		},
	)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	if rowsAffected != 1 {
		return productidentity.ErrCredentialNotFound
	}

	return nil
}

func (d *Database) RevokeAPIKeysByPrincipal(
	ctx context.Context,
	principalID uuid.UUID,
	revokedAt time.Time,
) error {
	_, err := d.queries.RevokeAPIKeysByPrincipal(
		ctx,
		storedb.RevokeAPIKeysByPrincipalParams{
			RevokedAt:   databaseTime(revokedAt),
			PrincipalID: databaseUUID(principalID),
		},
	)
	if err != nil {
		return fmt.Errorf("revoke principal API keys: %w", err)
	}

	return nil
}

func apiKeyFromRow(row storedb.ApiKey) (productidentity.APIKey, error) {
	digest, err := credentialDigest(row.TokenDigest)
	if err != nil {
		return productidentity.APIKey{}, fmt.Errorf("decode API key digest: %w", err)
	}

	scopes := make([]productidentity.Scope, 0, len(row.Scopes))
	for _, scope := range row.Scopes {
		scopes = append(scopes, productidentity.Scope(scope))
	}

	return productidentity.APIKey{
		ID:                   uuid.UUID(row.ID.Bytes),
		PrincipalID:          uuid.UUID(row.PrincipalID.Bytes),
		CreatedByPrincipalID: uuid.UUID(row.CreatedByPrincipalID.Bytes),
		Name:                 row.Name,
		TokenPrefix:          row.TokenPrefix,
		TokenDigest:          digest,
		Scopes:               scopes,
		CreatedAt:            row.CreatedAt.Time.UTC(),
		ExpiresAt:            applicationTime(row.ExpiresAt),
		LastUsedAt:           applicationTime(row.LastUsedAt),
		RevokedAt:            applicationTime(row.RevokedAt),
	}, nil
}

func apiKeyScopes(scopes []productidentity.Scope) []string {
	values := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		values = append(values, string(scope))
	}

	return values
}
