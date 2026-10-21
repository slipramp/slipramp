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
        run_id IS NULL OR superseded_at IS NULL
    )
);

CREATE INDEX git_trigger_deliveries_pending
ON git_trigger_deliveries (created_at, id)
WHERE run_id IS NULL AND superseded_at IS NULL;

CREATE INDEX git_trigger_deliveries_target_pending
ON git_trigger_deliveries (repository_id, target_name)
WHERE run_id IS NULL AND superseded_at IS NULL;

-- +goose Down
DROP TABLE git_trigger_deliveries;
