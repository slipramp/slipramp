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

-- name: GetRun :one
SELECT *
FROM runs
WHERE id = sqlc.arg(id);

-- name: ListRuns :many
SELECT *
FROM runs
WHERE (
    sqlc.narg(repository_id)::uuid IS NULL
    OR repository_id = sqlc.narg(repository_id)
)
AND (
    sqlc.narg(target_name)::text IS NULL
    OR target_name = sqlc.narg(target_name)
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(result_limit);

-- name: CreateRunArtifactInput :exec
INSERT INTO run_artifact_inputs (run_id, input_name, artifact_id)
VALUES (sqlc.arg(run_id), sqlc.arg(input_name), sqlc.arg(artifact_id));

-- name: GetArtifactInputCandidate :one
SELECT
    artifacts.id AS artifact_id,
    artifacts.run_id AS producer_run_id,
    artifacts.name,
    artifacts.state,
    artifacts.media_type,
    artifacts.size_bytes,
    artifacts.sha256,
    artifacts.sensitive,
    artifacts.retention_state,
    runs.repository_id,
    runs.catalog_revision_id,
    runs.commit_sha,
    runs.target_name,
    runs.action_name
FROM artifacts
JOIN runs ON runs.id = artifacts.run_id
WHERE artifacts.id = sqlc.arg(artifact_id);

-- name: GetRunForCancel :one
SELECT *
FROM runs
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: CancelRun :one
UPDATE runs
SET cancellation_requested_at = sqlc.arg(requested_at),
    phase = CASE WHEN phase = 'queued' THEN 'finished' ELSE phase END,
    outcome = CASE WHEN phase = 'queued' THEN 'canceled' ELSE outcome END,
    finished_at = CASE
        WHEN phase = 'queued' THEN sqlc.arg(requested_at)
        ELSE finished_at
    END,
    outcome_reason = CASE
        WHEN phase = 'queued' THEN 'CancelRequested'
        ELSE outcome_reason
    END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetRunToCancelForCluster :one
SELECT *
FROM runs
WHERE cluster_name = sqlc.arg(cluster_name)
  AND cancellation_requested_at IS NOT NULL
  AND phase IN ('assigned', 'preparing', 'running')
ORDER BY cancellation_requested_at, id
LIMIT 1;

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
SELECT phase, outcome, job_uid, cancellation_requested_at
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
