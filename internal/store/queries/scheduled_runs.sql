-- name: ListScheduledTriggers :many
SELECT targets.id AS target_id, targets.catalog_revision_id, action.name::text AS action_name,
       trigger.definition::jsonb AS trigger_definition
FROM targets
JOIN repositories ON repositories.id = targets.repository_id
JOIN catalog_revisions ON catalog_revisions.id = targets.catalog_revision_id
JOIN catalog_targets ON catalog_targets.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
                   AND catalog_targets.name = targets.name
CROSS JOIN LATERAL jsonb_each(catalog_targets.definition->'spec'->'actions') AS action(name, definition)
CROSS JOIN LATERAL jsonb_array_elements(action.definition->'triggers') WITH ORDINALITY AS trigger(definition, ordinal)
WHERE targets.archived_at IS NULL AND repositories.removed_at IS NULL
  AND jsonb_typeof(trigger.definition->'schedule') = 'object'
ORDER BY targets.id, action.name, trigger.ordinal;

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
