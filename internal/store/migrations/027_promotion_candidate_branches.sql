-- +goose Up
CREATE UNIQUE INDEX promotions_candidate_branch_identity_key
ON promotions (repository_id, candidate_branch)
WHERE candidate_branch IS NOT NULL;

CREATE TABLE promotion_candidate_branch_cleanups (
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

-- +goose Down
DROP TABLE promotion_candidate_branch_cleanups;
DROP INDEX promotions_candidate_branch_identity_key;
