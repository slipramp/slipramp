-- name: UpsertPromotionStage :one
INSERT INTO promotion_stages (
    id,
    integration,
    cluster_name,
    project,
    name,
    kubernetes_uid,
    resource_version,
    application,
    environment,
    repository_name,
    repository_url,
    target_branch,
    source_paths,
    requested_freight_json,
    freight_json,
    successful_render_acknowledgement,
    first_observed_at,
    last_observed_at,
    archived_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(integration),
    sqlc.arg(cluster_name),
    sqlc.arg(project),
    sqlc.arg(name),
    sqlc.arg(kubernetes_uid),
    sqlc.arg(resource_version),
    sqlc.arg(application),
    sqlc.arg(environment),
    sqlc.arg(repository_name),
    sqlc.arg(repository_url),
    sqlc.arg(target_branch),
    sqlc.arg(source_paths),
    sqlc.arg(requested_freight_json),
    sqlc.arg(freight_json),
    sqlc.narg(successful_render_acknowledgement),
    sqlc.arg(first_observed_at),
    sqlc.arg(last_observed_at),
    sqlc.narg(archived_at)
)
ON CONFLICT (integration, project, name) DO UPDATE
SET cluster_name = EXCLUDED.cluster_name,
    kubernetes_uid = EXCLUDED.kubernetes_uid,
    resource_version = EXCLUDED.resource_version,
    application = EXCLUDED.application,
    environment = EXCLUDED.environment,
    repository_name = EXCLUDED.repository_name,
    repository_url = EXCLUDED.repository_url,
    target_branch = EXCLUDED.target_branch,
    source_paths = EXCLUDED.source_paths,
    requested_freight_json = EXCLUDED.requested_freight_json,
    freight_json = EXCLUDED.freight_json,
    successful_render_acknowledgement = EXCLUDED.successful_render_acknowledgement,
    last_observed_at = EXCLUDED.last_observed_at,
    archived_at = EXCLUDED.archived_at
WHERE promotion_stages.cluster_name = EXCLUDED.cluster_name
  AND promotion_stages.last_observed_at <= EXCLUDED.last_observed_at
RETURNING *;

-- name: GetPromotionStage :one
SELECT *
FROM promotion_stages
WHERE id = sqlc.arg(id);

-- name: GetPromotionStageByIdentity :one
SELECT *
FROM promotion_stages
WHERE integration = sqlc.arg(integration)
  AND project = sqlc.arg(project)
  AND name = sqlc.arg(name);

-- name: ListPromotionStages :many
SELECT *
FROM promotion_stages
ORDER BY integration, project, name;

-- name: UpsertPromotionStagePolicy :one
INSERT INTO promotion_stage_policies (
    stage_id,
    mode,
    expires_at,
    reason,
    updated_by_principal_id,
    updated_by_snapshot,
    updated_at
) VALUES (
    sqlc.arg(stage_id),
    sqlc.arg(mode),
    sqlc.narg(expires_at),
    sqlc.narg(reason),
    sqlc.arg(updated_by_principal_id),
    sqlc.arg(updated_by_snapshot),
    sqlc.arg(updated_at)
)
ON CONFLICT (stage_id) DO UPDATE
SET mode = EXCLUDED.mode,
    expires_at = EXCLUDED.expires_at,
    reason = EXCLUDED.reason,
    updated_by_principal_id = EXCLUDED.updated_by_principal_id,
    updated_by_snapshot = EXCLUDED.updated_by_snapshot,
    updated_at = EXCLUDED.updated_at
WHERE promotion_stage_policies.updated_at <= EXCLUDED.updated_at
RETURNING *;

-- name: GetPromotionStagePolicy :one
SELECT *
FROM promotion_stage_policies
WHERE stage_id = sqlc.arg(stage_id);

-- name: GetActivePromotionRepositoryByName :one
SELECT
    id,
    name,
    clone_url,
    branch,
    checkout_credential_name
FROM repositories
WHERE name = sqlc.arg(name)
  AND removed_at IS NULL;

-- name: CreateKargoStageSnapshot :one
INSERT INTO kargo_stage_snapshots (
    snapshot_id,
    integration,
    cluster_name,
    observed_at,
    accepted_at
) VALUES (
    sqlc.arg(snapshot_id),
    sqlc.arg(integration),
    sqlc.arg(cluster_name),
    sqlc.arg(observed_at),
    sqlc.arg(accepted_at)
)
RETURNING *;

