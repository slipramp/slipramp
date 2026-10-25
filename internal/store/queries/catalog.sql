-- name: LockActiveRepository :one
SELECT id, active_catalog_revision_id, commit_indexing_mode
FROM repositories
WHERE id = sqlc.arg(id)
  AND removed_at IS NULL
FOR UPDATE;

-- name: FindCatalogRevisionID :one
SELECT id
FROM catalog_revisions
WHERE repository_id = sqlc.arg(repository_id)
  AND commit_sha = sqlc.arg(commit_sha);

-- name: CreateCatalogRevision :exec
INSERT INTO catalog_revisions (
    id,
    repository_id,
    commit_sha,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(repository_id),
    sqlc.arg(commit_sha),
    sqlc.arg(created_at)
);

-- name: CreateCatalogRuntime :exec
INSERT INTO catalog_runtimes (
    catalog_revision_id,
    name,
    source_path,
    source_document,
    definition
) VALUES (
    sqlc.arg(catalog_revision_id),
    sqlc.arg(name),
    sqlc.arg(source_path),
    sqlc.arg(source_document),
    sqlc.arg(definition)
);

-- name: CreateCatalogTarget :exec
INSERT INTO catalog_targets (
    catalog_revision_id,
    name,
    source_path,
    source_document,
    definition
) VALUES (
    sqlc.arg(catalog_revision_id),
    sqlc.arg(name),
    sqlc.arg(source_path),
    sqlc.arg(source_document),
    sqlc.arg(definition)
);

-- name: RecordCatalogValidationFailure :exec
INSERT INTO catalog_validation_failures (
    repository_id,
    commit_sha,
    recorded_at,
    failure_reason
) VALUES (
    sqlc.arg(repository_id),
    sqlc.arg(commit_sha),
    sqlc.arg(recorded_at),
    sqlc.arg(failure_reason)
)
ON CONFLICT (repository_id, commit_sha) DO NOTHING;

-- name: ActivateCatalogRevision :exec
UPDATE repositories
SET refresh_status = 'succeeded',
    observed_commit_sha = sqlc.arg(commit_sha),
    last_refresh_attempted_at = sqlc.arg(attempted_at),
    last_refresh_succeeded_at = sqlc.arg(attempted_at),
    last_refresh_error = NULL,
    refresh_failure_started_at = NULL,
    active_catalog_revision_id = sqlc.arg(catalog_revision_id)
WHERE id = sqlc.arg(repository_id)
  AND removed_at IS NULL;

-- name: GetActiveCatalogRevision :one
SELECT
    catalog_revisions.id,
    catalog_revisions.commit_sha,
    catalog_revisions.created_at
FROM repositories
JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
WHERE repositories.id = sqlc.arg(repository_id)
  AND repositories.removed_at IS NULL;

-- name: GetCatalogRevision :one
SELECT id, commit_sha, created_at
FROM catalog_revisions
WHERE id = sqlc.arg(id)
  AND repository_id = sqlc.arg(repository_id);

-- name: ListCatalogRuntimes :many
SELECT definition
FROM catalog_runtimes
WHERE catalog_revision_id = sqlc.arg(catalog_revision_id)
ORDER BY name;

-- name: ListCatalogTargets :many
SELECT definition
FROM catalog_targets
WHERE catalog_revision_id = sqlc.arg(catalog_revision_id)
ORDER BY name;

-- name: ListRepositoryTargetStates :many
SELECT id, name, desired_commit_sha, archived_at
FROM targets
WHERE repository_id = sqlc.arg(repository_id)
ORDER BY name;

-- name: ArchiveMissingTargets :exec
UPDATE targets
SET updated_at = sqlc.arg(updated_at),
    archived_at = sqlc.arg(updated_at)
WHERE repository_id = sqlc.arg(repository_id)
  AND archived_at IS NULL
  AND NOT (name = ANY(sqlc.arg(active_names)::text[]));

-- name: UpsertTargetState :exec
INSERT INTO targets (
    id,
    repository_id,
    name,
    catalog_revision_id,
    desired_commit_sha,
    created_at,
    updated_at,
    archived_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(repository_id),
    sqlc.arg(name),
    sqlc.arg(catalog_revision_id),
    sqlc.arg(desired_commit_sha),
    sqlc.arg(updated_at),
    sqlc.arg(updated_at),
    NULL
)
ON CONFLICT (repository_id, name) DO UPDATE
SET catalog_revision_id = EXCLUDED.catalog_revision_id,
    desired_commit_sha = EXCLUDED.desired_commit_sha,
    updated_at = EXCLUDED.updated_at,
    archived_at = NULL;

-- name: ListCatalogTargetStates :many
SELECT name, desired_commit_sha
FROM targets
WHERE catalog_revision_id = sqlc.arg(catalog_revision_id)
  AND archived_at IS NULL
ORDER BY name;

