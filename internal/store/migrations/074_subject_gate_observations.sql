-- +goose Up
ALTER TABLE catalog_gates ADD COLUMN subject_scoped boolean NOT NULL DEFAULT false;
ALTER TABLE catalog_gates ALTER COLUMN subject_scoped DROP DEFAULT;

CREATE OR REPLACE VIEW gate_evaluation_demand AS
WITH demand AS (
    SELECT repository_id, gate_name, count(*) AS waiter_count, max(requested_at) AS last_requested_at
    FROM waiting_gate_requirements
    GROUP BY repository_id, gate_name
)
SELECT
    demand.repository_id,
    demand.gate_name,
    demand.waiter_count,
    demand.last_requested_at::timestamptz AS last_requested_at,
    repositories.active_catalog_revision_id AS catalog_revision_id,
    (catalog_gates.definition->'spec')::jsonb AS spec,
    catalog_gates.definition_digest
FROM demand
JOIN repositories
  ON repositories.id = demand.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
JOIN catalog_gates
  ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
 AND catalog_gates.name = demand.gate_name
WHERE repositories.removed_at IS NULL AND NOT catalog_gates.subject_scoped;

CREATE VIEW subject_gate_evaluation_demand AS
SELECT w.subject_type, w.subject_id, w.repository_id, w.gate_name, w.requested_at,
    repo.name AS repository_name, rev.id AS catalog_revision_id, rev.commit_sha,
    g.definition->'spec' AS spec, g.definition_digest
FROM waiting_gate_requirements AS w
JOIN repositories AS repo ON repo.id = w.repository_id AND repo.removed_at IS NULL
JOIN catalog_revisions AS rev ON rev.id = repo.active_catalog_revision_id
JOIN catalog_gates AS g ON g.catalog_snapshot_id = rev.catalog_snapshot_id AND g.name = w.gate_name
WHERE g.subject_scoped;

