-- +goose Up
ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:runs:review',
        'slipramp:inbox:write',
        'slipramp:promotions:submit',
        'slipramp:promotions:write',
        'slipramp:admin'
    ]::text[]
);

-- +goose Down
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