-- name: GetKargoStageSnapshot :one
SELECT *
FROM kargo_stage_snapshots
WHERE snapshot_id = sqlc.arg(snapshot_id);

-- name: LockKargoStageSnapshots :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(integration), 0));

-- name: GetLatestKargoStageSnapshot :one
SELECT *
FROM kargo_stage_snapshots
WHERE integration = sqlc.arg(integration)
  AND cluster_name = sqlc.arg(cluster_name)
ORDER BY observed_at DESC, accepted_at DESC, snapshot_id DESC
LIMIT 1;

-- name: ArchiveMissingPromotionStages :exec
UPDATE promotion_stages
SET archived_at = sqlc.arg(archived_at)
WHERE integration = sqlc.arg(integration)
  AND cluster_name = sqlc.arg(cluster_name)
  AND archived_at IS NULL
  AND last_observed_at < sqlc.arg(archived_at);

-- name: CreatePromotion :one
INSERT INTO promotions (
    id,
    source,
    integration,
    external_promotion_id,
    request_digest,
    stage_id,
    source_stage_id,
    stage_snapshot,
    repository_id,
    repository_snapshot,
    base_revision,
    candidate_revision,
    candidate_branch,
    target_branch,
    policy_snapshot,
    selected_freight_snapshot,
    review_required,
    status,
    message,
    dispatch_attempt_count,
    next_dispatch_at,
    last_dispatch_at,
    dispatched_at,
    kargo_promotion_uid,
    preparation_repository_id,
    preparation_catalog_revision_id,
    preparation_run_id,
    git_diff_artifact_id,
    argo_diff_artifact_id,
    requested_by_principal_id,
    requested_by_snapshot,
    created_at,
    preparation_completed_at,
    decided_at,
    expires_at,
    updated_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(source),
    sqlc.arg(integration),
    sqlc.arg(external_promotion_id),
    sqlc.narg(request_digest),
    sqlc.arg(stage_id),
    sqlc.narg(source_stage_id),
    sqlc.arg(stage_snapshot),
    sqlc.arg(repository_id),
    sqlc.arg(repository_snapshot),
    sqlc.narg(base_revision),
    sqlc.narg(candidate_revision),
    sqlc.narg(candidate_branch),
    sqlc.arg(target_branch),
    sqlc.arg(policy_snapshot),
    sqlc.narg(selected_freight_snapshot),
    sqlc.arg(review_required),
    sqlc.arg(status),
    sqlc.narg(message),
    sqlc.arg(dispatch_attempt_count),
    sqlc.narg(next_dispatch_at),
    sqlc.narg(last_dispatch_at),
    sqlc.narg(dispatched_at),
    sqlc.narg(kargo_promotion_uid),
    sqlc.narg(preparation_repository_id),
    sqlc.narg(preparation_catalog_revision_id),
    sqlc.narg(preparation_run_id),
    sqlc.narg(git_diff_artifact_id),
    sqlc.narg(argo_diff_artifact_id),
    sqlc.arg(requested_by_principal_id),
    sqlc.arg(requested_by_snapshot),
    sqlc.arg(created_at),
    sqlc.narg(preparation_completed_at),
    sqlc.narg(decided_at),
    sqlc.narg(expires_at),
    sqlc.arg(updated_at)
)
RETURNING *;

-- name: GetPromotion :one
SELECT *
FROM promotions
WHERE id = sqlc.arg(id);

-- name: GetPromotionByExternalIdentity :one
SELECT *
FROM promotions
WHERE integration = sqlc.arg(integration)
  AND external_promotion_id = sqlc.arg(external_promotion_id);

-- name: LockPromotionByExternalIdentity :one
SELECT *
FROM promotions
WHERE integration = sqlc.arg(integration)
  AND external_promotion_id = sqlc.arg(external_promotion_id)
FOR UPDATE;

