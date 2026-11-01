-- +goose Up
ALTER TABLE promotions
ADD COLUMN kargo_outcome_phase text,
ADD COLUMN kargo_outcome_observed_at timestamptz,
ADD COLUMN kargo_outcome_diagnostic text;

DROP INDEX promotions_creation_reconciliation_idx;

ALTER TABLE promotions
DROP CONSTRAINT promotions_source_state_valid,
DROP CONSTRAINT promotions_dispatch_valid;

UPDATE promotions
SET next_reconciliation_at = COALESCE(next_reconciliation_at, updated_at)
WHERE status = 'approved';

ALTER TABLE promotions
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
ADD CONSTRAINT promotions_kargo_outcome_valid CHECK (
    (
        kargo_outcome_phase IS NULL
        AND kargo_outcome_observed_at IS NULL
        AND kargo_outcome_diagnostic IS NULL
    )
    OR (
        kargo_outcome_phase IN ('Succeeded', 'Failed', 'Errored', 'Aborted')
        AND kargo_outcome_observed_at IS NOT NULL
        AND kargo_outcome_observed_at >= created_at
        AND kargo_promotion_uid IS NOT NULL
        AND (kargo_outcome_diagnostic IS NULL OR length(kargo_outcome_diagnostic) BETWEEN 1 AND 4096)
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
            OR (
                source = 'slipramp'
                AND status = 'creating'
                AND kargo_promotion_uid IS NOT NULL
            )
        )
    ))
    AND (
        status <> 'approved'
        OR kargo_outcome_phase IS NOT NULL
        OR next_reconciliation_at IS NOT NULL
    )
    AND (
        status <> 'creating'
        OR kargo_promotion_uid IS NULL
        OR kargo_outcome_phase IS NOT NULL
        OR next_reconciliation_at IS NOT NULL
    )
    AND (kargo_outcome_phase IS NULL OR next_reconciliation_at IS NULL)
    AND (last_reconciliation_at IS NULL OR last_reconciliation_at >= created_at)
);

CREATE INDEX promotions_reconciliation_idx
ON promotions (next_reconciliation_at, created_at, id)
WHERE next_reconciliation_at IS NOT NULL
  AND kargo_outcome_phase IS NULL;

-- +goose Down
DROP INDEX promotions_reconciliation_idx;

ALTER TABLE promotions
DROP CONSTRAINT promotions_dispatch_valid,
DROP CONSTRAINT promotions_kargo_outcome_valid,
DROP CONSTRAINT promotions_source_state_valid;

UPDATE promotions
SET next_reconciliation_at = NULL,
    last_reconciliation_at = NULL,
    kargo_promotion_uid = CASE WHEN source = 'kargo' THEN NULL ELSE kargo_promotion_uid END;

ALTER TABLE promotions
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

ALTER TABLE promotions
DROP COLUMN kargo_outcome_diagnostic,
DROP COLUMN kargo_outcome_observed_at,
DROP COLUMN kargo_outcome_phase;
