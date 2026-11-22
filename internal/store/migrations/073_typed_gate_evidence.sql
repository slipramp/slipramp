-- +goose Up
DROP FUNCTION eligible_gate_evidence(uuid, timestamptz);
DROP VIEW subject_gate_evidence;
DROP VIEW active_passing_gate_evidence;

-- Each evaluator admits only its bounded public payload. A JSON null represents
-- neither a sample nor the SQL NULL used for an absent observation.
-- +goose StatementBegin
CREATE FUNCTION valid_gate_evidence(payload jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE WHEN jsonb_typeof(payload #> '{prometheus,value}') = 'number'
        THEN payload = jsonb_build_object('prometheus', jsonb_build_object('value', payload #> '{prometheus,value}'))
            AND (payload #>> '{prometheus,value}')::double precision BETWEEN
                '-1.7976931348623157e308'::double precision AND '1.7976931348623157e308'::double precision
        ELSE false END;
$$;
-- +goose StatementEnd

ALTER TABLE gate_observations DROP CONSTRAINT gate_observations_value_finite;
ALTER TABLE gate_observations RENAME COLUMN value TO evidence;
ALTER TABLE gate_observations ALTER COLUMN evidence TYPE jsonb USING
    CASE WHEN evidence IS NOT NULL THEN jsonb_build_object('prometheus', jsonb_build_object('value', evidence)) END;
ALTER TABLE gate_observations ADD CONSTRAINT gate_observations_evidence_valid CHECK (valid_gate_evidence(evidence));

ALTER TABLE run_gates DROP CONSTRAINT run_gates_release_observed_value_finite;
ALTER TABLE run_gates RENAME COLUMN release_observed_value TO release_evidence;
ALTER TABLE run_gates ALTER COLUMN release_evidence TYPE jsonb USING
    CASE WHEN release_evidence IS NOT NULL THEN jsonb_build_object('prometheus', jsonb_build_object('value', release_evidence)) END;
ALTER TABLE run_gates ADD CONSTRAINT run_gates_release_evidence_valid CHECK (valid_gate_evidence(release_evidence));

UPDATE run_gates SET overrides = (
    SELECT jsonb_agg(CASE WHEN decision ? 'observedValue'
        THEN (decision - 'observedValue') || jsonb_build_object('observedEvidence', jsonb_build_object('prometheus', jsonb_build_object('value', decision->'observedValue')))
        ELSE decision END ORDER BY ordinal)
    FROM jsonb_array_elements(overrides) WITH ORDINALITY AS history(decision, ordinal)
) WHERE jsonb_path_exists(overrides, '$[*].observedValue');

ALTER TABLE promotion_gates DROP CONSTRAINT promotion_gates_release_observed_value_finite;
ALTER TABLE promotion_gates RENAME COLUMN release_observed_value TO release_evidence;
ALTER TABLE promotion_gates ALTER COLUMN release_evidence TYPE jsonb USING
    CASE WHEN release_evidence IS NOT NULL THEN jsonb_build_object('prometheus', jsonb_build_object('value', release_evidence)) END;
ALTER TABLE promotion_gates ADD CONSTRAINT promotion_gates_release_evidence_valid CHECK (valid_gate_evidence(release_evidence));

UPDATE promotion_gates SET overrides = (
    SELECT jsonb_agg(CASE WHEN decision ? 'observedValue'
        THEN (decision - 'observedValue') || jsonb_build_object('observedEvidence', jsonb_build_object('prometheus', jsonb_build_object('value', decision->'observedValue')))
        ELSE decision END ORDER BY ordinal)
    FROM jsonb_array_elements(overrides) WITH ORDINALITY AS history(decision, ordinal)
) WHERE jsonb_path_exists(overrides, '$[*].observedValue');

CREATE VIEW subject_gate_evidence AS
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

-- +goose StatementBegin
CREATE FUNCTION eligible_gate_evidence(process_id uuid, checked_at timestamptz)
RETURNS SETOF subject_gate_evidence
LANGUAGE sql STABLE AS $$
    SELECT * FROM subject_gate_evidence
    WHERE overridden OR (evaluator_process_id = process_id AND evaluated_at <= checked_at AND valid_until > checked_at);
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION eligible_gate_evidence(uuid, timestamptz);
DROP VIEW subject_gate_evidence;

ALTER TABLE gate_observations DROP CONSTRAINT gate_observations_evidence_valid;
ALTER TABLE gate_observations ALTER COLUMN evidence TYPE double precision USING (evidence #>> '{prometheus,value}')::double precision;
ALTER TABLE gate_observations RENAME COLUMN evidence TO value;
ALTER TABLE gate_observations ADD CONSTRAINT gate_observations_value_finite CHECK (
    value NOT IN ('NaN'::double precision, 'Infinity'::double precision, '-Infinity'::double precision)
);

ALTER TABLE run_gates DROP CONSTRAINT run_gates_release_evidence_valid;
ALTER TABLE run_gates ALTER COLUMN release_evidence TYPE double precision USING (release_evidence #>> '{prometheus,value}')::double precision;
ALTER TABLE run_gates RENAME COLUMN release_evidence TO release_observed_value;
ALTER TABLE run_gates ADD CONSTRAINT run_gates_release_observed_value_finite CHECK (
    release_observed_value NOT IN ('NaN'::double precision, 'Infinity'::double precision, '-Infinity'::double precision)
);

UPDATE run_gates SET overrides = (
    SELECT jsonb_agg(CASE WHEN decision ? 'observedEvidence'
        THEN (decision - 'observedEvidence') || jsonb_build_object('observedValue', decision #> '{observedEvidence,prometheus,value}')
        ELSE decision END ORDER BY ordinal)
    FROM jsonb_array_elements(overrides) WITH ORDINALITY AS history(decision, ordinal)
) WHERE jsonb_path_exists(overrides, '$[*].observedEvidence');

ALTER TABLE promotion_gates DROP CONSTRAINT promotion_gates_release_evidence_valid;
ALTER TABLE promotion_gates ALTER COLUMN release_evidence TYPE double precision USING (release_evidence #>> '{prometheus,value}')::double precision;
ALTER TABLE promotion_gates RENAME COLUMN release_evidence TO release_observed_value;
ALTER TABLE promotion_gates ADD CONSTRAINT promotion_gates_release_observed_value_finite CHECK (
    release_observed_value NOT IN ('NaN'::double precision, 'Infinity'::double precision, '-Infinity'::double precision)
);

UPDATE promotion_gates SET overrides = (
    SELECT jsonb_agg(CASE WHEN decision ? 'observedEvidence'
        THEN (decision - 'observedEvidence') || jsonb_build_object('observedValue', decision #> '{observedEvidence,prometheus,value}')
        ELSE decision END ORDER BY ordinal)
    FROM jsonb_array_elements(overrides) WITH ORDINALITY AS history(decision, ordinal)
) WHERE jsonb_path_exists(overrides, '$[*].observedEvidence');

DROP FUNCTION valid_gate_evidence(jsonb);

CREATE VIEW active_passing_gate_evidence AS
SELECT
    observation.repository_id,
    observation.gate_name,
    observation.catalog_revision_id,
    observation.definition_digest,
    (definition.definition->'spec')::jsonb AS spec,
    observation.evaluator_process_id,
    observation.value,
    observation.evaluated_at,
    observation.valid_until
FROM gate_observations AS observation
JOIN repositories
  ON repositories.id = observation.repository_id
 AND repositories.active_catalog_revision_id = observation.catalog_revision_id
JOIN catalog_revisions ON catalog_revisions.id = observation.catalog_revision_id
JOIN catalog_gates AS definition
  ON definition.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
 AND definition.name = observation.gate_name
 AND definition.definition_digest = observation.definition_digest
WHERE repositories.removed_at IS NULL
  AND observation.state = 'passing';

CREATE VIEW subject_gate_evidence AS
SELECT requirements.subject_type, requirements.subject_id, requirements.gate_name,
    revision.id AS catalog_revision_id, definition.definition_digest,
    definition.definition->'spec' AS spec,
    COALESCE(requirements.override_digest = definition.definition_digest, false) AS overridden,
    observation.evaluator_process_id, observation.value, observation.evaluated_at, observation.valid_until
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

-- +goose StatementBegin
CREATE FUNCTION eligible_gate_evidence(process_id uuid, checked_at timestamptz)
RETURNS SETOF subject_gate_evidence
LANGUAGE sql STABLE AS $$
    SELECT * FROM subject_gate_evidence
    WHERE overridden OR (evaluator_process_id = process_id AND evaluated_at <= checked_at AND valid_until > checked_at);
$$;
-- +goose StatementEnd
