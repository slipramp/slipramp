# Repository-defined release gates

This document defines the repository-owned release Gate contract. Gates are
opt-in current policy that can delay a Run assignment or Kargo Promotion
release until an application-specific condition is safe. They are not
execution steps, approvals, terminal outcomes, or a Server-owned authorization
boundary.

The initial evaluator reads numeric observations from administrator-configured
Prometheus sources. The feature is introduced in vertical slices: Run release
first, then Promotion release. The same Gate semantics and evidence model apply
to both subjects.

## Repository contract

`Gate` is a catalog resource alongside `Runtime` and `Target`:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Gate
metadata:
  name: teamspeak-idle
  title: TeamSpeak has no active users
spec:
  prometheus:
    source: homelab
    query: max_over_time(teamspeak_active_users{server="main"}[1m])
    condition:
      operator: equal
      value: 0
```

One Gate has exactly one typed Prometheus evaluator. Supported numeric
operators are `equal`, `not-equal`, `less-than`, `less-than-or-equal`,
`greater-than`, and `greater-than-or-equal`. Equality is exact. Queries can
express tolerance or duration when either is required.

Portable catalog validation rejects duplicate Gate names, invalid resource or
source names, empty or excessively large queries, unsupported operators,
non-finite thresholds, duplicate Action references, and references to a Gate
absent from the same catalog. Deployment validation additionally rejects a
Gate that selects an unknown configured source. A configured source being
temporarily unreachable does not invalidate a catalog.

Actions attach an ordered, unique list of repository-local Gate names:

```yaml
actions:
  deploy:
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
  digest, and observation into immutable subject evidence.

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
    prometheus:
      url: http://prometheus:9090
      evaluationInterval: 15s
      timeout: 5s
```

Source configuration owns the fixed Prometheus base URL, evaluation interval,
request timeout, bounded concurrency, and any bearer-token, TLS CA, or TLS
client-certificate file references. Repository authors cannot change cadence,
transport, or credentials. The default interval is 15 seconds and the default
timeout is five seconds; timeout must be shorter than interval.

Source URLs cannot contain user information, query parameters, or fragments.
Credential values and file contents never enter PostgreSQL, product APIs,
diagnostics, or structured logs. Plaintext HTTP is valid only when a trusted
encrypted network already protects the transport.

## Evaluation

The focused `internal/gate` package owns normalized definitions, source and
condition validation, definition digests, comparison semantics, Prometheus
response normalization, evaluation coordination, and bounded diagnostics.
Run and Promotion packages retain ownership of their release decisions; Gates
do not introduce a generic subject or workflow abstraction.

An observation has one of three states:

| State | Meaning |
| --- | --- |
| `passing` | One valid finite numeric result satisfies the condition. |
| `blocking` | One valid finite numeric result does not satisfy the condition. |
| `unavailable` | No trustworthy comparison can be made. |

No observation is distinct from `unavailable` internally, but both fail
closed. Product projections may use `unevaluated` before the first attempt.
Diagnostics are bounded and sanitized. They can identify timeout, source,
cardinality, parsing, or freshness failures but cannot contain credentials or
arbitrary upstream response bodies.

The evaluator sends an instant query at the Server's current UTC time. It
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
- Query once per unique normalized definition digest even when many subjects
  share it.
- Bound concurrency per source, add small jitter, and apply bounded failure
  backoff up to one minute.
- Reset backoff after a successful query and stop scheduling when no waiters
  remain.

A passing observation remains fresh for three normal source intervals. Release
requires evidence from the current evaluator process, the current active
definition, and an unexpired validity window. The Server generates a new
evaluator process identity at startup, preventing evidence obtained with old
endpoint or credential configuration from releasing work after restart.

One fresh passing observation is sufficient. Conditions that must hold for a
duration express that duration in PromQL rather than adding a second Gate
timer.

## Persistence and atomic release

PostgreSQL is authoritative for requirements, current observations, and
release evidence. Domain-specific association tables use real foreign keys;
there is no polymorphic subject table.

The current-observation projection is keyed by Repository, Gate name, active
catalog revision, and normalized definition digest. It stores evaluator process
identity, state, optional finite value, optional bounded diagnostic, evaluation
time, and validity time. Evaluations update the projection in place rather than
retaining every poll indefinitely.

`run_gates` and `promotion_gates` store the subject, Repository, Gate name,
stable attachment order, optional release time, and optional immutable
definition and observation evidence. Requirement rows commit atomically with
Run creation or Promotion acceptance.

Releasing a subject is one transaction that:

1. Serializes the subject's release operation.
2. Resolves every frozen name against the Repository's current catalog.
3. Requires current, fresh, passing evidence for every resolved definition.
4. Copies all definitions and observations into immutable evidence with one
   release time.
5. Performs the authoritative assignment or delivery transition.

Every Gate must pass in the same release check. Individual requirements do not
latch at different times. A definition change between observation and release
cannot use evidence for the previous definition.

## Run release

Run Gate rows are created after Action resolution and within the Run insertion
transaction. Gates do not change a Run's queued phase, and cancellation remains
available while a Run waits.

Queue explanation uses this precedence:

1. Pending review.
2. Unreleased Gates.
3. Target active Run capacity.
4. Controller reachability.
5. Cluster or Placement capacity.
6. Ordinary FIFO eligibility.

The assignment transaction validates all Gate evidence, records immutable
release evidence, and changes the Run from queued to assigned atomically. Queue
position excludes Runs waiting for review or Gates. A later blocking
observation never cancels or rewrites an assigned Run.

## Promotion release

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
repository-defined Gates.

## Product projection

A subject Gate summary is `not-required`, `waiting`, or `released`. Individual
requirements project `unevaluated`, `passing`, `blocking`, `unavailable`, or
`released`, along with the Gate name and title, finite observed value when
present, operator and threshold, evaluation time, release time, and sanitized
diagnostic.

The product API never exposes Prometheus endpoints, credentials, response
labels, queries, or raw responses. Run scheduling details use
`awaiting-gates`; Run and Promotion details show a compact waiting state and a
focused Gate panel. Detail views refresh while waiting. Initial Gate support
does not add dedicated pages, history, filters, Inbox items, notifications, or
manual overrides.

## Operational boundaries

Repository-defined Gates are voluntary controls. Direct unreviewed Actions
remain valid when operators choose them, and repository maintainers can remove
attachments for future subjects. Gates do not provide separation of duties or
mandatory Server-owned policy.

Evaluation telemetry uses bounded labels for source type, result, and subject
type. It never labels metrics with PromQL, Repository, Gate, Target, or Stage
names. Structured logs may include stable IDs and bounded error classes, but
never query responses or credential material.

The initial contract excludes manual overrides, cancellation of active work,
additional evaluator types, subject-specific query interpolation,
repository-owned cadence or credentials, implicit attachment, workflow graphs,
and retained observation history.
