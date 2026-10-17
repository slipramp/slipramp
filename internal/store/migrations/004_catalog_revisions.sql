-- +goose Up
CREATE TABLE catalog_revisions (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id),
    commit_sha text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT catalog_revisions_commit_sha_valid CHECK (
        commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT catalog_revisions_repository_commit_key UNIQUE (
        repository_id,
        commit_sha
    ),
    CONSTRAINT catalog_revisions_id_repository_key UNIQUE (
        id,
        repository_id
    )
);

ALTER TABLE repositories
ADD COLUMN active_catalog_revision_id uuid,
ADD CONSTRAINT repositories_active_catalog_revision_fk FOREIGN KEY (
    active_catalog_revision_id,
    id
) REFERENCES catalog_revisions (id, repository_id);

CREATE TABLE catalog_runtimes (
    catalog_revision_id uuid NOT NULL REFERENCES catalog_revisions (id),
    name text NOT NULL,
    source_path text NOT NULL,
    source_document integer NOT NULL,
    definition jsonb NOT NULL,
    PRIMARY KEY (catalog_revision_id, name),
    CONSTRAINT catalog_runtimes_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT catalog_runtimes_source_path_not_empty CHECK (
        length(source_path) > 0
    ),
    CONSTRAINT catalog_runtimes_source_document_positive CHECK (
        source_document > 0
    ),
    CONSTRAINT catalog_runtimes_definition_object CHECK (
        jsonb_typeof(definition) = 'object'
    )
);

CREATE TABLE catalog_targets (
    catalog_revision_id uuid NOT NULL REFERENCES catalog_revisions (id),
    name text NOT NULL,
    source_path text NOT NULL,
    source_document integer NOT NULL,
    definition jsonb NOT NULL,
    PRIMARY KEY (catalog_revision_id, name),
    CONSTRAINT catalog_targets_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT catalog_targets_source_path_not_empty CHECK (
        length(source_path) > 0
    ),
    CONSTRAINT catalog_targets_source_document_positive CHECK (
        source_document > 0
    ),
    CONSTRAINT catalog_targets_definition_object CHECK (
        jsonb_typeof(definition) = 'object'
    )
);

CREATE TABLE catalog_validation_failures (
    repository_id uuid NOT NULL REFERENCES repositories (id),
    commit_sha text NOT NULL,
    recorded_at timestamptz NOT NULL,
    failure_reason text NOT NULL,
    PRIMARY KEY (repository_id, commit_sha),
    CONSTRAINT catalog_validation_failures_commit_sha_valid CHECK (
        commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT catalog_validation_failures_reason_not_empty CHECK (
        length(failure_reason) > 0
    )
);

-- +goose Down
DROP TABLE catalog_validation_failures;
DROP TABLE catalog_targets;
DROP TABLE catalog_runtimes;

ALTER TABLE repositories
DROP CONSTRAINT repositories_active_catalog_revision_fk,
DROP COLUMN active_catalog_revision_id;

DROP TABLE catalog_revisions;
