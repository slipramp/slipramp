# Gate expansion implementation plan

Phases 1 through 6 are complete. Run assignment enforces global and Target
policy. Promotion creation dispatch and callback approval enforce global and
destination Stage policy. Both record the lock, window, repository evidence,
and waivers used at release. The web app supports policy management, typed
Gate evidence on both subjects, and an installation-wide lock warning.
Operational polish remains in progress.

This plan expands Gate into the common release-policy concept for Runs and
Kargo Promotions. Prometheus and webhook Gates obtain observations from
configured sources. Window Gates evaluate a persisted schedule. Lock Gates
represent durable operator holds. All Gate types participate in the same
atomic release decision while retaining type-specific ownership, evaluation,
evidence, and override semantics.

The resulting model must preserve Slipramp's opt-in safety policy. With no
attached repository Gates and no configured Server Gates, work releases as it
does today. A Gate never cancels work that has already been released.

# Goals

- Support webhook evaluation alongside Prometheus.
- Keep evaluator-specific policy, source, and evidence formats out of the
  shared coordinator and release paths.
- Support deployment windows at global, Target, and Kargo Stage scope.
- Support one current operator lock at global, Target, and Kargo Stage scope.
- Present repository and Server Gates through one Gate summary and release
  decision.
- Apply current Gate policy atomically when a Run is assigned or a Promotion
  is released.
- Retain immutable evidence showing which Gate definitions and Server policy
  revisions allowed release.
- Allow an authorized operator to override any blocking, unavailable, or
  unevaluated Gate for one waiting Run or Promotion.
- Preserve independent Run and Promotion lifecycle ownership.
- Keep endpoints, credentials, cadence, and transport configuration out of
  repository-controlled Gate definitions.

# Non-goals

- Canceling or pausing active Runs or Promotions after release.
- Guaranteeing that execution finishes, or even starts remotely, before a
  deployment window closes. The controlled event is the durable Slipramp
  release transition.
- Enforcing Slipramp Gates on Kargo Promotions that neither originate in
  Slipramp nor call the Slipramp PromotionTask.
- Replacing Kargo-native promotion windows. Slipramp windows are an additional
  release constraint on mediated Promotions.
- Adding Cluster, Placement, Kargo Project, Repository, or Action-level Server
  Gate scopes.
- Adding an arbitrary policy language or running third-party evaluator code in
  the Slipramp Server.
- Supporting asynchronous webhook approvals. An evaluator request is a
  bounded, side-effect-free query; durable callbacks require a separate
  lifecycle design.
- Allowing repository definitions to provide webhook URLs, credentials,
  headers, timeouts, or polling cadence.

# Gate model

## Gate origins and types

Gate is the umbrella release-policy concept. Origin controls how a Gate is
attached and administered; type controls how its current state is determined.

| Origin | Type | Applicability | State source |
| --- | --- | --- | --- |
| Repository | Prometheus | Explicit Action or Stage attachment | Configured Prometheus source |
| Repository | Webhook | Explicit Action or Stage attachment | Configured HTTP evaluator |
| Server | Window | Global, Target, or Stage scope | Persisted schedule and current time |
| Server | Lock | Global, Target, or Stage scope | Current scope lock state |

Repository Gate attachment retains the current contract: a Run or Promotion
freezes the ordered Gate names, while each unreleased name resolves against the
Repository's active catalog. Server Gates are not copied onto subjects at
creation. Their current global and local scope state is read at release, so a
new lock or window change immediately affects existing waiters.

## Common state

All Gate checks project one of these states:

| State | Meaning |
| --- | --- |
| `unevaluated` | No current trustworthy evaluation exists. |
| `passing` | The Gate currently allows release. |
| `blocking` | The Gate currently denies release. |
| `unavailable` | The Gate cannot make a trustworthy decision and fails closed. |
| `released` | Immutable evidence for this Gate was recorded at subject release. |
| `stopped` | The subject stopped before release. |

