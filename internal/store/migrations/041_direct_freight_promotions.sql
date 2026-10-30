-- +goose Up
ALTER TABLE promotions
DROP CONSTRAINT promotions_source_state_valid;

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
);

-- +goose Down
ALTER TABLE promotions
DROP CONSTRAINT promotions_source_state_valid;

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
        AND source_stage_id IS NOT NULL
        AND jsonb_typeof(selected_freight_snapshot) = 'object'
    )
);