-- name: ListActiveTargets :many
SELECT
    targets.id,
    targets.repository_id,
    repositories.name AS repository_name,
    catalog_revisions.id AS catalog_revision_id,
    catalog_revisions.commit_sha AS catalog_commit_sha,
    targets.desired_commit_sha,
    catalog_targets.definition
FROM targets
JOIN repositories
  ON repositories.id = targets.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = targets.catalog_revision_id
JOIN catalog_targets
  ON catalog_targets.catalog_revision_id = targets.catalog_revision_id
 AND catalog_targets.name = targets.name
WHERE targets.archived_at IS NULL
  AND repositories.removed_at IS NULL
  AND (
      sqlc.narg(repository_id)::uuid IS NULL
      OR targets.repository_id = sqlc.narg(repository_id)
  )
  AND (
      sqlc.narg(runtime)::text IS NULL
      OR catalog_targets.definition->'spec'->>'runtime' = sqlc.narg(runtime)
  )
  AND (
      sqlc.narg(placement)::text IS NULL
      OR catalog_targets.definition->'spec'->>'placement' = sqlc.narg(placement)
  )
  AND (
      sqlc.narg(search_query)::text IS NULL
      OR strpos(lower(targets.name), lower(sqlc.narg(search_query))) > 0
      OR strpos(lower(repositories.name), lower(sqlc.narg(search_query))) > 0
      OR strpos(
          lower(coalesce(catalog_targets.definition->'metadata'->>'title', '')),
          lower(sqlc.narg(search_query))
      ) > 0
      OR EXISTS (
          SELECT 1
          FROM jsonb_object_keys(
              catalog_targets.definition->'spec'->'actions'
          ) AS action(name)
          WHERE strpos(lower(action.name), lower(sqlc.narg(search_query))) > 0
      )
  )
ORDER BY
    lower(coalesce(
        nullif(catalog_targets.definition->'metadata'->>'title', ''),
        targets.name
    )),
    lower(repositories.name),
    targets.name
LIMIT sqlc.arg(result_limit)
OFFSET sqlc.arg(result_offset);

-- name: GetTarget :one
SELECT
    targets.id,
    targets.repository_id,
    repositories.name AS repository_name,
    catalog_revisions.id AS catalog_revision_id,
    catalog_revisions.commit_sha AS catalog_commit_sha,
    targets.desired_commit_sha,
    targets.archived_at,
    catalog_targets.definition
FROM targets
JOIN repositories
  ON repositories.id = targets.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = targets.catalog_revision_id
JOIN catalog_targets
  ON catalog_targets.catalog_revision_id = targets.catalog_revision_id
 AND catalog_targets.name = targets.name
WHERE targets.id = sqlc.arg(id);

-- name: GetActiveTargetByName :one
SELECT
    targets.id,
    targets.repository_id,
    repositories.name AS repository_name,
    catalog_revisions.id AS catalog_revision_id,
    catalog_revisions.commit_sha AS catalog_commit_sha,
    targets.desired_commit_sha,
    targets.archived_at,
    catalog_targets.definition
FROM targets
JOIN repositories
  ON repositories.id = targets.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = targets.catalog_revision_id
JOIN catalog_targets
  ON catalog_targets.catalog_revision_id = targets.catalog_revision_id
 AND catalog_targets.name = targets.name
WHERE targets.repository_id = sqlc.arg(repository_id)
  AND targets.name = sqlc.arg(name)
  AND targets.archived_at IS NULL
  AND repositories.removed_at IS NULL;

-- name: CountActiveTargets :one
SELECT count(*)
FROM targets
JOIN repositories
  ON repositories.id = targets.repository_id
JOIN catalog_targets
  ON catalog_targets.catalog_revision_id = targets.catalog_revision_id
 AND catalog_targets.name = targets.name
WHERE targets.archived_at IS NULL
  AND repositories.removed_at IS NULL
  AND (
      sqlc.narg(repository_id)::uuid IS NULL
      OR targets.repository_id = sqlc.narg(repository_id)
  )
  AND (
      sqlc.narg(runtime)::text IS NULL
      OR catalog_targets.definition->'spec'->>'runtime' = sqlc.narg(runtime)
  )
  AND (
      sqlc.narg(placement)::text IS NULL
      OR catalog_targets.definition->'spec'->>'placement' = sqlc.narg(placement)
  )
  AND (
      sqlc.narg(search_query)::text IS NULL
      OR strpos(lower(targets.name), lower(sqlc.narg(search_query))) > 0
      OR strpos(lower(repositories.name), lower(sqlc.narg(search_query))) > 0
      OR strpos(
          lower(coalesce(catalog_targets.definition->'metadata'->>'title', '')),
          lower(sqlc.narg(search_query))
      ) > 0
      OR EXISTS (
          SELECT 1
          FROM jsonb_object_keys(
              catalog_targets.definition->'spec'->'actions'
          ) AS action(name)
          WHERE strpos(lower(action.name), lower(sqlc.narg(search_query))) > 0
      )
  );
