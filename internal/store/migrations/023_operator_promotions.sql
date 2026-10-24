-- +goose Up
ALTER TABLE promotion_stages
ADD COLUMN requested_freight_json jsonb NOT NULL DEFAULT '[]'::jsonb,
ADD CONSTRAINT promotion_stages_requested_freight_array CHECK (
    jsonb_typeof(requested_freight_json) = 'array'
);

ALTER TABLE promotion_stages
ALTER COLUMN requested_freight_json DROP DEFAULT;

ALTER TABLE promotions
ADD COLUMN source text NOT NULL DEFAULT 'kargo',
ADD COLUMN source_stage_id uuid REFERENCES promotion_stages (id),
ADD COLUMN selected_freight_snapshot jsonb,
ADD COLUMN dispatch_attempt_count integer NOT NULL DEFAULT 0,
ADD COLUMN next_dispatch_at timestamptz,
ADD COLUMN last_dispatch_at timestamptz,
ADD COLUMN dispatched_at timestamptz,
ADD COLUMN kargo_promotion_uid text;

ALTER TABLE promotions
ALTER COLUMN source DROP DEFAULT,
ALTER COLUMN request_digest DROP NOT NULL,
ALTER COLUMN base_revision DROP NOT NULL,
ALTER COLUMN candidate_revision DROP NOT NULL,
ALTER COLUMN candidate_branch DROP NOT NULL,
DROP CONSTRAINT promotions_request_digest_valid,
DROP CONSTRAINT promotions_revisions_valid,
DROP CONSTRAINT promotions_branches_valid,
DROP CONSTRAINT promotions_status_valid,
DROP CONSTRAINT promotions_automatic_state_valid,
DROP CONSTRAINT promotions_preparation_state_valid;

ALTER TABLE promotions
ADD CONSTRAINT promotions_source_valid CHECK (source IN ('kargo', 'slipramp')),
ADD CONSTRAINT promotions_source_state_valid CHECK (
    (
        source = 'kargo'
        AND source_stage_id IS NULL
        AND selected_freight_snapshot IS NULL
        AND next_dispatch_at IS NULL
        AND last_dispatch_at IS NULL
        AND dispatched_at IS NULL
        AND kargo_promotion_uid IS NULL
        AND dispatch_attempt_count = 0
    )
    OR (
        source = 'slipramp'
        AND source_stage_id IS NOT NULL
        AND jsonb_typeof(selected_freight_snapshot) = 'object'
    )
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
ADD CONSTRAINT promotions_status_valid CHECK (
    status IN ('creating', 'preparing', 'pending', 'approved', 'rejected', 'expired', 'error')
),
ADD CONSTRAINT promotions_dispatch_valid CHECK (
    dispatch_attempt_count >= 0
    AND (last_dispatch_at IS NULL OR last_dispatch_at >= created_at)
    AND (dispatched_at IS NULL OR dispatched_at >= created_at)
    AND (kargo_promotion_uid IS NULL OR length(kargo_promotion_uid) BETWEEN 1 AND 200)
    AND (kargo_promotion_uid IS NULL OR dispatched_at IS NOT NULL)
    AND (next_dispatch_at IS NULL OR dispatched_at IS NULL)
    AND (status <> 'creating' OR source = 'slipramp')
    AND (status <> 'creating' OR next_dispatch_at IS NOT NULL OR dispatched_at IS NOT NULL)
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
    OR (
        status = 'error'
        AND decided_at IS NULL
        AND (review_required OR source = 'slipramp')
    )
    OR (
        status = 'approved'
        AND NOT review_required
    )
);

CREATE INDEX promotions_creation_dispatch_idx
ON promotions (next_dispatch_at, created_at, id)
WHERE source = 'slipramp' AND status = 'creating';

-- +goose Down
DROP INDEX promotions_creation_dispatch_idx;

ALTER TABLE promotions
DROP CONSTRAINT promotions_preparation_state_valid,
DROP CONSTRAINT promotions_automatic_state_valid,
DROP CONSTRAINT promotions_dispatch_valid,
DROP CONSTRAINT promotions_status_valid,
DROP CONSTRAINT promotions_branches_valid,
DROP CONSTRAINT promotions_callback_state_valid,
DROP CONSTRAINT promotions_source_state_valid,
DROP CONSTRAINT promotions_source_valid;

DELETE FROM promotions WHERE source = 'slipramp';

ALTER TABLE promotions
ALTER COLUMN request_digest SET NOT NULL,
ALTER COLUMN base_revision SET NOT NULL,
ALTER COLUMN candidate_revision SET NOT NULL,
ALTER COLUMN candidate_branch SET NOT NULL,
DROP COLUMN kargo_promotion_uid,
DROP COLUMN dispatched_at,
DROP COLUMN last_dispatch_at,
DROP COLUMN next_dispatch_at,
DROP COLUMN dispatch_attempt_count,
DROP COLUMN selected_freight_snapshot,
DROP COLUMN source_stage_id,
DROP COLUMN source;

ALTER TABLE promotions
ADD CONSTRAINT promotions_request_digest_valid CHECK (octet_length(request_digest) = 32),
ADD CONSTRAINT promotions_revisions_valid CHECK (
    base_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    AND candidate_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
),
ADD CONSTRAINT promotions_branches_valid CHECK (
    length(candidate_branch) BETWEEN 17 AND 255
    AND candidate_branch LIKE 'kargo/promotion/%'
    AND length(target_branch) BETWEEN 1 AND 255
),
ADD CONSTRAINT promotions_status_valid CHECK (
    status IN ('preparing', 'pending', 'approved', 'rejected', 'expired', 'error')
),
ADD CONSTRAINT promotions_automatic_state_valid CHECK (
    review_required
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
    OR (
        status = 'error'
        AND review_required
        AND decided_at IS NULL
    )
    OR (
        status = 'approved'
        AND NOT review_required
    )
);

ALTER TABLE promotion_stages
DROP CONSTRAINT promotion_stages_requested_freight_array,
DROP COLUMN requested_freight_json;
