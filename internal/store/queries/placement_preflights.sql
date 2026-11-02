-- name: CreatePlacementPreflight :one
INSERT INTO placement_preflights (
    id,
    placement_name,
    cluster_name,
    placement_snapshot,
    state,
    cleanup_state,
    requested_by_principal_id,
    requested_by_snapshot,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(placement_name),
    sqlc.arg(cluster_name),
    sqlc.arg(placement_snapshot),
    'queued',
    'pending',
    sqlc.arg(requested_by_principal_id),
    sqlc.arg(requested_by_snapshot),
    sqlc.arg(created_at)
)
RETURNING *;

-- name: CountPlacementPreflights :one
SELECT count(*)::bigint
FROM placement_preflights
WHERE placement_name = sqlc.arg(placement_name);

-- name: ListPlacementPreflights :many
SELECT *
FROM placement_preflights
WHERE placement_name = sqlc.arg(placement_name)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(result_limit)
OFFSET sqlc.arg(result_offset);

-- name: ClaimPlacementPreflight :one
WITH selected AS (
    SELECT id
    FROM placement_preflights
    WHERE placement_preflights.cluster_name = sqlc.arg(cluster_name)
      AND placement_preflights.state IN ('queued', 'running')
      AND (
          placement_preflights.last_dispatched_at IS NULL
          OR placement_preflights.last_dispatched_at <= sqlc.arg(dispatch_before)
      )
    ORDER BY placement_preflights.created_at, placement_preflights.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE placement_preflights
SET last_dispatched_at = sqlc.arg(dispatched_at)
FROM selected
WHERE placement_preflights.id = selected.id
RETURNING placement_preflights.*;

-- name: GetPlacementPreflightForUpdate :one
SELECT *
FROM placement_preflights
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: MarkPlacementPreflightRunning :one
UPDATE placement_preflights
SET state = 'running',
    started_at = COALESCE(started_at, sqlc.arg(observed_at)),
    job_uid = COALESCE(sqlc.narg(job_uid), job_uid),
    reason = NULL,
    message = NULL,
    diagnostics = sqlc.arg(diagnostics)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: FinishPlacementPreflight :one
UPDATE placement_preflights
SET state = sqlc.arg(state),
    cleanup_state = sqlc.arg(cleanup_state),
    started_at = COALESCE(started_at, sqlc.arg(observed_at)),
    finished_at = sqlc.arg(observed_at),
    job_uid = COALESCE(sqlc.narg(job_uid), job_uid),
    reason = sqlc.narg(reason),
    message = sqlc.narg(message),
    diagnostics = sqlc.arg(diagnostics)
WHERE id = sqlc.arg(id)
RETURNING *;
