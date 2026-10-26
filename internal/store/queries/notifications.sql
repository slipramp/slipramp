-- name: CreateNotificationEvent :one
INSERT INTO notification_events (
    id,
    event_type,
    schema_version,
    subject_type,
    subject_id,
    occurred_at,
    payload_json,
    deduplication_key,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(event_type),
    sqlc.arg(schema_version),
    sqlc.arg(subject_type),
    sqlc.arg(subject_id),
    sqlc.arg(occurred_at),
    sqlc.arg(payload_json),
    sqlc.arg(deduplication_key),
    sqlc.arg(created_at)
)
ON CONFLICT (deduplication_key) DO NOTHING
RETURNING id;

-- name: GetNotificationEventByDeduplicationKey :one
SELECT *
FROM notification_events
WHERE deduplication_key = sqlc.arg(deduplication_key);

-- name: CreateNotificationDelivery :exec
INSERT INTO notification_deliveries (
    id,
    event_id,
    destination_name,
    destination_type,
    state,
    next_attempt_at,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(event_id),
    sqlc.arg(destination_name),
    sqlc.arg(destination_type),
    'pending',
    sqlc.arg(created_at),
    sqlc.arg(created_at)
);

-- name: GetEligibleNotificationDeliveryForUpdate :one
SELECT
    delivery.id,
    delivery.destination_name,
    delivery.destination_type,
    delivery.attempt_count,
    event.id AS event_id,
    event.event_type,
    event.schema_version,
    event.subject_type,
    event.subject_id,
    event.occurred_at,
    event.payload_json,
    event.deduplication_key,
    event.created_at AS event_created_at
FROM notification_deliveries AS delivery
JOIN notification_events AS event ON event.id = delivery.event_id
WHERE delivery.state = 'pending'
  AND delivery.next_attempt_at <= sqlc.arg(attempted_at)
ORDER BY delivery.created_at, delivery.id
FOR UPDATE OF delivery SKIP LOCKED
LIMIT 1;

-- name: ClaimNotificationDelivery :execrows
UPDATE notification_deliveries
SET
    attempt_count = attempt_count + 1,
    next_attempt_at = sqlc.arg(next_attempt_at),
    first_attempted_at = coalesce(first_attempted_at, sqlc.arg(attempted_at)),
    last_attempted_at = sqlc.arg(attempted_at)
WHERE id = sqlc.arg(id)
  AND state = 'pending'
  AND attempt_count = sqlc.arg(previous_attempt_count);

-- name: MarkNotificationDeliveryDelivered :execrows
UPDATE notification_deliveries
SET
    state = 'delivered',
    next_attempt_at = NULL,
    last_error = NULL,
    delivered_at = sqlc.arg(delivered_at)
WHERE id = sqlc.arg(id)
  AND state = 'pending'
  AND attempt_count = sqlc.arg(attempt_count);

-- name: RetryNotificationDelivery :execrows
UPDATE notification_deliveries
SET
    next_attempt_at = sqlc.arg(next_attempt_at),
    last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id)
  AND state = 'pending'
  AND attempt_count = sqlc.arg(attempt_count);

-- name: FailNotificationDelivery :execrows
UPDATE notification_deliveries
SET
    state = 'failed',
    next_attempt_at = NULL,
    last_error = sqlc.arg(last_error),
    failed_at = sqlc.arg(failed_at)
WHERE id = sqlc.arg(id)
  AND state = 'pending'
  AND attempt_count = sqlc.arg(attempt_count);

-- name: ListNotificationDeliveries :many
SELECT
    delivery.id,
    delivery.event_id,
    event.event_type,
    event.subject_type,
    event.subject_id,
    event.occurred_at,
    delivery.destination_name,
    delivery.destination_type,
    delivery.state,
    delivery.attempt_count,
    delivery.next_attempt_at,
    delivery.last_error,
    delivery.first_attempted_at,
    delivery.last_attempted_at,
    delivery.delivered_at,
    delivery.failed_at,
    delivery.created_at
FROM notification_deliveries AS delivery
JOIN notification_events AS event ON event.id = delivery.event_id
ORDER BY delivery.created_at DESC, delivery.id DESC
LIMIT sqlc.arg(result_limit) OFFSET sqlc.arg(result_offset);

-- name: CountNotificationDeliveries :one
SELECT count(*) FROM notification_deliveries;

-- name: CountNotificationDeliveryStates :many
SELECT state, count(*)::bigint AS delivery_count
FROM notification_deliveries
GROUP BY state
ORDER BY state;

-- name: DeleteTerminalNotificationEventsBefore :execrows
DELETE FROM notification_events AS event
WHERE EXISTS (
      SELECT 1
      FROM notification_deliveries AS delivery
      WHERE delivery.event_id = event.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM notification_deliveries AS delivery
      WHERE delivery.event_id = event.id
        AND delivery.state = 'pending'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM notification_deliveries AS delivery
      WHERE delivery.event_id = event.id
        AND coalesce(delivery.delivered_at, delivery.failed_at) >= sqlc.arg(terminal_before)
  );
