-- +goose Up
CREATE TABLE catalog_gates (
    catalog_snapshot_id text NOT NULL REFERENCES catalog_snapshots (id),
    name text NOT NULL,
    source_path text NOT NULL,
    source_document integer NOT NULL,
    definition jsonb NOT NULL,
    PRIMARY KEY (catalog_snapshot_id, name),
    CONSTRAINT catalog_gates_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT catalog_gates_source_path_not_empty CHECK (length(source_path) > 0),
    CONSTRAINT catalog_gates_source_document_positive CHECK (source_document > 0),
    CONSTRAINT catalog_gates_definition_object CHECK (jsonb_typeof(definition) = 'object')
);

-- +goose Down
DROP TABLE catalog_gates;