The shared projection also records:

- stable Gate identity, title, origin, type, and scope;
- definition digest or Server lock/window version;
- evaluation and validity times when applicable;
- bounded type-specific evidence;
- bounded diagnostics for unavailable evaluations;
- a stable logical Gate key, exact current version, and override history;
- release time and immutable release evidence after release.

Gate evaluation results remain distinct from review, cancellation, lifecycle,
capacity, and execution outcomes. Product views may aggregate these facts into
one release summary without persisting another lifecycle state.

## Override policy

Every Gate type is overrideable for one waiting Run or Promotion by an
authorized operator or administrator. An override satisfies only the exact
Gate and subject named by the decision. It never changes the Gate itself,
unlocks other work, or applies to another subject.

Every projected Gate check has a stable logical key and an opaque current
version:

- a repository Gate uses `repository/<gate-name>` and the active definition
  digest;
- a window Gate uses `window/<scope-id>` and the scope's window version;
- a lock Gate uses `lock/<scope-id>` and the scope's lock version.

An override request submits the Gate key, current version, and an optional
reason. The decision records the actor, time, any reason, current Gate state, and
bounded current evidence. It is accepted only while the subject is waiting and
the Gate is `blocking`, `unavailable`, or `unevaluated`.

Changing a repository definition or window increments its version and
invalidates earlier overrides. Re-locking a scope increments its lock version,
so an override of the previous lock cannot bypass the new lock. Releasing a
lock removes its blocker without rewriting its override history. Release
copies every matching override into immutable subject evidence.

# Action applicability

Server Gates apply to all Promotions and to Runs whose frozen Action intent is
`reconcile` or `operation`. They do not apply to `preview` Runs. Repository
Gates remain explicitly attached and may gate a preview when the catalog says
so.

Every Action declares its intent explicitly. Run creation freezes that value
with the resolved Action. An exact rerun uses the original intent and a latest
rerun uses the newly resolved intent.

# Repository Gate definitions

## Typed definition union

Repository Gates declare their evaluator through the `spec.evaluator.type`
discriminator. Prometheus uses the following envelope.

```yaml
spec:
  evaluator:
    type: prometheus
    source: production
    query: max_over_time(teamspeak_active_users[1m])
    condition:
      operator: equal
      value: 0
```

```yaml
spec:
  evaluator:
    type: webhook
    source: change-policy
    input:
      policy: production-deploy
```

The normalized complete definition, including evaluator type, contributes to
the definition digest. Presentation metadata and Gate name remain excluded.

Webhook `input` is bounded non-secret JSON. Validation must bound encoded size,
nesting depth, property count, key length, string length, and supported scalar
types. It must reject non-finite numbers. Repository authors cannot inject HTTP
headers or transport configuration through this object.

## Evaluation scope

Prometheus remains definition-scoped and shares one observation per normalized
definition digest. Webhook Gates are always subject-scoped: each Run or
Promotion is evaluated independently and an answer is never reused for another
subject. Supporting two webhook caching modes is deferred until a concrete
need justifies the additional demand and persistence paths.

Webhook input contains a versioned, bounded envelope with only
the identifiers and non-secret facts needed for a release decision:

- subject type and UUID;
- Repository UUID and name;
- catalog revision UUID and Git commit;
- for Runs, Target UUID and name plus Action name and intent;
- for Promotions, Stage UUID, integration, Cluster, Project, and Stage name;
- Gate definition digest, configured input, evaluation ID, and evaluation
  time.

Do not include credentials, environment variables, Artifact contents, logs,
arbitrary Kubernetes objects, or raw principal snapshots. Add more fields only
for a demonstrated policy use case and include them in the versioned contract.

## Built-in evaluators

Prometheus and webhook are the supported repository evaluator types.
The Server uses each type's policy, source, and evidence codecs to validate
authoring and decode stored data. The coordinator groups demand by evaluation
scope and calls the configured source. Webhooks are the interface for external
policy implementations.

