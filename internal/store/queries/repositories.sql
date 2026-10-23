-- name: CreateRepository :one
INSERT INTO repositories (
    id,
    name,
    clone_url,
    branch,
    commit_indexing_mode,
    commit_url_template,
    refresh_credential_name,
    checkout_credential_name,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(name),
    sqlc.arg(clone_url),
    sqlc.arg(branch),
    sqlc.arg(commit_indexing_mode),
    sqlc.narg(commit_url_template),
    sqlc.narg(refresh_credential_name),
    sqlc.narg(checkout_credential_name),
    sqlc.arg(created_at)
)
RETURNING
    id,
    name,
    clone_url,
    branch,
    commit_indexing_mode,
    commit_url_template,
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
    repositories.id,
    repositories.name,
    repositories.clone_url,
    repositories.branch,
    repositories.commit_indexing_mode,
    repositories.commit_url_template,
    repositories.refresh_credential_name,
    repositories.checkout_credential_name,
    repositories.created_at,
    repositories.refresh_status,
    repositories.observed_commit_sha,
    repositories.last_refresh_attempted_at,
    repositories.last_refresh_succeeded_at,
    repositories.last_refresh_error,
    catalog_revisions.commit_sha AS active_catalog_commit_sha
FROM repositories
LEFT JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
WHERE repositories.removed_at IS NULL
ORDER BY repositories.name;

-- name: GetActiveRepository :one
SELECT
    repositories.id,
    repositories.name,
    repositories.clone_url,
    repositories.branch,
    repositories.commit_indexing_mode,
    repositories.commit_url_template,
    repositories.refresh_credential_name,
    repositories.checkout_credential_name,
    repositories.created_at,
    repositories.refresh_status,
    repositories.observed_commit_sha,
    repositories.last_refresh_attempted_at,
    repositories.last_refresh_succeeded_at,
    repositories.last_refresh_error,
    catalog_revisions.commit_sha AS active_catalog_commit_sha
FROM repositories
LEFT JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
WHERE repositories.id = sqlc.arg(id)
  AND repositories.removed_at IS NULL;

-- name: UpdateRepositoryCommitIndexing :execrows
UPDATE repositories
SET commit_indexing_mode = sqlc.arg(commit_indexing_mode)
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;

-- name: RecordRepositoryRefreshFailure :exec
UPDATE repositories
SET refresh_status = 'failed',
    observed_commit_sha = COALESCE(
        sqlc.narg(observed_commit_sha),
        observed_commit_sha
    ),
    last_refresh_attempted_at = sqlc.arg(attempted_at),
    last_refresh_error = sqlc.arg(refresh_error)
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;

-- name: RemoveRepository :execrows
UPDATE repositories
SET removed_at = sqlc.arg(removed_at)
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;
