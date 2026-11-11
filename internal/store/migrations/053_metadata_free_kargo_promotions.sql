-- +goose Up
ALTER TABLE promotions
ADD COLUMN dispatch_failed_at timestamptz,
ADD COLUMN dispatch_diagnostic text;

UPDATE promotions
SET dispatch_failed_at = updated_at,
    dispatch_diagnostic = COALESCE(message, 'Kargo Promotion creation failed.')
WHERE source = 'slipramp'
  AND dispatched_at IS NULL
  AND next_dispatch_at IS NULL;

UPDATE promotions
SET stage_snapshot = stage_snapshot
    - 'application'
    - 'environment'
    - 'repository'
    - 'repositoryUrl'
    - 'targetBranch'
    - 'sourcePaths'
    || jsonb_build_object(
        'enrichment',
        jsonb_build_object(
            'application', stage_snapshot -> 'application',
            'environment', stage_snapshot -> 'environment',
            'repository', stage_snapshot -> 'repository',
            'repositoryUrl', stage_snapshot -> 'repositoryUrl',
            'targetBranch', stage_snapshot -> 'targetBranch',
            'sourcePaths', stage_snapshot -> 'sourcePaths'
        )
    );

ALTER TABLE promotions
ALTER COLUMN repository_id DROP NOT NULL,
ALTER COLUMN repository_snapshot DROP NOT NULL,
ALTER COLUMN target_branch DROP NOT NULL,
DROP CONSTRAINT promotions_snapshots_valid,
DROP CONSTRAINT promotions_callback_state_valid,
DROP CONSTRAINT promotions_branches_valid,
DROP CONSTRAINT promotions_source_state_valid,
DROP CONSTRAINT promotions_dispatch_valid,
DROP CONSTRAINT promotions_automatic_state_valid,
DROP CONSTRAINT promotions_preparation_state_valid;

UPDATE promotions
SET status = 'approved',
    message = NULL
WHERE source = 'slipramp'
  AND NOT review_required
  AND request_digest IS NULL
  AND status IN ('creating', 'error');

