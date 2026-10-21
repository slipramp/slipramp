package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) CreatePrincipal(
	ctx context.Context,
	record productidentity.Principal,
) (productidentity.Principal, error) {
	row, err := d.queries.CreatePrincipal(
		ctx,
		storedb.CreatePrincipalParams{
			ID:          databaseUUID(record.ID),
			Kind:        string(record.Kind),
			DisplayName: record.DisplayName,
			Role:        string(record.Role),
			CreatedAt:   databaseTime(record.CreatedAt),
			UpdatedAt:   databaseTime(record.UpdatedAt),
		},
	)
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("insert product principal: %w", err)
	}

	return principalFromRow(row), nil
}

func (d *Database) GetPrincipal(ctx context.Context, principalID uuid.UUID) (productidentity.Principal, error) {
	row, err := d.queries.GetPrincipal(ctx, databaseUUID(principalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.ErrPrincipalNotFound
	}
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("get product principal: %w", err)
	}

	return principalFromRow(row), nil
}

func (d *Database) ListServiceAccounts(ctx context.Context) ([]productidentity.Principal, error) {
	rows, err := d.queries.ListServiceAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list product service accounts: %w", err)
	}

	principals := make([]productidentity.Principal, 0, len(rows))
	for _, row := range rows {
		principals = append(principals, principalFromRow(row))
	}

	return principals, nil
}

func (d *Database) GetOIDCUser(
	ctx context.Context,
	issuer string,
	subject string,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	row, err := d.queries.GetOIDCIdentity(ctx, storedb.GetOIDCIdentityParams{Issuer: issuer, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityNotFound
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("get OIDC identity: %w", err)
	}

	principal, err := d.GetPrincipal(ctx, uuid.UUID(row.PrincipalID.Bytes))
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{}, err
	}

	return principal, oidcIdentityFromRow(row), nil
}

func (d *Database) CreateOIDCUser(
	ctx context.Context,
	principal productidentity.Principal,
	identity productidentity.OIDCIdentity,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("begin OIDC user transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	principalRow, err := queries.CreatePrincipal(
		ctx,
		storedb.CreatePrincipalParams{
			ID:          databaseUUID(principal.ID),
			Kind:        string(principal.Kind),
			DisplayName: principal.DisplayName,
			Role:        string(principal.Role),
			CreatedAt:   databaseTime(principal.CreatedAt),
			UpdatedAt:   databaseTime(principal.UpdatedAt),
		},
	)
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("insert OIDC principal: %w", err)
	}

	identityRow, err := queries.CreateOIDCIdentity(
		ctx,
		storedb.CreateOIDCIdentityParams{
			ID:                  databaseUUID(identity.ID),
			PrincipalID:         databaseUUID(identity.PrincipalID),
			Issuer:              identity.Issuer,
			Subject:             identity.Subject,
			Email:               identity.Email,
			PreferredUsername:   identity.PreferredUsername,
			DisplayName:         identity.DisplayName,
			LastAuthenticatedAt: databaseTime(identity.LastAuthenticatedAt),
			CreatedAt:           databaseTime(identity.CreatedAt),
			UpdatedAt:           databaseTime(identity.UpdatedAt),
		},
	)
	if identityConflict(err) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityConflict
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("insert OIDC identity: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("commit OIDC user transaction: %w", err)
	}

	return principalFromRow(principalRow), oidcIdentityFromRow(identityRow), nil
}

