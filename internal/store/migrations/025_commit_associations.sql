-- +goose Up
CREATE INDEX runs_repository_commit_created_at
ON runs (repository_id, commit_sha, created_at DESC, id DESC);

-- +goose Down
DROP INDEX runs_repository_commit_created_at;
