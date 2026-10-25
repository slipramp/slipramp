-- +goose Up
ALTER TABLE targets
ADD COLUMN id uuid;

ALTER TABLE targets
DROP CONSTRAINT targets_active_state_valid;

-- Older installations can contain Runs for Targets that disappeared before the
-- durable Target state table was introduced. Reconstruct those archived
-- identities before linking historical Runs to them.
INSERT INTO targets (
    id,
    repository_id,
    name,
    catalog_revision_id,
    desired_commit_sha,
    created_at,
    updated_at,
    archived_at
)
SELECT DISTINCT ON (runs.repository_id, runs.target_name)
    gen_random_uuid(),
    runs.repository_id,
    runs.target_name,
    runs.catalog_revision_id,
    runs.commit_sha,
    runs.created_at,
    runs.created_at,
    runs.created_at
FROM runs
LEFT JOIN targets
  ON targets.repository_id = runs.repository_id
 AND targets.name = runs.target_name
WHERE targets.repository_id IS NULL
ORDER BY runs.repository_id, runs.target_name, runs.created_at DESC, runs.id DESC;

UPDATE targets
SET id = gen_random_uuid()
WHERE id IS NULL;

-- Archived Targets previously discarded this link. Preserve the most recent
-- accepted definition so durable Target URLs remain useful after archival.
UPDATE targets
SET catalog_revision_id = (
    SELECT catalog_targets.catalog_revision_id
    FROM catalog_targets
    JOIN catalog_revisions
      ON catalog_revisions.id = catalog_targets.catalog_revision_id
    WHERE catalog_revisions.repository_id = targets.repository_id
      AND catalog_targets.name = targets.name
    ORDER BY catalog_revisions.created_at DESC, catalog_revisions.id DESC
    LIMIT 1
)
WHERE targets.catalog_revision_id IS NULL;

ALTER TABLE targets
ALTER COLUMN id SET NOT NULL,
ALTER COLUMN catalog_revision_id SET NOT NULL;

ALTER TABLE targets
DROP CONSTRAINT targets_pkey;

ALTER TABLE targets
ADD CONSTRAINT targets_pkey PRIMARY KEY (id),
ADD CONSTRAINT targets_repository_name_key UNIQUE (repository_id, name);

ALTER TABLE runs
ADD COLUMN target_id uuid;

UPDATE runs
SET target_id = targets.id
FROM targets
WHERE targets.repository_id = runs.repository_id
  AND targets.name = runs.target_name;

ALTER TABLE runs
ALTER COLUMN target_id SET NOT NULL,
ADD CONSTRAINT runs_target_fk FOREIGN KEY (target_id) REFERENCES targets (id);

DROP INDEX runs_target_created_at;
DROP INDEX runs_one_active_per_target;

CREATE INDEX runs_target_created_at
ON runs (target_id, created_at DESC);

CREATE UNIQUE INDEX runs_one_active_per_target
ON runs (target_id)
WHERE phase IN ('assigned', 'preparing', 'running');

ALTER TABLE git_trigger_deliveries
ADD COLUMN target_id uuid;

UPDATE git_trigger_deliveries
SET target_id = targets.id
FROM targets
WHERE targets.repository_id = git_trigger_deliveries.repository_id
  AND targets.name = git_trigger_deliveries.target_name;

ALTER TABLE git_trigger_deliveries
ALTER COLUMN target_id SET NOT NULL,
ADD CONSTRAINT git_trigger_deliveries_target_fk
    FOREIGN KEY (target_id) REFERENCES targets (id);

DROP INDEX git_trigger_deliveries_target_pending;

CREATE INDEX git_trigger_deliveries_target_pending
ON git_trigger_deliveries (target_id)
WHERE run_id IS NULL AND superseded_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP INDEX git_trigger_deliveries_target_pending;

ALTER TABLE git_trigger_deliveries
DROP CONSTRAINT git_trigger_deliveries_target_fk,
DROP COLUMN target_id;

CREATE INDEX git_trigger_deliveries_target_pending
ON git_trigger_deliveries (repository_id, target_name)
WHERE run_id IS NULL AND superseded_at IS NULL AND failed_at IS NULL;

DROP INDEX runs_one_active_per_target;
DROP INDEX runs_target_created_at;

ALTER TABLE runs
DROP CONSTRAINT runs_target_fk,
DROP COLUMN target_id;

CREATE INDEX runs_target_created_at
ON runs (repository_id, target_name, created_at DESC);

CREATE UNIQUE INDEX runs_one_active_per_target
ON runs (repository_id, target_name)
WHERE phase IN ('assigned', 'preparing', 'running');

ALTER TABLE targets
DROP CONSTRAINT targets_pkey,
DROP CONSTRAINT targets_repository_name_key;

ALTER TABLE targets
ADD CONSTRAINT targets_pkey PRIMARY KEY (repository_id, name);

ALTER TABLE targets
ALTER COLUMN catalog_revision_id DROP NOT NULL;

UPDATE targets
SET catalog_revision_id = NULL
WHERE archived_at IS NOT NULL;

ALTER TABLE targets
DROP COLUMN id;

ALTER TABLE targets
ADD CONSTRAINT targets_active_state_valid CHECK (
    (catalog_revision_id IS NULL) = (archived_at IS NOT NULL)
);
