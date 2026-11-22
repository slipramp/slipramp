-- +goose Up
-- Historical assignments keep their original evidence. A new mutating assignment
-- records both scopes even when neither contains an active policy.
ALTER TABLE runs ADD COLUMN server_gate_release_evidence jsonb,
    ADD CONSTRAINT runs_server_gate_release_shape CHECK (
        server_gate_release_evidence IS NULL OR (
            assigned_at IS NOT NULL
            AND snapshot #>> '{action,Intent}' IN ('operation','reconcile')
            AND (server_gate_release_evidence->>'checkedAt')::timestamptz = assigned_at
            AND jsonb_typeof(server_gate_release_evidence->'scopes') = 'array'
            AND jsonb_array_length(server_gate_release_evidence->'scopes') = 2
            AND server_gate_release_evidence #>> '{scopes,0,kind}' = 'global'
            AND server_gate_release_evidence #>> '{scopes,1,kind}' = 'target'
        ) IS TRUE
    );

-- +goose StatementBegin
CREATE FUNCTION run_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(run_id uuid, scope_id uuid, gate_type text, version bigint, passing boolean, overridden boolean)
LANGUAGE sql STABLE AS $$
    SELECT r.id, s.id, policy.kind, policy.version, policy.passing,
        EXISTS (SELECT FROM run_server_gate_overrides o
                WHERE o.run_id = r.id AND o.scope_id = s.id AND o.gate_type = policy.kind AND o.version = policy.version)
    FROM server_gate_scopes s
    CROSS JOIN LATERAL (
        SELECT 'lock'::text AS kind, s.lock_version AS version, false AS passing
        WHERE s.locked_at IS NOT NULL AND (s.lock_expires_at IS NULL OR s.lock_expires_at > checked_at)
        UNION ALL
        SELECT 'window', s.window_version,
            s.window_state = 'passing' AND s.window_evaluated_at <= checked_at AND s.window_valid_until > checked_at
        WHERE s.window_definition IS NOT NULL
    ) policy
    JOIN runs r ON (s.kind = 'global' OR s.target_id = r.target_id)
    WHERE r.phase = 'queued' AND r.snapshot #>> '{action,Intent}' IN ('operation','reconcile');
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION run_server_gate_checks(timestamptz);
ALTER TABLE runs DROP COLUMN server_gate_release_evidence;
