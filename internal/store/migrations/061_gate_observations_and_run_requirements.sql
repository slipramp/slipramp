-- +goose Up
ALTER TABLE runs
ADD CONSTRAINT runs_id_repository_unique UNIQUE (id, repository_id);

CREATE TABLE gate_observations (
    repository_id uuid NOT NULL REFERENCES repositories (id),
    gate_name text NOT NULL,
    catalog_revision_id uuid NOT NULL,
    definition_digest text NOT NULL,
    evaluator_process_id uuid NOT NULL,
    state text NOT NULL,
    value double precision,
    diagnostic text,
    evaluated_at timestamptz NOT NULL,
    valid_until timestamptz NOT NULL,
    PRIMARY KEY (
        repository_id,
        gate_name,
        catalog_revision_id,
        definition_digest
    ),
    CONSTRAINT gate_observations_catalog_revision_fk FOREIGN KEY (
        catalog_revision_id,
        repository_id
    ) REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT gate_observations_gate_name_valid CHECK (
        length(gate_name) BETWEEN 1 AND 63
        AND gate_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
    ),
    CONSTRAINT gate_observations_definition_digest_valid CHECK (
        definition_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT gate_observations_state_valid CHECK (
        state IN ('passing', 'blocking', 'unavailable')
    ),
    CONSTRAINT gate_observations_value_finite CHECK (
        value IS NULL OR value NOT IN (
            'NaN'::double precision,
            'Infinity'::double precision,
            '-Infinity'::double precision
        )
    ),
    CONSTRAINT gate_observations_result_valid CHECK (
        (
            state IN ('passing', 'blocking')
            AND value IS NOT NULL
            AND diagnostic IS NULL
        )
        OR (
            state = 'unavailable'
            AND value IS NULL
            AND length(diagnostic) BETWEEN 1 AND 512
        )
    ),
    CONSTRAINT gate_observations_validity_ordered CHECK (
        valid_until > evaluated_at
    )
);

CREATE TABLE run_gates (
    run_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    gate_name text NOT NULL,
    attachment_order integer NOT NULL,
    released_at timestamptz,
    release_catalog_revision_id uuid,
    release_definition jsonb,
    release_definition_digest text,
    release_evaluator_process_id uuid,
    release_observed_value double precision,
    release_evaluated_at timestamptz,
    release_valid_until timestamptz,
    PRIMARY KEY (run_id, gate_name),
    UNIQUE (run_id, attachment_order),
    CONSTRAINT run_gates_run_fk FOREIGN KEY (run_id, repository_id)
        REFERENCES runs (id, repository_id),
    CONSTRAINT run_gates_release_catalog_revision_fk FOREIGN KEY (
        release_catalog_revision_id,
        repository_id
    ) REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT run_gates_gate_name_valid CHECK (
        length(gate_name) BETWEEN 1 AND 63
        AND gate_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
    ),
    CONSTRAINT run_gates_attachment_order_valid CHECK (attachment_order >= 0),
    CONSTRAINT run_gates_release_definition_valid CHECK (
        release_definition IS NULL OR jsonb_typeof(release_definition) = 'object'
    ),
    CONSTRAINT run_gates_release_definition_digest_valid CHECK (
        release_definition_digest IS NULL
        OR release_definition_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT run_gates_release_observed_value_finite CHECK (
        release_observed_value IS NULL OR release_observed_value NOT IN (
            'NaN'::double precision,
            'Infinity'::double precision,
            '-Infinity'::double precision
        )
    ),
    CONSTRAINT run_gates_release_evidence_complete CHECK (
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
    CONSTRAINT run_gates_release_evidence_fresh CHECK (
        released_at IS NULL
        OR (
            released_at >= release_evaluated_at
            AND released_at < release_valid_until
        )
    )
);

CREATE INDEX run_gates_unreleased_repository
ON run_gates (repository_id, gate_name)
WHERE released_at IS NULL;

-- +goose Down
DROP TABLE run_gates;
DROP TABLE gate_observations;

ALTER TABLE runs
DROP CONSTRAINT runs_id_repository_unique;
