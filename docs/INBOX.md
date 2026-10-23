# Operational Inbox Design

This unimplemented proposal defines the focused design for moving the
operational Inbox behind a dedicated Slipramp Server API and adding durable,
per-principal problem acknowledgements.

The implemented architecture is described in [DESIGN.md](./DESIGN.md). The
Inbox already exists as the web homepage, but the web process currently composes
it from repository, cluster, Run, Git-trigger failure, Promotion, readiness, and
identity APIs. This design replaces that browser-facing composition with one
authenticated Server read model. It does not turn Inbox problems into a second
source of truth for the underlying domains.

Delivery tracking and deferred choices live in the
[roadmap](./ROADMAP.md#operational-inbox). Update [DESIGN.md](./DESIGN.md) as
behavior is implemented.

## Goals

- Expose the complete role-aware operational Inbox through one generated API.
- Keep pending decisions separate from operational problems.
- Let operators and administrators acknowledge one or more current problems.
- Hide acknowledged problems only for the principal who acknowledged them.
- Make a later occurrence of the same logical problem visible again.
- Make **Acknowledge all** apply to the exact problems shown to the operator,
  without suppressing problems that arrive concurrently.
- Preserve the existing grouping of stale Runs beneath an unreachable
  Cluster.
- Remove the web route's domain-specific pagination limits and partial client
  composition.
- Keep OpenAPI as the source of truth and PostgreSQL authoritative for
  acknowledgement state.

## Non-goals

The first acknowledgement slice does not add:

- installation-wide or team-wide acknowledgement;
- assignment, ownership, comments, escalation, or incident timelines;
- acknowledgement of pending Promotion decisions;
- notification delivery, unread badges, or external paging integrations;
- configurable severity, suppression rules, or time-based snoozing;
- a durable table containing every derived Inbox problem;
- acknowledgement history after a problem is no longer current;
- a general audit-event framework;
- an unacknowledge or historical **Acknowledged** view;
- pagination for the operational Inbox;
- a compatibility layer for the existing browser-composed Inbox shape.

The API and web application may add an explicit unacknowledge operation later
if operators demonstrate that they need recovery beyond waiting for a new
problem occurrence.

## Accepted Design Decisions

### The Server owns the Inbox projection

Add an `internal/inbox` package that composes current decisions, current
problems, Run activity, authorization capabilities, and acknowledgement state.
The web application consumes that projection instead of reconstructing product
semantics from several paginated APIs.

The Inbox is a read model over authoritative Repository, Cluster, Run,
Git-trigger delivery, Promotion, readiness, principal, and acknowledgement state. It does not own or
copy the lifecycle of those resources. Fixing the underlying condition removes
the problem naturally on the next read.

The service evaluates all time-sensitive state against one UTC `generatedAt`
value captured at the beginning of the request. Cluster connectivity, Run
observation freshness, Promotion expiry, dependency state, sorting, and
occurrence identity must not each use unrelated clock reads.

### Problems are derived; acknowledgements are durable

Do not create a durable `inbox_problems` table. A repository refresh failure,
unreachable Cluster, stale Run observation, Promotion error, or unavailable
dependency remains owned by its existing domain.

Persist only the fact that a principal acknowledged one exact derived problem
occurrence. This avoids lifecycle synchronization between an Inbox table and
the authoritative domain records.

### Acknowledgement is personal

An acknowledgement belongs to one durable product principal. It follows that
principal across browser sessions and API keys, but it does not hide the
problem from another operator. A disabled or renamed principal does not alter
another principal's Inbox.

Viewers may read the Inbox but cannot acknowledge problems. Operators and
administrators may acknowledge. A principal that is later downgraded to viewer
retains its existing acknowledgement filtering but cannot create new
acknowledgements.

### Acknowledgement applies to an occurrence, not a resource forever

Every problem returned by the Server has a deterministic, opaque `id` for its
current occurrence. The ID is derived from a versioned canonical tuple:

```text
problem kind + logical resource identity + occurrence marker
```

The implementation should hash the canonical tuple with SHA-256 and expose the
lowercase hexadecimal digest. Include a private version prefix in the hashed
input so the canonical form can change deliberately while the API continues to
treat the result as opaque.

The initial occurrence markers are:

| Problem kind             | Logical resource | Occurrence marker                                     |
| ------------------------ | ---------------- | ----------------------------------------------------- |
| `repository-refresh`     | Repository ID    | failed refresh `attemptedAt`                          |
| `cluster-unreachable`    | Cluster name     | Controller `lastSeenAt`                               |
| `run-observation`        | Run ID           | `lastObservedAt`, then `assignedAt`, then `createdAt` |
| `promotion-error`        | Promotion ID     | error transition `updatedAt`                          |
| `git-trigger-failure`    | delivery ID      | terminal delivery `failedAt`                          |
| `dependency-unavailable` | dependency name  | first observed unavailable time                       |

All timestamps use normalized UTC RFC 3339 with nanosecond precision in the
canonical input. Tests must fix the complete byte representation, not only the
resulting user-visible behavior.

A later failed refresh, a Cluster that reconnects and becomes unreachable
again, or a Run that becomes current and later stale therefore receives a new
ID and reappears. Changes to presentation-only data, such as the number of
Runs affected by the same Cluster outage, do not create a new occurrence.

### Decisions cannot be acknowledged

Pending Promotion reviews remain decisions. They leave the Inbox only through
their existing Promotion lifecycle. The acknowledgement endpoint accepts only
IDs that the Server currently derives as problems; a decision ID can never be
persisted as an acknowledgement.

### Acknowledge all is an exact batch

The web application implements **Acknowledge all** by posting the IDs from the
currently rendered Problems section. The API does not provide a bodyless
"acknowledge whatever is current now" operation.

This gives the operation snapshot semantics without creating a durable Inbox
snapshot:

- problems visible to the operator are acknowledged together;
- a problem that appears after the page load is absent from the request and
  remains visible;
- a problem that resolves before the request is ignored;
- a problem whose occurrence changes before the request is not suppressed;
- repeating the same request is idempotent.

The individual button uses the same batch endpoint with one ID.

### The API never returns a knowingly partial Inbox

The Server may use several domain reads to build an Inbox, and those reads are
not a cross-domain serializable transaction. However, if any required source
cannot be read, `GET /api/v1/inbox` fails rather than returning an incomplete
list that could be mistaken for **All clear** or acknowledged in bulk.

The service reads all relevant current records. It must not silently inherit
the public list endpoints' pagination limits. The first API intentionally has
no pagination because the actionable set is expected to stay small for the
supported installation size. Add pagination later only with explicit bulk
acknowledgement semantics.

### Readiness keeps a separate failure path

A successful authenticated Inbox request proves PostgreSQL was available for
authentication and the projection read. A PostgreSQL outage therefore cannot
be represented as a normal acknowledgeable Inbox problem: the acknowledgement
store is unavailable at the same time.

The web loader continues to call unauthenticated `/readyz` alongside the Inbox
request. If the Inbox request fails, the page shows a critical,
non-acknowledgeable availability state and must not render **All clear** or an
acknowledgement action.

Artifact-storage unavailability can be a normal Inbox problem while
PostgreSQL remains available. Extend the readiness service with a
concurrency-safe observed-transition tracker that supplies `unavailableSince`
internally.
An available observation clears the tracked incident; the next unavailable
observation starts a new occurrence. A Server restart starts a new observed
occurrence and intentionally resurfaces a continuing dependency outage. This
safety bias is preferable to permanently suppressing an outage across an
unobserved process lifetime.

## User Experience

The homepage retains its two sections:

1. **Needs a decision** contains pending Promotion reviews the authenticated
   credential is authorized to decide.
2. **Problems** contains only unacknowledged current problems for the current
   principal.

Each problem card adds a secondary **Acknowledge** action beside its existing
domain navigation action. The Problems section header adds **Acknowledge all**
when at least one problem is visible and the current credential has the new
write permission.

While an acknowledgement request is pending, disable the affected action and
prevent duplicate submission. On success, normal React Router revalidation
reloads the authoritative Inbox and removes the acknowledged cards. On
failure, keep every card visible and show a focused error notification.

The attention badge counts visible decisions plus visible, unacknowledged
problems. Acknowledging problems never changes the decision count or the Run
activity summary.

Viewers see the same current problems, filtered by any acknowledgements they
created before a role downgrade, but see no acknowledgement controls.

If the dedicated Inbox API is unavailable, the page shows the readiness-backed
failure state. It does not fall back to the old browser-side composition path;
the project does not retain dual product behavior during the pre-stable phase.

## API Contract

Add domain-sized `api/paths/inbox.yaml` and `api/components/inbox.yaml` files
and reference them from `api/openapi.yaml`. Regenerate the Go server contract
and TypeScript client through `task api:generate`; never edit generated output
by hand.

### Get the current Inbox

```http
GET /api/v1/inbox
```

Operation ID: `getInbox`

Security: product session or product bearer with `read` permission.

Successful response:

```json
{
  "generatedAt": "2026-10-22T20:00:00Z",
  "capabilities": {
    "acknowledgeProblems": true
  },
  "decisions": [],
  "problems": [],
  "activity": {
    "queued": 0,
    "starting": 0,
    "running": 0
  }
}
```

The response schemas are:

- `Inbox`: `generatedAt`, `capabilities`, `decisions`, `problems`, and
  `activity`;
- `InboxCapabilities`: currently only `acknowledgeProblems`;
- `InboxActivity`: exact queued, starting, and running Run counts;
- `InboxItem`: opaque `id`, `kind`, `title`, `description`, `occurredAt`,
  `actionLabel`, and a structured `destination`;
- `InboxDestination`: a `type` enum and optional resource `id`.

The initial item kinds are:

- `promotion-review`;
- `promotion-error`;
- `git-trigger-failure`;
- `repository-refresh`;
- `cluster-unreachable`;
- `run-observation`;
- `dependency-unavailable`.

The destination types are `promotion`, `repository`, `cluster`, `run`, and
`system-status`. The Server returns semantic destinations rather than React
Router paths. The web application maps them to its current routes.

The Server owns the stable title, description, and action label in this
presentation-oriented read model. The web application owns icons, colors,
layout, relative-time formatting, and routing.

The Inbox service sorts decisions and problems independently by `occurredAt`
descending, then by `id` for deterministic ties.

### Acknowledge current problems

```http
POST /api/v1/inbox/problem-acknowledgements
Content-Type: application/json
```

Operation ID: `acknowledgeInboxProblems`

Security: product session or product bearer with `inbox:write` permission.

Request:

```json
{
  "problemIds": ["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]
}
```

`problemIds` is required, non-empty, unique, and bounded to 500 entries. Every
ID is a 64-character lowercase hexadecimal occurrence ID.

The Server recomputes the current raw problem set before writing. It persists
only requested IDs that are currently active problems. Unknown, resolved,
replaced, decision, or already acknowledged IDs are ignored so retries and
resolution races remain successful and idempotent. The operation returns
`204 No Content` after the transaction completes.

Malformed bodies and invalid IDs return `400`. Missing authentication returns
`401`; insufficient role or scope returns `403`. Persistence or projection
failure is a Server error and writes no partial batch.

## Authorization

Add:

- product permission `inbox:write`;
- API-key scope `slipramp:inbox:write`.

The authorization matrix becomes:

| Operation                  | Minimum role | API-key scope          |
| -------------------------- | ------------ | ---------------------- |
| Get Inbox                  | `viewer`     | `slipramp:read`        |
| Acknowledge Inbox problems | `operator`   | `slipramp:inbox:write` |

Administrators satisfy the role requirement. `slipramp:admin` continues to
grant every permission subject to role. A non-admin API key needs both
`slipramp:read` to retrieve the Inbox and `slipramp:inbox:write` to
acknowledge.

Update the API-key scope schema, product identity validation, PostgreSQL scope
constraint, Access UI, authorization tests, and the matrix in
[DESIGN.md](./DESIGN.md#api-keys-and-permissions) together. Include the new scope
in owner API-key revocation behavior as well.

The `InboxCapabilities.acknowledgeProblems` value is derived from the complete
authentication result, including role and credential scope. It is not inferred
from role alone in the web application.

## Persistence

Add `internal/store/migrations/019_inbox_acknowledgements.sql`:

```sql
CREATE TABLE inbox_problem_acknowledgements (
    principal_id uuid NOT NULL REFERENCES principals (id),
    problem_id text NOT NULL,
    acknowledged_at timestamptz NOT NULL,
    PRIMARY KEY (principal_id, problem_id),
    CONSTRAINT inbox_problem_acknowledgements_problem_id_valid CHECK (
        problem_id ~ '^[0-9a-f]{64}$'
    )
);
```

The same migration extends the `api_keys_scopes_valid` constraint with
`slipramp:inbox:write`. Its down migration removes acknowledgements before
restoring the previous scope constraint.

Add focused SQL in `internal/store/queries/inbox.sql` for:

- listing acknowledgement IDs for one principal;
- inserting a validated batch with `ON CONFLICT DO NOTHING`;
- reading all active Runs needed for freshness and activity;
- reading pending or error Promotions needed by the projection.

Reuse the existing complete Repository and Cluster-observation reads. Keep the
queries grouped with the Inbox projection instead of expanding catch-all store
files.

The acknowledgement table deliberately stores one row per principal and
occurrence. It does not overwrite an older occurrence for the same logical
resource, which prevents a delayed request for an old occurrence from erasing
an acknowledgement for a newer one.

Only the Server may insert IDs, and it inserts only IDs matched against its
current raw problem set. The API therefore cannot be used to fill the table
with arbitrary caller-selected values. Initial cleanup is unnecessary at the
supported scale; retention or resolved-occurrence cleanup remains a later
decision that must not cause an active acknowledged problem to reappear
silently.

## Server Design

### Package ownership

Add focused files under `internal/inbox`:

- `inbox.go` for kinds, items, destinations, activity, and validation;
- `occurrences.go` for canonical occurrence identity;
- `service.go` for projection and acknowledgement behavior;
- matching focused unit tests.

The service has narrow dependencies for:

- Repository refresh state;
- raw Cluster observations;
- active Runs and Run counts;
- pending and error Promotions;
- Promotion expiry before projection;
- dependency readiness observations;
- acknowledgement reads and writes.

Use existing domain types and pure derivation behavior where possible. Expose a
pure Cluster connection-state function that accepts the evaluation time, just
as Run observation freshness already accepts an explicit time. Do not
duplicate connection or observation thresholds inside the Inbox package.

### Projection sequence

`Get` performs the following work:

1. Authenticate the principal and capture one UTC evaluation time.
2. Expire pending Promotions through the existing Promotion service boundary.
3. Load Repository state, Cluster observations, active Runs, relevant
   Promotions, acknowledgements for the principal, and readiness.
4. Derive Cluster connectivity and active Run observation state at the shared
   evaluation time.
5. Create pending Promotion decision items only when the authentication result
   authorizes `promotions:write`.
6. Create repository refresh, Cluster, standalone stale Run, Promotion error,
   and artifact-storage dependency problem items.
7. Group unknown Runs beneath an unreachable Cluster and omit their standalone
   problem cards.
8. Remove problem items whose occurrence IDs are acknowledged by this
   principal.
9. Sort the two sections and return exact Run activity counts and capability
   flags.

Source reads may run concurrently after the shared evaluation time is fixed.
Any required error cancels the request and prevents a partial response.

### Acknowledgement sequence

`Acknowledge` performs the following work:

1. Validate the non-empty, unique, bounded ID list.
2. Capture one evaluation time and derive the complete raw current problem set
   without applying acknowledgement filtering.
3. Intersect requested IDs with current problem IDs.
4. Insert the matching IDs for the authenticated principal in one database
   transaction using the captured acknowledgement time.
5. Return success even when the intersection is empty.

The HTTP handler passes the authenticated principal and does not accept actor
identity from the body. Authorization middleware rejects viewers before the
handler executes.

### Handler and wiring

Add `internal/api/handler/inbox.go` with a narrow `InboxService` interface and
register it through `handler.Dependencies`. Construct the service in
`internal/server/server.go` from existing domain services, readiness, and the
database.

Add `GetInbox` to the read-permission operation map and
`AcknowledgeInboxProblems` to the new write permission. Keep controller and
execution authentication unchanged.

The Slipramp Server remains free of Kubernetes clients. The Inbox consumes
only state already reported by Controllers.

## Web Application

Replace the homepage loader's domain fan-out with:

- `getInbox` through the generated authenticated client;
- `getReadiness` as the independent failure fallback.

Delete the old client-side Inbox domain composition after the cutover. Retain
only small presentation helpers if they still improve the route organization;
do not keep a second implementation of problem inclusion or grouping.

Add a React Router action to the index route. It accepts one or more repeated
`problemId` form values, validates their shape at the web boundary, and calls
`acknowledgeInboxProblems` through the generated client. It never accepts a
principal ID.

Use `useFetcher` for individual and bulk actions so the page remains in place.
The route should rely on post-action revalidation for authoritative removal;
an optimistic visual state is optional but must restore cards on failure.

Map structured API destinations to existing routes in one exhaustive helper.
An unknown destination is a compile-time or explicit runtime error, not a
silent link to the homepage.

Remove the current limited-source and unavailable-source bookkeeping. A
successful response is complete. When `getInbox` fails, render a critical
unavailable state informed by `/readyz`, keep Run activity unknown, and never
show **All clear**.

## Concurrency and Failure Semantics

- Repeating an acknowledgement request is idempotent.
- Two credentials for the same principal acknowledging the same occurrence
  converge on one row.
- Different principals always write independent rows.
- A resolved occurrence requested by a stale page is ignored.
- A replacement occurrence is not hidden by acknowledgement of its predecessor.
- A new problem racing with **Acknowledge all** remains visible because its ID
  was not submitted.
- If raw problem projection fails during acknowledgement, no rows are written.
- If the acknowledgement transaction fails, the API returns an error and the
  web application keeps the problems visible.
- Acknowledgement never mutates Repository, Cluster, Run, Promotion, or
  readiness state.
- Server restart preserves PostgreSQL acknowledgements. A continuing in-memory
  dependency incident is deliberately assigned a new occurrence after restart.

## Required Tests

### Domain and service tests

- deterministic occurrence IDs for every problem kind;
- occurrence ID changes when and only when the accepted occurrence marker
  changes;
- one shared evaluation time for Cluster and Run freshness;
- stale Runs grouped under unreachable Clusters;
- stale Runs on connected Clusters remain standalone problems;
- pending decisions require the complete `promotions:write` authorization;
- decisions are never accepted by acknowledgement;
- acknowledgements filter only the matching principal and occurrence;
- a later occurrence of the same resource reappears;
- artifact-storage unavailable transitions produce stable per-outage IDs;
- Server restart behavior for dependency occurrences;
- deterministic sorting and exact Run activity counts;
- any required source failure prevents a partial Inbox.

### Persistence tests

- migration up and down behavior;
- acknowledgement foreign-key and ID-shape constraints;
- idempotent repeated insertion;
- atomic batch insertion;
- independent rows for different principals;
- complete active Run and relevant Promotion projection queries;
- generated SQL freshness.

### HTTP and authorization tests

- generated `GET /api/v1/inbox` response conversion;
- `POST` body validation and `204` response;
- anonymous `401` behavior;
- viewer and insufficient-scope `403` behavior;
- operator session and correctly scoped API-key success;
- administrator-scope success;
- stale, unknown, decision, and already acknowledged IDs are harmless;
- every product operation remains present in the fail-closed authorization
  map.

### Web tests

- homepage uses the dedicated Inbox API rather than domain list APIs;
- individual acknowledgement submits one current problem ID;
- **Acknowledge all** submits every displayed problem ID and no decision IDs;
- acknowledgement controls follow the API capability rather than role-only
  inference;
- successful actions revalidate and remove acknowledged cards;
- failed actions preserve cards and display an error;
- failed Inbox loading cannot render **All clear**;
- readiness provides the PostgreSQL-outage fallback;
- destination mapping covers every generated destination type.

### End-to-end acceptance

Exercise two user principals:

1. Produce at least one repository or observation problem.
2. Confirm both principals see it.
3. Acknowledge it as the first operator and reload or restart the web process.
4. Confirm it remains hidden only for that principal.
5. Confirm the second principal still sees it.
6. Produce a later occurrence for the same resource and confirm it reappears
   for the first principal.
7. Create multiple problems, use **Acknowledge all**, and confirm pending
   decisions remain.
