-- name: GetServerGateScope :one
SELECT sqlc.embed(server_gate_scopes), statement_timestamp()::timestamptz AS checked_at
FROM server_gate_scopes
WHERE kind = sqlc.arg(kind)
  AND (kind = 'global' OR target_id = sqlc.arg(owner_id)::uuid OR stage_id = sqlc.arg(owner_id)::uuid);

-- name: LockServerGateScopes :many
-- Every writer takes the global row before the local scope.
SELECT * FROM server_gate_scopes
WHERE kind = 'global' OR (kind = sqlc.arg(kind)
    AND (target_id = sqlc.arg(owner_id)::uuid OR stage_id = sqlc.arg(owner_id)::uuid))
ORDER BY (kind <> 'global')
FOR NO KEY UPDATE;

-- name: ServerGateCheckTime :one
SELECT clock_timestamp()::timestamptz;

-- name: UpdateServerGateLock :one
UPDATE server_gate_scopes
SET lock_version = lock_version + 1,
    lock_reason = sqlc.narg(reason),
    lock_actor_id = sqlc.narg(actor_id),
    lock_actor = sqlc.narg(actor),
    locked_at = sqlc.narg(locked_at),
    lock_expires_at = sqlc.narg(expires_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertServerGateLockEvent :exec
INSERT INTO server_gate_scope_events (scope_id, gate_type, version, action, actor_id, actor, reason, recorded_at, expires_at)
VALUES (sqlc.arg(scope_id), 'lock', sqlc.arg(lock_version), sqlc.arg(action), sqlc.arg(actor_id), sqlc.arg(actor),
    sqlc.arg(reason), sqlc.arg(recorded_at), sqlc.narg(expires_at));

-- name: ListServerGateScopeEvents :many
SELECT * FROM server_gate_scope_events WHERE scope_id = sqlc.arg(scope_id)
AND gate_type = 'lock' ORDER BY version DESC LIMIT 20;
