# Release Gates

Repository authors can attach Prometheus or webhook Gates to Actions and Kargo
Stages. The Server evaluates these Gates before assigning a Run or releasing
a Promotion.

## Accepted expansion

| Gate type | Owner | Applies to | Evidence at release |
| --- | --- | --- | --- |
| Prometheus | Repository | Attached Action or Stage | Shared numeric observation for the definition digest |
| Webhook | Repository | Attached Action or Stage | Evaluator decision for the individual Run or Promotion |
| Window | Server | Global and Target or Stage | Open interval at the release instant and scope window version |
| Lock | Server | Global and Target or Stage | Scope lock state, expiry and version at the release instant |

The Server provides lock and window management for global, Target, and Stage scopes.
Runs with frozen `reconcile` or `operation` intent check current global and
Target policies, including policies added while the Run waits. Previews check
explicitly attached repository Gates. Mediated Promotions check the global
and destination Stage scopes together with their attached repository Gates.

Run assignment requires every applicable Gate to pass or have an authorized
override for that Run, Gate key, and exact version. The assignment records
immutable evidence. Later observations and policy changes affect future claims.
[Gate expansion implementation plan](./gate-expansion-plan.md)

## Server lock persistence

A scope has its own UUID and a unique reference to its Target or Stage. The
global scope is a singleton. Migration 075 backfills all durable owners,
including archived ones. An insert trigger creates the scope in the same
transaction as each new owner. Archival and reactivation preserve that row.

