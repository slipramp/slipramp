-- name: InitializeRepositoryCommitHistory :exec
INSERT INTO repository_commit_history_indexes (
    repository_id,
    status,
    desired_head_sha
)
SELECT id, 'pending', observed_commit_sha
FROM repositories
WHERE id = sqlc.arg(repository_id)
  AND removed_at IS NULL
  AND commit_indexing_mode = 'trackedBranchHistory'
ON CONFLICT (repository_id) DO UPDATE
SET desired_head_sha = EXCLUDED.desired_head_sha,
    status = CASE
        WHEN repository_commit_history_indexes.indexed_head_sha = EXCLUDED.desired_head_sha
            THEN 'succeeded'
        ELSE 'pending'
    END,
    failure_reason = NULL;

-- name: MarkRepositoryCommitHistoryPending :exec
INSERT INTO repository_commit_history_indexes (
    repository_id,
    status,
    desired_head_sha
) VALUES (
    sqlc.arg(repository_id),
    'pending',
    sqlc.arg(desired_head_sha)
)
ON CONFLICT (repository_id) DO UPDATE
SET desired_head_sha = EXCLUDED.desired_head_sha,
    status = CASE
        WHEN repository_commit_history_indexes.indexed_head_sha = EXCLUDED.desired_head_sha
            THEN 'succeeded'
        ELSE 'pending'
    END,
    failure_reason = NULL;

-- name: LockRepositoryCommitHistory :one
SELECT
    repositories.commit_indexing_mode,
    repository_commit_history_indexes.desired_head_sha,
    repository_commit_history_indexes.indexed_head_sha
FROM repositories
JOIN repository_commit_history_indexes
  ON repository_commit_history_indexes.repository_id = repositories.id
WHERE repositories.id = sqlc.arg(repository_id)
  AND repositories.removed_at IS NULL
FOR UPDATE OF repositories, repository_commit_history_indexes;

-- name: DeleteTrackedBranchCommits :exec
DELETE FROM repository_tracked_branch_commits
WHERE repository_id = sqlc.arg(repository_id);

-- name: AddTrackedBranchCommits :exec
INSERT INTO repository_tracked_branch_commits (repository_id, commit_sha)
SELECT sqlc.arg(repository_id), unnest(sqlc.arg(commit_shas)::text[])
ON CONFLICT DO NOTHING;

-- name: FinishRepositoryCommitHistory :exec
UPDATE repository_commit_history_indexes
SET status = CASE
        WHEN repository_commit_history_indexes.desired_head_sha = sqlc.arg(indexed_head_sha)
            THEN 'succeeded'
        ELSE 'pending'
    END,
    indexed_head_sha = sqlc.arg(indexed_head_sha),
    commit_count = (
        SELECT count(*)::integer
        FROM repository_tracked_branch_commits
        WHERE repository_tracked_branch_commits.repository_id = sqlc.arg(repository_id)
    ),
    last_attempted_at = sqlc.arg(attempted_at),
    last_succeeded_at = sqlc.arg(attempted_at),
    failure_reason = NULL
WHERE repository_id = sqlc.arg(repository_id);

-- name: FailRepositoryCommitHistory :exec
UPDATE repository_commit_history_indexes
SET status = 'failed',
    last_attempted_at = sqlc.arg(attempted_at),
    failure_reason = sqlc.arg(failure_reason)
WHERE repository_id = sqlc.arg(repository_id)
  AND desired_head_sha = sqlc.arg(attempted_head_sha);
