-- +goose Up
ALTER TABLE git_trigger_deliveries
ADD CONSTRAINT git_trigger_deliveries_lifecycle_consistent CHECK (
    next_attempt_at >= created_at
    AND (
        (
            failure_count = 0
            AND diagnostic IS NULL
            AND next_attempt_at = created_at
        )
        OR (
            failure_count > 0
            AND diagnostic IS NOT NULL
            AND length(diagnostic) > 0
            AND next_attempt_at > created_at
        )
    )
    AND (
        failed_at IS NULL
        OR (
            failed_at >= created_at
            AND failed_at < next_attempt_at
        )
    )
    AND (superseded_at IS NULL OR superseded_at >= created_at)
);

-- +goose Down
ALTER TABLE git_trigger_deliveries
DROP CONSTRAINT git_trigger_deliveries_lifecycle_consistent;
