-- +goose Up
ALTER TABLE promotion_stages
ADD COLUMN health_status text,
ADD COLUMN argo_application_namespace text,
ADD CONSTRAINT promotion_stages_health_status_valid CHECK (
    health_status IS NULL OR length(health_status) BETWEEN 1 AND 200
),
ADD CONSTRAINT promotion_stages_argo_application_namespace_valid CHECK (
    argo_application_namespace IS NULL
    OR argo_application_namespace ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
);

-- +goose Down
ALTER TABLE promotion_stages
DROP CONSTRAINT promotion_stages_argo_application_namespace_valid,
DROP CONSTRAINT promotion_stages_health_status_valid,
DROP COLUMN argo_application_namespace,
DROP COLUMN health_status;
