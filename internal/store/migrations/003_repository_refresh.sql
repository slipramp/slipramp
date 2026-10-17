-- +goose Up
ALTER TABLE repositories
ADD COLUMN refresh_status text NOT NULL DEFAULT 'pending',
ADD COLUMN observed_commit_sha text,
ADD COLUMN last_refresh_attempted_at timestamptz,
ADD COLUMN last_refresh_succeeded_at timestamptz,
ADD COLUMN last_refresh_error text,
ADD CONSTRAINT repositories_refresh_status_valid CHECK (
    refresh_status IN ('pending', 'succeeded', 'failed')
),
ADD CONSTRAINT repositories_observed_commit_sha_valid CHECK (
    observed_commit_sha IS NULL OR observed_commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
),
ADD CONSTRAINT repositories_refresh_attempt_after_creation CHECK (
    last_refresh_attempted_at IS NULL OR last_refresh_attempted_at >= created_at
),
ADD CONSTRAINT repositories_refresh_success_after_creation CHECK (
    last_refresh_succeeded_at IS NULL OR last_refresh_succeeded_at >= created_at
),
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
);

-- +goose Down
ALTER TABLE repositories
DROP CONSTRAINT repositories_refresh_error_state_valid,
DROP CONSTRAINT repositories_refresh_success_after_creation,
DROP CONSTRAINT repositories_refresh_attempt_after_creation,
DROP CONSTRAINT repositories_observed_commit_sha_valid,
DROP CONSTRAINT repositories_refresh_status_valid,
DROP COLUMN last_refresh_error,
DROP COLUMN last_refresh_succeeded_at,
DROP COLUMN last_refresh_attempted_at,
DROP COLUMN observed_commit_sha,
DROP COLUMN refresh_status;
