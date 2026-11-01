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
    direct_freight_json,
    auto_promotion_enabled,
    health_status,
    argo_application_namespace,
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
    sqlc.arg(direct_freight_json),
    sqlc.arg(auto_promotion_enabled),
    sqlc.narg(health_status),
    sqlc.narg(argo_application_namespace),
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
    direct_freight_json = EXCLUDED.direct_freight_json,
    auto_promotion_enabled = EXCLUDED.auto_promotion_enabled,
    health_status = EXCLUDED.health_status,
    argo_application_namespace = EXCLUDED.argo_application_namespace,
    successful_render_acknowledgement = COALESCE(
        EXCLUDED.successful_render_acknowledgement,
        promotion_stages.successful_render_acknowledgement
    ),
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

-- name: CountPromotionStageReviews :one
SELECT count(*)
FROM promotion_stages
WHERE (sqlc.narg(integration)::text IS NULL OR integration = sqlc.narg(integration))
  AND (sqlc.narg(cluster_name)::text IS NULL OR cluster_name = sqlc.narg(cluster_name))
  AND (sqlc.narg(project)::text IS NULL OR project = sqlc.narg(project))
  AND (sqlc.narg(application)::text IS NULL OR application = sqlc.narg(application))
  AND (sqlc.narg(archived)::boolean IS NULL OR (archived_at IS NOT NULL) = sqlc.narg(archived));

-- name: ListPromotionStageReviews :many
WITH paged_stages AS (
    SELECT id
    FROM promotion_stages
    WHERE (sqlc.narg(integration)::text IS NULL OR integration = sqlc.narg(integration))
      AND (sqlc.narg(cluster_name)::text IS NULL OR cluster_name = sqlc.narg(cluster_name))
      AND (sqlc.narg(project)::text IS NULL OR project = sqlc.narg(project))
      AND (sqlc.narg(application)::text IS NULL OR application = sqlc.narg(application))
      AND (sqlc.narg(archived)::boolean IS NULL OR (archived_at IS NOT NULL) = sqlc.narg(archived))
    ORDER BY integration COLLATE "C", project COLLATE "C", name COLLATE "C"
    LIMIT sqlc.arg(result_limit)
    OFFSET sqlc.arg(result_offset)::bigint
), pending_counts AS (
    SELECT promotions.stage_id, count(*) AS pending_promotion_count
    FROM promotions
    JOIN paged_stages ON paged_stages.id = promotions.stage_id
    WHERE promotions.status = 'pending'
    GROUP BY promotions.stage_id
)
SELECT
    sqlc.embed(promotion_stages),
    promotion_stage_policies.stage_id AS policy_stage_id,
    promotion_stage_policies.mode AS policy_mode,
    promotion_stage_policies.expires_at AS policy_expires_at,
    promotion_stage_policies.reason AS policy_reason,
    promotion_stage_policies.updated_by_principal_id AS policy_updated_by_principal_id,
    promotion_stage_policies.updated_by_snapshot AS policy_updated_by_snapshot,
    promotion_stage_policies.updated_at AS policy_updated_at,
    COALESCE(pending_counts.pending_promotion_count, 0)::bigint AS pending_promotion_count
FROM paged_stages
JOIN promotion_stages ON promotion_stages.id = paged_stages.id
LEFT JOIN promotion_stage_policies ON promotion_stage_policies.stage_id = promotion_stages.id
LEFT JOIN pending_counts ON pending_counts.stage_id = promotion_stages.id
ORDER BY
    promotion_stages.integration COLLATE "C",
    promotion_stages.project COLLATE "C",
    promotion_stages.name COLLATE "C";

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

-- name: ListPromotionStagePolicies :many
SELECT *
FROM promotion_stage_policies
WHERE stage_id = ANY(sqlc.arg(stage_ids)::uuid[]);

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
    next_reconciliation_at,
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
    sqlc.narg(next_reconciliation_at),
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
    next_reconciliation_at = CASE
        WHEN review_required OR kargo_outcome_phase IS NOT NULL THEN NULL
        ELSE sqlc.arg(hydrated_at)::timestamptz
    END,
    message = NULL,
    updated_at = GREATEST(updated_at, sqlc.arg(hydrated_at)::timestamptz)
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
        WHEN status IN ('creating', 'approved')
        THEN COALESCE(next_reconciliation_at, sqlc.arg(dispatched_at))
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

