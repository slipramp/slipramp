-- name: CreateRun :one
INSERT INTO runs (
    id,
    phase,
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
    sqlc.arg(phase),
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

-- name: CreateRunPhaseTransition :exec
INSERT INTO run_phase_transitions (
    run_id,
    phase,
    outcome,
    reason,
    message,
    exit_code,
    recorded_at
) VALUES (
    sqlc.arg(run_id),
    sqlc.arg(phase),
    sqlc.narg(outcome),
    sqlc.narg(reason),
    sqlc.narg(message),
    sqlc.narg(exit_code),
    sqlc.arg(recorded_at)
);

-- name: GetRunObservationStateForUpdate :one
SELECT phase, outcome, job_uid
FROM runs
WHERE id = sqlc.arg(id)
  AND cluster_name = sqlc.arg(cluster_name)
FOR UPDATE;

-- name: UpdateRunObservation :exec
UPDATE runs
SET phase = sqlc.arg(phase),
    outcome = sqlc.narg(outcome),
    job_uid = COALESCE(job_uid, sqlc.narg(job_uid)),
    last_observed_at = GREATEST(last_observed_at, sqlc.arg(observed_at)),
    preparing_at = CASE
        WHEN sqlc.arg(phase) IN ('preparing', 'running', 'finished')
             AND (job_uid IS NOT NULL OR sqlc.narg(job_uid) IS NOT NULL)
            THEN COALESCE(preparing_at, sqlc.arg(observed_at))
        ELSE preparing_at
    END,
    started_at = CASE
        WHEN sqlc.arg(phase) = 'running'
            THEN COALESCE(started_at, sqlc.arg(observed_at))
        ELSE started_at
    END,
    finished_at = CASE
        WHEN sqlc.arg(phase) = 'finished'
            THEN COALESCE(finished_at, sqlc.arg(observed_at))
        ELSE finished_at
    END,
    outcome_reason = CASE
        WHEN sqlc.arg(phase) = 'finished'
            THEN COALESCE(outcome_reason, sqlc.narg(reason))
        ELSE outcome_reason
    END,
    outcome_message = CASE
        WHEN sqlc.arg(phase) = 'finished'
            THEN COALESCE(outcome_message, sqlc.narg(message))
        ELSE outcome_message
    END,
    exit_code = CASE
        WHEN sqlc.arg(phase) = 'finished'
            THEN COALESCE(exit_code, sqlc.narg(exit_code))
        ELSE exit_code
    END
WHERE id = sqlc.arg(id);

-- name: GetAssignedRunForCluster :one
SELECT *
FROM runs
WHERE cluster_name = sqlc.arg(cluster_name)
  AND phase = 'assigned'
ORDER BY assigned_at, id
LIMIT 1;

-- name: ClaimQueuedRun :one
WITH candidate AS (
    SELECT queued.id
    FROM runs AS queued
    WHERE queued.cluster_name = sqlc.arg(cluster_name)
      AND queued.phase = 'queued'
      AND NOT EXISTS (
          SELECT 1
          FROM runs AS active
          WHERE active.repository_id = queued.repository_id
            AND active.target_name = queued.target_name
            AND active.phase IN ('assigned', 'preparing', 'running')
      )
    ORDER BY queued.created_at, queued.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE runs
SET phase = 'assigned',
    assigned_at = sqlc.arg(assigned_at)
FROM candidate
WHERE runs.id = candidate.id
RETURNING runs.*;
