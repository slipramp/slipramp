-- +goose Up
ALTER TABLE promotions ADD COLUMN server_gate_release_evidence jsonb,
    ADD CONSTRAINT promotions_server_gate_release_shape CHECK (
        server_gate_release_evidence IS NULL OR (
            (callback_released OR (source = 'slipramp' AND NOT review_required AND dispatch_attempt_count > 0))
            AND (server_gate_release_evidence->>'checkedAt')::timestamptz IS NOT NULL
            AND jsonb_array_length(server_gate_release_evidence->'scopes') = 2
            AND server_gate_release_evidence #>> '{scopes,0,kind}' = 'global'
            AND server_gate_release_evidence #>> '{scopes,1,kind}' = 'stage'
        ) IS TRUE
    );

-- Old callback receipts and automatic dispatches remain authoritative after upgrade.
CREATE VIEW waiting_promotion_gate_subjects AS
SELECT id, stage_id FROM promotions
WHERE status IN ('creating', 'preparing', 'pending', 'approved')
  AND cancellation_requested_at IS NULL AND dispatch_failed_at IS NULL AND kargo_outcome_phase IS NULL
  AND NOT callback_released AND (source <> 'slipramp' OR review_required OR dispatch_attempt_count = 0);

-- +goose StatementBegin
CREATE FUNCTION current_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(scope_id uuid, kind text, target_id uuid, stage_id uuid, gate_type text, version bigint, passing boolean)
LANGUAGE sql STABLE AS $$
    SELECT id, kind, target_id, stage_id, 'lock'::text, lock_version, false FROM server_gate_scopes
    WHERE locked_at IS NOT NULL AND (lock_expires_at IS NULL OR lock_expires_at > checked_at)
    UNION ALL
    SELECT id, kind, target_id, stage_id, 'window', window_version,
        window_state = 'passing' AND window_evaluated_at <= checked_at AND checked_at < window_valid_until
    FROM server_gate_scopes WHERE window_definition IS NOT NULL;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION run_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(run_id uuid, scope_id uuid, gate_type text, version bigint, passing boolean, overridden boolean)
LANGUAGE sql STABLE AS $$
    SELECT r.id, c.scope_id, c.gate_type, c.version, c.passing,
        EXISTS (SELECT FROM run_server_gate_overrides o WHERE o.run_id = r.id
                AND (o.scope_id, o.gate_type, o.version) = (c.scope_id, c.gate_type, c.version))
    FROM runs r JOIN current_server_gate_checks(checked_at) c ON c.kind = 'global' OR c.target_id = r.target_id
    WHERE r.phase = 'queued' AND r.snapshot #>> '{action,Intent}' IN ('operation','reconcile');
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION promotion_server_gate_checks(checked_at timestamptz)
RETURNS TABLE(promotion_id uuid, scope_id uuid, gate_type text, version bigint, passing boolean, overridden boolean)
LANGUAGE sql STABLE AS $$
    SELECT p.id, c.scope_id, c.gate_type, c.version, c.passing,
        EXISTS (SELECT FROM promotion_server_gate_overrides o WHERE o.promotion_id = p.id
                AND (o.scope_id, o.gate_type, o.version) = (c.scope_id, c.gate_type, c.version))
    FROM waiting_promotion_gate_subjects p JOIN current_server_gate_checks(checked_at) c
        ON c.kind = 'global' OR c.stage_id = p.stage_id;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION promotion_server_gate_checks(timestamptz);
DROP VIEW waiting_promotion_gate_subjects;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION run_server_gate_checks(checked_at timestamptz)
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
DROP FUNCTION current_server_gate_checks(timestamptz);
ALTER TABLE promotions DROP COLUMN server_gate_release_evidence;