-- name: LockNextPromotionReconciliation :one
SELECT *
FROM promotions
WHERE next_reconciliation_at <= sqlc.arg(due_at)
  AND kargo_outcome_phase IS NULL
  AND (
      (status = 'approved' AND (source = 'kargo' OR kargo_promotion_uid IS NOT NULL))
      OR (source = 'slipramp' AND status = 'creating' AND kargo_promotion_uid IS NOT NULL)
  )
  AND stage_snapshot ->> 'cluster' = sqlc.arg(cluster_name)::text
ORDER BY next_reconciliation_at, created_at, id
FOR UPDATE SKIP LOCKED
LIMIT 1;

-- name: ClaimPromotionReconciliation :one
UPDATE promotions
SET next_reconciliation_at = sqlc.arg(next_reconciliation_at),
    last_reconciliation_at = sqlc.arg(claimed_at)
WHERE id = sqlc.arg(id)
  AND kargo_outcome_phase IS NULL
  AND (
      (status = 'approved' AND (source = 'kargo' OR kargo_promotion_uid IS NOT NULL))
      OR (source = 'slipramp' AND status = 'creating' AND kargo_promotion_uid IS NOT NULL)
  )
  AND next_reconciliation_at = sqlc.arg(expected_next_reconciliation_at)
RETURNING *;

-- name: BindKargoPromotionReconciliationIdentity :one
UPDATE promotions
SET kargo_promotion_uid = sqlc.arg(kargo_promotion_uid),
    updated_at = GREATEST(updated_at, sqlc.arg(observed_at))
WHERE id = sqlc.arg(id)
  AND source = 'kargo'
  AND kargo_promotion_uid IS NULL
  AND kargo_outcome_phase IS NULL
RETURNING *;

-- name: RecordTerminalKargoPromotionOutcome :one
UPDATE promotions
SET status = CASE
        WHEN status = 'creating' AND request_digest IS NULL THEN 'error'
        ELSE status
    END,
    next_reconciliation_at = NULL,
    kargo_outcome_phase = sqlc.arg(kargo_outcome_phase),
    kargo_outcome_observed_at = sqlc.arg(kargo_outcome_observed_at),
    kargo_outcome_diagnostic = sqlc.narg(kargo_outcome_diagnostic),
    message = CASE
        WHEN status = 'creating' AND request_digest IS NULL THEN sqlc.narg(kargo_outcome_diagnostic)
        ELSE message
    END,
    updated_at = GREATEST(updated_at, sqlc.arg(kargo_outcome_observed_at))
WHERE id = sqlc.arg(id)
  AND kargo_outcome_phase IS NULL
RETURNING *;

-- name: CountPromotionReviews :one
SELECT count(*)
FROM promotions
WHERE (sqlc.narg(repository_id)::uuid IS NULL OR repository_id = sqlc.narg(repository_id))
  AND (
      sqlc.narg(commit_sha)::text IS NULL
      OR base_revision = sqlc.narg(commit_sha)
      OR candidate_revision = sqlc.narg(commit_sha)
  )
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(integration)::text IS NULL OR integration = sqlc.narg(integration))
  AND (sqlc.narg(stage_id)::uuid IS NULL OR stage_id = sqlc.narg(stage_id))
  AND (sqlc.narg(cluster_name)::text IS NULL OR stage_snapshot->>'cluster' = sqlc.narg(cluster_name))
  AND (sqlc.narg(project)::text IS NULL OR stage_snapshot->>'project' = sqlc.narg(project))
  AND (sqlc.narg(application)::text IS NULL OR stage_snapshot->>'application' = sqlc.narg(application));

-- name: ListPromotionReviews :many
SELECT *
FROM promotions
WHERE (sqlc.narg(repository_id)::uuid IS NULL OR repository_id = sqlc.narg(repository_id))
  AND (
      sqlc.narg(commit_sha)::text IS NULL
      OR base_revision = sqlc.narg(commit_sha)
      OR candidate_revision = sqlc.narg(commit_sha)
  )
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(integration)::text IS NULL OR integration = sqlc.narg(integration))
  AND (sqlc.narg(stage_id)::uuid IS NULL OR stage_id = sqlc.narg(stage_id))
  AND (sqlc.narg(cluster_name)::text IS NULL OR stage_snapshot->>'cluster' = sqlc.narg(cluster_name))
  AND (sqlc.narg(project)::text IS NULL OR stage_snapshot->>'project' = sqlc.narg(project))
  AND (sqlc.narg(application)::text IS NULL OR stage_snapshot->>'application' = sqlc.narg(application))
