-- +goose Up
ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:runs:review',
        'slipramp:runs:override-gates',
        'slipramp:promotions:override-gates',
        'slipramp:inbox:write',
        'slipramp:promotions:submit',
        'slipramp:promotions:write',
        'slipramp:admin'
    ]::text[]
);

ALTER TABLE run_gates
    ADD COLUMN overrides jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(overrides) = 'array'),
    DROP CONSTRAINT run_gates_release_evidence_complete,
    ADD CONSTRAINT run_gates_release_evidence_complete CHECK (
        (
            released_at IS NULL
            AND release_catalog_revision_id IS NULL
            AND release_definition IS NULL
            AND release_definition_digest IS NULL
            AND release_evaluator_process_id IS NULL
            AND release_observed_value IS NULL
            AND release_evaluated_at IS NULL
            AND release_valid_until IS NULL
        )
        OR (
            released_at IS NOT NULL
            AND release_catalog_revision_id IS NOT NULL
            AND release_definition IS NOT NULL
            AND release_definition_digest IS NOT NULL
            AND (
                num_nonnulls(release_evaluator_process_id, release_observed_value, release_evaluated_at, release_valid_until) = 4
                OR (
                    num_nonnulls(release_evaluator_process_id, release_observed_value, release_evaluated_at, release_valid_until) = 0
                    AND (overrides -> -1 ->> 'definitionDigest') IS NOT DISTINCT FROM release_definition_digest
                )
            )
        )
    );

ALTER TABLE promotion_gates
    ADD COLUMN overrides jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(overrides) = 'array'),
    DROP CONSTRAINT promotion_gates_release_evidence_complete,
    ADD CONSTRAINT promotion_gates_release_evidence_complete CHECK (
        (
            released_at IS NULL
            AND release_catalog_revision_id IS NULL
            AND release_definition IS NULL
            AND release_definition_digest IS NULL
            AND release_evaluator_process_id IS NULL
            AND release_observed_value IS NULL
            AND release_evaluated_at IS NULL
            AND release_valid_until IS NULL
        )
        OR (
            released_at IS NOT NULL
            AND release_catalog_revision_id IS NOT NULL
            AND release_definition IS NOT NULL
            AND release_definition_digest IS NOT NULL
            AND (
                num_nonnulls(release_evaluator_process_id, release_observed_value, release_evaluated_at, release_valid_until) = 4
                OR (
                    num_nonnulls(release_evaluator_process_id, release_observed_value, release_evaluated_at, release_valid_until) = 0
                    AND (overrides -> -1 ->> 'definitionDigest') IS NOT DISTINCT FROM release_definition_digest
                )
            )
        )
    );

-- Assignment changes the Run phase before copying evidence in the same transaction.
-- Release evidence must remain available until the requirement itself is released.
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

DROP FUNCTION run_gates_allow_dispatch(uuid, uuid, timestamptz);

-- +goose StatementBegin
CREATE FUNCTION runs_awaiting_gates(process_id uuid, checked_at timestamptz)
RETURNS TABLE (run_id uuid)
LANGUAGE sql STABLE AS $$
    SELECT requirement.subject_id
    FROM waiting_gate_requirements AS requirement
    WHERE requirement.subject_type = 'run'
      AND NOT EXISTS (
          SELECT 1 FROM eligible_gate_evidence(process_id, checked_at) AS evidence
          WHERE evidence.subject_type = 'run'
            AND evidence.subject_id = requirement.subject_id
            AND evidence.gate_name = requirement.gate_name
      );
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION run_gates_allow_dispatch(candidate_id uuid, process_id uuid, checked_at timestamptz)
RETURNS boolean
LANGUAGE sql STABLE
AS $$
    SELECT NOT EXISTS (
        SELECT 1
        FROM run_gates AS requirement
        WHERE requirement.run_id = candidate_id
          AND requirement.released_at IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM active_passing_gate_evidence AS evidence
              WHERE evidence.repository_id = requirement.repository_id
                AND evidence.gate_name = requirement.gate_name
                AND evidence.evaluator_process_id = process_id
                AND evidence.evaluated_at <= checked_at
                AND evidence.valid_until > checked_at
          )
    );
$$;
-- +goose StatementEnd

DROP FUNCTION runs_awaiting_gates(uuid, timestamptz);
DROP FUNCTION eligible_gate_evidence(uuid, timestamptz);
DROP VIEW subject_gate_evidence;

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

ALTER TABLE run_gates
    DROP CONSTRAINT run_gates_release_evidence_complete,
    ADD CONSTRAINT run_gates_release_evidence_complete CHECK (
        (
            released_at IS NULL
            AND release_catalog_revision_id IS NULL
            AND release_definition IS NULL
            AND release_definition_digest IS NULL
            AND release_evaluator_process_id IS NULL
            AND release_observed_value IS NULL
            AND release_evaluated_at IS NULL
            AND release_valid_until IS NULL
        )
        OR (
            released_at IS NOT NULL
            AND release_catalog_revision_id IS NOT NULL
            AND release_definition IS NOT NULL
            AND release_definition_digest IS NOT NULL
            AND release_evaluator_process_id IS NOT NULL
            AND release_observed_value IS NOT NULL
            AND release_evaluated_at IS NOT NULL
            AND release_valid_until IS NOT NULL
        )
    ),
    DROP COLUMN overrides;

ALTER TABLE promotion_gates
    DROP CONSTRAINT promotion_gates_release_evidence_complete,
    ADD CONSTRAINT promotion_gates_release_evidence_complete CHECK (
        (
            released_at IS NULL
            AND release_catalog_revision_id IS NULL
            AND release_definition IS NULL
            AND release_definition_digest IS NULL
            AND release_evaluator_process_id IS NULL
            AND release_observed_value IS NULL
            AND release_evaluated_at IS NULL
            AND release_valid_until IS NULL
        )
        OR (
            released_at IS NOT NULL
            AND release_catalog_revision_id IS NOT NULL
            AND release_definition IS NOT NULL
            AND release_definition_digest IS NOT NULL
            AND release_evaluator_process_id IS NOT NULL
            AND release_observed_value IS NOT NULL
            AND release_evaluated_at IS NOT NULL
            AND release_valid_until IS NOT NULL
        )
    ),
    DROP COLUMN overrides;
