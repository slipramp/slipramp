-- +goose Up
ALTER TABLE git_trigger_deliveries
ADD COLUMN review_required boolean NOT NULL DEFAULT false;

ALTER TABLE git_trigger_deliveries
ALTER COLUMN review_required DROP DEFAULT;

ALTER TABLE runs
ADD COLUMN review_state text NOT NULL DEFAULT 'not-required',
ADD COLUMN review_decided_by_principal_id uuid REFERENCES principals (id),
ADD COLUMN review_decided_by_snapshot jsonb,
ADD COLUMN review_decided_at timestamptz,
ADD CONSTRAINT runs_review_state_valid CHECK (
    review_state IN ('not-required', 'pending', 'approved', 'rejected')
),
ADD CONSTRAINT runs_review_decision_consistent CHECK (
    (review_state IN ('approved', 'rejected')) = (review_decided_at IS NOT NULL)
    AND (review_decided_by_principal_id IS NULL) = (review_decided_at IS NULL)
    AND (review_decided_by_snapshot IS NULL) = (review_decided_at IS NULL)
),
ADD CONSTRAINT runs_review_actor_snapshot_object CHECK (
    review_decided_by_snapshot IS NULL
    OR jsonb_typeof(review_decided_by_snapshot) = 'object'
),
ADD CONSTRAINT runs_review_git_source CHECK (
    review_state = 'not-required' OR source->>'type' = 'git'
),
ADD CONSTRAINT runs_rejected_review_terminal CHECK (
    review_state <> 'rejected'
    OR (
        phase = 'finished'
        AND outcome = 'canceled'
        AND outcome_reason = 'ReviewRejected'
    )
);

ALTER TABLE runs
ALTER COLUMN review_state DROP DEFAULT;

-- +goose Down
ALTER TABLE runs
DROP CONSTRAINT runs_rejected_review_terminal,
DROP CONSTRAINT runs_review_git_source,
DROP CONSTRAINT runs_review_actor_snapshot_object,
DROP CONSTRAINT runs_review_decision_consistent,
DROP CONSTRAINT runs_review_state_valid,
DROP COLUMN review_decided_at,
DROP COLUMN review_decided_by_snapshot,
DROP COLUMN review_decided_by_principal_id,
DROP COLUMN review_state;

ALTER TABLE git_trigger_deliveries
DROP COLUMN review_required;
