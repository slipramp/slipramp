-- +goose Up
ALTER TABLE repository_commits
ADD COLUMN committed_at timestamptz;

CREATE TABLE repository_commit_history_indexes (
    repository_id uuid PRIMARY KEY REFERENCES repositories (id),
    status text NOT NULL,
    desired_head_sha text,
    indexed_head_sha text,
    commit_count integer NOT NULL DEFAULT 0,
    last_attempted_at timestamptz,
    last_succeeded_at timestamptz,
    failure_reason text,
    CONSTRAINT repository_commit_history_status_valid CHECK (
        status IN ('pending', 'succeeded', 'failed')
    ),
    CONSTRAINT repository_commit_history_desired_head_valid CHECK (
        desired_head_sha IS NULL
        OR desired_head_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT repository_commit_history_indexed_head_valid CHECK (
        indexed_head_sha IS NULL
        OR indexed_head_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT repository_commit_history_count_valid CHECK (commit_count >= 0),
    CONSTRAINT repository_commit_history_failure_reason_valid CHECK (
        failure_reason IS NULL OR length(failure_reason) > 0
    )
);

CREATE TABLE repository_tracked_branch_commits (
    repository_id uuid NOT NULL,
    commit_sha text NOT NULL,
    PRIMARY KEY (repository_id, commit_sha),
    CONSTRAINT repository_tracked_branch_commit_fk FOREIGN KEY (
        repository_id,
        commit_sha
    ) REFERENCES repository_commits (repository_id, sha)
);

-- +goose Down
DROP TABLE repository_tracked_branch_commits;
DROP TABLE repository_commit_history_indexes;

ALTER TABLE repository_commits
DROP COLUMN committed_at;
