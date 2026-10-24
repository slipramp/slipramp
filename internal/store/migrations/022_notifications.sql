-- +goose Up
CREATE TABLE notification_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    subject_type text NOT NULL,
    subject_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    payload_json jsonb NOT NULL,
    deduplication_key text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    CONSTRAINT notification_events_event_type_valid CHECK (event_type IN ('review.requested')),
    CONSTRAINT notification_events_schema_version_valid CHECK (schema_version = 1),
    CONSTRAINT notification_events_subject_type_valid CHECK (subject_type IN ('promotion')),
    CONSTRAINT notification_events_payload_valid CHECK (jsonb_typeof(payload_json) = 'object'),
    CONSTRAINT notification_events_deduplication_key_valid CHECK (
        length(deduplication_key) BETWEEN 1 AND 512
    )
);

-- +goose StatementBegin
CREATE FUNCTION reject_notification_event_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'notification events are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER notification_events_immutable
BEFORE UPDATE ON notification_events
FOR EACH ROW EXECUTE FUNCTION reject_notification_event_update();

CREATE TABLE notification_deliveries (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES notification_events (id) ON DELETE CASCADE,
    destination_name text NOT NULL,
    destination_type text NOT NULL,
    state text NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    last_error text,
    first_attempted_at timestamptz,
    last_attempted_at timestamptz,
    delivered_at timestamptz,
    failed_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (event_id, destination_name),
    CONSTRAINT notification_deliveries_destination_name_valid CHECK (
        destination_name ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT notification_deliveries_destination_type_valid CHECK (destination_type IN ('slack')),
    CONSTRAINT notification_deliveries_state_valid CHECK (state IN ('pending', 'delivered', 'failed')),
    CONSTRAINT notification_deliveries_attempt_count_valid CHECK (attempt_count >= 0),
    CONSTRAINT notification_deliveries_pending_schedule_valid CHECK (
        state <> 'pending' OR next_attempt_at IS NOT NULL
    ),
    CONSTRAINT notification_deliveries_last_error_valid CHECK (
        last_error IS NULL OR length(last_error) BETWEEN 1 AND 512
    ),
    CONSTRAINT notification_deliveries_terminal_state_valid CHECK (
        (state = 'pending' AND delivered_at IS NULL AND failed_at IS NULL)
        OR (state = 'delivered' AND delivered_at IS NOT NULL AND failed_at IS NULL)
        OR (state = 'failed' AND delivered_at IS NULL AND failed_at IS NOT NULL)
    ),
    CONSTRAINT notification_deliveries_attempt_times_valid CHECK (
        (attempt_count = 0 AND first_attempted_at IS NULL AND last_attempted_at IS NULL)
        OR (attempt_count > 0 AND first_attempted_at IS NOT NULL AND last_attempted_at IS NOT NULL)
    )
);

CREATE INDEX notification_deliveries_pending_idx
ON notification_deliveries (next_attempt_at, created_at, id)
WHERE state = 'pending';

CREATE INDEX notification_deliveries_history_idx
ON notification_deliveries (created_at DESC, id DESC);

-- +goose Down
DROP TABLE notification_deliveries;
DROP TRIGGER notification_events_immutable ON notification_events;
DROP FUNCTION reject_notification_event_update();
DROP TABLE notification_events;
