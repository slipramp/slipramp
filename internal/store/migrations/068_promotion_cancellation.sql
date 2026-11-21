-- +goose Up
ALTER TABLE promotions
    ADD COLUMN cancellation_requested_at timestamptz,
    ADD COLUMN cancellation_requested_by_id uuid REFERENCES principals(id),
    ADD COLUMN cancellation_requested_by_snapshot jsonb,
    ADD COLUMN cancellation_confirmed_at timestamptz,
    ADD COLUMN next_cancel_attempt_at timestamptz,
    ADD CONSTRAINT promotions_cancellation_valid CHECK (
        num_nonnulls(cancellation_requested_at, cancellation_requested_by_id, cancellation_requested_by_snapshot) IN (0, 3)
        AND (cancellation_confirmed_at IS NULL OR cancellation_requested_at IS NOT NULL)
        AND (next_cancel_attempt_at IS NULL OR cancellation_requested_at IS NOT NULL)
    );

CREATE INDEX promotions_cancellation_due ON promotions ((stage_snapshot->>'cluster'), next_cancel_attempt_at)
WHERE next_cancel_attempt_at IS NOT NULL AND cancellation_confirmed_at IS NULL AND kargo_outcome_phase IS NULL;

CREATE OR REPLACE VIEW waiting_gate_requirements AS
SELECT
    'run'::text AS subject_type,
    runs.id AS subject_id,
    run_gates.repository_id,
    run_gates.gate_name,
    GREATEST(runs.created_at, runs.review_decided_at) AS requested_at
FROM run_gates
JOIN runs ON runs.id = run_gates.run_id
WHERE run_gates.released_at IS NULL
  AND runs.phase = 'queued'

UNION ALL

SELECT
    'promotion'::text AS subject_type,
    promotions.id AS subject_id,
    promotion_gates.repository_id,
    promotion_gates.gate_name,
    GREATEST(promotions.created_at, promotions.decided_at) AS requested_at
FROM promotion_gates
JOIN promotions ON promotions.id = promotion_gates.promotion_id
WHERE promotion_gates.released_at IS NULL
  AND promotions.status IN ('creating', 'preparing', 'pending', 'approved')
  AND promotions.cancellation_requested_at IS NULL
  AND promotions.dispatch_failed_at IS NULL
  AND promotions.kargo_outcome_phase IS NULL;

-- +goose Down
CREATE OR REPLACE VIEW waiting_gate_requirements AS
SELECT
    'run'::text AS subject_type,
    runs.id AS subject_id,
    run_gates.repository_id,
    run_gates.gate_name,
    GREATEST(runs.created_at, runs.review_decided_at) AS requested_at
FROM run_gates
JOIN runs ON runs.id = run_gates.run_id
WHERE run_gates.released_at IS NULL
  AND runs.phase = 'queued'

UNION ALL

SELECT
    'promotion'::text AS subject_type,
    promotions.id AS subject_id,
    promotion_gates.repository_id,
    promotion_gates.gate_name,
    GREATEST(promotions.created_at, promotions.decided_at) AS requested_at
FROM promotion_gates
JOIN promotions ON promotions.id = promotion_gates.promotion_id
WHERE promotion_gates.released_at IS NULL
  AND promotions.status IN ('creating', 'preparing', 'pending', 'approved')
  AND promotions.dispatch_failed_at IS NULL
  AND promotions.kargo_outcome_phase IS NULL;

ALTER TABLE promotions
    DROP COLUMN next_cancel_attempt_at,
    DROP COLUMN cancellation_confirmed_at,
    DROP COLUMN cancellation_requested_by_snapshot,
    DROP COLUMN cancellation_requested_by_id,
    DROP COLUMN cancellation_requested_at;