func (d *Database) UpdateOIDCUser(
	ctx context.Context,
	principal productidentity.Principal,
	identity productidentity.OIDCIdentity,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("begin OIDC user update: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	principalRow, err := queries.UpdateOIDCPrincipal(
		ctx,
		storedb.UpdateOIDCPrincipalParams{
			DisplayName: principal.DisplayName,
			Role:        string(principal.Role),
			UpdatedAt:   databaseTime(principal.UpdatedAt),
			ID:          databaseUUID(principal.ID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityNotFound
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("update OIDC principal: %w", err)
	}

	identityRow, err := queries.UpdateOIDCIdentity(
		ctx,
		storedb.UpdateOIDCIdentityParams{
			Email:               identity.Email,
			PreferredUsername:   identity.PreferredUsername,
			DisplayName:         identity.DisplayName,
			LastAuthenticatedAt: databaseTime(identity.LastAuthenticatedAt),
			UpdatedAt:           databaseTime(identity.UpdatedAt),
			ID:                  databaseUUID(identity.ID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityNotFound
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("update OIDC identity: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("commit OIDC user update: %w", err)
	}

	return principalFromRow(principalRow), oidcIdentityFromRow(identityRow), nil
}

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
	if credentialConflict(err) {
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

func (d *Database) CreateWebSession(
	ctx context.Context,
	record productidentity.WebSession,
) (productidentity.WebSession, error) {
	row, err := d.queries.CreateWebSession(
		ctx,
		storedb.CreateWebSessionParams{
			ID:          databaseUUID(record.ID),
			PrincipalID: databaseUUID(record.PrincipalID),
			TokenDigest: record.TokenDigest[:],
			CreatedAt:   databaseTime(record.CreatedAt),
			ExpiresAt:   databaseTime(record.ExpiresAt),
			LastSeenAt:  databaseTime(record.LastSeenAt),
		},
	)
	if credentialConflict(err) {
		return productidentity.WebSession{}, productidentity.ErrCredentialConflict
	}
	if err != nil {
		return productidentity.WebSession{}, fmt.Errorf("insert web session: %w", err)
	}

	return webSessionFromRow(row)
}

func (d *Database) GetWebSessionPrincipal(
	ctx context.Context,
	tokenDigest productidentity.CredentialDigest,
) (productidentity.WebSession, productidentity.Principal, error) {
	row, err := d.queries.GetWebSessionByDigest(ctx, tokenDigest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.WebSession{}, productidentity.Principal{},
			productidentity.ErrCredentialNotFound
	}
	if err != nil {
		return productidentity.WebSession{}, productidentity.Principal{},
			fmt.Errorf("get web session: %w", err)
	}

	session, err := webSessionFromRow(row)
	if err != nil {
		return productidentity.WebSession{}, productidentity.Principal{}, err
	}
	principal, err := d.GetPrincipal(ctx, session.PrincipalID)
	if err != nil {
		return productidentity.WebSession{}, productidentity.Principal{}, err
	}

	return session, principal, nil
}

func (d *Database) TouchWebSession(ctx context.Context, sessionID uuid.UUID, lastSeenAt time.Time) error {
	rowsAffected, err := d.queries.TouchWebSession(
		ctx,
		storedb.TouchWebSessionParams{
			LastSeenAt: databaseTime(lastSeenAt),
			ID:         databaseUUID(sessionID),
		},
	)
	if err != nil {
		return fmt.Errorf("touch web session: %w", err)
	}
	if rowsAffected != 1 {
		return productidentity.ErrCredentialNotFound
	}

	return nil
}

func (d *Database) DeleteWebSession(ctx context.Context, sessionID uuid.UUID) error {
	rowsAffected, err := d.queries.DeleteWebSession(ctx, databaseUUID(sessionID))
	if err != nil {
		return fmt.Errorf("delete web session: %w", err)
	}
	if rowsAffected != 1 {
		return productidentity.ErrCredentialNotFound
	}

	return nil
}

func principalFromRow(row storedb.Principal) productidentity.Principal {
	return productidentity.Principal{
		ID:          uuid.UUID(row.ID.Bytes),
		Kind:        productidentity.Kind(row.Kind),
		DisplayName: row.DisplayName,
		Role:        productidentity.Role(row.Role),
		DisabledAt:  applicationTime(row.DisabledAt),
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

func oidcIdentityFromRow(row storedb.OidcIdentity) productidentity.OIDCIdentity {
	return productidentity.OIDCIdentity{
		ID:                  uuid.UUID(row.ID.Bytes),
		PrincipalID:         uuid.UUID(row.PrincipalID.Bytes),
		Issuer:              row.Issuer,
		Subject:             row.Subject,
		Email:               row.Email,
		PreferredUsername:   row.PreferredUsername,
		DisplayName:         row.DisplayName,
		LastAuthenticatedAt: row.LastAuthenticatedAt.Time.UTC(),
		CreatedAt:           row.CreatedAt.Time.UTC(),
		UpdatedAt:           row.UpdatedAt.Time.UTC(),
	}
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

func webSessionFromRow(row storedb.WebSession) (productidentity.WebSession, error) {
	digest, err := credentialDigest(row.TokenDigest)
	if err != nil {
		return productidentity.WebSession{}, fmt.Errorf("decode web session digest: %w", err)
	}

	return productidentity.WebSession{
		ID:          uuid.UUID(row.ID.Bytes),
		PrincipalID: uuid.UUID(row.PrincipalID.Bytes),
		TokenDigest: digest,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		ExpiresAt:   row.ExpiresAt.Time.UTC(),
		LastSeenAt:  row.LastSeenAt.Time.UTC(),
	}, nil
}

func credentialDigest(value []byte) (productidentity.CredentialDigest, error) {
	var digest productidentity.CredentialDigest
	if len(value) != len(digest) {
		return digest, fmt.Errorf("digest has %d bytes, want %d", len(value), len(digest))
	}

	copy(digest[:], value)

	return digest, nil
}

func identityConflict(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}

	return postgresError.ConstraintName == "oidc_identities_issuer_subject_key"
}

func credentialConflict(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}

	return postgresError.ConstraintName == "api_keys_token_prefix_key" ||
		postgresError.ConstraintName == "api_keys_token_digest_key" ||
		postgresError.ConstraintName == "web_sessions_token_digest_key"
}
