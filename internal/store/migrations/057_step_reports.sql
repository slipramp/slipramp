-- +goose Up
ALTER TABLE artifact_reports
DROP CONSTRAINT artifact_reports_kind_valid,
DROP CONSTRAINT artifact_reports_mode_valid;

ALTER TABLE artifact_reports
ADD CONSTRAINT artifact_reports_kind_valid CHECK (
    kind IN ('terraform-plan', 'opentofu-plan', 'pyinfra-deploy', 'ansible-playbook', 'step-report')
),
ADD CONSTRAINT artifact_reports_mode_valid CHECK (
    (kind IN ('terraform-plan', 'opentofu-plan') AND mode = 'preview')
    OR (kind IN ('pyinfra-deploy', 'ansible-playbook', 'step-report') AND mode IN ('preview', 'apply'))
);

-- +goose Down
DELETE FROM artifact_reports
WHERE kind = 'step-report';

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
