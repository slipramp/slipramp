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
    last_refresh_error,
    refresh_failure_started_at;

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
    repositories.refresh_failure_started_at,
    catalog_revisions.commit_sha AS active_catalog_commit_sha,
    repository_commit_history_indexes.status AS commit_history_status,
    repository_commit_history_indexes.desired_head_sha AS commit_history_desired_head_sha,
    repository_commit_history_indexes.indexed_head_sha AS commit_history_indexed_head_sha,
    repository_commit_history_indexes.commit_count AS commit_history_commit_count,
    repository_commit_history_indexes.last_attempted_at AS commit_history_last_attempted_at,
    repository_commit_history_indexes.last_succeeded_at AS commit_history_last_succeeded_at,
    repository_commit_history_indexes.failure_reason AS commit_history_failure_reason
FROM repositories
LEFT JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
LEFT JOIN repository_commit_history_indexes
  ON repository_commit_history_indexes.repository_id = repositories.id
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
    repositories.refresh_failure_started_at,
    catalog_revisions.commit_sha AS active_catalog_commit_sha,
    repository_commit_history_indexes.status AS commit_history_status,
    repository_commit_history_indexes.desired_head_sha AS commit_history_desired_head_sha,
    repository_commit_history_indexes.indexed_head_sha AS commit_history_indexed_head_sha,
    repository_commit_history_indexes.commit_count AS commit_history_commit_count,
    repository_commit_history_indexes.last_attempted_at AS commit_history_last_attempted_at,
    repository_commit_history_indexes.last_succeeded_at AS commit_history_last_succeeded_at,
    repository_commit_history_indexes.failure_reason AS commit_history_failure_reason
FROM repositories
LEFT JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
LEFT JOIN repository_commit_history_indexes
  ON repository_commit_history_indexes.repository_id = repositories.id
WHERE repositories.id = sqlc.arg(id)
  AND repositories.removed_at IS NULL;

-- name: CountRepositoryRefreshStates :many
SELECT refresh_status, count(*)::bigint AS repository_count
FROM repositories
WHERE removed_at IS NULL
GROUP BY refresh_status
ORDER BY refresh_status;

-- name: UpdateRepository :execrows
UPDATE repositories
SET commit_indexing_mode = sqlc.arg(commit_indexing_mode),
    commit_url_template = sqlc.narg(commit_url_template),
    refresh_credential_name = sqlc.narg(refresh_credential_name),
    checkout_credential_name = sqlc.narg(checkout_credential_name)
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
    last_refresh_error = sqlc.arg(refresh_error),
    refresh_failure_started_at = CASE
        WHEN refresh_status = 'failed' THEN refresh_failure_started_at
        ELSE sqlc.arg(attempted_at)
    END
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;

-- name: CreateRepositoryRefreshAttempt :exec
INSERT INTO repository_refresh_attempts (
    id,
    repository_id,
    source,
    outcome,
    started_at,
    duration_milliseconds,
    commit_sha,
    diagnostic
) VALUES (
    sqlc.arg(id),
    sqlc.arg(repository_id),
    sqlc.arg(source),
    sqlc.arg(outcome),
    sqlc.arg(started_at),
    sqlc.arg(duration_milliseconds),
    sqlc.narg(commit_sha),
    sqlc.narg(diagnostic)
);

-- name: PruneRepositoryRefreshAttempts :exec
DELETE FROM repository_refresh_attempts AS attempt
WHERE attempt.repository_id = sqlc.arg(repository_id)
  AND attempt.id IN (
      SELECT retained.id
      FROM repository_refresh_attempts AS retained
      WHERE retained.repository_id = sqlc.arg(repository_id)
      ORDER BY retained.started_at DESC, retained.id DESC
      OFFSET sqlc.arg(retain_count)
  );

-- name: CountRepositoryRefreshAttempts :one
SELECT count(*)::bigint
FROM repository_refresh_attempts
WHERE repository_id = sqlc.arg(repository_id);

-- name: ListRepositoryRefreshAttempts :many
SELECT
    id,
    repository_id,
    source,
    outcome,
    started_at,
    duration_milliseconds,
    commit_sha,
    diagnostic
FROM repository_refresh_attempts
WHERE repository_id = sqlc.arg(repository_id)
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(result_limit)
OFFSET sqlc.arg(result_offset);

-- name: RemoveRepository :execrows
UPDATE repositories
SET removed_at = sqlc.arg(removed_at)
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL;
