-- +goose Up
CREATE TABLE run_artifact_inputs (
    run_id uuid NOT NULL REFERENCES runs (id),
    input_name text NOT NULL,
    artifact_id uuid NOT NULL REFERENCES artifacts (id),
    CONSTRAINT run_artifact_inputs_pkey PRIMARY KEY (run_id, input_name),
    CONSTRAINT run_artifact_inputs_name_valid CHECK (
        input_name ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    )
);

CREATE INDEX run_artifact_inputs_artifact_id
ON run_artifact_inputs (artifact_id);

-- +goose Down
DROP TABLE run_artifact_inputs;
