-- +goose Up
CREATE TABLE git_trigger_deliveries (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id),
    catalog_revision_id uuid NOT NULL,
    target_name text NOT NULL,
    action_name text NOT NULL,
    trigger_name text NOT NULL,
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    failure_count integer NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    next_attempt_at timestamptz NOT NULL,
    diagnostic text CHECK (length(diagnostic) <= 512),
    failed_at timestamptz,
    run_id uuid UNIQUE REFERENCES runs (id),
    superseded_at timestamptz,
    FOREIGN KEY (catalog_revision_id, repository_id)
        REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT git_trigger_deliveries_target_name_not_empty CHECK (
        length(target_name) > 0
    ),
    CONSTRAINT git_trigger_deliveries_action_name_not_empty CHECK (
        length(action_name) > 0
    ),
    CONSTRAINT git_trigger_deliveries_trigger_name_not_empty CHECK (
        length(trigger_name) > 0
    ),
    CONSTRAINT git_trigger_deliveries_parameters_object CHECK (
        jsonb_typeof(parameters) = 'object'
    ),
    CONSTRAINT git_trigger_deliveries_result_exclusive CHECK (
        num_nonnulls(run_id, superseded_at, failed_at) <= 1
    )
);

CREATE INDEX git_trigger_deliveries_pending
ON git_trigger_deliveries (next_attempt_at, created_at, id)
WHERE run_id IS NULL AND superseded_at IS NULL AND failed_at IS NULL;

CREATE INDEX git_trigger_deliveries_target_pending
ON git_trigger_deliveries (repository_id, target_name)
WHERE run_id IS NULL AND superseded_at IS NULL AND failed_at IS NULL;

CREATE INDEX git_trigger_deliveries_failed
ON git_trigger_deliveries (failed_at DESC, id DESC)
WHERE failed_at IS NOT NULL;

-- +goose Down
DROP TABLE git_trigger_deliveries;
