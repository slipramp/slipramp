-- +goose Up
CREATE TABLE clusters (
    name text PRIMARY KEY,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    CONSTRAINT clusters_name_not_empty CHECK (length(name) > 0)
);

-- +goose Down
DROP TABLE clusters;
