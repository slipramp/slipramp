-- +goose Up
CREATE TABLE runs (
    id uuid PRIMARY KEY,
    state text NOT NULL,
    repository_id uuid NOT NULL REFERENCES repositories (id),
    catalog_revision_id uuid NOT NULL,
    commit_sha text NOT NULL,
    target_name text NOT NULL,
    action_name text NOT NULL,
    parameters jsonb NOT NULL,
    placement_name text NOT NULL,
    cluster_name text NOT NULL,
    source_type text NOT NULL,
    idempotency_key text,
    request_digest text NOT NULL,
    snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT runs_catalog_revision_fk FOREIGN KEY (
        catalog_revision_id,
        repository_id
    ) REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT runs_state_valid CHECK (
        state IN (
            'queued',
            'preparing',
            'running',
            'succeeded',
            'failed',
            'canceling',
            'canceled',
            'timed-out',
            'unknown'
        )
    ),
    CONSTRAINT runs_commit_sha_valid CHECK (
        commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT runs_target_name_not_empty CHECK (length(target_name) > 0),
    CONSTRAINT runs_action_name_not_empty CHECK (length(action_name) > 0),
    CONSTRAINT runs_parameters_object CHECK (jsonb_typeof(parameters) = 'object'),
    CONSTRAINT runs_placement_name_not_empty CHECK (length(placement_name) > 0),
    CONSTRAINT runs_cluster_name_not_empty CHECK (length(cluster_name) > 0),
    CONSTRAINT runs_source_type_valid CHECK (source_type IN ('manual', 'api')),
    CONSTRAINT runs_idempotency_key_valid CHECK (
        idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 128
    ),
    CONSTRAINT runs_request_digest_valid CHECK (
        request_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT runs_snapshot_object CHECK (jsonb_typeof(snapshot) = 'object')
);

CREATE UNIQUE INDEX runs_source_idempotency_key
ON runs (source_type, idempotency_key)
WHERE idempotency_key IS NOT NULL;

CREATE INDEX runs_repository_created_at
ON runs (repository_id, created_at DESC);

CREATE INDEX runs_target_created_at
ON runs (repository_id, target_name, created_at DESC);

CREATE TABLE run_state_transitions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES runs (id),
    state text NOT NULL,
    recorded_at timestamptz NOT NULL,
    CONSTRAINT run_state_transitions_state_valid CHECK (
        state IN (
            'queued',
            'preparing',
            'running',
            'succeeded',
            'failed',
            'canceling',
            'canceled',
            'timed-out',
            'unknown'
        )
    )
);

CREATE INDEX run_state_transitions_run_recorded_at
ON run_state_transitions (run_id, recorded_at, id);

-- +goose Down
DROP TABLE run_state_transitions;
DROP TABLE runs;
