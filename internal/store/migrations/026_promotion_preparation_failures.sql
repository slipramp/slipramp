-- +goose Up
ALTER TABLE promotions
ADD COLUMN preparation_failure_count integer NOT NULL DEFAULT 0,
ADD COLUMN next_preparation_attempt_at timestamptz,
ADD CONSTRAINT promotions_preparation_retry_valid CHECK (
    preparation_failure_count >= 0
    AND (review_required OR preparation_failure_count = 0)
    AND (next_preparation_attempt_at IS NULL OR (
        status = 'preparing'
        AND preparation_failure_count > 0
        AND next_preparation_attempt_at >= created_at
    ))
);

-- +goose Down
ALTER TABLE promotions
DROP CONSTRAINT promotions_preparation_retry_valid,
DROP COLUMN next_preparation_attempt_at,
DROP COLUMN preparation_failure_count;
