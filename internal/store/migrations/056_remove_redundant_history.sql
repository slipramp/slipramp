-- +goose Up
DROP TABLE run_phase_transitions;
DROP TABLE catalog_validation_failures;

-- +goose Down
CREATE TABLE catalog_validation_failures (
    repository_id uuid NOT NULL REFERENCES repositories (id),
    commit_sha text NOT NULL,
    recorded_at timestamptz NOT NULL,
    failure_reason text NOT NULL,
    PRIMARY KEY (repository_id, commit_sha),
    CONSTRAINT catalog_validation_failures_commit_sha_valid CHECK (
        commit_sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT catalog_validation_failures_reason_not_empty CHECK (
        length(failure_reason) > 0
    )
);

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