# Gate source configuration

Each configured source uses the same evaluator type discriminator. Its
remaining fields are decoded strictly by the registered evaluator:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  production:
    type: prometheus
    url: https://prometheus.example.com
    evaluationInterval: 15s
    timeout: 5s
    maximumConcurrency: 4
    bearerTokenFile: /var/run/slipramp/gates/prometheus-token
  change-policy:
    type: webhook
    url: https://policy.example.com/slipramp/evaluate
    evaluationInterval: 30s
    timeout: 5s
    maximumConcurrency: 4
    bearerTokenFile: /var/run/slipramp/gates/policy-token
```

A Gate definition's evaluator type must match the selected source type.

Webhook sources reuse the existing bearer-token and TLS file conventions.
Validation requires an absolute HTTP or HTTPS URL without user information,
query, or fragment. Plaintext HTTP retains the existing trusted-network
restriction. Redirects are disabled.

The evaluator sends a fixed-version JSON request and accepts a fixed-version
JSON response:

```json
{
  "apiVersion": "slipramp.com/gate-evaluation/v1alpha1",
  "decision": "passing",
  "code": "change-approved",
  "message": "Approved change record is active"
}
```

`decision` is `passing`, `blocking`, or `unavailable`. `code` is a bounded
machine-readable value and `message` is a bounded operator-facing explanation.
The Server never persists or logs the raw response body.

The evaluator treats timeout, transport failure, non-2xx response, oversized
body, invalid media type, malformed JSON, unknown fields, unsupported decision,
or invalid evidence as unavailable. Evaluation remains demand-driven, bounded
per source, jittered, and backed off after failure. The Server owns freshness;
the remote response cannot extend its validity.

Webhook requests include a unique evaluation ID and stable evaluation key.
Endpoints must be side-effect-free and safe to retry. The first implementation
does not accept callbacks or hold an HTTP request open for asynchronous work.

# Server Gate scopes

## Scope identity

Create one durable global scope and one durable scope for every Target and
observed Kargo Stage. Target and Stage scope identity uses their existing UUID,
not mutable names. Scope rows are created with the owning durable identity and
backfilled for existing Targets and Stages.

Each scope has independent monotonic `lock_version` and `window_version`
values. Locking or unlocking advances the lock version; changing or removing a
window advances the window version. Automatic lock expiry is evaluated from
its persisted timestamp and does not require a timer or version update.
Release transactions copy the applicable versions and common check time into
immutable subject evidence.

A scope has at most one current lock and one current window. Without a
subject-specific override, local policy can restrict global policy but cannot
broaden it:

- an active global or local lock must have a matching override or it blocks;
- every configured global and local window must be open or have a matching
  override;
- absence of a window at a scope means unrestricted at that scope.

## Lock Gate records

Store the current lock on its scope. The lock contains:

- optional reason of up to 1024 characters;
- creating Principal UUID and actor snapshot;
- creation time;
- optional expiry time;
- lock version.

A lock is active at the release instant when the scope is locked and its
optional expiry is later than that instant. Locking an already locked scope
returns a conflict; the operator must unlock it before creating a new lock.
Repeated unlock is idempotent. Every lock and unlock appends a bounded audit
event containing the prior and resulting version, actor, reason, and time.

A per-subject override references `lock/<scope-id>` and the exact lock version.
Unlocking and re-locking the scope produces a new version and requires a new
override.

## Window Gate definition

Use an interval contract rather than cron:

```yaml
timeZone: America/Denver
weekly:
  - days: [monday, tuesday, wednesday, thursday]
    start: "09:00"
    end: "17:00"