ALTER TABLE promotions
ADD CONSTRAINT promotions_snapshots_valid CHECK (
    jsonb_typeof(stage_snapshot) = 'object'
    AND jsonb_typeof(policy_snapshot) = 'object'
    AND (repository_snapshot IS NULL OR jsonb_typeof(repository_snapshot) = 'object')
),
ADD CONSTRAINT promotions_review_context_valid CHECK (
    (
        repository_id IS NULL
        AND repository_snapshot IS NULL
        AND target_branch IS NULL
        AND base_revision IS NULL
        AND candidate_revision IS NULL
        AND candidate_branch IS NULL
    )
    OR (
        repository_id IS NOT NULL
        AND repository_snapshot IS NOT NULL
        AND jsonb_typeof(repository_snapshot) = 'object'
        AND target_branch IS NOT NULL
        AND length(target_branch) BETWEEN 1 AND 255
        AND (
            (
                base_revision IS NULL
                AND candidate_revision IS NULL
                AND candidate_branch IS NULL
            )
            OR (
                base_revision IS NOT NULL
                AND candidate_revision IS NOT NULL
                AND candidate_branch IS NOT NULL
                AND base_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
                AND candidate_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
                AND length(candidate_branch) BETWEEN 17 AND 255
                AND candidate_branch LIKE 'kargo/promotion/%'
            )
        )
    )
),
ADD CONSTRAINT promotions_review_policy_context_valid CHECK (
    NOT review_required
    OR (
        repository_id IS NOT NULL
        AND (
            status IN ('creating', 'error')
            OR (
                base_revision IS NOT NULL
                AND candidate_revision IS NOT NULL
                AND candidate_branch IS NOT NULL
            )
        )
    )
),
ADD CONSTRAINT promotions_callback_state_valid CHECK (
    request_digest IS NULL OR octet_length(request_digest) = 32
),
ADD CONSTRAINT promotions_source_state_valid CHECK (
    (
        source = 'kargo'
        AND request_digest IS NOT NULL
        AND source_stage_id IS NULL
        AND selected_freight_snapshot IS NULL
        AND dispatch_attempt_count = 0
        AND next_dispatch_at IS NULL
        AND last_dispatch_at IS NULL
        AND dispatched_at IS NULL
        AND dispatch_failed_at IS NULL
        AND dispatch_diagnostic IS NULL
    )
    OR (
        source = 'slipramp'
        AND selected_freight_snapshot IS NOT NULL
        AND jsonb_typeof(selected_freight_snapshot) = 'object'
    )
),
ADD CONSTRAINT promotions_dispatch_valid CHECK (
    dispatch_attempt_count >= 0
    AND (last_dispatch_at IS NULL OR last_dispatch_at >= created_at)
    AND (dispatched_at IS NULL OR dispatched_at >= created_at)
    AND (dispatch_failed_at IS NULL OR dispatch_failed_at >= created_at)
    AND (dispatch_diagnostic IS NULL OR length(dispatch_diagnostic) BETWEEN 1 AND 4096)
    AND (dispatch_failed_at IS NULL OR dispatch_diagnostic IS NOT NULL)
    AND num_nonnulls(next_dispatch_at, dispatched_at, dispatch_failed_at) = CASE
        WHEN source = 'slipramp' THEN 1
        ELSE 0
    END
    AND (kargo_promotion_uid IS NULL OR length(kargo_promotion_uid) BETWEEN 1 AND 200)
    AND (kargo_promotion_uid IS NULL OR source = 'kargo' OR dispatched_at IS NOT NULL)
    AND (next_reconciliation_at IS NULL OR (
        kargo_outcome_phase IS NULL
        AND next_reconciliation_at >= created_at
        AND (
            (source = 'kargo' AND status = 'approved')
            OR (
                source = 'slipramp'
                AND dispatched_at IS NOT NULL
                AND kargo_promotion_uid IS NOT NULL
                AND status IN ('creating', 'approved')
            )
        )
    ))
    AND (
        source <> 'kargo'
        OR status <> 'approved'
        OR kargo_outcome_phase IS NOT NULL
        OR next_reconciliation_at IS NOT NULL
    )
    AND (
        source <> 'slipramp'
        OR dispatched_at IS NULL
        OR status NOT IN ('creating', 'approved')
        OR kargo_outcome_phase IS NOT NULL
        OR next_reconciliation_at IS NOT NULL
    )
    AND (kargo_outcome_phase IS NULL OR next_reconciliation_at IS NULL)
    AND (last_reconciliation_at IS NULL OR last_reconciliation_at >= created_at)
),
ADD CONSTRAINT promotions_automatic_state_valid CHECK (
    review_required
    OR (
        status IN ('creating', 'approved', 'error')
        AND preparation_repository_id IS NULL
        AND preparation_catalog_revision_id IS NULL
        AND preparation_run_id IS NULL
        AND git_diff_artifact_id IS NULL
        AND argo_diff_artifact_id IS NULL
        AND preparation_completed_at IS NULL
        AND decided_at IS NULL
    )
),
ADD CONSTRAINT promotions_preparation_state_valid CHECK (
    (
        status = 'creating'
        AND review_required
        AND preparation_repository_id IS NULL
        AND preparation_catalog_revision_id IS NULL
        AND preparation_run_id IS NULL
        AND preparation_completed_at IS NULL
        AND git_diff_artifact_id IS NULL
        AND argo_diff_artifact_id IS NULL
        AND decided_at IS NULL
    )
    OR (
        status = 'preparing'
        AND review_required
        AND preparation_completed_at IS NULL
        AND git_diff_artifact_id IS NULL
        AND argo_diff_artifact_id IS NULL
        AND decided_at IS NULL
    )
    OR (
        status IN ('pending', 'approved', 'rejected', 'expired')
        AND review_required
        AND preparation_run_id IS NOT NULL
        AND preparation_completed_at IS NOT NULL
        AND git_diff_artifact_id IS NOT NULL
        AND argo_diff_artifact_id IS NOT NULL
        AND ((status IN ('approved', 'rejected')) = (decided_at IS NOT NULL))
    )
    OR (
        status = 'error'
        AND review_required
        AND decided_at IS NULL
    )
    OR (
        NOT review_required
        AND status IN ('creating', 'approved', 'error')
    )
);

