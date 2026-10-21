-- name: CreateGitTriggerDelivery :exec
INSERT INTO git_trigger_deliveries (
    id,
    repository_id,
    catalog_revision_id,
    target_name,
    action_name,
    trigger_name,
    parameters,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(repository_id),
    sqlc.arg(catalog_revision_id),
    sqlc.arg(target_name),
    sqlc.arg(action_name),
    sqlc.arg(trigger_name),
    sqlc.arg(parameters),
    sqlc.arg(created_at)
);

-- name: GetPendingGitTriggerDelivery :one
SELECT *
FROM git_trigger_deliveries
WHERE run_id IS NULL
  AND superseded_at IS NULL
ORDER BY created_at, id
LIMIT 1;

-- name: CompleteGitTriggerDelivery :execrows
UPDATE git_trigger_deliveries
SET run_id = sqlc.arg(run_id)
WHERE id = sqlc.arg(id)
  AND superseded_at IS NULL
  AND (run_id IS NULL OR run_id = sqlc.arg(run_id));

-- name: SupersedeGitTriggerDelivery :execrows
UPDATE git_trigger_deliveries
SET superseded_at = sqlc.arg(superseded_at)
WHERE id = sqlc.arg(id)
  AND run_id IS NULL
  AND superseded_at IS NULL;

-- name: SupersedePendingGitTriggerDeliveries :exec
UPDATE git_trigger_deliveries AS delivery
SET superseded_at = sqlc.arg(superseded_at)
WHERE delivery.repository_id = sqlc.arg(repository_id)
  AND delivery.target_name = sqlc.arg(target_name)
  AND delivery.run_id IS NULL
  AND delivery.superseded_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM runs
      WHERE runs.source->>'type' = 'git'
        AND runs.idempotency_key = delivery.id::text
  );

-- name: LockTargetStateForGitRun :one
SELECT desired_commit_sha, archived_at
FROM targets
WHERE repository_id = sqlc.arg(repository_id)
  AND name = sqlc.arg(target_name)
FOR UPDATE;

-- name: FinishSupersededQueuedGitRuns :many
UPDATE runs
SET phase = 'finished',
    outcome = 'canceled',
    finished_at = sqlc.arg(finished_at),
    outcome_reason = 'SupersededByGitChange'
WHERE repository_id = sqlc.arg(repository_id)
  AND target_name = sqlc.arg(target_name)
  AND phase = 'queued'
  AND source->>'type' = 'git'
  AND (
      sqlc.arg(cancel_all)::boolean
      OR commit_sha <> sqlc.arg(desired_commit_sha)
  )
RETURNING id;
