-- name: InsertServerGateOverride :exec
INSERT INTO server_gate_overrides (subject_type, subject_id, scope_id, gate_type, version,
    actor_id, actor, reason, recorded_at, observed_state, window_observation)
VALUES (sqlc.arg(subject_type), sqlc.arg(subject_id), sqlc.arg(scope_id), sqlc.arg(gate_type), sqlc.arg(version),
    sqlc.arg(actor_id), sqlc.arg(actor), sqlc.arg(reason), sqlc.arg(recorded_at),
    sqlc.arg(observed_state), sqlc.narg(window_observation));

-- name: GetGateOverrideSubject :one
SELECT 'target'::text AS kind, target_id AS owner_id, statement_timestamp()::timestamptz AS checked_at,
    (phase = 'queued' AND cancellation_requested_at IS NULL)::boolean AS waiting,
    coalesce(snapshot #>> '{action,Intent}' IN ('reconcile', 'operation'), false)::boolean AS uses_server_gates
FROM runs WHERE sqlc.arg(subject_type)::text = 'run' AND runs.id = sqlc.arg(subject_id)
UNION ALL
SELECT 'stage', stage_id, statement_timestamp(),
    EXISTS (SELECT FROM waiting_promotion_gate_subjects w WHERE w.id = promotions.id),
    true
FROM promotions WHERE sqlc.arg(subject_type)::text = 'promotion' AND promotions.id = sqlc.arg(subject_id);

-- name: ListApplicableServerGateScopes :many
SELECT * FROM server_gate_scopes
WHERE kind = 'global' OR (kind = sqlc.arg(kind)
    AND (target_id = sqlc.arg(owner_id)::uuid OR stage_id = sqlc.arg(owner_id)::uuid))
ORDER BY (kind <> 'global');

-- name: ListSubjectServerGateOverrides :many
SELECT o.subject_id, o.scope_id, o.gate_type, o.version, o.observed_state, o.window_observation,
    e.window_digest, coalesce(e.window_definition->>'timeZone', '')::text AS time_zone,
    o.actor_id, o.actor, o.reason, o.recorded_at,
    coalesce(e.reason,'')::text AS lock_reason, e.actor_id AS lock_actor_id, e.actor AS lock_actor,
    e.recorded_at AS locked_at, e.expires_at
FROM server_gate_overrides o
JOIN server_gate_scope_events e USING (scope_id, gate_type, version)
WHERE o.subject_type = sqlc.arg(subject_type) AND o.subject_id = ANY(sqlc.arg(subject_ids)::uuid[])
ORDER BY o.subject_id, o.scope_id, o.gate_type, o.version;
