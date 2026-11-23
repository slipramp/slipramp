-- +goose Up
ALTER TABLE server_gate_scopes
    DROP CONSTRAINT server_gate_scopes_lock_reason_check,
    ADD CONSTRAINT server_gate_scopes_lock_reason_check
        CHECK (length(lock_reason) <= 1024 AND (lock_reason = '' OR btrim(lock_reason) <> ''));
ALTER TABLE server_gate_scope_events
    DROP CONSTRAINT server_gate_scope_events_reason_check,
    ADD CONSTRAINT server_gate_scope_events_reason_check
        CHECK (length(reason) <= 1024 AND (reason = '' OR btrim(reason) <> ''));

-- +goose Down
ALTER TABLE server_gate_scopes
    DROP CONSTRAINT server_gate_scopes_lock_reason_check,
    ADD CONSTRAINT server_gate_scopes_lock_reason_check
        CHECK (length(lock_reason) BETWEEN 1 AND 1024 AND btrim(lock_reason) <> '');
ALTER TABLE server_gate_scope_events
    DROP CONSTRAINT server_gate_scope_events_reason_check,
    ADD CONSTRAINT server_gate_scope_events_reason_check
        CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> '');
