-- +goose Up
CREATE TABLE runs (
    id uuid PRIMARY KEY,
    phase text NOT NULL,
    outcome text,
    repository_id uuid NOT NULL REFERENCES repositories (id),
    catalog_revision_id uuid NOT NULL,
    commit_sha text NOT NULL,
    target_name text NOT NULL,
    action_name text NOT NULL,
    parameters jsonb NOT NULL,
    placement_name text NOT NULL,
    cluster_name text NOT NULL,
    source jsonb NOT NULL,
    idempotency_key text,
    request_digest text NOT NULL,
    snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    assigned_at timestamptz,
    preparing_at timestamptz,
    started_at timestamptz,
    finished_at timestamptz,
    last_observed_at timestamptz,
    cancellation_requested_at timestamptz,
    job_uid text,
    outcome_reason text,
    outcome_message text,
    exit_code integer,
    CONSTRAINT runs_catalog_revision_fk FOREIGN KEY (
        catalog_revision_id,
        repository_id
    ) REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT runs_phase_valid CHECK (
        phase IN (
            'queued',
            'assigned',
            'preparing',
            'running',
            'finished'
        )
    ),
    CONSTRAINT runs_outcome_valid CHECK (
        outcome IS NULL OR outcome IN (
            'succeeded',
            'failed',
            'canceled',
            'timed-out'
        )
    ),
    CONSTRAINT runs_finished_outcome_consistent CHECK (
        (phase = 'finished') = (outcome IS NOT NULL)
    ),
    CONSTRAINT runs_exit_code_valid CHECK (
        exit_code IS NULL OR exit_code >= 0
    ),
    CONSTRAINT runs_commit_sha_valid CHECK (
        commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT runs_target_name_not_empty CHECK (length(target_name) > 0),
    CONSTRAINT runs_action_name_not_empty CHECK (length(action_name) > 0),
    CONSTRAINT runs_parameters_object CHECK (jsonb_typeof(parameters) = 'object'),
    CONSTRAINT runs_placement_name_not_empty CHECK (length(placement_name) > 0),
    CONSTRAINT runs_cluster_name_not_empty CHECK (length(cluster_name) > 0),
    CONSTRAINT runs_source_object CHECK (jsonb_typeof(source) = 'object'),
    CONSTRAINT runs_source_valid CHECK ((
        (
            source->>'type' IN ('manual', 'api')
            AND source = jsonb_build_object('type', source->>'type')
        )
        OR (
            source->>'type' = 'git'
            AND length(source->'git'->>'triggerName') > 0
            AND source->'git'->>'deliveryId' ~
                '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            AND idempotency_key = source->'git'->>'deliveryId'
            AND source = jsonb_build_object(
                'type',
                'git',
                'git',
                jsonb_build_object(
                    'triggerName',
                    source->'git'->>'triggerName',
                    'deliveryId',
                    source->'git'->>'deliveryId'
                )
            )
        )
        OR (
            source->>'type' = 'promotion'
            AND source->'promotion'->>'promotionId' ~
                '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            AND idempotency_key = source->'promotion'->>'promotionId'
            AND source = jsonb_build_object(
                'type',
                'promotion',
                'promotion',
                jsonb_build_object(
                    'promotionId',
                    source->'promotion'->>'promotionId'
                )
            )
        )
    ) IS TRUE),
    CONSTRAINT runs_idempotency_key_valid CHECK (
        idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 128
    ),
    CONSTRAINT runs_request_digest_valid CHECK (
        request_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT runs_snapshot_object CHECK (jsonb_typeof(snapshot) = 'object')
);

CREATE UNIQUE INDEX runs_source_idempotency_key
ON runs ((source->>'type'), idempotency_key)
WHERE idempotency_key IS NOT NULL;

CREATE INDEX runs_repository_created_at
ON runs (repository_id, created_at DESC);

CREATE INDEX runs_target_created_at
ON runs (repository_id, target_name, created_at DESC);

CREATE TABLE run_phase_transitions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES runs (id),
    phase text NOT NULL,
    outcome text,
    reason text,
    message text,
    exit_code integer,
    recorded_at timestamptz NOT NULL,
    CONSTRAINT run_phase_transitions_phase_valid CHECK (
        phase IN (
            'queued',
            'assigned',
            'preparing',
            'running',
            'finished'
        )
    ),
    CONSTRAINT run_phase_transitions_outcome_valid CHECK (
        outcome IS NULL OR outcome IN (
            'succeeded',
            'failed',
            'canceled',
            'timed-out'
        )
    ),
    CONSTRAINT run_phase_transitions_finished_outcome_consistent CHECK (
        (phase = 'finished') = (outcome IS NOT NULL)
    ),
    CONSTRAINT run_phase_transitions_exit_code_valid CHECK (
        exit_code IS NULL OR exit_code >= 0
    )
);

CREATE INDEX run_phase_transitions_run_recorded_at
ON run_phase_transitions (run_id, recorded_at, id);

-- +goose Down
DROP TABLE run_phase_transitions;
DROP TABLE runs;
