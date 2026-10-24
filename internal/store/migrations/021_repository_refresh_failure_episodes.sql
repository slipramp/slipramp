-- +goose Up
ALTER TABLE repositories
ADD COLUMN refresh_failure_started_at timestamptz;

UPDATE repositories
SET refresh_failure_started_at = last_refresh_attempted_at
WHERE refresh_status = 'failed';

ALTER TABLE repositories
DROP CONSTRAINT repositories_refresh_error_state_valid,
ADD CONSTRAINT repositories_refresh_failure_started_after_creation CHECK (
    refresh_failure_started_at IS NULL OR refresh_failure_started_at >= created_at
),
ADD CONSTRAINT repositories_refresh_failure_started_before_attempt CHECK (
    refresh_failure_started_at IS NULL
    OR refresh_failure_started_at <= last_refresh_attempted_at
),
ADD CONSTRAINT repositories_refresh_error_state_valid CHECK (
    (refresh_status = 'pending'
        AND last_refresh_attempted_at IS NULL
        AND last_refresh_error IS NULL
        AND refresh_failure_started_at IS NULL)
    OR (refresh_status = 'succeeded'
        AND observed_commit_sha IS NOT NULL
        AND last_refresh_attempted_at IS NOT NULL
        AND last_refresh_succeeded_at = last_refresh_attempted_at
        AND last_refresh_error IS NULL
        AND refresh_failure_started_at IS NULL)
    OR (refresh_status = 'failed'
        AND last_refresh_attempted_at IS NOT NULL
        AND last_refresh_error IS NOT NULL
        AND refresh_failure_started_at IS NOT NULL)
);

-- +goose Down
ALTER TABLE repositories
DROP CONSTRAINT repositories_refresh_error_state_valid,
DROP CONSTRAINT repositories_refresh_failure_started_before_attempt,
DROP CONSTRAINT repositories_refresh_failure_started_after_creation,
ADD CONSTRAINT repositories_refresh_error_state_valid CHECK (
    (refresh_status = 'pending'
        AND last_refresh_attempted_at IS NULL
        AND last_refresh_error IS NULL)
    OR (refresh_status = 'succeeded'
        AND observed_commit_sha IS NOT NULL
        AND last_refresh_attempted_at IS NOT NULL
        AND last_refresh_succeeded_at = last_refresh_attempted_at
        AND last_refresh_error IS NULL)
    OR (refresh_status = 'failed'
        AND last_refresh_attempted_at IS NOT NULL
        AND last_refresh_error IS NOT NULL)
),
DROP COLUMN refresh_failure_started_at;
