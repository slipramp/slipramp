-- +goose Up
ALTER TABLE notification_events
DROP CONSTRAINT notification_events_subject_type_valid,
ADD CONSTRAINT notification_events_subject_type_valid CHECK (
    subject_type IN ('promotion', 'run')
);

-- +goose Down
DELETE FROM notification_events
WHERE subject_type = 'run';

ALTER TABLE notification_events
DROP CONSTRAINT notification_events_subject_type_valid,
ADD CONSTRAINT notification_events_subject_type_valid CHECK (
    subject_type IN ('promotion')
);
