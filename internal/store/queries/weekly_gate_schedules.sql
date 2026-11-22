-- name: SetServerGateWindow :one
UPDATE server_gate_scopes SET window_version = window_version + 1,
    window_definition = sqlc.narg(definition), window_digest = sqlc.narg(digest),
    window_state = sqlc.narg(state), window_evaluated_at = sqlc.narg(evaluated_at),
    window_valid_until = sqlc.narg(valid_until), window_interval_start = sqlc.narg(interval_start),
    window_next_transition = sqlc.narg(next_transition)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: ObserveServerGateWindow :exec
UPDATE server_gate_scopes SET window_state = sqlc.arg(state), window_evaluated_at = sqlc.arg(evaluated_at),
    window_valid_until = sqlc.arg(valid_until), window_interval_start = sqlc.narg(interval_start),
    window_next_transition = sqlc.narg(next_transition)
WHERE id = sqlc.arg(id) AND window_version = sqlc.arg(version) AND window_definition IS NOT NULL;

-- name: InsertServerWindowEvent :exec
INSERT INTO server_gate_scope_events (scope_id, gate_type, version, action, actor_id, actor, recorded_at, window_definition, window_digest)
VALUES (sqlc.arg(scope_id), 'window', sqlc.arg(version), sqlc.arg(action), sqlc.arg(actor_id), sqlc.arg(actor),
    sqlc.arg(recorded_at), sqlc.narg(definition), sqlc.narg(digest));

-- name: ListServerWindowEvents :many
SELECT * FROM server_gate_scope_events WHERE scope_id = sqlc.arg(scope_id) AND gate_type = 'window'
ORDER BY version DESC LIMIT 20;

-- name: ListDueServerWindows :many
SELECT id, kind, target_id, stage_id FROM server_gate_scopes WHERE window_definition IS NOT NULL
AND (window_evaluated_at < sqlc.arg(started_at) OR window_evaluated_at > statement_timestamp()
    OR window_valid_until <= statement_timestamp())
ORDER BY window_valid_until, id LIMIT 64;

-- name: NextServerWindowCheck :one
SELECT statement_timestamp()::timestamptz AS checked_at,
    COALESCE(min(window_valid_until), statement_timestamp() + interval '1 minute')::timestamptz AS next_check
FROM server_gate_scopes WHERE window_definition IS NOT NULL;
