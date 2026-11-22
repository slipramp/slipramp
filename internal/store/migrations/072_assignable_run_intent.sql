-- +goose Up
-- Queued and assigned Runs can still be delivered to a Controller.
ALTER TABLE runs ADD CONSTRAINT runs_assignable_action_intent CHECK (
    phase NOT IN ('queued', 'assigned')
    OR (snapshot #>> '{action,Intent}' IN ('preview', 'reconcile', 'operation')) IS TRUE
);

-- +goose Down
ALTER TABLE runs DROP CONSTRAINT runs_assignable_action_intent;
