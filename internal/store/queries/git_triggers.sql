-- name: CreateGitTriggerDelivery :exec
INSERT INTO git_trigger_deliveries (
    id,
    target_id,
    repository_id,
    catalog_revision_id,
    target_name,
    action_name,
    trigger_name,
    parameters,
    created_at,
    next_attempt_at
) SELECT
    sqlc.arg(id),
    targets.id,
    sqlc.arg(repository_id),
    sqlc.arg(catalog_revision_id),
    sqlc.arg(target_name),
    sqlc.arg(action_name),
    sqlc.arg(trigger_name),
    sqlc.arg(parameters),
    sqlc.arg(created_at),
    sqlc.arg(created_at)
FROM targets
WHERE targets.repository_id = sqlc.arg(repository_id)
  AND targets.name = sqlc.arg(target_name)
  AND targets.archived_at IS NULL;

-- name: GetEligibleGitTriggerDelivery :one
SELECT *
FROM git_trigger_deliveries
WHERE run_id IS NULL
  AND superseded_at IS NULL
  AND failed_at IS NULL
  AND next_attempt_at <= sqlc.arg(attempted_at)
ORDER BY created_at, id
LIMIT 1;

-- name: LinkGitTriggerRun :execrows
UPDATE git_trigger_deliveries
SET run_id = sqlc.arg(run_id)
WHERE id = sqlc.arg(id)
  AND repository_id = sqlc.arg(repository_id)
  AND target_id = sqlc.arg(target_id)
  AND catalog_revision_id = sqlc.arg(catalog_revision_id)
  AND target_name = sqlc.arg(target_name)
  AND action_name = sqlc.arg(action_name)
  AND trigger_name = sqlc.arg(trigger_name)
  AND superseded_at IS NULL
  AND failed_at IS NULL
  AND (run_id = sqlc.arg(run_id) OR (
      run_id IS NULL AND next_attempt_at <= sqlc.arg(attempted_at)
  ));

-- name: RecordGitTriggerDeliveryFailure :one
UPDATE git_trigger_deliveries
SET failure_count = failure_count + 1,
    diagnostic = sqlc.arg(diagnostic),
    next_attempt_at = sqlc.arg(attempted_at)::timestamptz + make_interval(secs =>
        LEAST(sqlc.arg(max_delay_seconds)::double precision,
              sqlc.arg(initial_delay_seconds)::double precision * power(2, failure_count))),
    failed_at = CASE WHEN failure_count + 1 >= sqlc.arg(failure_limit)::integer
        THEN sqlc.arg(attempted_at)::timestamptz ELSE NULL END
WHERE id = sqlc.arg(id)
  AND run_id IS NULL AND superseded_at IS NULL AND failed_at IS NULL
RETURNING failure_count, failed_at;

-- name: ListFailedGitTriggerDeliveries :many
SELECT delivery.*, repositories.name AS repository_name, catalog_revisions.commit_sha
FROM git_trigger_deliveries AS delivery
JOIN repositories ON repositories.id = delivery.repository_id
JOIN catalog_revisions ON catalog_revisions.id = delivery.catalog_revision_id
WHERE delivery.failed_at IS NOT NULL
ORDER BY delivery.failed_at DESC, delivery.id DESC
LIMIT sqlc.arg(result_limit) OFFSET sqlc.arg(result_offset);

-- name: CountFailedGitTriggerDeliveries :one
SELECT count(*) FROM git_trigger_deliveries WHERE failed_at IS NOT NULL;

-- name: SupersedeGitTriggerDelivery :execrows
UPDATE git_trigger_deliveries
SET superseded_at = sqlc.arg(superseded_at)
WHERE id = sqlc.arg(id)
  AND run_id IS NULL
  AND superseded_at IS NULL
  AND failed_at IS NULL;

-- name: SupersedePendingGitTriggerDeliveries :exec
UPDATE git_trigger_deliveries AS delivery
SET superseded_at = sqlc.arg(superseded_at)
WHERE delivery.target_id = sqlc.arg(target_id)
  AND delivery.run_id IS NULL
  AND delivery.superseded_at IS NULL
  AND delivery.failed_at IS NULL;

-- name: LockTargetStateForGitRun :one
SELECT desired_commit_sha, archived_at
FROM targets
WHERE id = sqlc.arg(target_id)
FOR UPDATE;

-- name: FinishSupersededQueuedGitRuns :many
UPDATE runs
SET phase = 'finished',
    outcome = 'canceled',
    finished_at = sqlc.arg(finished_at),
    outcome_reason = 'SupersededByGitChange'
WHERE target_id = sqlc.arg(target_id)
  AND phase = 'queued'
  AND source->>'type' = 'git'
  AND (
      sqlc.arg(cancel_all)::boolean
      OR commit_sha <> sqlc.arg(desired_commit_sha)
  )
RETURNING id;
