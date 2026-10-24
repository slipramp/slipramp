-- +goose Up
ALTER TABLE promotions
ADD COLUMN next_reconciliation_at timestamptz,
ADD COLUMN last_reconciliation_at timestamptz;

UPDATE promotions
SET next_reconciliation_at = COALESCE(dispatched_at, updated_at)
WHERE source = 'slipramp'
  AND status = 'creating'
  AND kargo_promotion_uid IS NOT NULL;

ALTER TABLE promotions
DROP CONSTRAINT promotions_external_id_valid,
DROP CONSTRAINT promotions_source_state_valid,
DROP CONSTRAINT promotions_dispatch_valid;

ALTER TABLE promotions
ADD CONSTRAINT promotions_external_id_valid CHECK (
    length(external_promotion_id) BETWEEN 1 AND 253
),
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
        AND next_reconciliation_at IS NULL
        AND last_reconciliation_at IS NULL
    )
    OR (
        source = 'slipramp'
        AND source_stage_id IS NOT NULL
        AND jsonb_typeof(selected_freight_snapshot) = 'object'
    )
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
    AND (next_reconciliation_at IS NULL OR (
        source = 'slipramp'
        AND status = 'creating'
        AND kargo_promotion_uid IS NOT NULL
        AND next_reconciliation_at >= created_at
    ))
    AND (status <> 'creating' OR kargo_promotion_uid IS NULL OR next_reconciliation_at IS NOT NULL)
    AND (last_reconciliation_at IS NULL OR (
        source = 'slipramp'
        AND kargo_promotion_uid IS NOT NULL
        AND last_reconciliation_at >= created_at
    ))
);

CREATE INDEX promotions_creation_reconciliation_idx
ON promotions (next_reconciliation_at, created_at, id)
WHERE source = 'slipramp'
  AND status = 'creating'
  AND kargo_promotion_uid IS NOT NULL;

-- +goose Down
DROP INDEX promotions_creation_reconciliation_idx;

ALTER TABLE promotions
DROP CONSTRAINT promotions_dispatch_valid,
DROP CONSTRAINT promotions_source_state_valid,
DROP CONSTRAINT promotions_external_id_valid;

ALTER TABLE promotions
ADD CONSTRAINT promotions_external_id_valid CHECK (
    length(external_promotion_id) BETWEEN 1 AND 200
),
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
ADD CONSTRAINT promotions_dispatch_valid CHECK (
    dispatch_attempt_count >= 0
    AND (last_dispatch_at IS NULL OR last_dispatch_at >= created_at)
    AND (dispatched_at IS NULL OR dispatched_at >= created_at)
    AND (kargo_promotion_uid IS NULL OR length(kargo_promotion_uid) BETWEEN 1 AND 200)
    AND (kargo_promotion_uid IS NULL OR dispatched_at IS NOT NULL)
    AND (next_dispatch_at IS NULL OR dispatched_at IS NULL)
    AND (status <> 'creating' OR source = 'slipramp')
    AND (status <> 'creating' OR next_dispatch_at IS NOT NULL OR dispatched_at IS NOT NULL)
);

ALTER TABLE promotions
DROP COLUMN last_reconciliation_at,
DROP COLUMN next_reconciliation_at;
