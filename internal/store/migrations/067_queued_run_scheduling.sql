-- +goose Up
CREATE INDEX runs_queued_cluster_created_at
ON runs (cluster_name, created_at, id)
WHERE phase = 'queued';

-- +goose Down
DROP INDEX runs_queued_cluster_created_at;
