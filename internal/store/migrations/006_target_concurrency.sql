-- +goose Up
CREATE UNIQUE INDEX runs_one_active_per_target
ON runs (repository_id, target_name)
WHERE state IN ('preparing', 'running', 'canceling', 'unknown');

-- +goose Down
DROP INDEX runs_one_active_per_target;