```

Window validation must:

- require a valid IANA timezone;
- bound weekly intervals;
- reject zero-length or reversed intervals;
- define whether a weekly interval may cross midnight and normalize it
  consistently;
- use half-open `[start, end)` boundaries;
- define conservative behavior for nonexistent and repeated local times;
- compute the next state transition without unbounded search.

The evaluator returns passing or blocking plus the current interval and next
transition. Configuration errors are rejected when written rather than
projected as unavailable.

Updating a window advances the scope's window version and invalidates overrides
of the earlier version even when the normalized schedule is equal. Temporary
blackouts use the scope lock. An exceptional release outside the weekly window
uses a per-subject override. Calendar exceptions remain deferred.

## Window observation projection

A Server coordinator evaluates a window immediately when it is created or
changed, on Server startup, and at its next transition. Persist the current
state, window version, definition digest, evaluation time, validity time, and
next transition directly on the scope row.

A passing observation is valid only until the next transition. The release
query requires `valid_until > checked_at`, so a delayed coordinator can delay
opening a window but cannot release work after a window has closed. Blocking
state may remain current until the next transition, at which point it also
fails closed until the coordinator records the new state.

Lock state is read directly from the durable scope row while it is locked. It
does not require polling or a synthetic observation.

# Persistence changes

Use a forward migration and update SQLC queries and generated code together.
The intended relational shape is:

- `server_gate_scopes`
  - global, Target, or Stage scope with real nullable foreign keys and a check
    constraint enforcing exactly one valid scope shape;
  - current lock fields, lock version, and optional expiry;
  - current weekly window definition, digest, and window version;
  - computed window state, evaluation time, validity time, and next
    transition;
  - last-update actor and time.
- `server_gate_scope_events`
  - append-only bounded audit events for lock, unlock, window update, and
    window removal with prior and resulting versions.
- `server_gate_overrides`
  - Append-only Run and Promotion decisions use subject type and ID, scope,
    Gate type, and exact version as the key.
  - Each override stores the decision actor, observed state, and reason.
    Foreign keys link it to the subject and the policy event.

Add bounded `server_gate_release_evidence` JSON to Runs and Promotions. It
copies the global and local scope IDs, lock and window versions, checked time,
Gate evidence, and matching overrides used at release. This avoids separate
release tables while preserving an immutable audit snapshot.

Store subject-scoped webhook observations on their existing `run_gates` and
`promotion_gates` requirement rows. Retain `gate_observations` only for shared
definition-scoped evaluators such as Prometheus. Existing repository override
history remains on the requirement and matches the active definition digest.

Generalize current and release observations from Prometheus-specific numeric
columns to validated type-tagged JSON evidence while retaining explicit state,
evaluator process identity, evaluation time, and validity columns used by
release queries. Backfill retained Prometheus release evidence as:

```json
{"prometheus":{"value":0}}
```

Database checks must constrain every persisted enum, require object-shaped
evidence, preserve finite numeric values, and enforce complete release evidence.
Released historical subjects must remain readable after the migration.

Run and Promotion references retain their table-specific foreign keys. Views
and queries expose common Gate checks for the following operations.

- waiting repository Gate requirements;
- shared and subject-scoped evaluation demand;
- eligible repository Gate evidence;
- current Server Gate scope state;
- eligible Server Gate evidence including exact per-subject overrides;
- Run and Promotion release summaries.

# Evaluation coordinator changes

The coordinator selects the configured source by name and verifies its type.
Each source supplies the following settings.

- evaluator type;
- request interval and validity calculation;
- bounded concurrency semaphore;
- evaluator implementation;
- type-specific telemetry label.

Demand has an explicit evaluation key:

- definition digest for definition-scoped evaluation;
- subject type, subject UUID, and definition digest for subject-scoped
  evaluation.

The coordinator groups demand by evaluation key before calling the configured
source. A subject-scoped result applies only to requirements for that subject.
Store methods route shared observations to `gate_observations` and subject
observations to the corresponding `run_gates` or `promotion_gates` row.

Keep the latest-demand, wakeup, jitter, concurrency, failure-backoff, process
identity, and restart behavior already used by Prometheus. Source removal
leaves dependent Gates unavailable while other sources continue normally.

Window evaluation should use the same coordinator lifecycle conventions but a
separate persisted demand path because Server windows are not repository
attachments and wake at exact schedule transitions.

# Atomic release

Run and Promotion packages continue to own their release transitions. Do not
create a generic lifecycle engine in `internal/gate`.

## Lock ordering

Every release transaction and Server Gate mutation uses the same ordering:

1. Lock the subject when one exists.
2. Lock the global Server Gate scope.
3. Lock the Target or Stage Server Gate scope.
4. Lock repository rows needed to resolve attached Gate definitions in stable
   UUID order.
5. Perform the guarded release or policy mutation.

Creating a lock and updating a window must lock the scope before writing child
rows. This prevents phantom lock insertion from racing a release check.
Override operations use the same ordering, re-resolve the exact current Gate
under those locks, and record the decision before release can consume it.

## Run assignment

At one `assigned_at` instant, a Run with `reconcile` or `operation` intent is
eligible only when:

- review is not required or is approved;
- every attached repository Gate has fresh passing evidence or a matching
  override;
- every active global and Target lock has a matching override;
- every configured global and Target window has fresh passing evidence or a
  matching override;
- Target, Cluster, and Placement capacity allow assignment.

Preview Runs skip Server Gate scope checks but still honor explicitly attached
repository Gates.

The queue query must exclude Gate-blocked Runs before FIFO ranking so a blocked
Run does not starve later eligible work. Extend the same eligibility predicate
to scheduling details and the oldest-eligible-Run metric. Avoid a service-level
precheck that can disagree with the authoritative claim transaction.

After claiming the candidate, copy every attached repository Gate release
evidence plus the applicable Server Gate evidence and matching override
decisions into the Run's release snapshot before commit. Any count, Gate key,
version, digest, freshness, active-lock, or override mismatch rolls back the
assignment.

Activating a lock or closing a window does not affect assigned, preparing, or
running Runs. Queued Git-triggered Runs continue latest-wins coalescing.
Scheduled and manual Runs remain queued rather than being silently discarded.

## Promotion release

Replace the Gate-specific release helper with a Promotion release operation
that checks attached repository Gates plus global and Stage Server Gates under
the Promotion lock.

- Slipramp-originated automatic Promotions wait before creation delivery.
- Review-required and callback-mediated Promotions may exist in Kargo but keep
  receiving the effective `gated` callback state until every Gate passes or
  has a matching override.
- A matching per-Promotion override may satisfy any repository, window, or
  lock Gate without changing that Gate for other Promotions.
- Cancellation, review, creation delivery, Gate release, and Kargo outcome
  remain separate facts.
- A later lock or closed window does not affect a Promotion that already
  released.

The existing `gated` callback name remains accurate because webhook, window,
and lock checks are all Gate types. Kargo PromotionTask timeout continues to
bound callback-mediated waiting and does not become a Gate outcome.

# OpenAPI and authorization

OpenAPI remains the source of truth. Add domain-sized Gate paths and schemas,
then regenerate Go and TypeScript clients together.

## Gate management endpoints

Expose explicit Server scope resources:

- `GET /api/v1/gate-scopes/global`
- `GET /api/v1/gate-scopes/targets/{targetId}`
- `GET /api/v1/gate-scopes/stages/{stageId}`
- `PUT` and `DELETE` the `/window` child of each scope
- `PUT` and `DELETE` the `/lock` child of each scope

Responses include lock and window versions, current configuration, recent
audit events, effective current state, and next transition. Clearing an absent policy and repeating an identical active
lock request are idempotent. A conflicting lock request returns a conflict.
Every accepted window update advances its version.

Each subject exposes an override collection at the following paths.

- `/api/v1/runs/{runId}/gate-overrides`
- `/api/v1/promotions/{promotionId}/gate-overrides`

A `GET` lists eligible Gates and the subject's prior decisions. A `POST`
includes the logical Gate key, opaque current version, and an optional reason.
A stale version, changed window, unlocked or expired scope, passing Gate,
stopped subject, or Gate that is no longer applicable returns a conflict.
Successful responses return the complete typed Gate projection and override
history.

## Permissions

Add `slipramp:gates:manage` for Server Gate mutation.

- Readers may inspect Gate state.
- Operators and administrators may manage Target and Stage Server Gates.
- Only administrators may manage global Server Gates.
- Existing `slipramp:runs:override-gates` and
  `slipramp:promotions:override-gates` authorize eligible overrides of every
  Gate type for their respective subject.

Update API-key database constraints, authorization middleware, identity
documentation, generated types, and integration coverage together.

# Product behavior

## Shared release summary

Run and Promotion APIs expose one release summary containing all applicable
Gate checks in deterministic order:

1. global lock Gate;
2. local lock Gate;
3. global window Gate;
4. local window Gate;
5. attached repository Gates in attachment order.

The release summary is `not-required`, `waiting`, `released`, or `stopped`.
Individual checks retain the common Gate states. Review remains adjacent but
separate because it is a durable human decision, not a Gate observation.

For queued Runs, primary scheduling explanation uses:

1. pending review;
2. active lock Gate;
3. blocking or unavailable window Gate;
4. blocking, unavailable, or unevaluated repository Gate;
5. Target capacity;
6. Controller reachability;
7. Cluster or Placement capacity;
8. ordinary FIFO eligibility.

Return all Gate blockers in detail even when the list view shows one primary
reason.

## Web application

- Replace the Prometheus-specific Gate panel with a typed Gate panel that
  renders evaluator, window, and lock evidence without nested cards.
- Add global Gate management under an administrative operational view.
- Add Gate controls to Target and Stage detail views.
- Show a persistent global-lock warning to authorized users.
- Show window timezone, current state, current interval, and next transition.
- Show lock actor, reason, creation, expiry, and release history.
- Offer an override for every blocking, unavailable, or unevaluated Gate when
  the subject is waiting and the principal has the matching subject override
  permission.
- Show the exact overridden Gate version, decision actor, reason, and time
  before and after release.
- Use route-level dialog and tab URL state according to the Web URL-state
  contract.
- Continue refreshing waiting subject detail and scope views at a cadence that
  accounts for `nextTransitionAt` without introducing per-row timers.

# Observability and operations

Extend bounded Gate telemetry:

- evaluation attempts and duration with `source_type=prometheus|webhook` and
  normalized result;
- waiting subjects by subject type and blocker type;
- active Server locks by scope type, without scope IDs or names;
- window transitions and unavailable/stale projections by scope type;
- webhook failure classes without endpoint, response body, Gate, Target,
  Stage, or Repository labels.

Structured logs may contain stable internal IDs and bounded error classes.
They must not contain webhook input, credentials, raw response bodies,
Prometheus labels, or repository-defined query contents.

Configuration and operations documentation must cover:

- webhook endpoint security and retry behavior;
- window timezone and DST behavior;
- lock creation, expiry, and recovery;
- per-subject override behavior and version invalidation for every Gate type;
- Server restart behavior;
- the distinction between Slipramp and Kargo-native windows;
- the mediated-Promotion enforcement boundary.

# Implementation sequence

## Phase 1: Contract and Gate domain foundation

- [x] Record the accepted Gate model and release boundaries in the governing
  design and roadmap.
- [x] Require explicit Action intent across catalog validation, Run snapshots,
  assignment, clients, and documentation.
- [x] Introduce typed evaluator policies and observation evidence for the
  existing common requirement states.
- [x] Extend the Gate and override domain types with origin, scope, logical
  Gate key, and exact policy version.
- [x] Define codecs for supported evaluator types and use a discriminated
  evaluator envelope in the catalog Gate schema.
- [x] Generalize summary projection and release evidence while preserving
  Prometheus behavior.
- [x] Add the forward evidence migration and backfill existing Prometheus history.

Acceptance:

- Existing Prometheus Gates retain their behavior and immutable evidence.
- Historical released Gate details remain readable.
- Catalog validation rejects missing, unknown, or malformed evaluator
  definitions.
- The coordinator groups shared Prometheus demand by definition and isolates
  webhook demand by subject.
- `task generate`, focused Go tests, and API stale-generation checks pass.

## Phase 2: Webhook evaluator

- [x] Add strict webhook source configuration and file loading.
- [x] Implement hardened HTTP transport, fixed envelopes, response normalization,
  size limits, TLS and token loading, redirect rejection, and sanitization.
- [x] Preserve definition-scoped Prometheus evaluation and add subject-scoped
  webhook evaluation keys and persistence.
- [x] Extend coordinator routing, demand views, metrics, and cleanup.
- [x] Add catalog deployment validation against configured source types.
- [x] Update CLI catalog validation and source-name inputs.

Acceptance:

- Passing, blocking, explicit unavailable, timeout, non-2xx, malformed,
  oversized, restart, source removal, and recovery cases are covered.
- Prometheus results share evaluation by digest.
- Webhook results never cross Run or Promotion boundaries.
- Digest changes invalidate observations and overrides.
- No raw response or credential appears in APIs, logs, or persisted evidence.

## Phase 3: Server Gate scopes and lock Gates

- [x] Add and backfill global, Target, and Stage scope rows.
- [x] Add current scope lock persistence, expiry evaluation, idempotent lock and
  unlock operations, versioning, and audit events.
- [x] Add lock management services, OpenAPI operations, authorization, generated
  clients, and integration tests.
- [x] Project the current lock, version, and recent events on each scope resource.
- [x] Record and expose Run and Promotion lock overrides for the exact scope
  and lock version. Each subject's override collection lists eligible Gates
  and preserves the original lock evidence for earlier decisions.

Acceptance:

- Global and local locks compose correctly while each scope permits only one
  current lock.
- One subject can override one lock without unlocking it for other work.
- Unlocking and re-locking a scope is not satisfied by the earlier override.
- Release and expiry are idempotent and auditable.
- Scope identity survives Target or Stage archival and later reactivation.
- Global mutation requires administrator authority; local mutation accepts
  operator or administrator authority with the new API scope.

## Phase 4: Window Gates

- [x] Implement weekly-window validation, normalization, state evaluation, DST
  behavior, and next-transition calculation.
- [x] Add window persistence and independent scope window versioning.
- [x] Add the window coordinator and fail-closed observation validity.
- [x] Add management APIs and projections.
- [x] Extend per-subject overrides to window Gates using the current window version.

Acceptance:

- Boundary, cross-midnight, timezone, spring-forward, fall-back, update,
  removal, restart, delayed-coordinator, and next-transition cases are covered.
- A stale passing observation cannot release work after its window closes.
- Opening may be delayed by coordinator failure but never occurs early.
- A subject-specific window override matches only the current window version;
  any window update invalidates it.

## Phase 5: Run release integration

- [x] Extend Run scheduling SQL, assignment transaction, scheduling projection,
  queue position, and oldest-eligible metric with Server Gate eligibility.
- [x] Record immutable global and Target Gate evidence in the Run release snapshot
  at assignment.
- [x] Recheck current policy on each Controller poll after relevant Gate changes.
- [x] Add Run API and web projections for all Gate types.

Acceptance:

- Global and Target locks and windows compose by intersection.
- `preview` bypasses Server Gates; `reconcile` and `operation` do not.
- Explicit repository Gates still apply to previews.
- A blocked Run does not starve later eligible work.
- Repository, global window, Target window, global lock, and Target lock
  overrides independently satisfy only their exact Gate.
- Lock-versus-claim, unlock-versus-claim, and window-update-versus-claim races
  resolve atomically.
- Active Runs are unaffected by later Gate changes.

## Phase 6: Promotion release integration

- [x] Replace Promotion Gate-only release with the complete Gate release check.
- [x] Record immutable global and Stage Gate evidence in the Promotion release
  snapshot.
- [x] Apply Server Gates to creation delivery and callback-mediated release at the
  same boundaries used by current repository Gates.
- [x] Show Gate blockers in Promotion and Stage views. Pipeline health and Inbox
  review and failure counts retain their existing meanings.
- [x] Add Promotion API and web projections for all Gate types.

Acceptance:

- Automatic Promotions wait before creation delivery.
- Review-required Promotions may prepare and receive a decision but remain
  `gated` until all Gates pass or have matching overrides.
- Cancellation wins against Gate release under the existing Promotion lock.
- Repository, global window, Stage window, global lock, and Stage lock
  overrides independently satisfy only their exact Gate.
- Lock and window races resolve atomically.
- Active/released Promotions are unaffected by later Gate changes.
- Direct Kargo bypass remains documented and tested where observable.

## Phase 7: Product management and operational polish

- [x] Manage global locks and weekly windows under Administer. Manage local
  policy on each Target and Stage Gates tab, alongside the applicable global
  policy. Readers can inspect policy and its history.
- [x] Show lock and window changes, window state and next transition on scope
  views. Keep the selected editor in the URL and refresh at policy boundaries.
- [x] Show typed observations, policy versions, and override evidence in Run
  and Promotion details, including the evidence frozen at release.
- [x] Show the active global lock and its expiry across signed-in product views.
- Complete list-state labels and source-appropriate troubleshooting affordances.
- Complete metrics, logs, user documentation, configuration examples, local
  development fixtures, and upgrade notes.

Acceptance:

- Product views use one-level card surfaces and canonical URL state.
- Operators can identify every current blocker and its scope.
- Released subjects show immutable Gate evidence rather than current policy.
- Documentation distinguishes Gate types, source configuration, overrides,
  per-subject lock and window overrides, lock release, window changes, and
  Kargo enforcement boundaries.

# Verification strategy

Each phase includes focused unit and integration coverage. The completed change
requires these end-to-end checks:

- Run and Promotion release with every Gate type individually.
- Repository Gate plus global window plus local lock composition.
- Effective and stale overrides for every Gate type on Runs and Promotions.
- A new repository definition, window revision, or lock invalidating or
  bypassing only the intended prior override.
- Multiple simultaneous blockers and deterministic projection ordering.
- Current catalog definition changes while work waits.
- Current Server scope changes while work waits.
- Server restart with fresh, stale, and unavailable evaluator observations.
- Database migration from existing Prometheus Gate evidence.
- Concurrent release, lock, unlock, window update, review, and cancellation.
- API authorization for readers, operators, administrators, and scoped API
  keys.
- OpenAPI validation and reproducible Go, TypeScript, and iOS generated output.
- Strict TypeScript, Go race tests, formatter, linter, and web tests.

Run the repository verification commands after generated output is current:

```sh
task generate
task check
task integration
```

Use focused integration packages during development, but do not treat mocked
HTTP evaluator tests as a replacement for PostgreSQL release-race coverage.

# Completion criteria

The work is complete when:

- Prometheus, webhook, window, and lock Gates appear through one public Gate
  model without erasing their distinct evidence.
- Every Gate type supports a version-matched, auditable, per-subject override.
- Global and local Server Gates govern every Slipramp-mediated mutating release
  path and no preview path unintentionally inherits them.
- Run assignment and Promotion release record Gate evidence atomically with the
  release transition.
- A stale observation, concurrent lock, closed window, changed definition, or
  canceled subject cannot release through a race.
- Existing Prometheus Gate history remains readable after migration.
- Generated clients, web behavior, metrics, configuration, and governing
  documentation describe the same contract.
- The full repository check and integration suites pass.
