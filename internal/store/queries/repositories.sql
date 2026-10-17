-- name: CreateRepository :one
INSERT INTO repositories (
    id,
    name,
    clone_url,
    branch,
    refresh_credential_name,
    checkout_credential_name,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(name),
    sqlc.arg(clone_url),
    sqlc.arg(branch),
    sqlc.narg(refresh_credential_name),
    sqlc.narg(checkout_credential_name),
    sqlc.arg(created_at)
)
RETURNING
    id,
    name,
    clone_url,
    branch,
    refresh_credential_name,
    checkout_credential_name,
    created_at,
    refresh_status,
    observed_commit_sha,
    last_refresh_attempted_at,
    last_refresh_succeeded_at,
    last_refresh_error;

-- name: ListRepositories :many
SELECT
    id,
    name,
    clone_url,
    branch,
    refresh_credential_name,
    checkout_credential_name,
    created_at,
    refresh_status,
    observed_commit_sha,
    last_refresh_attempted_at,
    last_refresh_succeeded_at,
    last_refresh_error
FROM repositories
WHERE removed_at IS NULL
ORDER BY name;

-- name: RecordRepositoryRefreshSuccess :execrows
UPDATE repositories
SET refresh_status = 'succeeded',
    observed_commit_sha = sqlc.arg(observed_commit_sha),
    last_refresh_attempted_at = sqlc.arg(attempted_at),
    last_refresh_succeeded_at = sqlc.arg(attempted_at),
    last_refresh_error = NULL
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;

-- name: RecordRepositoryRefreshFailure :execrows
UPDATE repositories
SET refresh_status = 'failed',
    last_refresh_attempted_at = sqlc.arg(attempted_at),
    last_refresh_error = sqlc.arg(refresh_error)
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;

-- name: RemoveRepository :execrows
UPDATE repositories
SET removed_at = sqlc.arg(removed_at)
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;