A lock mutation acquires the global scope row first, followed by the local row
when applicable. The Server samples time after acquiring both rows. Each
change updates the current lock and appends an event with its version in one
transaction. The event retains the actor, time, and any supplied reason or expiry.
Expiry makes the hold inactive at the specified instant. A new lock after
expiry takes the next version. Reads return a consistent snapshot of current
state and recent events. [Lock management API](../GATES.md#manage-server-locks)

Each subject's override collection exposes active global and applicable local
locks under the key `lock/<scope-uuid>`. A mutating Run can waive its global
or Target lock while queued. A Promotion can waive its global or Stage lock
until its release boundary. Cancellation closes both paths. A preview can
still waive an explicitly attached repository Gate.

A lock override serializes with changes to the subject and the applicable scopes.
It checks expiry after acquiring the locks. Run and Promotion overrides of
Server Gates share one decision table with foreign keys to their subject and
the exact policy event. The history retains both actors, any override reason, any
lock reason or expiry, and the decision time after unlock, expiry, or re-lock.
A retry by the same principal with the same reason returns the existing decision
while the subject and lock remain eligible.

## Weekly window schedules

A schedule names an IANA time zone and up to 128 weekday intervals. Times use
24-hour `HH:MM` and include the opening minute. The closing minute is exclusive.
Use `24:00` as the end of a day. Split an overnight interval at midnight across
the two weekdays. The Server merges overlapping or adjacent intervals, including
across Sunday and Monday.

If a boundary falls in a skipped clock interval, that occurrence stays closed.
For a repeated boundary, the later opening and earlier closing apply. An interval
that spans the clock change keeps its real opening and closing instants. An
all-week window has no next transition and renews its observation weekly.

The Server evaluates the normalized schedule in the transaction that stores
it. A coordinator refreshes observations at startup and at their deadlines.
The effective state becomes unavailable at the expiry instant and for a future
observation. Refreshes preserve the policy version. Each accepted update,
including an equivalent schedule, advances the version. Removal advances it
once. Updates and removals retain their actor and time in the scope audit.

The key `window/<scope-uuid>` identifies the window on a waiting subject.
Overrides are available for blocking and unavailable windows. Each decision
retains the observation, definition digest, and time zone that the operator
saw. A later opening or closing preserves the decision for that policy
version. Replacing or removing the policy invalidates the waiver.
[Window management API](../GATES.md#manage-weekly-windows)

## Repository contract

`Gate` is a catalog resource alongside `Runtime` and `Target`:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Gate
metadata:
  name: teamspeak-idle
  title: TeamSpeak has no active users
spec:
  evaluator:
    type: prometheus
    source: homelab
    query: max_over_time(teamspeak_active_users{server="main"}[1m])
    condition:
      operator: equal
      value: 0
```

The Server recognizes two repository evaluator types, Prometheus and webhook.
Each type defines its policy, source configuration, and public evidence.
Prometheus shares evaluations by normalized definition digest. Supported
numeric operators are `equal`, `not-equal`, `less-than`, `less-than-or-equal`,
`greater-than`, and `greater-than-or-equal`. Equality is exact. Queries can
express tolerance or duration when either is required.

Portable catalog validation rejects duplicate Gate names, invalid resource or
source names, empty or excessively large queries, unsupported operators,
non-finite thresholds, duplicate Action references, and references to a Gate
absent from the same catalog. Deployment validation additionally rejects a
Gate whose source is missing or has a different evaluator type. A configured source being
temporarily unreachable does not invalidate a catalog.

Actions attach an ordered, unique list of repository-local Gate names:

```yaml
actions:
  deploy:
    intent: reconcile
    command: [./deploy.sh]
    gates:
      - teamspeak-idle
```

Attachment is explicit per Action. Read-only and diagnostic Actions do not
inherit the Gates of a mutating Action, and there is no Target default or
removal syntax. Run creation freezes the attached names. An exact rerun retains
the original names; a latest rerun resolves the current Action and its current
attachments.

Observed Kargo Stages use a strict JSON-array annotation:

```yaml
metadata:
  annotations:
    slipramp.com/promotion-gates: '["teamspeak-idle"]'
```

The Controller validates and normalizes the ordered, unique names but does not
resolve definitions. A gated Stage must have complete enrichment that maps its
Repository URL to exactly one active Repository, and each name must resolve in
that Repository's active catalog before Slipramp accepts a gated Promotion.
Malformed annotation data rejects the observation. Missing Repository or Gate
context makes the Gate capability unavailable without removing the Stage from
native inventory.

Promotion acceptance freezes the attached names and Repository identity. It
never loads Gate definitions from a candidate revision supplied by Kargo.

## Current policy and immutable attachment

Attachments and definitions have different lifetimes:

- A Run or Promotion freezes its ordered set of repository-local Gate names.
- Until release, each name resolves against the Repository's current active
  catalog.
- Updating a definition changes current policy for existing waiters and can
  repair a broken definition without recreating them.
- Removing a required definition leaves the requirement unresolved and fails
  closed until the Gate is restored.
- Activating a catalog wakes evaluation immediately.
- Release copies the exact active definition, catalog revision, definition
  digest, and passing observation or operator decision into immutable subject evidence.

The current catalog may therefore change the definition of an existing
requirement, but it cannot silently remove the frozen requirement itself.

## Prometheus source configuration

The Server loads an optional strict configuration file selected by
`SLIPRAMP_GATE_SOURCES_FILE`:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  homelab:
    type: prometheus
    url: http://prometheus:9090
    evaluationInterval: 15s
    timeout: 5s
    maximumConcurrency: 4
```

Source configuration owns the fixed Prometheus base URL, evaluation interval,
request timeout, bounded concurrency, and any bearer-token, TLS CA, or TLS
client-certificate file references. Repository authors cannot change cadence,
transport, or credentials. The default interval is 15 seconds, the default
timeout is five seconds, and the default source concurrency is four with a
maximum of 32. Timeout must be shorter than interval.

Bearer-token, CA, client-certificate, and client-key references are absolute
file paths. Client certificate and key files are configured together, and TLS
file settings require an HTTPS source.

Source URLs cannot contain user information, query parameters, or fragments.
Credential values and file contents never enter PostgreSQL, product APIs,
diagnostics, or structured logs. Plaintext HTTP is valid only when a trusted
encrypted network already protects the transport.

## Evaluation

The focused `internal/gate` package owns normalized definitions, source and
condition validation, definition digests, comparison semantics, Prometheus
and webhook response normalization, evaluation coordination, bounded
diagnostics, and the shared Run and Promotion Gate summary. The Run and Promotion packages own
their respective release decisions.

An observation has one of three states:

| State | Meaning |
| --- | --- |
| `passing` | The evaluator allows this release. |
| `blocking` | The evaluator denies this release. |
| `unavailable` | The evaluator cannot make a trustworthy decision. |

No observation is distinct from `unavailable` internally, but both fail
closed. Product projections may use `unevaluated` before the first attempt.
Diagnostics are bounded and sanitized. They can identify timeout, source,
cardinality, parsing, or freshness failures but cannot contain credentials or
arbitrary upstream response bodies.

The Prometheus evaluator sends an instant query at the Server's current UTC time. It
accepts a Prometheus scalar or an instant vector containing exactly one sample.
It rejects empty or multi-series vectors, matrices, strings, histograms,
unsupported result types, non-finite values, unsuccessful or malformed
responses, and responses over a fixed conservative byte limit. Product
clients receive only the numeric value, never labels or raw responses.

Evaluation is demand-driven for definitions referenced by unreleased Runs or
Promotions:

- Evaluate immediately for the first waiter and on new requirements, relevant
  review approval, definition changes, and Server startup.
- Re-evaluate on the source interval while waiters remain.
- Query Prometheus once per normalized definition digest. Webhook requests
  additionally key on the subject type and UUID.
- Read evaluation demand once per reconciliation and let each configured source
  process its latest demand independently.
- Bound concurrency per source, add small jitter, and apply bounded failure
  backoff up to one minute.
- Reset backoff after a successful query and stop scheduling when no waiters
  remain.

A passing observation remains fresh for three normal source intervals. Release
by observation requires evidence from the current evaluator process, the current active
definition, and an unexpired validity window. The Server generates a new
evaluator process identity at startup, preventing evidence obtained with old
endpoint or credential configuration from releasing work after restart. If the
active catalog still references a removed source, its Gates remain unevaluated
while Gates using configured sources continue polling.

One fresh passing observation is sufficient. Conditions that must hold for a
duration express that duration in PromQL rather than adding a second Gate
timer.

## Persistence and atomic release

PostgreSQL is authoritative for requirements, current observations, and
release evidence. Domain-specific association tables use real foreign keys;
there is no polymorphic subject table.

Prometheus observations are keyed by Repository, Gate name, active catalog
revision, and normalized definition digest. A webhook observation also belongs
to one subject and occupies that subject's requirement row. Observation writes
lock the subject so release and overrides read a consistent decision. For the
same revision, the evaluation start time orders observations so a delayed
response cannot replace a newer decision. The Server
sends the active policy revision and frozen subject facts to the operator-configured
endpoint. Successful protocol responses carry a bounded public code and message.
[Webhook protocol](../GATES.md#use-a-webhook-policy)

Each observation stores evaluator process identity, state, optional type-tagged
JSON evidence, optional bounded diagnostic,
evaluation time, and validity time. A finite Prometheus sample is stored as
`{"prometheus":{"value":0}}`. An unavailable observation has a diagnostic
and SQL NULL evidence. Evaluations update the projection in place rather than
retaining every poll indefinitely. Each hour the Server maintenance coordinator
removes observations that expired at least 24 hours earlier and have no matching
unreleased requirement. The Run or Promotion retains its release evidence.
Evaluation demand and waiting counts share the same set of queued Runs and
in-flight Promotions with unreleased requirements. Only demand depends on
whether a Gate currently resolves in the active catalog.

`run_gates` and `promotion_gates` store the subject, Repository, Gate name,
stable attachment order, optional release time, and optional immutable
definition and observation evidence. Requirement rows commit atomically with
Run creation or Promotion acceptance.

Releasing a subject is one transaction that:

1. Serializes the subject's release operation.
2. Resolves every frozen name against the Repository's current catalog.
3. Requires a fresh passing observation or a matching operator override for each definition.
4. Records the evidence used for each requirement with one
   release time.
5. Performs the authoritative assignment or delivery transition.

The release check evaluates all requirements together. Passing observations do
not latch independently. A changed definition requires evidence matching its
new digest.

## Per-Gate overrides

The Run and Promotion projections identify each repository attachment by
`repository/<gate-name>`. Its origin is `repository` and its scope carries the
owning Repository UUID. A resolved Gate reports the evaluator type and its
exact version, the normalized definition digest. A stopped or missing
definition retains its attachment identity. Released details use the frozen
definition, version, observation and validity interval.

Each historical decision derives its key from its attachment and its version
from its recorded definition digest. This keeps same-policy attachments
distinct even when they share an observation.

Operators and administrators can override a blocking, unavailable, or unevaluated Gate on one
waiting Run or Promotion. The submitted key and version must match the attachment and its active definition.
A missing definition continues to block release. The decision records the actor, time,
observed state, and any supplied reason. A decision with a sample stores the same typed evidence
under `observedEvidence`. The latest decision applies while its digest matches. A digest change
requires a new decision and earlier decisions remain visible. Reruns and other subjects start
with their own requirements.

Review, cancellation, and capacity still govern release. At assignment, creation dispatch,
or callback release, PostgreSQL records the common release time and exact definition for every
requirement. Requirements satisfied by override retain the decision as evidence.

## Run release

Run Gate rows are created after Action resolution and within the Run insertion
transaction. Gates do not change a Run's queued phase, and cancellation remains
available while a Run waits.

Queue explanation uses this precedence:

1. Pending review.
2. Active locks, global before Target.
3. Closed or unavailable weekly windows, global before Target.
4. Unsatisfied repository Gates in attachment order.
5. Target active Run capacity.
6. Controller reachability.
7. Cluster or Placement capacity.
8. Ordinary FIFO eligibility.

The queue skips blocked Runs before assigning FIFO positions. The same Gate
predicate filters the oldest eligible Run metric. A Controller poll first locks
the candidate Run and its Target. For mutating Actions it acquires shared
global and Target policy locks in that order. For an attachment it also locks
the Repository. It samples database time after acquiring those locks and checks
eligibility again. A weekly observation is fresh from its evaluation time up
to its exclusive validity deadline.

The assignment freezes both scope IDs and their independent lock and window
versions, including scopes without active policy. It copies applicable lock
and window evidence and the matching decisions. Every attached repository
Gate records the same release time. The assignment and all evidence commit in
one transaction. Controller redelivery returns the assigned Run. Historical
assignments retain the evidence captured at the time.

## Promotion release

The Server records successful callback approval in the release transaction,
including for a Stage with no repository Gates. That receipt closes the
callback override path. For a Slipramp-created automatic Promotion, the first
creation dispatch closes it. Migration 076 treats historical approved,
hydrated callbacks with no outstanding repository Gates as already released,
because the earlier schema could not distinguish an ungated approval from an
approval already returned to Kargo. Migration 080 preserves those callback
receipts and the first-dispatch boundary for historical automatic Promotions.
An existing waiter picks up the current policies on its next release check.

Release locks the Promotion, global scope, destination Stage scope, and any
attached Gate Repositories before sampling database time. Every active lock
needs a waiver for its current version. Every configured window needs a fresh
pass or a matching waiver. All repository Gates must pass or have their own
matching waivers. The transaction records both scope versions, the observed
policy and matching decisions, and the repository evidence at that instant.
If any requirement still blocks, the transaction rolls back. A later poll or
delivery retry uses the committed release. Cancellation and release serialize
on the same Promotion row.

Gate state remains separate from Promotion lifecycle, review, creation
delivery, and Kargo execution outcome. An automatic Promotion can have
lifecycle `approved` while execution is still waiting for Gate release.

Slipramp-originated automatic Promotions are persisted immediately but are not
delivered for creation until their Gates release. Release evidence and command
claim commit together, allowing indefinite waiting without consuming a Kargo
PromotionTask retry window.

Slipramp-originated review-required Promotions are delivered before release so
Kargo can render the candidate, call Slipramp, and allow preparation and human
review. After approval, the PromotionTask polls while Gates remain blocked.
Kargo-originated Promotions persist their requirements atomically on the first
callback and follow the same automatic or review-required ordering.

The Kargo callback response has an effective `gated` status distinct from the
persisted Promotion lifecycle enum. `gated` keeps the PromotionTask polling;
only `approved` satisfies the success expression. The task retry timeout bounds
how long callback-mediated flows can wait and its expiry remains a Kargo
execution failure, not a Gate outcome.

Slipramp can enforce only Promotion paths it mediates. A Kargo Promotion that
neither originates in Slipramp nor calls the Slipramp PromotionTask bypasses
Slipramp release checks. The Controller can report that Promotion through
Stage inventory. Observing a Kargo outcome creates no Slipramp release receipt.

## Product projection

PostgreSQL reads the evidence for Run and Promotion summaries through one
projection. The Gate package turns each row into a requirement state using the
current evaluator process and observation validity window.

Cancellation before Gate release changes the subject summary and its unreleased
requirements to `stopped`. The detail retains the attached Gate names and omits
live observations. Cancellation after release preserves the release values,
conditions, and timestamps. Ungated subjects remain `not-required`.

Other subject summaries are `waiting` or `released`. Their individual
requirements project `unevaluated`, `passing`, `blocking`, `unavailable`, or
`released`, along with the Gate name and title, typed `evidence` when
present, operator and threshold, evaluation time, release time, and sanitized
diagnostic. Released requirements use the title and condition from the release
revision. While waiting, a Run whose Gate definition has disappeared uses the
title from its attached revision.

The product API never exposes Prometheus endpoints, credentials, response
labels, queries, or raw responses. Run scheduling details use
`awaiting-gates`; Run and Promotion details show a compact waiting state and a
focused Gate panel. Detail views refresh while waiting and retain override decisions after release.

## Operational boundaries

Repository-defined Gates are voluntary controls. Direct unreviewed Actions
remain valid when operators choose them, and repository maintainers can remove
attachments for future subjects. Gates do not provide separation of duties or
mandatory Server-owned policy.

Evaluation telemetry uses bounded labels for source type, result, and subject
type. It never labels metrics with PromQL, Repository, Gate, Target, or Stage
names. Structured logs may include stable IDs and bounded error classes, but
never query responses or credential material.

The implemented Prometheus contract excludes cancellation of active work,
subject-specific query interpolation,
repository-owned cadence or credentials, implicit attachment, workflow graphs,
and retained observation history.