ORDER BY (status = 'pending') DESC, created_at DESC, id DESC
LIMIT sqlc.arg(result_limit)
OFFSET sqlc.arg(result_offset)::bigint;

-- name: CountPendingPromotionsByStage :many
SELECT stage_id, count(*) AS pending_promotion_count
FROM promotions
WHERE status = 'pending'
  AND stage_id = ANY(sqlc.arg(stage_ids)::uuid[])
GROUP BY stage_id;

-- name: GetPromotionAwaitingDispatch :one
SELECT *
FROM promotions
WHERE review_required
  AND status = 'preparing'
  AND preparation_run_id IS NULL
  AND (next_preparation_attempt_at IS NULL OR next_preparation_attempt_at <= sqlc.arg(attempted_at))
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
  AND (promotions.next_preparation_attempt_at IS NULL
       OR promotions.next_preparation_attempt_at <= sqlc.arg(attempted_at))
ORDER BY promotions.created_at, promotions.id
LIMIT 1;

-- name: RecordPromotionPreparationFailure :one
UPDATE promotions
SET preparation_failure_count = preparation_failure_count + 1,
    next_preparation_attempt_at = CASE
        WHEN sqlc.arg(permanent)::boolean
            OR preparation_failure_count + 1 >= sqlc.arg(failure_limit)::integer THEN NULL
        ELSE sqlc.arg(failed_at)::timestamptz + make_interval(secs =>
            LEAST(sqlc.arg(max_delay_seconds)::double precision,
                  sqlc.arg(initial_delay_seconds)::double precision * power(2, preparation_failure_count)))
    END,
    status = CASE
        WHEN sqlc.arg(permanent)::boolean
            OR preparation_failure_count + 1 >= sqlc.arg(failure_limit)::integer THEN 'error'
        ELSE status
    END,
    preparation_completed_at = CASE
        WHEN sqlc.arg(permanent)::boolean
            OR preparation_failure_count + 1 >= sqlc.arg(failure_limit)::integer THEN sqlc.arg(failed_at)
        ELSE preparation_completed_at
    END,
    message = CASE
        WHEN NOT sqlc.arg(permanent)::boolean
            AND preparation_failure_count + 1 >= sqlc.arg(failure_limit)::integer
        THEN sqlc.arg(exhausted_message)::text
        ELSE sqlc.arg(diagnostic)::text
    END,
    updated_at = sqlc.arg(failed_at)
WHERE id = sqlc.arg(id)
  AND review_required
  AND status = 'preparing'
  AND preparation_run_id IS NOT DISTINCT FROM sqlc.narg(preparation_run_id)::uuid
  AND (next_preparation_attempt_at IS NULL OR next_preparation_attempt_at <= sqlc.arg(failed_at))
RETURNING preparation_failure_count, status;

-- name: LinkPromotionPreparationRun :execrows
UPDATE promotions
SET preparation_run_id = sqlc.arg(preparation_run_id),
    preparation_failure_count = 0,
    next_preparation_attempt_at = NULL,
    message = NULL,
    updated_at = sqlc.arg(created_at)
WHERE id = sqlc.arg(id)
  AND review_required
  AND status = 'preparing'
  AND preparation_repository_id = sqlc.arg(repository_id)
  AND preparation_catalog_revision_id = sqlc.arg(catalog_revision_id)
  AND requested_by_principal_id = sqlc.arg(created_by_principal_id)
  AND requested_by_snapshot = sqlc.arg(created_by_snapshot)::jsonb
  AND preparation_run_id IS NULL
  AND (next_preparation_attempt_at IS NULL OR next_preparation_attempt_at <= sqlc.arg(created_at));

-- name: CompletePromotionPreparation :one
UPDATE promotions
SET status = 'pending',
    next_preparation_attempt_at = NULL,
    message = NULL,
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
    next_preparation_attempt_at = NULL,
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
    next_reconciliation_at = CASE
        WHEN sqlc.arg(status)::text = 'approved' AND kargo_outcome_phase IS NULL
        THEN sqlc.arg(decided_at)::timestamptz
        ELSE NULL
    END,
    updated_at = sqlc.arg(decided_at)::timestamptz
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
