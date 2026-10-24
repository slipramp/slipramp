-- +goose Up
CREATE TABLE IF NOT EXISTS promotion_candidate_branch_cleanups (
    repository_id uuid PRIMARY KEY REFERENCES repositories (id),
    status text NOT NULL,
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    observed_branches integer NOT NULL DEFAULT 0,
    deleted_branches integer NOT NULL DEFAULT 0,
    retained_branches integer NOT NULL DEFAULT 0,
    error text,
    CONSTRAINT promotion_candidate_branch_cleanups_status_valid CHECK (
        status IN ('running', 'succeeded', 'failed')
    ),
    CONSTRAINT promotion_candidate_branch_cleanups_counts_valid CHECK (
        observed_branches >= 0
        AND deleted_branches >= 0
        AND retained_branches >= 0
    ),
    CONSTRAINT promotion_candidate_branch_cleanups_error_valid CHECK (
        error IS NULL OR length(error) BETWEEN 1 AND 4096
    ),
    CONSTRAINT promotion_candidate_branch_cleanups_completion_valid CHECK (
        (status = 'running' AND finished_at IS NULL AND error IS NULL)
        OR (status = 'succeeded' AND finished_at IS NOT NULL AND error IS NULL)
        OR (status = 'failed' AND finished_at IS NOT NULL AND error IS NOT NULL)
    )
);

-- Existing development databases may already have migration 027's original
-- per-branch inventory. Collapse its latest scan into the repository summary
-- before removing the obsolete table.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('promotion_candidate_branch_observations') IS NOT NULL THEN
        INSERT INTO promotion_candidate_branch_cleanups (
            repository_id,
            status,
            started_at,
            finished_at,
            observed_branches,
            deleted_branches,
            retained_branches,
            error
        )
        SELECT
            observations.repository_id,
            CASE
                WHEN bool_or(observations.status = 'failed') THEN 'failed'
                ELSE 'succeeded'
            END,
            max(observations.scanned_at),
            GREATEST(
                max(observations.scanned_at),
                max(observations.cleanup_attempted_at)
            ),
            count(*) FILTER (WHERE observations.remote_revision IS NOT NULL),
            count(*) FILTER (WHERE observations.status = 'deleted'),
            count(*) FILTER (
                WHERE observations.remote_revision IS NOT NULL
                  AND observations.status <> 'deleted'
            ),
            CASE
                WHEN bool_or(observations.status = 'failed')
                    THEN 'Candidate branch cleanup failed; check Server logs and Git credentials.'
                ELSE NULL
            END
        FROM promotion_candidate_branch_observations AS observations
        WHERE observations.scanned_at = (
            SELECT max(latest.scanned_at)
            FROM promotion_candidate_branch_observations AS latest
            WHERE latest.repository_id = observations.repository_id
        )
        GROUP BY observations.repository_id
        ON CONFLICT (repository_id) DO NOTHING;

        DROP TABLE promotion_candidate_branch_observations;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
CREATE TABLE IF NOT EXISTS promotion_candidate_branch_observations (
    repository_id uuid NOT NULL REFERENCES repositories (id),
    candidate_branch text NOT NULL,
    promotion_id uuid REFERENCES promotions (id),
    expected_revision text,
    remote_revision text,
    status text NOT NULL,
    expires_at timestamptz,
    message text,
    scanned_at timestamptz NOT NULL,
    cleanup_attempted_at timestamptz,
    deleted_at timestamptz,
    PRIMARY KEY (repository_id, candidate_branch)
);

DROP TABLE promotion_candidate_branch_cleanups;
