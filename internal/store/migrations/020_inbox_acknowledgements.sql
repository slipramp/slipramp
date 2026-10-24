-- +goose Up
ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:inbox:write',
        'slipramp:promotions:submit',
        'slipramp:promotions:write',
        'slipramp:admin'
    ]::text[]
);

CREATE TABLE inbox_problem_acknowledgements (
    principal_id uuid NOT NULL REFERENCES principals (id),
    problem_id text NOT NULL,
    acknowledged_at timestamptz NOT NULL,
    PRIMARY KEY (principal_id, problem_id),
    CONSTRAINT inbox_problem_acknowledgements_problem_id_valid CHECK (
        problem_id ~ '^[0-9a-f]{64}$'
    )
);

-- +goose Down
DROP TABLE inbox_problem_acknowledgements;

ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:promotions:submit',
        'slipramp:promotions:write',
        'slipramp:admin'
    ]::text[]
);