-- Only normalized evaluator fields may appear in current or released evidence.
-- +goose StatementBegin
CREATE FUNCTION valid_webhook_evidence(payload jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT COALESCE(jsonb_typeof(payload #> '{webhook,code}') = 'string'
        AND jsonb_typeof(payload #> '{webhook,message}') = 'string'
        AND payload #>> '{webhook,code}' ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
        AND length(payload #>> '{webhook,message}') BETWEEN 1 AND 256
        AND btrim(payload #>> '{webhook,message}') <> ''
        AND payload #>> '{webhook,message}' !~ '[[:cntrl:]]'
        AND payload = jsonb_build_object('webhook', jsonb_build_object(
            'code', payload #> '{webhook,code}', 'message', payload #> '{webhook,message}')), false);
$$;
-- +goose StatementEnd

ALTER TABLE run_gates
    ADD COLUMN current_catalog_revision_id uuid,
    ADD COLUMN current_definition_digest text CHECK (current_definition_digest ~ '^[0-9a-f]{64}$'),
    ADD COLUMN current_evaluator_process_id uuid,
    ADD COLUMN current_state text CHECK (current_state IN ('passing', 'blocking', 'unavailable')),
    ADD COLUMN current_evidence jsonb CHECK (valid_webhook_evidence(current_evidence)),
    ADD COLUMN current_diagnostic text,
    ADD COLUMN current_evaluated_at timestamptz,
    ADD COLUMN current_valid_until timestamptz CHECK (current_valid_until > current_evaluated_at),
    ADD FOREIGN KEY (current_catalog_revision_id, repository_id) REFERENCES catalog_revisions (id, repository_id),
    ADD CHECK (
        (num_nonnulls(current_catalog_revision_id, current_definition_digest, current_evaluator_process_id,
            current_state, current_evidence, current_diagnostic, current_evaluated_at, current_valid_until) = 0)
        OR (num_nonnulls(current_catalog_revision_id, current_definition_digest, current_evaluator_process_id,
            current_state, current_evaluated_at, current_valid_until) = 6
            AND ((current_state IN ('passing', 'blocking') AND current_evidence IS NOT NULL AND current_diagnostic IS NULL)
                OR (current_state = 'unavailable' AND current_evidence IS NULL AND current_diagnostic IS NOT NULL
                    AND length(current_diagnostic) BETWEEN 1 AND 512)))
    ),
    DROP CONSTRAINT run_gates_release_evidence_valid,
    ADD CONSTRAINT run_gates_release_evidence_valid CHECK (
        valid_gate_evidence(release_evidence) OR valid_webhook_evidence(release_evidence)
    );

ALTER TABLE promotion_gates
    ADD COLUMN current_catalog_revision_id uuid,
    ADD COLUMN current_definition_digest text CHECK (current_definition_digest ~ '^[0-9a-f]{64}$'),
    ADD COLUMN current_evaluator_process_id uuid,
    ADD COLUMN current_state text CHECK (current_state IN ('passing', 'blocking', 'unavailable')),
    ADD COLUMN current_evidence jsonb CHECK (valid_webhook_evidence(current_evidence)),
    ADD COLUMN current_diagnostic text,
    ADD COLUMN current_evaluated_at timestamptz,
    ADD COLUMN current_valid_until timestamptz CHECK (current_valid_until > current_evaluated_at),
    ADD FOREIGN KEY (current_catalog_revision_id, repository_id) REFERENCES catalog_revisions (id, repository_id),
    ADD CHECK (
        (num_nonnulls(current_catalog_revision_id, current_definition_digest, current_evaluator_process_id,
            current_state, current_evidence, current_diagnostic, current_evaluated_at, current_valid_until) = 0)
        OR (num_nonnulls(current_catalog_revision_id, current_definition_digest, current_evaluator_process_id,
            current_state, current_evaluated_at, current_valid_until) = 6
            AND ((current_state IN ('passing', 'blocking') AND current_evidence IS NOT NULL AND current_diagnostic IS NULL)
                OR (current_state = 'unavailable' AND current_evidence IS NULL AND current_diagnostic IS NOT NULL
                    AND length(current_diagnostic) BETWEEN 1 AND 512)))
    ),
    DROP CONSTRAINT promotion_gates_release_evidence_valid,
    ADD CONSTRAINT promotion_gates_release_evidence_valid CHECK (
        valid_gate_evidence(release_evidence) OR valid_webhook_evidence(release_evidence)
    );

CREATE VIEW gate_current_observations AS
SELECT 'shared'::text AS subject_type, NULL::uuid AS subject_id, repository_id, gate_name,
    catalog_revision_id, definition_digest, evaluator_process_id, state, evidence, diagnostic, evaluated_at, valid_until
FROM gate_observations
UNION ALL
SELECT 'run', run_id, repository_id, gate_name, current_catalog_revision_id, current_definition_digest,
    current_evaluator_process_id, current_state, current_evidence, current_diagnostic, current_evaluated_at, current_valid_until
FROM run_gates WHERE current_state IS NOT NULL AND released_at IS NULL
UNION ALL
SELECT 'promotion', promotion_id, repository_id, gate_name, current_catalog_revision_id, current_definition_digest,
    current_evaluator_process_id, current_state, current_evidence, current_diagnostic, current_evaluated_at, current_valid_until
FROM promotion_gates WHERE current_state IS NOT NULL AND released_at IS NULL;

CREATE OR REPLACE VIEW subject_gate_evidence AS
SELECT requirements.subject_type, requirements.subject_id, requirements.gate_name,
    revision.id AS catalog_revision_id, definition.definition_digest,
    definition.definition->'spec' AS spec,
    COALESCE(requirements.override_digest = definition.definition_digest, false) AS overridden,
    observation.evaluator_process_id, observation.evidence, observation.evaluated_at, observation.valid_until
FROM (
    SELECT 'run'::text AS subject_type, run_id AS subject_id, repository_id, gate_name,
        overrides -> -1 ->> 'definitionDigest' AS override_digest
    FROM run_gates WHERE released_at IS NULL
    UNION ALL
    SELECT 'promotion', promotion_id, repository_id, gate_name, overrides -> -1 ->> 'definitionDigest'
    FROM promotion_gates WHERE released_at IS NULL
) AS requirements
JOIN repositories ON repositories.id = requirements.repository_id AND repositories.removed_at IS NULL
JOIN catalog_revisions AS revision ON revision.id = repositories.active_catalog_revision_id
JOIN catalog_gates AS definition ON definition.catalog_snapshot_id = revision.catalog_snapshot_id
    AND definition.name = requirements.gate_name
LEFT JOIN gate_current_observations AS observation ON
    observation.subject_type = CASE WHEN definition.subject_scoped THEN requirements.subject_type ELSE 'shared' END
    AND (observation.subject_type = 'shared' OR observation.subject_id = requirements.subject_id)
    AND observation.repository_id = requirements.repository_id
    AND observation.gate_name = requirements.gate_name AND observation.catalog_revision_id = revision.id
    AND observation.definition_digest = definition.definition_digest AND observation.state = 'passing'
    AND requirements.override_digest IS DISTINCT FROM definition.definition_digest;

-- +goose Down
CREATE OR REPLACE VIEW subject_gate_evidence AS
SELECT requirements.subject_type, requirements.subject_id, requirements.gate_name,
    revision.id AS catalog_revision_id, definition.definition_digest,
    definition.definition->'spec' AS spec,
    COALESCE(requirements.override_digest = definition.definition_digest, false) AS overridden,
    observation.evaluator_process_id, observation.evidence, observation.evaluated_at, observation.valid_until
FROM (
    SELECT 'run'::text AS subject_type, run_id AS subject_id, repository_id, gate_name,
        overrides -> -1 ->> 'definitionDigest' AS override_digest
    FROM run_gates WHERE released_at IS NULL
    UNION ALL
    SELECT 'promotion', promotion_id, repository_id, gate_name, overrides -> -1 ->> 'definitionDigest'
    FROM promotion_gates WHERE released_at IS NULL
) AS requirements
JOIN repositories ON repositories.id = requirements.repository_id AND repositories.removed_at IS NULL
JOIN catalog_revisions AS revision ON revision.id = repositories.active_catalog_revision_id
JOIN catalog_gates AS definition ON definition.catalog_snapshot_id = revision.catalog_snapshot_id
    AND definition.name = requirements.gate_name
LEFT JOIN gate_observations AS observation ON observation.repository_id = requirements.repository_id
    AND observation.gate_name = requirements.gate_name AND observation.catalog_revision_id = revision.id
    AND observation.definition_digest = definition.definition_digest AND observation.state = 'passing'
    AND requirements.override_digest IS DISTINCT FROM definition.definition_digest;

DROP VIEW gate_current_observations;

ALTER TABLE run_gates
    DROP CONSTRAINT run_gates_release_evidence_valid,
    ADD CONSTRAINT run_gates_release_evidence_valid CHECK (valid_gate_evidence(release_evidence)),
    DROP COLUMN current_catalog_revision_id,
    DROP COLUMN current_definition_digest,
    DROP COLUMN current_evaluator_process_id,
    DROP COLUMN current_state,
    DROP COLUMN current_evidence,
    DROP COLUMN current_diagnostic,
    DROP COLUMN current_evaluated_at,
    DROP COLUMN current_valid_until;

ALTER TABLE promotion_gates
    DROP CONSTRAINT promotion_gates_release_evidence_valid,
    ADD CONSTRAINT promotion_gates_release_evidence_valid CHECK (valid_gate_evidence(release_evidence)),
    DROP COLUMN current_catalog_revision_id,
    DROP COLUMN current_definition_digest,
    DROP COLUMN current_evaluator_process_id,
    DROP COLUMN current_state,
    DROP COLUMN current_evidence,
    DROP COLUMN current_diagnostic,
    DROP COLUMN current_evaluated_at,
    DROP COLUMN current_valid_until;

DROP FUNCTION valid_webhook_evidence(jsonb);

DROP VIEW subject_gate_evaluation_demand;
CREATE OR REPLACE VIEW gate_evaluation_demand AS
WITH demand AS (
    SELECT repository_id, gate_name, count(*) AS waiter_count, max(requested_at) AS last_requested_at
    FROM waiting_gate_requirements
    GROUP BY repository_id, gate_name
)
SELECT
    demand.repository_id,
    demand.gate_name,
    demand.waiter_count,
    demand.last_requested_at::timestamptz AS last_requested_at,
    repositories.active_catalog_revision_id AS catalog_revision_id,
    (catalog_gates.definition->'spec')::jsonb AS spec,
    catalog_gates.definition_digest
FROM demand
JOIN repositories
  ON repositories.id = demand.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
JOIN catalog_gates
  ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
 AND catalog_gates.name = demand.gate_name
WHERE repositories.removed_at IS NULL;

ALTER TABLE catalog_gates DROP COLUMN subject_scoped;
