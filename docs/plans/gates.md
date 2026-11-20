# Repository-defined gates

Status: proposed implementation plan

## Goal

Add opt-in, repository-defined release gates that can hold both Runs and Kargo
Promotions until application-specific conditions are safe. The first expected
use case is preventing TeamSpeak changes while users are active.

Gates are current release policy, not execution steps, approvals, or terminal
outcomes. They prevent work from starting but never stop work that has already
started. The initial evaluator reads numeric observations from operator-named
Prometheus sources.

## Governing behavior

- Repositories define Gate names, PromQL, and numeric passing conditions.
- Server configuration defines Prometheus endpoints, credentials, evaluation
  cadence, timeouts, and transport policy.
- Actions opt into repository-local Gates explicitly.
- Kargo Stages opt into repository-local Gates through strict enrichment
  metadata.
- Creation succeeds while a Gate is blocking. The Run or Promotion remains
  durably visible and waits for release.
- Missing definitions, unknown sources, stale observations, query failures,
  malformed results, and non-finite values fail closed.
- Every required Gate must have fresh passing evidence at the release instant.
  Individual Gates are not latched at different times.
- Release is latched once. A later blocking observation never cancels an
  assigned Run or a released Promotion.
- Release records the exact catalog revision, normalized Gate definition, and
  observation evidence used.
- Gate state remains orthogonal to Run lifecycle, Run review, Promotion review,
  Promotion creation delivery, and Kargo execution outcome.
- Repository-defined Gates are voluntary operational controls. They are not an
  operator-enforced authorization or separation-of-duties boundary.
- Slipramp can enforce only the Promotion paths it mediates. A Kargo Promotion
  that neither originates in Slipramp nor calls the Slipramp PromotionTask can
  bypass these Gates.

## Scope

The first implementation includes:

- a `Gate` catalog resource;
- Action Gate references;
- a Kargo Stage Gate annotation;
- named Prometheus sources in Server configuration;
- durable current observations and immutable release evidence;
- Run assignment gating;
- Promotion creation or callback gating, depending on source and review mode;
- generated API types and focused Web presentation;
- unit, database integration, and end-to-end coverage.

The first implementation does not include:

- manual Gate overrides;
- canceling active work when a Gate later blocks;
- HTTP, script, Kubernetes, or plugin evaluators;
- Gate parameters or subject-specific query interpolation;
- repository-defined evaluation intervals, timeouts, or credentials;
- mandatory Server-owned Gate attachment policy;
- Gate history pages, notifications, Inbox categories, or list filters;
- a workflow or dependency graph between Actions;
- implicit Gate attachment based on Target paths, Stage application names, or
  other inferred relationships.

## Repository contract

### Gate resource

Add `Gate` as a third catalog resource kind alongside `Runtime` and `Target`.
The initial contract has one typed Prometheus evaluator:

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

Supported numeric operators are:

- `equal`;
- `not-equal`;
- `less-than`;
- `less-than-or-equal`;
- `greater-than`;
- `greater-than-or-equal`.

The threshold must be a finite number. Equality is exact numeric equality; a
Gate that needs tolerance can express it directly in PromQL or use range
comparisons.

Catalog validation must reject:

- duplicate Gate names;
- invalid resource or source names;
- empty or excessively large queries;
- unsupported operators;
- non-finite thresholds;
- duplicate Action references;
- Action references to a Gate absent from the same catalog.

Standalone catalog validation can validate source-name syntax but cannot know
an installation's configured sources. Server-side catalog activation must also
reject a Gate that selects an unknown local source. A temporarily unreachable
configured source does not invalidate the catalog.

Update catalog loading, strict YAML decoding, source locations, schema
generation, explanation output, examples, and validation fixtures together.

### Action attachment

Add ordered, unique Gate names to `Action`:

```yaml
actions:
  deploy:
    gates:
      - teamspeak-idle
```

Attach Gates only to Actions whose start must be constrained. A read-only plan
or diagnostic Action does not inherit the Gates of a mutating Action. Target
level defaults and Action-level removal syntax are intentionally omitted.

Run creation freezes the attached Gate names from its selected Action. Exact
reruns retain those names. Latest reruns resolve the current Action and its
current attachments.

### Promotion attachment

Add a strict JSON-array annotation to observed Kargo Stages:

```yaml
metadata:
  annotations:
    slipramp.com/promotion-gates: '["teamspeak-idle"]'
```

The Controller parses and validates the annotation but does not resolve Gate
definitions. It submits the ordered, unique names in the complete Stage
snapshot. Malformed metadata rejects the observation instead of being guessed.

A Stage with Gate names must have enrichment that maps its repository URL to
exactly one active Repository. Every name must resolve in that Repository's
active catalog before Slipramp accepts a gated Promotion. Missing repository or
Gate context makes the Gate capability unavailable without removing the Stage
from native inventory.

