# Notifications and Inbox

This document defines outbound notification events and the operational Inbox.
They share domain semantics but have separate storage and lifecycle.

## Notification authority

Notifications report selected durable transitions without owning or changing
them. Delivery failure never changes a Run, Promotion, Repository, Cluster, or
other subject. Domain state remains the audit and recovery source of truth;
notification records exist only to operate outbound delivery.

A notification event is created at the durable transition it describes.
Polling, listing, rendering, and Server restart never recreate it. The immutable
versioned snapshot contains subject, transition time, channel-neutral
presentation, and an application-relative action path. Adapters do not reread
mutable subject state to enrich old messages.

Every event type needs explicit deduplication, recovery, and noise semantics.
Elapsed-time or derived-health events additionally need a durable episode
boundary before they can be added.

## Routing and persistence

Installation-wide Server configuration names provider-neutral destinations and
routes exact event types. Slack is the first adapter. Routes resolve once at
enqueue time and each delivery snapshots destination name and type, so later
configuration cannot redirect committed work.

The enqueue service validates the payload and resolves every destination before
writing. PostgreSQL stores one immutable event and one mutable delivery per
selected destination. Event deduplication key is unique;
`(event_id, destination_name)` is the delivery uniqueness boundary. Reusing a
key with identical content returns the existing event; conflicting content is
rejected. If no route matches, the domain transition proceeds without creating
notification records.

When a transactional service owns the subject transition, subject state, event,
and intended deliveries commit in one PostgreSQL transaction. A notification
write failure therefore rolls the transition back for ordinary domain recovery.

The current event contract is `review.requested` for a Run or Promotion. It
contains bounded title, summary, ordered facts, optional expiry, and a relative
link to the authenticated product. Messages cannot approve or reject work.

## Delivery

Delivery is at least once. PostgreSQL stores `pending`, `delivered`, or terminal
`failed` state, attempt count, retry time, bounded diagnostic, and operational
timestamps. A worker claims the oldest eligible delivery, advances durable
attempt metadata, then contacts the provider. Restart after provider acceptance
but before success persistence may duplicate a message but cannot lose it.

Timeouts, connection failures, rate limits, and retryable provider responses use
bounded backoff. Permanent rendering, configuration, and provider rejections
fail immediately. Provider bodies, raw transport errors, webhook URLs, and
secret paths are never persisted. Pending deliveries are not removed by history
retention.

Destinations use administrator-mounted secret files and an explicit public
Slipramp URL. Interactive provider decisions would require signed requests,
replay protection, provider-identity mapping to a Principal, and ordinary
product authorization; a shared webhook secret cannot act as a human.

## Review-request integration

When guarded Promotion preparation reaches `pending`, the same transaction may
insert its deduplicated review event and deliveries. Automatic approval,
preparation failure, decisions, expiry, and repeated reconciliation cannot emit
another request.

When a review-required Git delivery creates its queued Run, the same transaction
inserts the Run, links the delivery, records initial phase, and may enqueue its
review event. Bounded Git-delivery recovery may retry that transaction but never
retry the resulting infrastructure command.

The shared event shape does not create shared decision persistence. Runs and
Promotions continue to own their distinct review lifecycles.

## Operational Inbox

The Inbox is a generated current-state projection of pending decisions and
operational problems. It is not a notification queue, durable problem table, or
fallback health endpoint. Source domains own every condition and stable
occurrence identity.

Acknowledgement is stored per Principal and exact problem occurrence. It hides
that occurrence for the acknowledging Principal without changing source state,
notification delivery, or other Principals' views. A recovered condition ends
its episode; recurrence creates a new occurrence. Decisions cannot be
acknowledged.

Inbox reads use one evaluation time and complete role-aware projection. A
required source read failure produces an unavailable result and must never be
presented as **All clear**. Dependency health remains in readiness and
monitoring rather than the Inbox.

Resolving or expiring a subject removes its current Inbox item without
retracting delivered messages or erasing notification history. Conversely, an
Inbox condition cannot become an outbound event until it has a durable
transition and explicit delivery semantics.
