-- +goose Up
ALTER TABLE runs
ADD COLUMN target_relevance_commit_sha text,
ADD CONSTRAINT runs_target_relevance_commit_sha_valid CHECK (
    target_relevance_commit_sha IS NULL
    OR target_relevance_commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
);

ALTER TABLE targets
ADD COLUMN last_attempted_sync_run_id uuid,
ADD COLUMN last_successful_sync_run_id uuid,
ADD CONSTRAINT targets_last_attempted_sync_run_fk
    FOREIGN KEY (last_attempted_sync_run_id) REFERENCES runs (id),
ADD CONSTRAINT targets_last_successful_sync_run_fk
    FOREIGN KEY (last_successful_sync_run_id) REFERENCES runs (id);

-- Existing Run checkout commits do not establish Target relevance. Leave both
-- the Run markers and Target checkpoints unknown until trustworthy new
-- transitions occur.

-- +goose Down
ALTER TABLE targets
DROP CONSTRAINT targets_last_successful_sync_run_fk,
DROP CONSTRAINT targets_last_attempted_sync_run_fk,
DROP COLUMN last_successful_sync_run_id,
DROP COLUMN last_attempted_sync_run_id;

ALTER TABLE runs
DROP CONSTRAINT runs_target_relevance_commit_sha_valid,
DROP COLUMN target_relevance_commit_sha;
