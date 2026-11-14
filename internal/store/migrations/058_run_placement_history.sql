-- +goose Up
CREATE INDEX runs_placement_created_at
ON runs (placement_name, created_at DESC, id DESC);

-- +goose Down
DROP INDEX runs_placement_created_at;
