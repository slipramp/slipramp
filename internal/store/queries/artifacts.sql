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

-- name: GetArtifactLogChunk :one
SELECT *
FROM artifact_log_chunks
WHERE artifact_id = sqlc.arg(artifact_id)
  AND offset_bytes = sqlc.arg(offset_bytes);

-- name: ListArtifactLogChunks :many
SELECT *
FROM artifact_log_chunks
WHERE artifact_id = sqlc.arg(artifact_id)
  AND offset_bytes >= sqlc.arg(from_offset_bytes)
ORDER BY offset_bytes;