Promotion acceptance freezes the attached Gate names and Repository identity.
It does not load Gate definitions from a candidate revision supplied by Kargo.

## Definition update semantics

Gate attachments and Gate definitions have different lifetimes:

- A Run or Promotion freezes the set of required repository-local Gate names.
- Until release, each name resolves against the Repository's current active
  catalog.
- Updating a Gate definition therefore repairs or intentionally changes policy
  for waiting subjects without recreating them.
- Removing a still-required Gate does not remove the requirement. It makes the
  requirement unavailable and fail-closed until the Gate is restored.
- Repository activation wakes evaluation immediately.
- Release copies the exact active definition, catalog revision, digest, and
  observation into immutable subject evidence.

This treats Gate definitions like current scheduling policy rather than part of
the immutable execution snapshot. The attached names still come from the
subject's accepted Action or Stage snapshot, so a later catalog cannot silently
remove an existing requirement.

## Server metric sources

Add an optional strict configuration file selected by
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

Source configuration owns:

- the fixed Prometheus base URL;
- evaluation interval and request timeout;
- optional bearer-token file;
- optional TLS CA and client-certificate files if required by the existing
  HTTP configuration conventions;
- bounded source concurrency.

URLs must not contain user information, query parameters, or fragments.
Credential values and credential file contents never enter PostgreSQL, product
APIs, diagnostics, or structured logs. Plaintext HTTP remains valid only across
a trusted encrypted network, matching the Controller transport policy.

Use a 15-second default evaluation interval and a five-second default timeout.
Require the timeout to be shorter than the interval. Repository authors cannot
change operational cadence or transport settings.

## Gate domain

Create a focused `internal/gate` package. It owns:

- catalog-independent normalized definitions;
- source and condition validation;
- definition digests;
- observation states and comparison semantics;
- Prometheus response normalization;
- evaluation coordination;
- bounded operator-facing diagnostics.

Keep subject release decisions in `internal/run` and `internal/promotion`.
Avoid a generic workflow service or a polymorphic subject abstraction.

### Observation states

An evaluation produces one of three states:

| State | Meaning |
| --- | --- |
| `passing` | One valid numeric result satisfies the condition. |
| `blocking` | One valid numeric result does not satisfy the condition. |
| `unavailable` | No trustworthy comparison can be made. |

No current observation is distinct from `unavailable` internally, but both
fail closed. Product projections may use `unevaluated` to explain that the
first attempt has not completed.

An unavailable diagnostic must be bounded and sanitized. It may describe a
timeout, source response, result cardinality, parse failure, or stale evidence,
but must not include credentials or arbitrary upstream response bodies.

### Prometheus result contract

Evaluate an instant query at the Server's current UTC time. Accept:

- a Prometheus scalar result; or
- an instant vector containing exactly one sample.

Reject:

- empty vectors;
- vectors with more than one series;
- matrices, strings, histograms, or unsupported response types;
- `NaN`, positive infinity, and negative infinity;
- unsuccessful API responses and malformed JSON;
- responses above a conservative byte limit.

Expose only the numeric value to product clients. Do not expose result labels,
which prevents repository queries from turning Gate diagnostics into a general
metrics-data retrieval API.

### Evaluation scheduling

The evaluator is active only for Gate definitions referenced by unreleased
Runs or Promotions.

- Evaluate immediately when the first waiter appears.
- Re-evaluate on the source interval while waiters remain.
- Wake immediately for new requirements, review approval, active Gate
  definition changes, and Server startup.
- Query once per unique normalized definition digest, even when many subjects
  reference it.
- Bound concurrency per source and add small scheduling jitter.
- Apply bounded backoff to transport and source failures, with a proposed
  maximum of one minute.
- Reset failure backoff after the next successful query.
- Stop scheduling a definition when no unreleased subject references it.

A passing observation remains fresh for three normal source intervals, 45
seconds under the default configuration. Release requires an observation from
the current evaluator process and the current active definition whose
`valid_until` has not passed.

One fresh pass is enough. A condition that must remain true for a duration
belongs in PromQL, such as `max_over_time(...[1m]) == 0`, rather than in a second
Gate timer model.

## Persistence

PostgreSQL remains authoritative for requirements, current observations, and
release evidence. Use domain-specific association tables with real foreign
keys instead of a `subject_type` and `subject_id` pair.

### Current observations

Add a current-observation table keyed by Repository, Gate name, active catalog
revision, and normalized definition digest. Store at least:

- Repository ID and Gate name;
- catalog revision ID and definition digest;
- evaluator process identity;
- observation state;
- optional finite numeric value;
- optional bounded diagnostic;
- evaluation and validity times.

The evaluator updates this projection in place. Do not append every 15-second
poll indefinitely. A maintenance operation may delete observations that have no
waiters after a grace period.

