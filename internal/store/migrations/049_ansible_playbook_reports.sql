-- +goose Up
ALTER TABLE artifact_reports
DROP CONSTRAINT artifact_reports_kind_valid,
DROP CONSTRAINT artifact_reports_mode_valid;

ALTER TABLE artifact_reports
ADD CONSTRAINT artifact_reports_kind_valid CHECK (
    kind IN ('terraform-plan', 'opentofu-plan', 'pyinfra-deploy', 'ansible-playbook')
),
ADD CONSTRAINT artifact_reports_mode_valid CHECK (
    (kind IN ('terraform-plan', 'opentofu-plan') AND mode = 'preview')
    OR (kind IN ('pyinfra-deploy', 'ansible-playbook') AND mode IN ('preview', 'apply'))
);

-- +goose Down
DELETE FROM artifact_reports
WHERE kind = 'ansible-playbook';

ALTER TABLE artifact_reports
DROP CONSTRAINT artifact_reports_kind_valid,
DROP CONSTRAINT artifact_reports_mode_valid;

ALTER TABLE artifact_reports
ADD CONSTRAINT artifact_reports_kind_valid CHECK (
    kind IN ('terraform-plan', 'opentofu-plan', 'pyinfra-deploy')
),
ADD CONSTRAINT artifact_reports_mode_valid CHECK (
    (kind IN ('terraform-plan', 'opentofu-plan') AND mode = 'preview')
    OR (kind = 'pyinfra-deploy' AND mode IN ('preview', 'apply'))
);
