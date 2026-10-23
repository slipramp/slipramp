-- name: UpsertRepositoryCommit :exec
INSERT INTO repository_commits (
    repository_id,
    sha,
    title,
    author_name,
    authored_at,
    committed_at
) VALUES (
    sqlc.arg(repository_id),
    sqlc.arg(sha),
    sqlc.narg(title),
    sqlc.narg(author_name),
    sqlc.narg(authored_at),
    sqlc.narg(committed_at)
)
ON CONFLICT (repository_id, sha) DO UPDATE
SET title = COALESCE(repository_commits.title, EXCLUDED.title),
    author_name = COALESCE(repository_commits.author_name, EXCLUDED.author_name),
    authored_at = COALESCE(repository_commits.authored_at, EXCLUDED.authored_at),
    committed_at = COALESCE(repository_commits.committed_at, EXCLUDED.committed_at);

-- name: UpsertRepositoryCommitHistory :exec
INSERT INTO repository_commits (
    repository_id,
    sha,
    title,
    author_name,
    authored_at,
    committed_at
)
SELECT
    sqlc.arg(repository_id),
    commits.sha,
    titles.title,
    authors.author_name,
    author_times.authored_at,
    commit_times.committed_at
FROM unnest(sqlc.arg(shas)::text[])
    WITH ORDINALITY AS commits(sha, ordinal)
JOIN unnest(sqlc.arg(titles)::text[])
    WITH ORDINALITY AS titles(title, ordinal)
  USING (ordinal)
JOIN unnest(sqlc.arg(author_names)::text[])
    WITH ORDINALITY AS authors(author_name, ordinal)
  USING (ordinal)
JOIN unnest(sqlc.arg(authored_ats)::timestamptz[])
    WITH ORDINALITY AS author_times(authored_at, ordinal)
  USING (ordinal)
JOIN unnest(sqlc.arg(committed_ats)::timestamptz[])
    WITH ORDINALITY AS commit_times(committed_at, ordinal)
  USING (ordinal)
ON CONFLICT (repository_id, sha) DO UPDATE
SET title = EXCLUDED.title,
    author_name = EXCLUDED.author_name,
    authored_at = EXCLUDED.authored_at,
    committed_at = EXCLUDED.committed_at;

-- name: ListRepositoryCommits :many
WITH requested AS (
    SELECT repository_ids.repository_id, commit_shas.sha
    FROM unnest(sqlc.arg(repository_ids)::uuid[])
        WITH ORDINALITY AS repository_ids(repository_id, ordinal)
    JOIN unnest(sqlc.arg(commit_shas)::text[])
        WITH ORDINALITY AS commit_shas(sha, ordinal)
      USING (ordinal)
)
SELECT
    requested.repository_id::uuid AS repository_id,
    requested.sha::text AS sha,
    repository_commits.title,
    repository_commits.author_name,
    repository_commits.authored_at,
    repository_commits.committed_at,
    repositories.clone_url,
    repositories.commit_url_template
FROM requested
JOIN repositories ON repositories.id = requested.repository_id
LEFT JOIN repository_commits
  ON repository_commits.repository_id = requested.repository_id
 AND repository_commits.sha = requested.sha
ORDER BY requested.repository_id, requested.sha;

-- name: ListTrackedBranchCommits :many
SELECT
    repository_commits.repository_id,
    repositories.name AS repository_name,
    repository_commits.sha,
    repository_commits.title,
    repository_commits.author_name,
    repository_commits.authored_at,
    repository_commits.committed_at,
    repositories.clone_url,
    repositories.commit_url_template
FROM repository_tracked_branch_commits
JOIN repository_commits
  ON repository_commits.repository_id = repository_tracked_branch_commits.repository_id
 AND repository_commits.sha = repository_tracked_branch_commits.commit_sha
JOIN repositories ON repositories.id = repository_tracked_branch_commits.repository_id
WHERE repositories.removed_at IS NULL
  AND repositories.commit_indexing_mode = 'trackedBranchHistory'
  AND (
      sqlc.narg(repository_id)::uuid IS NULL
      OR repository_tracked_branch_commits.repository_id = sqlc.narg(repository_id)
  )
  AND (
      sqlc.narg(query)::text IS NULL
      OR repository_commits.sha ILIKE '%' || sqlc.narg(query) || '%'
      OR repository_commits.title ILIKE '%' || sqlc.narg(query) || '%'
      OR repository_commits.author_name ILIKE '%' || sqlc.narg(query) || '%'
  )
ORDER BY
    repository_commits.committed_at DESC NULLS LAST,
    repository_commits.sha DESC
LIMIT sqlc.arg(result_limit)
OFFSET sqlc.arg(result_offset);

-- name: CountTrackedBranchCommits :one
SELECT count(*)
FROM repository_tracked_branch_commits
JOIN repository_commits
  ON repository_commits.repository_id = repository_tracked_branch_commits.repository_id
 AND repository_commits.sha = repository_tracked_branch_commits.commit_sha
JOIN repositories ON repositories.id = repository_tracked_branch_commits.repository_id
WHERE repositories.removed_at IS NULL
  AND repositories.commit_indexing_mode = 'trackedBranchHistory'
  AND (
      sqlc.narg(repository_id)::uuid IS NULL
      OR repository_tracked_branch_commits.repository_id = sqlc.narg(repository_id)
  )
  AND (
      sqlc.narg(query)::text IS NULL
      OR repository_commits.sha ILIKE '%' || sqlc.narg(query) || '%'
      OR repository_commits.title ILIKE '%' || sqlc.narg(query) || '%'
      OR repository_commits.author_name ILIKE '%' || sqlc.narg(query) || '%'
  );