Generate a new evaluator process identity at Server startup. Release ignores
observations from an earlier process so a changed endpoint or credential cannot
reuse evidence from before restart.

### Run requirements

Add a `run_gates` association with:

- Run ID;
- Repository ID;
- required Gate name;
- stable attachment order;
- nullable release time;
- nullable immutable definition and observation evidence.

Create these rows in the Run insertion transaction.

### Promotion requirements

Add a `promotion_gates` association with the same Gate and release evidence,
plus the Promotion foreign key. Create these rows in the Promotion acceptance
transaction after resolving the Stage's Repository identity.

### Atomic release

Release must:

1. lock or otherwise serialize the subject's release operation;
2. resolve every attached name against the Repository's current active catalog;
3. require a current, fresh, passing observation for every resolved definition;
4. copy every definition and observation into immutable release evidence with
   one release time;
5. perform the authoritative domain transition in the same transaction.

This prevents Gate A from latching on one day and Gate B from independently
latching later. It also closes the race where repository refresh changes a Gate
between evaluation and assignment.

## Run integration

### Creation and reruns

Run creation resolves and records the Action's Gate names after selecting the
catalog revision and before inserting the Run. Gate rows and the Run commit
atomically.

- Exact reruns copy the original attached names.
- Latest reruns use the current Action definition.
- Gate state does not change the Run's queued phase.
- Run cancellation continues to work while gated.

### Scheduling and assignment

Add `awaiting-gates` to Run scheduling explanations. Use this precedence for a
queued Run:

1. pending review;
2. unreleased Gates;
3. Target active Run capacity;
4. Controller reachability;
5. Cluster or Placement capacity;
6. ordinary FIFO eligibility.

The assignment transaction checks current Gate evidence, writes release
evidence, and changes the Run from queued to assigned atomically. A Gate that
becomes blocking after assignment is only a new observation; it cannot rewrite
or cancel the Run.

Queue position excludes Runs whose reviews or Gates are not released, matching
the existing review behavior.

## Promotion integration

Gate state remains separate from the persisted Promotion lifecycle. An
automatic Promotion may have lifecycle `approved` while still waiting for Gate
release; this means policy permission exists but execution has not been
released.

### Slipramp-originated automatic Promotions

Create the durable Promotion immediately, but exclude it from Controller
creation delivery until all Gates release. The creation-dispatch claim records
release evidence and claims the command in one transaction.

This path can wait indefinitely without consuming a Kargo PromotionTask retry
window because the Kargo object does not exist yet.

### Slipramp-originated review-required Promotions

Creation delivery must proceed before Gate release so Kargo can generate the
candidate branch, call Slipramp, and allow preparation and operator review.
After operator approval, the Kargo PromotionTask continues polling. Slipramp
returns a gated response until all Gates release, then latches release and
returns approval.

The Kargo PromotionTask retry timeout bounds how long this path can remain
gated after callback. Configuration documentation must tell operators to choose
a timeout appropriate for gated Stages. An expired Kargo task remains a Kargo
execution failure, not a Gate outcome.

### Kargo-originated Promotions

When the Kargo PromotionTask first submits a request, accept the durable
Promotion and Gate requirements atomically. For automatic policy, return a
gated response until release rather than immediate approval. For review policy,
preserve preparation and pending-review behavior, then apply Gates after
operator approval.

This path also inherits the Kargo PromotionTask retry timeout. A Kargo template
without the Slipramp callback remains outside Slipramp Gate enforcement.

### Kargo response contract

Split the Kargo callback response status from the product Promotion lifecycle
enum. Add an effective `gated` response status used only by the polling
contract. Keep product lifecycle values unchanged so Gate state does not become
another review status.

The existing success expression continues to require `approved`; `gated`
causes Kargo to poll. Existing rejected, expired, error, and HTTP failure
handling remains terminal.

## API and Web presentation

Add generated API schemas for an overall Gate summary and individual Gate
requirements.

The overall subject state is one of:

- `not-required`;
- `waiting`;
- `released`.

An individual requirement can present:

- `unevaluated`;
- `passing`;
- `blocking`;
- `unavailable`;
- `released`.

Expose only bounded, useful information:

- Gate name and title;
- current or released state;
- observed numeric value when present;
- operator and threshold;
- evaluation time;
- release time;
- sanitized diagnostic.

Do not expose Prometheus endpoint details, credentials, result labels, or raw
responses.

Product changes include:

- `awaiting-gates` in Run scheduling details;
- Gate summaries on Run and Promotion detail responses;
- a distinct effective Kargo callback status;
- a compact "Waiting for gates" activity state;
- a detail panel showing current value, expected condition, last evaluation,
  and diagnostic;
- detail refresh while a subject remains gated.

Initial work does not add Gate filters, dedicated pages, Inbox items, or
notifications.

