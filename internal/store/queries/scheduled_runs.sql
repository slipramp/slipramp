-- name: LockTargetForScheduledRun :one
SELECT id
FROM targets
WHERE id = sqlc.arg(target_id)
FOR UPDATE;

-- name: HasUnfinishedScheduledRun :one
SELECT EXISTS (
    SELECT 1
    FROM runs
    WHERE target_id = sqlc.arg(target_id)
      AND action_name = sqlc.arg(action_name)
      AND phase <> 'finished'
      AND source->>'type' = 'schedule'
      AND source->'schedule'->>'triggerName' = sqlc.arg(trigger_name)::text
);
