-- +goose Up
ALTER TABLE promotions DROP CONSTRAINT promotions_dispatch_valid,
ADD CONSTRAINT promotions_dispatch_valid CHECK (
    dispatch_attempt_count >= 0
    AND (last_dispatch_at IS NULL OR last_dispatch_at >= created_at)
    AND (dispatched_at IS NULL OR dispatched_at >= created_at)
    AND (dispatch_failed_at IS NULL OR dispatch_failed_at >= created_at)
    AND (dispatch_diagnostic IS NULL OR length(dispatch_diagnostic) BETWEEN 1 AND 4096)
    AND (dispatch_failed_at IS NULL OR dispatch_diagnostic IS NOT NULL)
    AND num_nonnulls(next_dispatch_at, dispatched_at, dispatch_failed_at) = CASE
        WHEN source = 'slipramp' AND (cancellation_requested_at IS NULL OR dispatched_at IS NOT NULL OR dispatch_failed_at IS NOT NULL) THEN 1
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
        OR cancellation_requested_at IS NOT NULL
    )
    AND (
        source <> 'slipramp'
        OR dispatched_at IS NULL
        OR status NOT IN ('creating', 'approved')
        OR kargo_outcome_phase IS NOT NULL
        OR next_reconciliation_at IS NOT NULL
        OR cancellation_requested_at IS NOT NULL
    )
    AND (kargo_outcome_phase IS NULL OR next_reconciliation_at IS NULL)
    AND (last_reconciliation_at IS NULL OR last_reconciliation_at >= created_at)
);

-- +goose Down
ALTER TABLE promotions DROP CONSTRAINT promotions_dispatch_valid,
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
);
