-- +goose Up
ALTER TABLE clusters
ADD COLUMN controller_version text,
ADD CONSTRAINT clusters_controller_version_valid CHECK (
    controller_version IS NULL
    OR length(controller_version) BETWEEN 1 AND 128
);

-- +goose Down
ALTER TABLE clusters
DROP CONSTRAINT clusters_controller_version_valid,
DROP COLUMN controller_version;