## Operational telemetry

Add low-cardinality telemetry for:

- evaluation attempts and duration by result and source type;
- current waiting Run and Promotion counts;
- unavailable evaluation counts;
- release counts by subject type.

Do not label process metrics with PromQL, Repository, Gate name, Target, Stage,
or other unbounded user-defined values. Structured logs may include stable IDs,
Repository name, Gate name, and bounded error classification, but never query
responses or credentials.

## Testing

### Catalog and configuration tests

- strict Gate resource decoding;
- duplicate names and references;
- missing Action references;
- every condition operator;
- invalid thresholds and queries;
- Stage annotation parsing and normalization;
- source URL, duration, timeout, TLS, and credential-file validation;
- unknown source handling during Server catalog activation;
- catalog schema and explanation output.

### Evaluator tests

- scalar and one-element vector success;
- blocking numeric values;
- empty and multi-series vectors;
- unsupported result types;
- `NaN` and infinities;
- malformed and oversized responses;
- source errors and timeouts;
- diagnostic sanitization;
- deduplication by definition digest;
- freshness, backoff, wakeups, and restart process identity.

### Database integration tests

- requirement insertion is atomic with Run or Promotion creation;
- no, stale, blocking, or unavailable evidence prevents release;
- every Gate must pass within the same release check;
- repository refresh between evaluation and release blocks old evidence;
- changed definitions become eligible only after reevaluation;
- removed definitions fail closed;
- concurrent release or assignment succeeds once;
- release evidence is immutable;
- later blocking observations do not affect released subjects;
- Run review precedence, Target limits, FIFO order, and Placement and Cluster
  capacity remain correct;
- Promotion creation dispatch and Kargo polling preserve their idempotency.

### End-to-end tests

Use a controllable fake Prometheus HTTP server rather than requiring the local
Prometheus container in CI.

- A Run remains queued while a value blocks and is assigned after it passes.
- Multiple Runs sharing one definition cause one evaluation per interval.
- A source outage blocks release and recovery resumes it.
- An automatic Slipramp Promotion waits before Kargo object creation.
- A reviewed Promotion prepares normally, remains gated after approval, and
  releases on a later Kargo poll.
- A Kargo-originated automatic callback receives `gated` and later `approved`.
- Restart cannot release from pre-restart observations.

## Delivery sequence

### 1. Design contract

- Refine this plan into `docs/design/gates.md` once the governing decisions are
  accepted.
- Link it from `docs/DESIGN.md`.
- Add or narrow the corresponding `docs/ROADMAP.md` entry while implementation
  remains incomplete.

### 2. Run Gate vertical slice

- Add the Gate catalog and Server source contracts.
- Add persistence, the Prometheus evaluator, and the evaluation worker.
- Integrate Run creation, reruns, scheduling, and assignment.
- Update OpenAPI, generated clients, CLI output, Web presentation, and tests.

This slice establishes real behavior rather than landing an unused generic
framework.

### 3. Promotion Gate vertical slice

- Add Stage metadata and snapshots.
- Add Promotion requirement persistence and resolution.
- Integrate creation dispatch, review approval, Kargo callback polling, API,
  Web presentation, and acceptance coverage.

### 4. Operational hardening

- Add telemetry, stale-observation cleanup, bounded concurrency, and recovery
  coverage.
- Complete configuration, installation, Kargo template, and troubleshooting
  documentation.

### 5. TeamSpeak rollout

- Add and validate the active-user metric outside Slipramp.
- Configure the Prometheus source.
- Add `teamspeak-idle` to the application Repository.
- Validate the PromQL directly against production-like data.
- Attach the Gate first to the intended mutating Actions, then to the relevant
  Kargo Stage.
- Exercise active-user, idle, source-unavailable, and recovery cases before
  relying on the Gate operationally.

## Rollout and compatibility

Gate resources and references are optional. Existing catalogs, Runs,
Promotions, and installations without configured Gate sources retain their
current behavior. No compatibility alias or legacy path is needed because the
new contract is additive and pre-stable.

Deploy source configuration before activating a catalog that references it.
Add and verify metrics before attaching a Gate. A repository may land an
unreferenced Gate definition first, but evaluation begins only when a waiting
Run or Promotion references it.

When the feature lands, update the current design documents to describe the
implemented contract and remove completed planning detail from the roadmap.

## Deferred follow-up

Revisit these only after the first production use demonstrates a need:

- explicit cancellation of an unreleased automatic Promotion;
- an operator-owned mandatory Gate attachment layer;
- manual emergency override with durable attribution;
- a Gate preflight or query-testing product workflow;
- additional evaluator types;
- per-Gate stability windows beyond PromQL;
- Gate transition notifications and operational Inbox projections;
- retained Gate observation history beyond immutable release evidence.