-- name: HydrateOperatorPromotion :one
UPDATE promotions
SET request_digest = sqlc.arg(request_digest),
    base_revision = sqlc.arg(base_revision),
    candidate_revision = sqlc.arg(candidate_revision),
    candidate_branch = sqlc.arg(candidate_branch),
    status = CASE WHEN review_required THEN 'preparing' ELSE 'approved' END,
    next_dispatch_at = NULL,
    next_reconciliation_at = NULL,
    message = NULL,
    updated_at = GREATEST(updated_at, sqlc.arg(hydrated_at))
WHERE id = sqlc.arg(id)
  AND source = 'slipramp'
  AND status = 'creating'
  AND request_digest IS NULL
RETURNING *;

-- name: LockNextPromotionCreationDispatch :one
SELECT *
FROM promotions
WHERE source = 'slipramp'
  AND status = 'creating'
  AND next_dispatch_at <= sqlc.arg(due_at)
  AND stage_snapshot ->> 'cluster' = sqlc.arg(cluster_name)::text
ORDER BY next_dispatch_at, created_at, id
FOR UPDATE SKIP LOCKED
LIMIT 1;

-- name: ClaimPromotionCreationDispatch :one
UPDATE promotions
SET dispatch_attempt_count = dispatch_attempt_count + 1,
    next_dispatch_at = sqlc.arg(next_dispatch_at),
    last_dispatch_at = sqlc.arg(claimed_at),
    message = NULL,
    updated_at = sqlc.arg(claimed_at)
WHERE id = sqlc.arg(id)
  AND source = 'slipramp'
  AND status = 'creating'
  AND dispatch_attempt_count = sqlc.arg(expected_attempt_count)
RETURNING *;

-- name: ExhaustPromotionCreationDispatch :one
UPDATE promotions
SET status = 'error',
    next_dispatch_at = NULL,
    next_reconciliation_at = NULL,
    message = sqlc.arg(message),
    updated_at = sqlc.arg(failed_at)
WHERE id = sqlc.arg(id)
  AND source = 'slipramp'
  AND status = 'creating'
RETURNING *;

