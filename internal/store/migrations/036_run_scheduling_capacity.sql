-- +goose Up
CREATE INDEX runs_active_cluster_placement
ON runs (cluster_name, placement_name)
WHERE phase IN ('assigned', 'preparing', 'running');

-- +goose Down
DROP INDEX runs_active_cluster_placement;
