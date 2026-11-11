-- +goose Up
CREATE TABLE catalog_snapshots (
    id text PRIMARY KEY,
    created_at timestamptz NOT NULL,
    CONSTRAINT catalog_snapshots_id_valid CHECK (id ~ '^[0-9a-f]{64}$')
);

ALTER TABLE catalog_revisions
ADD COLUMN catalog_snapshot_id text;

UPDATE catalog_revisions
SET catalog_snapshot_id = encode(
    sha256(
        convert_to(
            jsonb_build_object(
                'version', 1,
                'runtimes', coalesce(
                    (
                        SELECT jsonb_agg(
                            jsonb_build_object(
                                'name', catalog_runtimes.name,
                                'sourcePath', catalog_runtimes.source_path,
                                'sourceDocument', catalog_runtimes.source_document,
                                'definition', catalog_runtimes.definition
                            )
                            ORDER BY catalog_runtimes.name
                        )
                        FROM catalog_runtimes
                        WHERE catalog_runtimes.catalog_revision_id = catalog_revisions.id
                    ),
                    '[]'::jsonb
                ),
                'targets', coalesce(
                    (
                        SELECT jsonb_agg(
                            jsonb_build_object(
                                'name', catalog_targets.name,
                                'sourcePath', catalog_targets.source_path,
                                'sourceDocument', catalog_targets.source_document,
                                'definition', catalog_targets.definition
                            )
                            ORDER BY catalog_targets.name
                        )
                        FROM catalog_targets
                        WHERE catalog_targets.catalog_revision_id = catalog_revisions.id
                    ),
                    '[]'::jsonb
                )
            )::text,
            'UTF8'
        )
    ),
    'hex'
);

INSERT INTO catalog_snapshots (id, created_at)
SELECT catalog_snapshot_id, min(created_at)
FROM catalog_revisions
GROUP BY catalog_snapshot_id;

ALTER TABLE catalog_revisions
ALTER COLUMN catalog_snapshot_id SET NOT NULL,
ADD CONSTRAINT catalog_revisions_snapshot_fk FOREIGN KEY (catalog_snapshot_id)
    REFERENCES catalog_snapshots (id);

CREATE TABLE catalog_runtimes_by_snapshot (
    catalog_snapshot_id text NOT NULL REFERENCES catalog_snapshots (id),
    name text NOT NULL,
    source_path text NOT NULL,
    source_document integer NOT NULL,
    definition jsonb NOT NULL,
    PRIMARY KEY (catalog_snapshot_id, name),
    CONSTRAINT catalog_runtimes_snapshot_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT catalog_runtimes_snapshot_source_path_not_empty CHECK (
        length(source_path) > 0
    ),
    CONSTRAINT catalog_runtimes_snapshot_source_document_positive CHECK (
        source_document > 0
    ),
    CONSTRAINT catalog_runtimes_snapshot_definition_object CHECK (
        jsonb_typeof(definition) = 'object'
    )
);

INSERT INTO catalog_runtimes_by_snapshot (
    catalog_snapshot_id,
    name,
    source_path,
    source_document,
    definition
)
SELECT DISTINCT ON (catalog_revisions.catalog_snapshot_id, catalog_runtimes.name)
    catalog_revisions.catalog_snapshot_id,
    catalog_runtimes.name,
    catalog_runtimes.source_path,
    catalog_runtimes.source_document,
    catalog_runtimes.definition
FROM catalog_runtimes
JOIN catalog_revisions
  ON catalog_revisions.id = catalog_runtimes.catalog_revision_id
ORDER BY
    catalog_revisions.catalog_snapshot_id,
    catalog_runtimes.name,
    catalog_runtimes.catalog_revision_id;

CREATE TABLE catalog_targets_by_snapshot (
    catalog_snapshot_id text NOT NULL REFERENCES catalog_snapshots (id),
    name text NOT NULL,
    source_path text NOT NULL,
    source_document integer NOT NULL,
    definition jsonb NOT NULL,
    PRIMARY KEY (catalog_snapshot_id, name),
    CONSTRAINT catalog_targets_snapshot_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT catalog_targets_snapshot_source_path_not_empty CHECK (
        length(source_path) > 0
    ),
    CONSTRAINT catalog_targets_snapshot_source_document_positive CHECK (
        source_document > 0
    ),
    CONSTRAINT catalog_targets_snapshot_definition_object CHECK (
        jsonb_typeof(definition) = 'object'
    )
);

