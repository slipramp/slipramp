# Notification Delivery Design

This unimplemented proposal defines the intended design for outbound Slipramp
notifications. It is not an implemented or stable external contract.

Notification delivery is provider-neutral, with Slack as the first adapter.
Notifications report durable domain transitions but never own or alter them.
A failed notification must not change a Promotion, Run, repository, cluster,
or other product resource.

Outbound notifications complement the operational Inbox; they do not implement
or replace it. The Inbox is a current, role-aware projection of decisions and
problems, while notification events are durable snapshots used to push selected
transitions to configured external destinations. Either surface remains useful
when the other is disabled or unavailable.

Delivery tracking and deferred choices live in the
[roadmap](./ROADMAP.md#notifications). Update [DESIGN.md](./DESIGN.md) as behavior
is implemented.

## Goals

- Notify operators when a durable product transition requires attention.
- Deliver newly pending Kargo Promotion reviews to Slack first.
- Reuse the same delivery path for future Run reviews and operational events.
- Persist delivery state, attempts, retry timing, and terminal failure.
- Keep provider credentials out of PostgreSQL and product event payloads.
- Preserve authenticated Slipramp as the boundary for decisions and sensitive
  content.
- Make notification failure visible without making it part of the originating
  resource's state machine.

## Non-goals

The first implementation does not include:

- a general event bus, workflow engine, or provider-plugin framework;
- a generic approval resource or a rewrite of Kargo Promotion state;
- Slack, email, or webhook actions that approve or reject a resource;
- arbitrary routing expressions or user-managed notification preferences;
- notification templates editable through the product;
- sensitive artifact contents, logs, diffs, credentials, or secret values in
  outbound messages;
- guaranteed exactly-once delivery to an external provider;
- notification success as a prerequisite for Promotion or Run progress.

## Accepted Design Decisions

### Events describe durable transitions

A notification event is created only after a real domain transition, such as a
Promotion moving to `pending`. Listing a resource, polling it, rendering the
Inbox, or restarting the Server does not recreate the event.

The event snapshots the channel-neutral content needed to explain the
transition. Delivery workers do not reconstruct old messages from mutable
current state. Each event has a stable deduplication key so concurrent or
repeated transition handling cannot enqueue the same notification twice.

When a transition is owned by a transactional domain service, the originating
state change, event, and intended delivery records are written in one PostgreSQL
transaction. This applies to the initial Promotion integration. A committed
transition therefore cannot be silently lost between the domain service and the
notification worker.

Later events based on elapsed time or derived health, such as an unreachable
Controller, need a durable detection and deduplication boundary before they are
introduced. Reading or rendering the Inbox is never that boundary and must not
have the side effect of creating notification events.

### Inbox and notifications share semantics, not storage

The Inbox derives what needs attention now from current domain state. It does
not require a matching notification route, event, or successful delivery. A
guarded Promotion therefore appears in **Needs a decision** while it is pending
even when outbound notifications are not configured.

When outbound delivery is enabled and routing selects a destination, the
corresponding `review.requested` event is emitted once when that Promotion enters
`pending`. With no matching route, no notification record is needed; the domain
state remains the durable record. Resolving or expiring the Promotion removes
the current Inbox decision but does not retract an already sent message or erase
delivery history. Conversely, viewing the Inbox does not enqueue a notification.
Decisions cannot be acknowledged, and acknowledging an Inbox problem only hides
that exact problem occurrence for that Principal; it never suppresses a
notification, changes delivery state, or retries delivery.

Future operational events must use the same episode boundary as the source
domain and the Inbox. For example, repeated failed Repository refresh attempts
within one failure episode remain one Inbox occurrence and may produce at most
one notification event. A successful refresh ends the episode; a later failure
starts a new Inbox occurrence and may emit a new event. The identifiers remain
purpose-specific: an Inbox problem ID is an opaque acknowledgement key, while a
notification deduplication key identifies one outbound transition.

The two taxonomies do not need to mirror each other. A point-in-time event such
as a future `review.resolved` or `run.failed` may be useful to push externally
without remaining a current Inbox item. An Inbox condition derived from elapsed
time must not emit a notification until it has the durable transition and noise
semantics described above. Each new event documents any corresponding Inbox
condition explicitly; there is no automatic "notify for every Inbox item" rule.

A terminal notification delivery failure is operational data, but the initial
slice exposes it through a dedicated delivery surface rather than adding a
notification-specific source or item kind to the Inbox. If operator experience
later shows that terminal failures belong in the Inbox, they must be added as a
server-side projection source with a stable failure-occurrence identity. An
Inbox acknowledgement would then remain per Principal and would not reset the
delivery. A future explicit retry would make the failure no longer current, and
exhausting that new retry cycle would create a new occurrence that could surface
again.

### Delivery is provider-neutral and at least once

Slack is one destination type behind a small delivery adapter boundary. Future
signed HTTP webhooks and email delivery may implement the same boundary without
changing Promotion, Run, or other domain services.

External delivery is at least once. A process can stop after a provider accepts
a message but before PostgreSQL records success, so a later retry may produce a
duplicate. The event ID, deduplication key, and delivery history make this
failure mode explicit. Slipramp does not claim exactly-once behavior from a
provider that does not offer an idempotent request boundary.

### Routing is small deployment configuration

Notification destinations and routes are installation configuration loaded by
the Slipramp Server. Routes initially match exact event types and name one or
more destinations. They do not implement a general predicate language.

Destination credentials use administrator-mounted files or environment-backed
secret sources. PostgreSQL stores the destination name and non-secret delivery
state, not webhook URLs, tokens, SMTP passwords, or signing secrets. The Server
continues to avoid Kubernetes API access.

An installation with enabled outbound delivery configures an explicit public
Slipramp web URL for message links. The Server does not infer this URL from an
incoming request Host header.

An illustrative configuration is:

```yaml
notifications:
  publicUrl: https://slipramp.example.com
  destinations:
    operations:
      type: slack
      webhookUrlFile: /var/run/secrets/slipramp/slack-operations-url
  routes:
    - events:
        - review.requested
      destinations:
        - operations
```

The implementation milestone selects the exact Server environment or mounted
file representation and corresponding Helm values. Configuration validation
rejects unknown destination types, missing secret files, duplicate destination
names, unknown route destinations, empty routes, and non-absolute public URLs.

### Review notification is shared without sharing approval state

Kargo Promotion review and a future Run review emit the same
`review.requested` notification shape. The event identifies its subject type
and stable ID, carries a channel-neutral presentation snapshot, and links to
the authenticated Slipramp UI.

This shared event does not require a generic Review table. A Kargo Promotion
continues to own its snapshotted policy, preparation Run, decision, expiry, and
state. A future reviewed Run may own separate approval state with different
creation and scheduling semantics. The concrete state machines remain
domain-specific until a second implemented approval flow demonstrates a useful
shared persistence model.

### External messages link back to Slipramp

The first Slack adapter sends informational messages with a link to Slipramp.
Approvals and rejections remain authenticated Slipramp operations with the
existing role, scope, conflict, and actor-attribution rules.

Interactive provider controls remain later work. Adding them would require
request-signature validation, replay protection, an explicit mapping from the
provider identity to a Slipramp principal, and the same authorization checks as
the product API. A shared webhook credential must never become the actor for a
human decision.

## Core Model

### Notification Event

A notification event is the immutable outbound presentation of one durable
transition. Its fields include:

- stable UUID;
- event type and schema version;
- subject type and stable subject ID;
- transition time;
- deduplication key;
- channel-neutral payload snapshot;
- creation time.

The initial event type is `review.requested`. Candidate later event types
include `review.resolved`, `run.failed`, `repository.refresh-failed`,
`cluster.unreachable`, and `run.observation-stale`. Each type is introduced
only with defined transition, deduplication, recovery, and noise-suppression
semantics.

### Notification Delivery

A notification delivery represents one event routed to one configured
destination. Its fields include:

- stable UUID;
- event reference;
- snapshotted destination name and type;
- state: `pending`, `delivered`, or `failed`;
- attempt count;
- next-attempt time;
- last non-sensitive error summary;
- first-attempt, last-attempt, and delivered timestamps;
- terminal failure time.

The uniqueness boundary is `(event_id, destination_name)`. Retry updates the
same delivery instead of inserting another logical delivery.

`failed` is terminal after the configured bounded attempt policy is exhausted
or the payload cannot be rendered for that destination. A failed delivery does
not change the event's subject. The terminal failure time is stable for that
failure occurrence. Operators can diagnose it from the product and may receive
an explicit retry operation in a later milestone.

### Destination

A destination is named deployment configuration. The persisted delivery
snapshots its name and type, while the worker resolves the current secret
material from Server configuration when sending.

Removing or changing destination configuration does not erase delivery
history. A pending delivery whose destination can no longer be resolved is
retried according to the bounded policy and eventually becomes failed with a
non-sensitive diagnostic.

### Route

A route selects exact event types and one or more destination names. Routes are
resolved when the domain transaction enqueues an event, so later configuration
changes do not silently redirect an already committed event.

Initial routing is installation-wide. Typed filters such as Kargo integration,
Repository, Target, or Cluster may be added only when a concrete deployment
requires them. Arbitrary expressions, labels, and user subscriptions remain
out of scope.

If no route selects a destination for a transition, no notification event or
delivery is persisted. Notification records exist to operate outbound delivery,
not to form a second general audit log of domain transitions.

## Event Contract

The initial channel-neutral payload is:

```json
{
  "type": "review.requested",
  "schemaVersion": 1,
  "subject": {
    "type": "promotion",
    "id": "b21eaf3e-dced-4a48-b57a-7a5fb4469cbd"
  },
  "title": "Promotion to production needs review",
  "summary": "external-dns - candidate to production",
  "facts": [
    {
      "label": "Repository",
      "value": "kcloud-ops"
    },
    {
      "label": "Expires",
      "value": "2026-10-24T18:00:00Z"
    }
  ],
  "action": {
    "label": "Review in Slipramp",
    "path": "/promotions/b21eaf3e-dced-4a48-b57a-7a5fb4469cbd"
  },
  "expiresAt": "2026-10-24T18:00:00Z"
}
```

`subject.type` is an enumerated contract. The first value is `promotion`; a
future Run review adds `run`. `action.path` is an application-relative path
joined to the configured public URL by the destination adapter. Event payloads
never accept an arbitrary external action URL from repository content.

Facts are ordered, bounded label/value pairs intended for human context. The
event producer supplies them from already-authorized domain state. Destination
adapters may omit unsupported presentation details but may not add sensitive
content by re-reading the subject.

## Persistence

Add one focused migration and group SQL in
`internal/store/queries/notifications.sql`. The intended tables are:

```text
notification_events
  id
  event_type
  schema_version
  subject_type
  subject_id
  occurred_at
  payload_json
  deduplication_key UNIQUE
  created_at

notification_deliveries
  id
  event_id
  destination_name
  destination_type
  state
  attempt_count
  next_attempt_at
  last_error
  first_attempted_at
  last_attempted_at
  delivered_at
  failed_at
  created_at
  UNIQUE (event_id, destination_name)
```

Domain data remains the audit and recovery source of truth. Notification
events and deliveries are operational delivery records, not replacements for
Promotion decisions, Run attribution, or a future administrative audit log.
Terminal notification records may therefore use a bounded retention policy
introduced with the implementation; pending records are never removed by
retention.

## Delivery and Retry

A background dispatcher selects eligible pending deliveries in deterministic
creation order. Claiming a delivery durably advances its attempt metadata and
next eligible time before contacting the provider, allowing a Server restart
to retry an ambiguous attempt without an in-memory lease.

The dispatcher uses bounded exponential backoff with jitter. Configuration
supplies a small maximum attempt count and maximum backoff. Permanent rendering
or configuration errors may fail immediately; timeouts, connection errors,
provider rate limits, and retryable provider responses remain pending until the
attempt policy is exhausted.

Provider response bodies are never persisted wholesale. Error summaries are
bounded and scrubbed of configured secret values before logging or storage.

The dispatcher does not block repository refresh, Run scheduling, Promotion
reconciliation, or controller polling. One failing destination does not block
deliveries to another destination.

## Kargo Promotion Integration

When a guarded Kargo Promotion preparation succeeds, its artifacts validate,
and configured routing selects at least one destination, the same transaction
that moves the Promotion to `pending` enqueues one `review.requested` event and
its configured deliveries. With no matching destination, only the Promotion
transition is persisted.

The deduplication key is derived from the stable Promotion ID and transition,
for example `promotion:<id>:review-requested`. Repeated Kargo polls,
preparation reconciliation, and Server restart recovery therefore preserve one
logical notification event.

Automatic Promotions do not emit `review.requested` because they never require
a human decision. Preparation failures continue to move the Promotion to
`error`; notification of such operational failures is a separate future event
with its own routing and noise semantics.

Notification delivery success or failure never changes the Promotion status,
decision, expiry, or Kargo machine response.

## Future Run Approval Integration

Run approval remains a separate product-design milestone. The notification
design assumes the following boundaries without implementing them:

- A Run is created with its immutable catalog revision, commit, definitions,
  parameters, placement, source, and actor snapshot.
- Approval state is stored separately from the Run lifecycle phase and terminal
  outcome.
- A Run waiting for approval remains `queued`; the scheduler excludes it from
  eligible assignments until approval is satisfied.
- Rejection or expiry finishes the undispatched Run as canceled with a stable
  reason such as `ReviewRejected` or `ReviewExpired`.
- The transition into pending approval emits `review.requested` with subject
  type `run` and an authenticated `/runs/{runId}` action path.
- Direct Runs remain valid by default. Required review is an explicitly
  configured policy rather than a universal gate.

Do not add a separate invocation or request domain resource solely to support
approval. Do not add `pending-approval` to the execution phase enum. Review is
an orthogonal control fact, like cancellation intent, while the Run remains the
immutable execution resource.

## Authorization and Security

Reading notification delivery status follows ordinary product read permission.
Only non-sensitive destination names, states, timestamps, attempt counts, and
bounded error summaries are exposed. Secret paths, URLs, tokens, and provider
response bodies are not returned.

Slack messages contain operational metadata already visible to the intended
operator audience, but no sensitive artifact body. The linked Slipramp page
still requires OIDC or API-key authentication and applies the ordinary role and
scope checks. Sensitive Promotion diffs remain available only through the
existing authorized artifact path.

A later generic webhook destination must sign its stable event envelope and
include replay-resistant request metadata. It must not reuse controller,
artifact, product-session, or Kargo submission credentials.

## API and Web Application

OpenAPI remains the source of truth for every product endpoint. The initial
notification surface should expose bounded delivery history or detail so
operators can inspect attempt timing without reading Server logs.

The homepage continues to read one complete `/api/v1/inbox` projection. The Web
must not join a notification-delivery endpoint into the Inbox or gain a fallback
composition path. The initial notification work does not add a new Inbox source
or item kind. If terminal failures are added to the Inbox later, the Server must
extend the complete projection and treat failure data as required; a source
error cannot be presented as **All clear**.

The first UI slice does not require a template editor, routing editor, or
destination-secret management. Configuration remains deployment-owned. The UI
for delivery inspection shows the event type, subject link, destination name,
state, attempts, last attempt, and redacted failure summary. Transient Web
action toasts are local interaction feedback and are unrelated to this outbound
delivery subsystem.

An explicit operator retry endpoint may be added after automatic retry behavior
is exercised. If added, it returns the same delivery to `pending` under an
explicit retry policy; it never inserts a new event or delivery and never
replays or changes the originating domain transition. A new `failed_at` value is
recorded only if that retry cycle also terminates in failure.

## Required Tests

The implementation is not complete without focused coverage of:

- event deduplication under concurrent transition handling;
- atomic domain transition, event, and delivery insertion;
- exact route resolution and duplicate destination rejection;
- missing or changed destination configuration;
- Slack success, timeout, retryable failure, rate limiting, and permanent
  failure;
- Server restart before send, during an attempt, and after provider acceptance;
- one failing destination not blocking another;
- bounded attempt count and backoff;
- payload size and fact-count limits;
- secret and provider-response redaction;
- relative action-path validation and explicit public URL joining;
- authorization and redaction on delivery-list responses;
- Kargo automatic approval producing no review request;
- one Kargo pending transition producing one event across repeated polls and
  reconciliation;
- notification failure leaving Promotion state unchanged;
- no matching route leaving no notification record while the pending
  Promotion still appears in the Inbox;
- Inbox reads and acknowledgements never enqueue, suppress, or retry delivery;
- when a later operational event corresponds to an Inbox condition, one source
  failure episode producing at most one notification event and one stable Inbox
  problem occurrence;
- terminal delivery failure remaining visible through the dedicated delivery
  surface without changing the Inbox contract;
- OpenAPI, SQL, Helm schema, and generated-output freshness.
