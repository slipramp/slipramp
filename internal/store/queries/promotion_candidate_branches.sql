-- name: ListPromotionCandidateBranchReferences :many
SELECT
    id AS promotion_id,
    integration,
    repository_id,
    candidate_branch,
    candidate_revision,
    status,
    updated_at,
    kargo_outcome_observed_at
FROM promotions
WHERE repository_id = $1
  AND candidate_branch IS NOT NULL
ORDER BY candidate_branch COLLATE "C", updated_at DESC, id DESC;

-- name: UpsertPromotionCandidateBranchCleanup :exec
INSERT INTO promotion_candidate_branch_cleanups (
    repository_id,
    status,
    started_at,
    finished_at,
    observed_branches,
    deleted_branches,
    retained_branches,
    error
) VALUES (
    sqlc.arg(repository_id),
    sqlc.arg(status),
    sqlc.arg(started_at),
    sqlc.narg(finished_at),
    sqlc.arg(observed_branches),
    sqlc.arg(deleted_branches),
    sqlc.arg(retained_branches),
    sqlc.narg(error)
)
ON CONFLICT (repository_id) DO UPDATE SET
    status = EXCLUDED.status,
    started_at = EXCLUDED.started_at,
    finished_at = EXCLUDED.finished_at,
    observed_branches = EXCLUDED.observed_branches,
    deleted_branches = EXCLUDED.deleted_branches,
    retained_branches = EXCLUDED.retained_branches,
    error = EXCLUDED.error;

-- name: GetPromotionCandidateBranchCleanup :one
SELECT *
FROM promotion_candidate_branch_cleanups
WHERE repository_id = $1;
