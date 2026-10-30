-- +goose Up
ALTER TABLE artifact_reports
DROP CONSTRAINT artifact_reports_kind_valid,
DROP CONSTRAINT artifact_reports_mode_valid;

ALTER TABLE artifact_reports
ADD CONSTRAINT artifact_reports_kind_valid CHECK (
    kind IN ('terraform-plan', 'pyinfra-deploy')
),
ADD CONSTRAINT artifact_reports_mode_valid CHECK (
    (kind = 'terraform-plan' AND mode = 'preview')
    OR (kind = 'pyinfra-deploy' AND mode IN ('preview', 'apply'))
);

-- +goose Down
ALTER TABLE artifact_reports
DROP CONSTRAINT artifact_reports_kind_valid,
DROP CONSTRAINT artifact_reports_mode_valid;

ALTER TABLE artifact_reports
ADD CONSTRAINT artifact_reports_kind_valid CHECK (kind IN ('terraform-plan')),
ADD CONSTRAINT artifact_reports_mode_valid CHECK (mode IN ('preview'));
