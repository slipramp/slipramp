-- +goose Up
ALTER TABLE server_gate_overrides
    DROP CONSTRAINT server_gate_overrides_reason_check,
    ADD CONSTRAINT server_gate_overrides_reason_check
        CHECK (length(reason) <= 1024 AND (reason = '' OR btrim(reason) <> ''));

-- +goose Down
ALTER TABLE server_gate_overrides
    DROP CONSTRAINT server_gate_overrides_reason_check,
    ADD CONSTRAINT server_gate_overrides_reason_check
        CHECK (length(reason) BETWEEN 1 AND 1024 AND btrim(reason) <> '');
