-- name: CreatePrincipal :one
INSERT INTO principals (
    id,
    kind,
    display_name,
    role,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(kind),
    sqlc.arg(display_name),
    sqlc.arg(role),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
)
RETURNING *;

-- name: GetPrincipal :one
SELECT *
FROM principals
WHERE id = sqlc.arg(id);

-- name: ListPrincipals :many
SELECT *
FROM principals
ORDER BY display_name, id;

-- name: UpdatePrincipalDisabled :one
UPDATE principals
SET disabled_at = CASE
        WHEN sqlc.arg(disabled)::boolean THEN COALESCE(disabled_at, sqlc.arg(changed_at))
        ELSE NULL
    END,
    updated_at = CASE
        WHEN (sqlc.arg(disabled)::boolean AND disabled_at IS NULL)
          OR (NOT sqlc.arg(disabled)::boolean AND disabled_at IS NOT NULL)
        THEN GREATEST(updated_at, sqlc.arg(changed_at))
        ELSE updated_at
    END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateOIDCIdentity :one
INSERT INTO oidc_identities (
    id,
    principal_id,
    issuer,
    subject,
    email,
    preferred_username,
    display_name,
    last_authenticated_at,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(principal_id),
    sqlc.arg(issuer),
    sqlc.arg(subject),
    sqlc.narg(email),
    sqlc.narg(preferred_username),
    sqlc.narg(display_name),
    sqlc.arg(last_authenticated_at),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
)
RETURNING *;

-- name: GetOIDCIdentity :one
SELECT *
FROM oidc_identities
WHERE issuer = sqlc.arg(issuer)
  AND subject = sqlc.arg(subject);

-- name: UpdateOIDCPrincipal :one
UPDATE principals
SET display_name = sqlc.arg(display_name),
    role = sqlc.arg(role),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateOIDCIdentity :one
UPDATE oidc_identities
SET email = sqlc.narg(email),
    preferred_username = sqlc.narg(preferred_username),
    display_name = sqlc.narg(display_name),
    last_authenticated_at = sqlc.arg(last_authenticated_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateAPIKey :one
INSERT INTO api_keys (
    id,
    principal_id,
    created_by_principal_id,
    name,
    token_prefix,
    token_digest,
    scopes,
    created_at,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(principal_id),
    sqlc.arg(created_by_principal_id),
    sqlc.arg(name),
    sqlc.arg(token_prefix),
    sqlc.arg(token_digest),
    sqlc.arg(scopes),
    sqlc.arg(created_at),
    sqlc.narg(expires_at)
)
RETURNING *;

-- name: GetAPIKeyByDigest :one
SELECT *
FROM api_keys
WHERE token_digest = sqlc.arg(token_digest);

-- name: GetAPIKey :one
SELECT *
FROM api_keys
WHERE id = sqlc.arg(id);

-- name: ListAPIKeysByPrincipal :many
SELECT *
FROM api_keys
WHERE principal_id = sqlc.arg(principal_id)
ORDER BY created_at DESC, id DESC;

-- name: TouchAPIKey :execrows
UPDATE api_keys
SET last_used_at = GREATEST(
    COALESCE(last_used_at, created_at),
    sqlc.arg(last_used_at)
)
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > sqlc.arg(last_used_at));

-- name: RevokeAPIKey :execrows
UPDATE api_keys
SET revoked_at = COALESCE(revoked_at, sqlc.arg(revoked_at))
WHERE id = sqlc.arg(id);

-- name: RevokeAPIKeysByPrincipal :execrows
UPDATE api_keys
SET revoked_at = sqlc.arg(revoked_at)
WHERE principal_id = sqlc.arg(principal_id)
  AND revoked_at IS NULL;

-- name: CreateWebSession :one
INSERT INTO web_sessions (
    id,
    principal_id,
    token_digest,
    created_at,
    expires_at,
    last_seen_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(principal_id),
    sqlc.arg(token_digest),
    sqlc.arg(created_at),
    sqlc.arg(expires_at),
    sqlc.arg(last_seen_at)
)
RETURNING *;

-- name: GetWebSessionByDigest :one
SELECT *
FROM web_sessions
WHERE token_digest = sqlc.arg(token_digest);

-- name: TouchWebSession :execrows
UPDATE web_sessions
SET last_seen_at = GREATEST(last_seen_at, sqlc.arg(last_seen_at))
WHERE id = sqlc.arg(id)
  AND expires_at > sqlc.arg(last_seen_at);

-- name: DeleteWebSession :execrows
DELETE FROM web_sessions
WHERE id = sqlc.arg(id);

-- name: DeleteWebSessionsByPrincipal :execrows
DELETE FROM web_sessions
WHERE principal_id = sqlc.arg(principal_id);