DROP INDEX promotions_creation_dispatch_idx;

CREATE INDEX promotions_creation_dispatch_idx
ON promotions (next_dispatch_at, created_at, id)
WHERE source = 'slipramp'
  AND next_dispatch_at IS NOT NULL
  AND dispatched_at IS NULL
  AND dispatch_failed_at IS NULL;

-- +goose Down
DROP INDEX promotions_creation_dispatch_idx;

DELETE FROM promotions
WHERE repository_id IS NULL;

UPDATE promotions
SET status = CASE WHEN dispatch_failed_at IS NULL THEN 'creating' ELSE 'error' END,
    message = CASE WHEN dispatch_failed_at IS NULL THEN NULL ELSE dispatch_diagnostic END
WHERE source = 'slipramp'
  AND NOT review_required
  AND request_digest IS NULL
  AND status = 'approved';

UPDATE promotions
SET stage_snapshot = stage_snapshot
    - 'enrichment'
    || jsonb_build_object(
        'application', stage_snapshot -> 'enrichment' -> 'application',
        'environment', stage_snapshot -> 'enrichment' -> 'environment',
        'repository', stage_snapshot -> 'enrichment' -> 'repository',
        'repositoryUrl', stage_snapshot -> 'enrichment' -> 'repositoryUrl',
        'targetBranch', stage_snapshot -> 'enrichment' -> 'targetBranch',
        'sourcePaths', stage_snapshot -> 'enrichment' -> 'sourcePaths'
    );

ALTER TABLE promotions
DROP CONSTRAINT promotions_preparation_state_valid,
DROP CONSTRAINT promotions_automatic_state_valid,
DROP CONSTRAINT promotions_dispatch_valid,
DROP CONSTRAINT promotions_source_state_valid,
DROP CONSTRAINT promotions_callback_state_valid,
DROP CONSTRAINT promotions_review_policy_context_valid,
DROP CONSTRAINT promotions_review_context_valid,
DROP CONSTRAINT promotions_snapshots_valid;

