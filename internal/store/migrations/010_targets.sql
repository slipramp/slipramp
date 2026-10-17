-- +goose Up
CREATE TABLE targets (
    repository_id uuid NOT NULL REFERENCES repositories (id),
    name text NOT NULL,
    catalog_revision_id uuid,
    desired_commit_sha text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz,
    PRIMARY KEY (repository_id, name),
    FOREIGN KEY (catalog_revision_id, repository_id)
        REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT targets_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT targets_desired_commit_sha_valid CHECK (
        desired_commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT targets_updated_after_creation CHECK (updated_at >= created_at),
    CONSTRAINT targets_archived_after_creation CHECK (
        archived_at IS NULL OR archived_at >= created_at
    ),
    CONSTRAINT targets_active_state_valid CHECK (
        (catalog_revision_id IS NULL) = (archived_at IS NOT NULL)
    )
);

INSERT INTO targets (
    repository_id,
    name,
    catalog_revision_id,
    desired_commit_sha,
    created_at,
    updated_at
)
SELECT
    catalog_revisions.repository_id,
    catalog_targets.name,
    catalog_revisions.id,
    catalog_revisions.commit_sha,
    catalog_revisions.created_at,
    catalog_revisions.created_at
FROM repositories
JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
JOIN catalog_targets
  ON catalog_targets.catalog_revision_id = catalog_revisions.id;

-- +goose Down
DROP TABLE targets;
