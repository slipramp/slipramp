-- +goose Up
CREATE TABLE placement_preflights (
    id uuid PRIMARY KEY,
    placement_name text NOT NULL,
    cluster_name text NOT NULL,
    placement_snapshot jsonb NOT NULL,
    state text NOT NULL,
    cleanup_state text NOT NULL,
    requested_by_principal_id uuid NOT NULL REFERENCES principals (id),
    requested_by_snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    last_dispatched_at timestamptz,
    job_uid text,
    reason text,
    message text,
    diagnostics jsonb NOT NULL DEFAULT '[]'::jsonb,
    CONSTRAINT placement_preflights_identity_valid CHECK (
        length(placement_name) BETWEEN 1 AND 63
        AND length(cluster_name) BETWEEN 1 AND 63
        AND jsonb_typeof(placement_snapshot) = 'object'
        AND placement_snapshot @> jsonb_build_object(
            'placementName', placement_name,
            'cluster', cluster_name
        )
        AND COALESCE(
            placement_snapshot->>'configurationChecksum' ~ '^[0-9a-f]{64}$',
            false
        )
        AND jsonb_typeof(requested_by_snapshot) = 'object'
        AND requested_by_snapshot @> jsonb_build_object(
            'principalId', requested_by_principal_id::text
        )
    ),
    CONSTRAINT placement_preflights_state_valid CHECK (
        state IN ('queued', 'running', 'succeeded', 'failed')
        AND cleanup_state IN ('pending', 'not-required', 'succeeded', 'failed')
        AND (started_at IS NULL OR started_at >= created_at)
        AND (finished_at IS NULL OR finished_at >= created_at)
        AND (last_dispatched_at IS NULL OR last_dispatched_at >= created_at)
        AND (job_uid IS NULL OR length(job_uid) BETWEEN 1 AND 200)
        AND (reason IS NULL OR length(reason) BETWEEN 1 AND 200)
        AND (message IS NULL OR length(message) BETWEEN 1 AND 4096)
        AND jsonb_typeof(diagnostics) = 'array'
        AND jsonb_array_length(diagnostics) <= 20
        AND (
            (state = 'queued' AND started_at IS NULL AND finished_at IS NULL AND cleanup_state = 'pending')
            OR (state = 'running' AND started_at IS NOT NULL AND finished_at IS NULL AND cleanup_state = 'pending')
            OR (state IN ('succeeded', 'failed') AND started_at IS NOT NULL AND finished_at IS NOT NULL)
        )
    )
);

CREATE UNIQUE INDEX placement_preflights_active_placement_idx
ON placement_preflights (placement_name)
WHERE state IN ('queued', 'running');

CREATE INDEX placement_preflights_dispatch_idx
ON placement_preflights (cluster_name, last_dispatched_at, created_at)
WHERE state IN ('queued', 'running');

CREATE INDEX placement_preflights_history_idx
ON placement_preflights (placement_name, created_at DESC, id DESC);

-- +goose Down
DROP TABLE placement_preflights;
