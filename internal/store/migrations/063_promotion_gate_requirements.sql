-- +goose Up
CREATE TABLE promotion_gates (
    promotion_id uuid NOT NULL REFERENCES promotions (id),
    repository_id uuid NOT NULL REFERENCES repositories (id),
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
    PRIMARY KEY (promotion_id, gate_name),
    UNIQUE (promotion_id, attachment_order),
    CONSTRAINT promotion_gates_release_catalog_revision_fk FOREIGN KEY (
        release_catalog_revision_id,
        repository_id
    ) REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT promotion_gates_gate_name_valid CHECK (
        length(gate_name) BETWEEN 1 AND 63
        AND gate_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
    ),
    CONSTRAINT promotion_gates_attachment_order_valid CHECK (attachment_order >= 0),
    CONSTRAINT promotion_gates_release_definition_valid CHECK (
        release_definition IS NULL OR jsonb_typeof(release_definition) = 'object'
    ),
    CONSTRAINT promotion_gates_release_definition_digest_valid CHECK (
        release_definition_digest IS NULL
        OR release_definition_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT promotion_gates_release_observed_value_finite CHECK (
        release_observed_value IS NULL OR release_observed_value NOT IN (
            'NaN'::double precision,
            'Infinity'::double precision,
            '-Infinity'::double precision
        )
    ),
    CONSTRAINT promotion_gates_release_evidence_complete CHECK (
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
    CONSTRAINT promotion_gates_release_evidence_fresh CHECK (
        released_at IS NULL
        OR (
            released_at >= release_evaluated_at
            AND released_at < release_valid_until
        )
    )
);

CREATE INDEX promotion_gates_unreleased_repository
ON promotion_gates (repository_id, gate_name)
WHERE released_at IS NULL;

-- +goose Down
DROP TABLE promotion_gates;
