-- name: ListInboxProblemAcknowledgements :many
SELECT problem_id
FROM inbox_problem_acknowledgements
WHERE principal_id = sqlc.arg(principal_id)
ORDER BY problem_id;

-- name: InsertInboxProblemAcknowledgements :exec
INSERT INTO inbox_problem_acknowledgements (
    principal_id,
    problem_id,
    acknowledged_at
)
SELECT
    sqlc.arg(principal_id),
    problem_id,
    sqlc.arg(acknowledged_at)
FROM unnest(sqlc.arg(problem_ids)::text[]) AS problem_id
ON CONFLICT DO NOTHING;

-- name: ListInboxActiveRuns :many
SELECT
    id,
    phase,
    target_name,
    action_name,
    cluster_name,
    created_at,
    review_state,
    assigned_at,
    last_observed_at
FROM runs
WHERE phase IN ('queued', 'assigned', 'preparing', 'running')
ORDER BY created_at, id;

-- name: ListInboxPromotions :many
SELECT
    id,
    stage_snapshot,
    candidate_branch,
    target_branch,
    status,
    message,
    created_at,
    updated_at
FROM promotions
WHERE status IN ('pending', 'error')
ORDER BY created_at DESC, id DESC;

-- name: ListInboxFailedGitTriggerDeliveries :many
SELECT
    delivery.id,
    delivery.repository_id,
    repositories.name AS repository_name,
    delivery.catalog_revision_id,
    catalog_revisions.commit_sha,
    delivery.target_name,
    delivery.action_name,
    delivery.trigger_name,
    delivery.failure_count,
    delivery.diagnostic,
    delivery.failed_at
FROM git_trigger_deliveries AS delivery
JOIN repositories ON repositories.id = delivery.repository_id
JOIN catalog_revisions ON catalog_revisions.id = delivery.catalog_revision_id
WHERE delivery.failed_at IS NOT NULL
ORDER BY delivery.failed_at DESC, delivery.id DESC;