INSERT INTO catalog_targets_by_snapshot (
    catalog_snapshot_id,
    name,
    source_path,
    source_document,
    definition
)
SELECT DISTINCT ON (catalog_revisions.catalog_snapshot_id, catalog_targets.name)
    catalog_revisions.catalog_snapshot_id,
    catalog_targets.name,
    catalog_targets.source_path,
    catalog_targets.source_document,
    catalog_targets.definition
FROM catalog_targets
JOIN catalog_revisions
  ON catalog_revisions.id = catalog_targets.catalog_revision_id
ORDER BY
    catalog_revisions.catalog_snapshot_id,
    catalog_targets.name,
    catalog_targets.catalog_revision_id;

DROP TABLE catalog_targets;
DROP TABLE catalog_runtimes;

ALTER TABLE catalog_runtimes_by_snapshot RENAME TO catalog_runtimes;
ALTER TABLE catalog_targets_by_snapshot RENAME TO catalog_targets;

-- +goose Down
CREATE TABLE catalog_runtimes_by_revision (
    catalog_revision_id uuid NOT NULL REFERENCES catalog_revisions (id),
    name text NOT NULL,
    source_path text NOT NULL,
    source_document integer NOT NULL,
    definition jsonb NOT NULL,
    PRIMARY KEY (catalog_revision_id, name),
    CONSTRAINT catalog_runtimes_revision_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT catalog_runtimes_revision_source_path_not_empty CHECK (
        length(source_path) > 0
    ),
    CONSTRAINT catalog_runtimes_revision_source_document_positive CHECK (
        source_document > 0
    ),
    CONSTRAINT catalog_runtimes_revision_definition_object CHECK (
        jsonb_typeof(definition) = 'object'
    )
);

INSERT INTO catalog_runtimes_by_revision (
    catalog_revision_id,
    name,
    source_path,
    source_document,
    definition
)
SELECT
    catalog_revisions.id,
    catalog_runtimes.name,
    catalog_runtimes.source_path,
    catalog_runtimes.source_document,
    catalog_runtimes.definition
FROM catalog_revisions
JOIN catalog_runtimes
  ON catalog_runtimes.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id;

CREATE TABLE catalog_targets_by_revision (
    catalog_revision_id uuid NOT NULL REFERENCES catalog_revisions (id),
    name text NOT NULL,
    source_path text NOT NULL,
    source_document integer NOT NULL,
    definition jsonb NOT NULL,
    PRIMARY KEY (catalog_revision_id, name),
    CONSTRAINT catalog_targets_revision_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT catalog_targets_revision_source_path_not_empty CHECK (
        length(source_path) > 0
    ),
    CONSTRAINT catalog_targets_revision_source_document_positive CHECK (
        source_document > 0
    ),
    CONSTRAINT catalog_targets_revision_definition_object CHECK (
        jsonb_typeof(definition) = 'object'
    )
);

INSERT INTO catalog_targets_by_revision (
    catalog_revision_id,
    name,
    source_path,
    source_document,
    definition
)
SELECT
    catalog_revisions.id,
    catalog_targets.name,
    catalog_targets.source_path,
    catalog_targets.source_document,
    catalog_targets.definition
FROM catalog_revisions
JOIN catalog_targets
  ON catalog_targets.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id;

DROP TABLE catalog_targets;
DROP TABLE catalog_runtimes;

ALTER TABLE catalog_runtimes_by_revision RENAME TO catalog_runtimes;
ALTER TABLE catalog_targets_by_revision RENAME TO catalog_targets;

ALTER TABLE catalog_revisions
DROP CONSTRAINT catalog_revisions_snapshot_fk,
DROP COLUMN catalog_snapshot_id;

DROP TABLE catalog_snapshots;
