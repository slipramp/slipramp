-- name: CreateArtifactReport :one
INSERT INTO artifact_reports (
    artifact_id,
    kind,
    projection_schema_version,
    adapter_version,
    tool,
    tool_version,
    mode,
    summary,
    projection_object_key,
    projection_size_bytes,
    projection_sha256,
    created_at
) VALUES (
    sqlc.arg(artifact_id),
    sqlc.arg(kind),
    sqlc.arg(projection_schema_version),
    sqlc.arg(adapter_version),
    sqlc.arg(tool),
    sqlc.narg(tool_version),
    sqlc.arg(mode),
    sqlc.arg(summary),
    sqlc.arg(projection_object_key),
    sqlc.arg(projection_size_bytes),
    sqlc.arg(projection_sha256),
    sqlc.arg(created_at)
)
RETURNING *;

-- name: GetArtifactReport :one
SELECT *
FROM artifact_reports
WHERE artifact_id = sqlc.arg(artifact_id);

-- name: ListArtifactReportsForRun :many
SELECT artifact_reports.*
FROM artifact_reports
JOIN artifacts ON artifacts.id = artifact_reports.artifact_id
WHERE artifacts.run_id = sqlc.arg(run_id)
ORDER BY artifacts.created_at, artifacts.id;
