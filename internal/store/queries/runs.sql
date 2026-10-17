-- name: CreateRun :one
INSERT INTO runs (
    id,
    state,
    repository_id,
    catalog_revision_id,
    commit_sha,
    target_name,
    action_name,
    parameters,
    placement_name,
    cluster_name,
    source_type,
    idempotency_key,
    request_digest,
    snapshot,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(state),
    sqlc.arg(repository_id),
    sqlc.arg(catalog_revision_id),
    sqlc.arg(commit_sha),
    sqlc.arg(target_name),
    sqlc.arg(action_name),
    sqlc.arg(parameters),
    sqlc.arg(placement_name),
    sqlc.arg(cluster_name),
    sqlc.arg(source_type),
    sqlc.narg(idempotency_key),
    sqlc.arg(request_digest),
    sqlc.arg(snapshot),
    sqlc.arg(created_at)
)
ON CONFLICT (source_type, idempotency_key)
WHERE idempotency_key IS NOT NULL
DO NOTHING
RETURNING *;

-- name: GetRunByIdempotencyKey :one
SELECT *
FROM runs
WHERE source_type = sqlc.arg(source_type)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateRunStateTransition :exec
INSERT INTO run_state_transitions (
    run_id,
    state,
    recorded_at
) VALUES (
    sqlc.arg(run_id),
    sqlc.arg(state),
    sqlc.arg(recorded_at)
);

-- name: GetPreparingRunForCluster :one
SELECT *
FROM runs
WHERE cluster_name = sqlc.arg(cluster_name)
  AND state = 'preparing'
ORDER BY created_at, id
LIMIT 1;

-- name: ClaimQueuedRun :one
WITH candidate AS (
    SELECT queued.id
    FROM runs AS queued
    WHERE queued.cluster_name = sqlc.arg(cluster_name)
      AND queued.state = 'queued'
      AND NOT EXISTS (
          SELECT 1
          FROM runs AS active
          WHERE active.repository_id = queued.repository_id
            AND active.target_name = queued.target_name
            AND active.state IN (
                'preparing',
                'running',
                'canceling',
                'unknown'
            )
      )
    ORDER BY queued.created_at, queued.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE runs
SET state = 'preparing'
FROM candidate
WHERE runs.id = candidate.id
RETURNING runs.*;
