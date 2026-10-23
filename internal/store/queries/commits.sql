-- name: UpsertRepositoryCommit :exec
INSERT INTO repository_commits (
    repository_id,
    sha,
    title,
    author_name,
    authored_at
) VALUES (
    sqlc.arg(repository_id),
    sqlc.arg(sha),
    sqlc.narg(title),
    sqlc.narg(author_name),
    sqlc.narg(authored_at)
)
ON CONFLICT (repository_id, sha) DO UPDATE
SET title = COALESCE(repository_commits.title, EXCLUDED.title),
    author_name = COALESCE(repository_commits.author_name, EXCLUDED.author_name),
    authored_at = COALESCE(repository_commits.authored_at, EXCLUDED.authored_at);

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
    repositories.clone_url,
    repositories.commit_url_template
FROM requested
JOIN repositories ON repositories.id = requested.repository_id
LEFT JOIN repository_commits
  ON repository_commits.repository_id = requested.repository_id
 AND repository_commits.sha = requested.sha
ORDER BY requested.repository_id, requested.sha;