-- name: LockPromotionCreation :one
SELECT *
FROM promotions
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: RecordCreatedPromotionDispatch :one
UPDATE promotions
SET next_dispatch_at = NULL,
    dispatched_at = COALESCE(dispatched_at, sqlc.arg(dispatched_at)),
    external_promotion_id = sqlc.arg(kargo_name),
    kargo_promotion_uid = sqlc.arg(kargo_promotion_uid),
    next_reconciliation_at = CASE
        WHEN status = 'creating' THEN COALESCE(next_reconciliation_at, sqlc.arg(dispatched_at))
        ELSE NULL
    END,
    message = CASE WHEN status = 'creating' THEN NULL ELSE message END,
    updated_at = GREATEST(updated_at, sqlc.arg(dispatched_at))
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: RecordRetryablePromotionDispatch :one
UPDATE promotions
SET message = sqlc.arg(message),
    updated_at = sqlc.arg(reported_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: RecordFailedPromotionDispatch :one
UPDATE promotions
SET status = 'error',
    next_dispatch_at = NULL,
    next_reconciliation_at = NULL,
    message = sqlc.arg(message),
    updated_at = sqlc.arg(reported_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: LockNextPromotionCreationReconciliation :one
SELECT *
FROM promotions
WHERE source = 'slipramp'
  AND status = 'creating'
  AND kargo_promotion_uid IS NOT NULL
  AND next_reconciliation_at <= sqlc.arg(due_at)
  AND stage_snapshot ->> 'cluster' = sqlc.arg(cluster_name)::text
ORDER BY next_reconciliation_at, created_at, id
FOR UPDATE SKIP LOCKED
LIMIT 1;

-- name: ClaimPromotionCreationReconciliation :one
UPDATE promotions
SET next_reconciliation_at = sqlc.arg(next_reconciliation_at),
    last_reconciliation_at = sqlc.arg(claimed_at)
WHERE id = sqlc.arg(id)
  AND source = 'slipramp'
  AND status = 'creating'
  AND kargo_promotion_uid IS NOT NULL
  AND next_reconciliation_at = sqlc.arg(expected_next_reconciliation_at)
RETURNING *;

-- name: RecordTerminalPromotionCreationReconciliation :one
UPDATE promotions
SET status = 'error',
    next_reconciliation_at = NULL,
    message = sqlc.arg(message),
    updated_at = GREATEST(updated_at, sqlc.arg(reported_at))
WHERE id = sqlc.arg(id)
  AND source = 'slipramp'
  AND status = 'creating'
  AND request_digest IS NULL
RETURNING *;

-- name: ListPromotions :many
SELECT *
FROM promotions
ORDER BY created_at DESC, id DESC;

-- name: GetPromotionAwaitingDispatch :one
SELECT *
FROM promotions
WHERE review_required
  AND status = 'preparing'
  AND preparation_run_id IS NULL
ORDER BY created_at, id
LIMIT 1;

-- name: ReservePromotionPreparationRevision :one
UPDATE promotions
SET preparation_repository_id = sqlc.arg(preparation_repository_id),
    preparation_catalog_revision_id = sqlc.arg(preparation_catalog_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND review_required
  AND status = 'preparing'
  AND preparation_run_id IS NULL
  AND (
      preparation_catalog_revision_id IS NULL
      OR (
          preparation_repository_id = sqlc.arg(preparation_repository_id)
          AND preparation_catalog_revision_id = sqlc.arg(preparation_catalog_revision_id)
      )
  )
RETURNING *;

-- name: GetPromotionWithFinishedPreparation :one
SELECT promotions.*
FROM promotions
JOIN runs ON runs.id = promotions.preparation_run_id
WHERE promotions.review_required
  AND promotions.status = 'preparing'
  AND runs.phase = 'finished'
ORDER BY promotions.created_at, promotions.id
LIMIT 1;

-- name: AttachPromotionPreparationRun :one
UPDATE promotions
SET preparation_run_id = sqlc.arg(preparation_run_id),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND review_required
  AND status = 'preparing'
  AND preparation_catalog_revision_id IS NOT NULL
  AND (preparation_run_id IS NULL OR preparation_run_id = sqlc.arg(preparation_run_id))
RETURNING *;

-- name: CompletePromotionPreparation :one
UPDATE promotions
SET status = 'pending',
    git_diff_artifact_id = sqlc.arg(git_diff_artifact_id),
    argo_diff_artifact_id = sqlc.arg(argo_diff_artifact_id),
    preparation_completed_at = sqlc.arg(preparation_completed_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND review_required
  AND status = 'preparing'
  AND preparation_run_id = sqlc.arg(preparation_run_id)
RETURNING *;

-- name: FailPromotionPreparation :one
UPDATE promotions
SET status = 'error',
    message = sqlc.arg(message),
    preparation_completed_at = sqlc.arg(preparation_completed_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND review_required
  AND status = 'preparing'
  AND preparation_run_id = sqlc.arg(preparation_run_id)
RETURNING *;

-- name: LockPendingPromotion :one
SELECT *
FROM promotions
WHERE id = sqlc.arg(id)
  AND status = 'pending'
FOR UPDATE;

-- name: UpdatePromotionDecisionState :one
UPDATE promotions
SET status = sqlc.arg(status),
    message = sqlc.narg(message),
    decided_at = sqlc.arg(decided_at),
    updated_at = sqlc.arg(decided_at)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
RETURNING *;

-- name: InsertPromotionDecision :one
INSERT INTO promotion_decisions (
    promotion_id,
    decision,
    comment,
    decided_by_principal_id,
    decided_by_snapshot,
    decided_at
) VALUES (
    sqlc.arg(promotion_id),
    sqlc.arg(decision),
    sqlc.narg(comment),
    sqlc.arg(decided_by_principal_id),
    sqlc.arg(decided_by_snapshot),
    sqlc.arg(decided_at)
)
RETURNING *;

-- name: GetPromotionDecision :one
SELECT *
FROM promotion_decisions
WHERE promotion_id = sqlc.arg(promotion_id);

-- name: ExpirePendingPromotions :many
UPDATE promotions
SET status = 'expired',
    message = 'The review window expired before a decision was recorded.',
    updated_at = sqlc.arg(expired_at)
WHERE status = 'pending'
  AND expires_at <= sqlc.arg(expired_at)
RETURNING *;

-- name: ExpirePendingPromotion :one
UPDATE promotions
SET status = 'expired',
    message = 'The review window expired before a decision was recorded.',
    updated_at = sqlc.arg(expired_at)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
  AND expires_at <= sqlc.arg(expired_at)
RETURNING *;
