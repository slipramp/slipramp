-- +goose Up
CREATE UNIQUE INDEX runs_one_active_per_target
ON runs (repository_id, target_name)
WHERE phase IN ('assigned', 'preparing', 'running');

-- +goose Down
DROP INDEX runs_one_active_per_target;