ALTER TABLE promotions
ALTER COLUMN repository_id SET NOT NULL,
ALTER COLUMN repository_snapshot SET NOT NULL,
ALTER COLUMN target_branch SET NOT NULL,
ADD CONSTRAINT promotions_snapshots_valid CHECK (
    jsonb_typeof(stage_snapshot) = 'object'
    AND jsonb_typeof(repository_snapshot) = 'object'
    AND jsonb_typeof(policy_snapshot) = 'object'
),
ADD CONSTRAINT promotions_callback_state_valid CHECK (
    (
        request_digest IS NULL
        AND base_revision IS NULL
        AND candidate_revision IS NULL
        AND candidate_branch IS NULL
        AND source = 'slipramp'
        AND status IN ('creating', 'error')
    )
    OR (
        octet_length(request_digest) = 32
        AND base_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
        AND candidate_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
        AND length(candidate_branch) BETWEEN 17 AND 255
        AND candidate_branch LIKE 'kargo/promotion/%'
    )
),
ADD CONSTRAINT promotions_branches_valid CHECK (length(target_branch) BETWEEN 1 AND 255),
ADD CONSTRAINT promotions_source_state_valid CHECK (
    (
        source = 'kargo'
        AND source_stage_id IS NULL
        AND selected_freight_snapshot IS NULL
        AND next_dispatch_at IS NULL
        AND last_dispatch_at IS NULL
        AND dispatched_at IS NULL
        AND dispatch_attempt_count = 0
    )
    OR (
        source = 'slipramp'
        AND jsonb_typeof(selected_freight_snapshot) = 'object'
    )
),
ADD CONSTRAINT promotions_dispatch_valid CHECK (
    dispatch_attempt_count >= 0
    AND (last_dispatch_at IS NULL OR last_dispatch_at >= created_at)
    AND (dispatched_at IS NULL OR dispatched_at >= created_at)
    AND (kargo_promotion_uid IS NULL OR length(kargo_promotion_uid) BETWEEN 1 AND 200)
    AND (kargo_promotion_uid IS NULL OR source = 'kargo' OR dispatched_at IS NOT NULL)
    AND (next_dispatch_at IS NULL OR dispatched_at IS NULL)
    AND (status <> 'creating' OR source = 'slipramp')
    AND (status <> 'creating' OR next_dispatch_at IS NOT NULL OR dispatched_at IS NOT NULL)
    AND (next_reconciliation_at IS NULL OR (
        kargo_outcome_phase IS NULL
        AND next_reconciliation_at >= created_at
        AND (
            status = 'approved'
            OR (source = 'slipramp' AND status = 'creating' AND kargo_promotion_uid IS NOT NULL)
        )
    ))
    AND (status <> 'approved' OR kargo_outcome_phase IS NOT NULL OR next_reconciliation_at IS NOT NULL)
    AND (status <> 'creating' OR kargo_promotion_uid IS NULL OR kargo_outcome_phase IS NOT NULL OR next_reconciliation_at IS NOT NULL)
    AND (kargo_outcome_phase IS NULL OR next_reconciliation_at IS NULL)
    AND (last_reconciliation_at IS NULL OR last_reconciliation_at >= created_at)
),
ADD CONSTRAINT promotions_automatic_state_valid CHECK (
    review_required
    OR status IN ('creating', 'error')
    OR (
        status = 'approved'
        AND preparation_repository_id IS NULL
        AND preparation_catalog_revision_id IS NULL
        AND preparation_run_id IS NULL
        AND git_diff_artifact_id IS NULL
        AND argo_diff_artifact_id IS NULL
        AND preparation_completed_at IS NULL
        AND decided_at IS NULL
    )
),
ADD CONSTRAINT promotions_preparation_state_valid CHECK (
    (
        status = 'creating'
        AND preparation_repository_id IS NULL
        AND preparation_catalog_revision_id IS NULL
        AND preparation_run_id IS NULL
        AND preparation_completed_at IS NULL
        AND git_diff_artifact_id IS NULL
        AND argo_diff_artifact_id IS NULL
        AND decided_at IS NULL
    )
    OR (
        status = 'preparing'
        AND preparation_completed_at IS NULL
        AND git_diff_artifact_id IS NULL
        AND argo_diff_artifact_id IS NULL
        AND decided_at IS NULL
    )
    OR (
        status IN ('pending', 'approved', 'rejected', 'expired')
        AND review_required
        AND preparation_run_id IS NOT NULL
        AND preparation_completed_at IS NOT NULL
        AND git_diff_artifact_id IS NOT NULL
        AND argo_diff_artifact_id IS NOT NULL
        AND ((status IN ('approved', 'rejected')) = (decided_at IS NOT NULL))
    )
    OR (status = 'error' AND decided_at IS NULL AND (review_required OR source = 'slipramp'))
    OR (status = 'approved' AND NOT review_required)
);

CREATE INDEX promotions_creation_dispatch_idx
ON promotions (next_dispatch_at, created_at, id)
WHERE source = 'slipramp' AND status = 'creating';

ALTER TABLE promotions
DROP COLUMN dispatch_diagnostic,
DROP COLUMN dispatch_failed_at;
