-- name: GetArtifactRunClusterName :one
SELECT cluster_name
FROM runs
WHERE id = sqlc.arg(run_id);

-- name: CreateArtifact :one
INSERT INTO artifacts (
    id,
    run_id,
    name,
    kind,
    state,
    media_type,
    size_bytes,
    sha256,
    sensitive,
    content_object_key,
    retention_state,
    created_at,
    sealed_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(run_id),
    sqlc.arg(name),
    sqlc.arg(kind),
    sqlc.arg(state),
    sqlc.arg(media_type),
    sqlc.arg(size_bytes),
    sqlc.narg(sha256),
    sqlc.arg(sensitive),
    sqlc.narg(content_object_key),
    sqlc.arg(retention_state),
    sqlc.arg(created_at),
    sqlc.narg(sealed_at)
)
RETURNING *;

-- name: GetArtifact :one
SELECT *
FROM artifacts
WHERE id = sqlc.arg(id);

-- name: GetArtifactByRunAndName :one
SELECT *
FROM artifacts
WHERE run_id = sqlc.arg(run_id)
  AND name = sqlc.arg(name);

-- name: ListArtifactsForRun :many
SELECT *
FROM artifacts
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at, id;

-- name: GetArtifactProvenance :one
SELECT
    repository_id,
    catalog_revision_id,
    commit_sha,
    target_name,
    action_name
FROM runs
WHERE id = sqlc.arg(run_id);

-- name: GetRunArtifactContext :one
SELECT cluster_name, snapshot
FROM runs
WHERE id = sqlc.arg(run_id);

-- name: GetRunInputArtifact :one
SELECT artifacts.*
FROM run_artifact_inputs
JOIN artifacts ON artifacts.id = run_artifact_inputs.artifact_id
WHERE run_artifact_inputs.run_id = sqlc.arg(run_id)
  AND run_artifact_inputs.input_name = sqlc.arg(input_name);

-- name: CreateArtifactLogChunk :one
INSERT INTO artifact_log_chunks (
    artifact_id,
    offset_bytes,
    size_bytes,
    sha256,
    object_key,
    created_at
) SELECT
    sqlc.arg(artifact_id),
    sqlc.arg(offset_bytes),
    sqlc.arg(size_bytes),
    sqlc.arg(sha256),
    sqlc.arg(object_key),
    sqlc.arg(created_at)
FROM artifacts
WHERE id = sqlc.arg(artifact_id)
  AND kind = 'log'
RETURNING *;

-- name: LockArtifactForLogAppend :one
SELECT *
FROM artifacts
WHERE id = sqlc.arg(id)
  AND kind = 'log'
FOR UPDATE;

-- name: AdvanceArtifactLogSize :exec
UPDATE artifacts
SET size_bytes = sqlc.arg(next_offset_bytes)
WHERE id = sqlc.arg(id);

-- name: SealArtifact :one
UPDATE artifacts
SET state = 'sealed',
    size_bytes = sqlc.arg(size_bytes),
    sha256 = sqlc.arg(sha256),
    sealed_at = sqlc.arg(sealed_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: FailArtifact :one
UPDATE artifacts
SET state = 'failed'
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetArtifactLogChunk :one
SELECT *
FROM artifact_log_chunks
WHERE artifact_id = sqlc.arg(artifact_id)
  AND offset_bytes = sqlc.arg(offset_bytes);

-- name: ListArtifactLogChunks :many
SELECT *
FROM artifact_log_chunks
WHERE artifact_id = sqlc.arg(artifact_id)
  AND offset_bytes + size_bytes > sqlc.arg(from_offset_bytes)
ORDER BY offset_bytes;

-- name: ExpireArtifacts :many
WITH candidates AS (
    SELECT artifacts.id
    FROM artifacts
    WHERE artifacts.state = 'sealed'
      AND artifacts.retention_state = 'retained'
      AND artifacts.sealed_at <= sqlc.arg(sealed_before)
      AND EXISTS (
          SELECT 1
          FROM runs AS producer_run
          WHERE producer_run.id = artifacts.run_id
            AND producer_run.phase = 'finished'
      )
      AND NOT EXISTS (
          SELECT 1
          FROM run_artifact_inputs
          JOIN runs ON runs.id = run_artifact_inputs.run_id
          WHERE run_artifact_inputs.artifact_id = artifacts.id
            AND runs.phase <> 'finished'
      )
      AND NOT EXISTS (
          SELECT 1
          FROM promotions
          WHERE promotions.status IN ('preparing', 'pending')
            AND artifacts.id IN (
                promotions.git_diff_artifact_id,
                promotions.argo_diff_artifact_id
            )
      )
      AND NOT EXISTS (
          SELECT 1
          FROM runs AS promotion_run
          JOIN promotions
            ON promotions.id::text =
               promotion_run.source->'promotion'->>'promotionId'
          WHERE promotion_run.id = artifacts.run_id
            AND promotion_run.source->>'type' = 'promotion'
            AND promotions.status IN ('preparing', 'pending')
      )
    ORDER BY artifacts.sealed_at, artifacts.id
    LIMIT sqlc.arg(result_limit)
    FOR UPDATE SKIP LOCKED
)
UPDATE artifacts
SET retention_state = 'expired'
FROM candidates
WHERE artifacts.id = candidates.id
RETURNING artifacts.*;

-- name: ListExpiredArtifacts :many
SELECT *
FROM artifacts
WHERE retention_state = 'expired'
ORDER BY sealed_at, id;

-- name: ListArtifactObjectKeys :many
SELECT COALESCE(content_object_key, '') AS object_key
FROM artifacts
WHERE id = sqlc.arg(artifact_id)
  AND content_object_key IS NOT NULL
UNION ALL
SELECT object_key
FROM artifact_log_chunks
WHERE artifact_id = sqlc.arg(artifact_id)
ORDER BY object_key;

-- name: MarkArtifactDeleted :one
UPDATE artifacts
SET retention_state = 'deleted'
WHERE id = sqlc.arg(id)
  AND retention_state = 'expired'
RETURNING *;

-- name: ArtifactObjectKeyExists :one
SELECT EXISTS (
    SELECT 1
    FROM artifacts
    WHERE artifacts.retention_state <> 'deleted'
      AND artifacts.content_object_key = sqlc.arg(candidate_object_key)
    UNION ALL
    SELECT 1
    FROM artifact_log_chunks
    JOIN artifacts ON artifacts.id = artifact_log_chunks.artifact_id
    WHERE artifacts.retention_state <> 'deleted'
      AND artifact_log_chunks.object_key = sqlc.arg(candidate_object_key)
) AS exists;
