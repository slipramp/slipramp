-- +goose Up
CREATE INDEX runs_created_at_id
ON runs (created_at DESC, id DESC);

-- +goose Down
DROP INDEX runs_created_at_id;
