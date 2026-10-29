-- +goose Up
CREATE TABLE repository_refresh_attempts (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id),
    source text NOT NULL,
    outcome text NOT NULL,
    started_at timestamptz NOT NULL,
    duration_milliseconds bigint NOT NULL,
    commit_sha text,
    diagnostic text,
    CONSTRAINT repository_refresh_attempts_source_valid CHECK (
        source IN ('periodic', 'requested')
    ),
    CONSTRAINT repository_refresh_attempts_outcome_valid CHECK (
        outcome IN ('accepted', 'fetch-failed', 'catalog-rejected')
    ),
    CONSTRAINT repository_refresh_attempts_duration_valid CHECK (
        duration_milliseconds >= 0
    ),
    CONSTRAINT repository_refresh_attempts_commit_sha_valid CHECK (
        commit_sha IS NULL OR commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT repository_refresh_attempts_diagnostic_valid CHECK (
        diagnostic IS NULL OR (length(diagnostic) > 0 AND octet_length(diagnostic) <= 4096)
    ),
    CONSTRAINT repository_refresh_attempts_result_valid CHECK (
        (outcome = 'accepted' AND commit_sha IS NOT NULL AND diagnostic IS NULL)
        OR (outcome = 'fetch-failed' AND commit_sha IS NULL AND diagnostic IS NOT NULL)
        OR (outcome = 'catalog-rejected' AND commit_sha IS NOT NULL AND diagnostic IS NOT NULL)
    )
);

CREATE INDEX repository_refresh_attempts_repository_started_key
ON repository_refresh_attempts (repository_id, started_at DESC, id DESC);

-- +goose Down
DROP TABLE repository_refresh_attempts;
