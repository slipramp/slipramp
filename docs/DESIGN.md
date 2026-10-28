# Slipramp Design

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. Git supplies versioned operational definitions, container images supply
tools, and Kubernetes supplies disposable execution capacity across one or more
clusters. Slipramp coordinates tools such as Terraform/OpenTofu, pyinfra,
Ansible, and repository scripts rather than replacing them.

This document records the implemented architecture and its governing decisions.
[ROADMAP.md](./ROADMAP.md) tracks unfinished work and deferred choices. The
[README](../README.md) covers configuration, installation, and development;
[OpenAPI](../api/openapi.yaml) is authoritative for HTTP contracts, and
[examples/infrastructure-ops](../examples/infrastructure-ops/README.md) supplies
a complete repository catalog example.

Contracts remain pre-stable. Prefer deliberate breaking changes that update all
callers, fixtures, generated code, and documentation together. Do not introduce
compatibility aliases, legacy routes, migration shims, or dual product behavior
unless explicitly requested.

## Components and authority

| Component             | Responsibility                                                                                     | Boundary                                                               |
| --------------------- | -------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Slipramp Server       | Repository refresh, catalog validation, scheduling, identity, Promotions, notifications, artifact ingestion, API | No kubeconfig, Kubernetes client, or direct Kubernetes API access      |
| Controller            | Outbound command polling, local Jobs, observations, log forwarding, and configured Kargo discovery/materialization/reconciliation | One replica per execution Cluster; namespace-scoped Kubernetes access  |
| Harness               | Wrap checkout and action processes, transfer artifacts, forward signals, preserve exit status      | Injected by Slipramp; runtime images and tools need no Slipramp SDK    |
| Web                   | React Router SSR and browser UI                                                                    | Uses the generated Server client and forwards the browser's session    |
| PostgreSQL            | Authoritative product state, identity, history, metadata, and scheduling coordination              | Stores credential digests and references, never injected secret values |
| S3-compatible storage | Artifact contents, including live log chunks and sealed logs                                       | Only the Server accesses it; RustFS is the local implementation        |

The control plane is the central subsystem; the Server is its deployable Go
process. Kubernetes is an execution backend, not the product database. The
Server and execution Jobs need not share a cluster.

Controllers require outbound access to the Server. Execution Jobs require
outbound access to Git and the Server's execution-artifact endpoints. Execution
clusters need no inbound connection from the control plane. Use HTTPS across
untrusted networks; plaintext HTTP is valid when a trusted encrypted network
such as a tailnet already protects the transport.

## Domain model

| Name             | Meaning                                                                                                        |
| ---------------- | -------------------------------------------------------------------------------------------------------------- |
| Repository       | A Git clone URL and one tracked branch supplying versioned definitions                                         |
| Catalog revision | Immutable validated Runtime and Target definitions from one exact Git commit                                   |
| Target           | A durable operational unit sourced from one repository definition and containing user-defined Actions          |
| Action           | One explicit command with execution settings, parameters, optional triggers, and artifact declarations         |
| Runtime          | Repository-defined portable container environment and credential/process conventions                           |
| Placement        | Server configuration selecting a Cluster, namespace, service account, credentials, resources, and pod settings |
| Cluster          | Named Kubernetes execution destination represented by its in-cluster Controller                                |
| Run              | Immutable action execution snapshot, with independently recorded lifecycle observations                        |
| Artifact         | Durable Run output, including logs, plans, and diff files                                                      |
| Principal        | Durable user or service-account actor authenticated by a session or API key                                    |
| Promotion        | Durable Kargo request, snapshotted policy, preparation, and optional operator decision                         |
| Notification event | Immutable channel-neutral snapshot of one selected durable transition                                           |
| Notification delivery | Per-destination operational state for delivering one notification event                                       |

A Target has a Server-assigned UUID that remains stable while its catalog key,
Repository plus `metadata.name`, remains present or is later reactivated.
Renaming changes that key, so it creates a new Target and archives the old one.
Removing a Target archives it with its last accepted definition and never
destroys infrastructure. Historical Runs retain the Target ID, frozen names,
and catalog revision.

`sync` is an optional recognized Action name. Targets without it remain useful
collections of operations. The Target's desired commit is a relevance marker
for scheduling and Git delivery coalescing, not an applied-infrastructure or
Run checkout revision. Reconciliation checkpoints use only the Action whose
technical name is exactly `sync`; Action intent and other successful Runs do not
participate. Slipramp compares immutable checkpoints and never claims that
infrastructure is continuously in sync.

Every new Run freezes the Target's desired relevance commit independently from
its checkout commit. Exact reruns preserve the original marker, while latest
reruns capture the Target's current marker. A Run pinned by artifact inputs
inherits the producer Run's marker, so a plan from an older desired revision
cannot represent the current one. Multiple inputs must agree on that marker.
Historical Runs created before markers were recorded, and exact reruns of those
Runs, remain unknown; checkout commits are never used to infer or backfill them.

A Target durably references its last attempted and last successful sync Runs.
Assignment advances the attempted checkpoint atomically with the transition
from `queued` to `assigned`, so queued cancellation is not an attempt. A sync
Run finishing `succeeded` advances the successful checkpoint atomically with
its terminal transition. Failed, canceled, and timed-out attempts remain the
latest attempt without replacing an earlier success, and duplicate terminal
observations cannot move either checkpoint backward. Repository refresh may
advance the desired commit while a sync Run is active but never changes that
Run's marker. A later exact rerun of an older revision is a new factual attempt
and, if successful, a new successful checkpoint with the older marker.

For an active Target that still defines `sync`, reconciliation comparison is
`matched` when the last successful marker equals the desired commit,
`different` when both are known and differ, and `unknown` without a trustworthy
successful marker. Archived Targets retain both historical Run references but
always report `unknown`; reactivation retains the history and resumes the
factual comparison against the new desired commit.

Actions are independent executions. Multi-step procedures belong inside the
invoked script, task runner, or container rather than a Slipramp workflow graph.
A Run records its run source; there is no separate invocation domain resource.
Action, Parameter, and Artifact map keys remain their stable technical identities.
Optional Action titles, descriptions, and absolute HTTP(S) documentation links,
Parameter titles and descriptions, and Artifact descriptions are bounded
operator-facing presentation metadata. Runs freeze this metadata with the
selected catalog revision so historical views do not drift.

## Repository and catalog lifecycle

Registrations accept HTTPS, SSH, and SCP-style Git URLs, one tracked branch,
and optional logical refresh and checkout credential names. URL-embedded HTTPS
credentials are rejected. Removing a Repository hides its registration while
preserving its identity and history.

The Server fetches active Repositories at startup and on an interval. It checks
out the exact commit in temporary storage, validates the whole catalog, then
removes the checkout. Repository contents, source bundles, and Git archives are
never persisted in PostgreSQL or S3. Jobs clone their exact commits directly.

Catalog resources use `slipramp.com/v1alpha1`. Files ending in `.yaml` or `.yml`
are discovered recursively below `.slipramp` in lexical path order. Each file
may contain multiple Runtime or Target documents. Unknown fields and duplicate
mapping keys are rejected.

Validation covers paths, resource names and quantities, positive Go-duration
timeouts, commands, intents, parameters, credential injection and bindings,
configured Placements, artifact declarations and provenance, and Git triggers.
Repository paths and `watch` patterns are clean slash-separated relative paths;
container working directories, mounts, and artifact paths are clean, non-root
absolute paths. The `SLIPRAMP_` environment namespace is reserved for the
internal execution contract and rejected in catalog-defined environment names.

Catalog processing is one staged implementation shared by Server refresh and
the `slipramp catalog validate` CLI. Discovery and strict YAML decoding retain a
field-path-to-source index and aggregate stable, source-addressed diagnostics
across independent files and documents. Portable validation covers
repository-owned contracts while accepting unknown, well-formed Placement
names. Explicit deployment validation additionally requires a validated
`PlacementConfig` set and checks Placement existence, credential offerings, and
effective resource constraints. An empty Placement set never selects portable
behavior implicitly. Server refresh always runs both stages and rejects errors;
warnings do not prevent acceptance.

Diagnostic codes are the stable machine interface; messages and hints may
improve without requiring callers to parse prose. Related locations identify
both sides of duplicate or reference errors. Diagnostics sort by repository
path, YAML document, source position, severity, and code so text, JSON, and
SARIF output remain reproducible.

The CLI can render the same sorted diagnostics as compiler-style text,
schema-versioned JSON, or SARIF 2.1.0 without absolute checkout paths. Its
repository-aware warnings cover currently missing Target paths, watch patterns
with no matches, missing relative command files, command files outside the
Target watch scope, and duplicate or completely subsumed watch patterns. Strict
mode changes only the CLI exit result for warnings. Validation remains static:
neither the Server nor CLI contacts Kubernetes, resolves Secret values, or
inspects Runtime images.

A checked-in Draft 2020-12 JSON Schema is generated from the Go Runtime and
Target definitions and checked for freshness. It supports per-document editor
completion and structural feedback while leaving cross-document, resolved, and
deployment rules to the shared catalog validator.

Target defaults supply runtime, placement, timeout, credentials, and resources.
Action scalar overrides replace defaults; credential and resource maps merge
by key. Resolution copies definitions rather than mutating the catalog.
Runtimes and Targets always resolve from the same revision.

The general `cmd/slipramp` binary also provides `catalog explain TARGET ACTION`
for inspecting one effective execution contract in a repository working tree.
It runs the same complete catalog pipeline before resolving the selected Action.
The processed result retains the immutable Placement set used for deployment
validation, so explanation cannot silently resolve against different installation
configuration. Portable explanation resolves repository-owned values and marks
Placement-dependent values, credential sources, and remaining resource defaults
as unresolved. Deployment explanation applies the production Placement parser
and the full validation stage used by Server refresh.

One structured explanation model owns effective values and their provenance:
Action overrides/declarations, Target defaults/declarations, Runtime declarations,
Placement defaults, supplied CLI parameters, and built-in defaults. Repository
origins include source file, YAML document, and field locations. Text and
schema-versioned JSON render this same model, including process directories,
base command and final argv, supplied/omitted parameter delivery, non-secret
environment, credentials by reference, artifact producer/consumer compatibility,
watch patterns, triggers, and checkout diagnostics. Arbitrary Placement pod
configuration and credential contents are excluded.

An authenticated Server endpoint exposes that same model for an Action in an
active repository catalog, resolved against the Server's current Placement
configuration. The Web Action Run page uses the endpoint for its effective
command, process, Runtime, Placement, resource, credential, artifact, and
provenance display instead of resolving Target defaults in TypeScript. This
read-only form-loading explanation may retain required parameters as omitted;
it does not simulate live form values or weaken Run creation validation.

CLI parameter strings are parsed against declarations, then use the Run-creation
normalization and validation rules. Argument/environment delivery, repository
mount/process directory resolution, and resource-map merging are shared with
Controller execution. CLI explanation requires supplied values for required
parameters but does not select artifact contents or create a Run. Both catalog
commands remain offline: no Server/Kubernetes access, image inspection, Secret
lookup, Job rendering, or local Action execution.

The CLI also provides a focused remote operator boundary for listing active and
inspecting durable Targets; explaining active Actions against the Server's
current Placement configuration; listing, creating, inspecting, following,
waiting for, rerunning, reviewing, and canceling Runs; and listing and
downloading Artifacts. It uses the generated OpenAPI client and models for
ordinary requests, with one small handwritten SSE reader for log reconnection
and byte decoding. The Server URL comes from a command flag or `SLIPRAMP_URL`;
the API key comes only from `SLIPRAMP_API_KEY` and is sent as a Bearer
credential. TLS uses the system trust store and the client offers no insecure
mode or persisted context.

Cobra owns the CLI command tree, help, argument validation, and flag parsing.
Command construction is explicit and receives the `App` dependency container;
package-global commands and `init` registration are not used. Catalog behavior,
remote API access, rendering, and log streaming remain framework-independent
command handlers so their contracts can be tested without Cobra internals.

Remote Run creation selects an exact durable Target UUID and Action name. Without
Artifact inputs, the client reads the current Target definition and applies the
shared catalog parameter parser and Run normalization rules before submitting a
generated `RunCreate` request with an idempotency key. Repeatable explicit input
bindings map declared input names to exact Artifact UUIDs. An input-bound CLI
request cannot also supply parameter strings because the selected Artifact may
pin creation to an older catalog revision whose parameter declarations are not
available through the remote CLI contract. The Server remains authoritative,
resolves the producing revision, and validates input retention, provenance,
compatibility, completeness, and parameter defaults.

Remote Target explanation returns the Server's authoritative effective Action
contract with current Placement defaults and references. It allows required
parameters to remain omitted for inspection and does not accept parameter
simulation; Run creation remains the strict parameter-validation boundary.

Exact and latest reruns require an explicit mode and use the Server's ordinary
idempotent rerun boundary. Run review approval and rejection use the dedicated
review endpoint and optional normalized immutable comments. These mutations,
ordinary reads, and cancellation return the canonical generated Run
representation; text rendering is only a terminal projection of that response.

Target and Run listing support explicit offset pagination. Their opt-in `--all`
mode follows every page at or after the requested offset using `--limit` as the
page size, then renders one aggregate result. Aggregate JSON retains the
generated list envelope: `items` contains every fetched record, `limit` is the
request page size, `offset` is the initial offset, and `total` is the Server's
matching total. Run listing exposes the Server's exact Repository, Target,
commit, input Artifact, Cluster, phase, and review-state filters. Text output
preserves phase, outcome, review state, and observation freshness as separate
facts; ordinary single-page JSON returns the generated `RunList` unchanged. Run
waiting polls the ordinary generated Run read until the canonical record is
finished, with an optional client-side timeout and a configurable poll interval.
It writes the terminal Run before returning success for `succeeded` or a
dedicated unsuccessful-Run exit code for `failed`, `canceled`, and `timed-out`.
Client cancellation interrupts both the request and the interval wait.

Artifact listing returns the generated Artifact metadata for one exact Run.
Downloads require an explicit output path or standard output, stream the raw
response body, and verify the required SHA-256 `Digest` header. File output uses
mode `0600`, refuses to replace an existing path, and removes partial or
digest-invalid contents. A standard-output download may already have emitted
bytes before a terminal digest mismatch is known and therefore returns nonzero.

The log command writes decoded chunk contents directly to standard output and
keeps diagnostics on standard error. SSE event IDs and generated `LogEvent`
offsets are checked against the durable byte cursor. Retryable disconnects
resume from that cursor and overlapping bytes are suppressed, while permanent
HTTP failures are not retried. A sealed event is successful; a failed event is
an explicit incomplete-log result after the retained prefix is written. Context
cancellation interrupts both the HTTP request and reconnect wait. This boundary
does not add infrastructure-command retries, Kubernetes access, browser login,
credential persistence, or a general SSE framework.

A valid commit creates or reuses an immutable catalog revision. An invalid
commit records its SHA and a bounded safe diagnostic summary while the last
valid revision stays active; it does not advance Target state or create Runs.
Changed paths between accepted heads match Target `watch` patterns, including
recursive `**`, to advance each affected Target's durable desired commit. The
Web presents this as the Target's last relevant change rather than a convergence
status. New and reactivated Targets begin at the commit where they become active;
removed Targets archive. Repository refresh indexes Git; target reconciliation
executes optional `sync`. Watch patterns alone never start execution.

Catalog revision reads contain only immutable definitions and revision metadata.
The active catalog view pairs that revision with mutable Target state, read from
one repeatable-read PostgreSQL snapshot so refresh cannot mix different accepted
heads. A Target's desired commit may remain older than the active catalog commit
when its watched paths did not change.

### Parameters

Parameters are `string`, `boolean`, `number`, or `enum`, with required and enum
constraints validated before Run creation. Each maps to exactly one environment
variable or argument. An empty `argument: {}` is positional. Named string,
number, and enum arguments emit a name followed by the value; named booleans
emit a flag only when true. Positional and environment booleans use `true` or
`false`. Arguments append in lexical parameter-name order. Unset optional
parameters emit nothing. Commands are explicit argument arrays; shell behavior
must be requested explicitly.

The Web uses an optional Parameter title as its form label while continuing to
show the technical parameter name, its optional description, required state,
and delivery convention.

### Commit browsing

Commit metadata is presentation data, independent of exact SHAs required for
execution. Repositories select `disabled`, default `observedHeads`, or
`trackedBranchHistory`. Full-history indexing runs asynchronously, records
subject, author, author time, and commit time, catches up incrementally, and
replaces tracked-branch membership after a force push. Its durable progress and
failures do not block refresh or turn historical commits into catalogs or
trigger deliveries. Disabling collection hides the Repository from commit
browsing while retaining collected metadata for resumption. Provider links are
inferred for known public providers or supplied through a commit URL template.

A commit detail resolves any known commit in an active Repository by exact
Repository ID and full SHA, including commits retained only because product
state references them. It presents paginated associated Runs pinned to that
commit and Promotions where the commit is either the base or candidate
revision. Commit-scoped Run and Promotion filters require Repository identity
so equal object IDs in different Repositories never merge their histories.

## Run creation and scheduling

UI, API, Git delivery, and Promotion preparation use the same Run-creation
service. Requests select the durable Target ID and Action name; the service
resolves the Target's active repository catalog, validates parameters and
artifact bindings, accepts an optional idempotency key, and freezes the
Repository, checkout commit, independent Target relevance commit, catalog
revision, Target, Action, Runtime, Placement, source, and actor. Authenticated
request idempotency is scoped to the submitting Principal. Historical creator
and cancellation attribution includes both a Principal reference and an
immutable actor snapshot.

Promotion preparation Run insertion and linkage to its requesting Promotion
commit in the same transaction, so an ambiguous creation response cannot leave
an executing Run detached from preparation recovery.

A Controller claims the oldest eligible queued Run for its Cluster. PostgreSQL
coordinates one active Run per Target, a default maximum of four active Runs
per Cluster, optional tighter Placement maxima, and ordinary FIFO scheduling
among Runs that satisfy those constraints. Capacity is current deployment
policy rather than part of the immutable Run snapshot. Infrastructure commands
are never retried implicitly.

Queued Run reads derive a point-in-time scheduling explanation without
persisting mutable queue state. `awaiting-review` precedes `target-busy`, which
links the active Run serializing the same Target. Otherwise an unreachable
Controller yields `cluster-unreachable`, exhausted Cluster or Placement capacity
yields `cluster-capacity`, and a claimable Run is `eligible`. Queue age and an
eligible FIFO position share an explicit snapshot time and are historical facts,
not execution estimates. The position is present only when current review,
Target, and capacity constraints permit assignment.

Lifecycle phase (`queued`, `assigned`, `preparing`, `running`, `finished`),
terminal outcome (`succeeded`, `failed`, `canceled`, `timed-out`), cancellation
intent, Job identity, exit code, diagnostics, and observation freshness are
separate facts. A disconnected Controller or stale observation derives an
unknown observation; it does not change phase or fail an active Run.

The Run package owns Controller observation rules for phase progression,
pinned Job identity, repeated terminal outcomes, and cancellation intent.
PostgreSQL reads the current facts under a row lock and invokes that domain
validation before writing the observation and phase history in the same
transaction. Callers do not supply their own allowed predecessor phases.

Finished Runs support two explicit rerun modes. Exact reruns copy the frozen
execution snapshot, checkout commit, Target relevance marker, catalog revision,
parameters, and retained inputs. Latest reruns resolve the same Target, Action,
and parameter values against the active catalog and current Placement
configuration and capture the current Target relevance marker. Latest reruns
reject artifact inputs because inputs pin consumers to their producing revision.
Both modes create a new Run linked to its immediate parent and derive source and
actor from the new request boundary.

### Git triggers

Named Action triggers use `git: {}`, optional static `parameters`, an optional
`review: required`, and the Target's required `watch` patterns. Trigger names
are unique within an Action. Triggered Actions cannot have artifact inputs.
Catalog loading validates and normalizes static parameters with the ordinary
Run parameter rules. Omitting `review` preserves direct execution.

After initial indexing, accepting a changed head atomically activates the
catalog, updates Targets, and records one durable delivery for each matching
trigger. Unchanged or rejected heads create no deliveries. Delivery is per
accepted head, not per underlying commit in a multi-commit fetch. Each delivery
freezes the accepted revision, normalized parameters, and whether review is
required. Run creation snapshots that requirement as Run-owned review state.

A review-required Git Run remains `queued` with review state `pending` and is
ineligible for assignment. Review state is orthogonal to lifecycle phase and
outcome: `not-required`, `pending`, `approved`, or `rejected`. Approval records
one immutable Principal-attributed decision, including an optional comment of
up to 4,096 characters, and makes the existing queued Run eligible for ordinary
FIFO assignment; it does not assign or execute the Run. Rejection atomically
records the decision and optional comment and finishes the undispatched Run as
`canceled` with reason `ReviewRejected`. The operator role and dedicated
`slipramp:runs:review` API-key scope decide reviews. The review scope does not
grant Run creation or cancellation, and `slipramp:runs:write` does not grant
review decisions.

Review decisions are accepted only for queued pending Runs. Repeating the same
decision, Principal, and comment is idempotent while an approval remains
queued; a rejected decision is also idempotent because its own transition
finishes the Run. A different decision, Principal, or comment, or a decision
after cancellation, supersession, assignment, or another terminal transition,
conflicts. Ordinary cancellation retains `CancelRequested`. Exact and latest
reruns are ordinary manual or API requests and do not inherit the parent Run's
review requirement or decision. Run history supports an exact review-state
filter, including a stable `reviewState=pending` URL for operational review
lists.

The Web Run detail presents pending review separately from execution state and
offers operator-only Approve and Reject controls alongside ordinary Run
cancellation. It retains the deciding Principal and time for approved and
rejected Runs and displays any immutable decision comment. Run history and
recent-Run tables label queued pending Runs as awaiting review; after approval
they return to their ordinary queued or active execution presentation. The
operational Inbox includes queued pending Runs for Principals authorized to
decide them and links to the Run detail. A decision conflict refreshes the
canonical Run while retaining the API diagnostic for the operator.

Automatic Git work uses latest-wins coalescing per Repository and Target. When
an affected Target's desired commit advances or the Target is archived, the
same transaction supersedes older undispatched deliveries and finishes queued
Git Runs as canceled with reason `SupersededByGitChange`, including phase
transitions. Superseded deliveries remain history. Assigned, preparing, and
running Git Runs finish normally; manual and API Runs are never superseded.
Pending and approved-but-still-queued Runs receive the same latest-wins
treatment; approval never protects stale work.

The dispatcher pins ordinary Run creation to the delivery revision and uses its
stable delivery ID for idempotency. At insertion, PostgreSQL locks and reads
current Target state, refusing a pinned commit that no longer equals the
desired commit and superseding its delivery. This closes the refresh/dispatch
race at the durable boundary. A Git source contains the trigger name and
delivery ID and has no human actor. Run insertion and delivery linkage commit
atomically, including idempotent recovery after an ambiguous commit.

Failed Run-creation attempts persist their count, safe diagnostic, and next due
time in PostgreSQL. The single dispatcher drains other eligible
deliveries after a failure, choosing creation order and ID among due deliveries.
Backoff starts at five seconds, doubles to a five-minute cap, and becomes
terminal on the eighth failure. Restarts retain both the count and due time.
A newer desired commit can supersede a pending delivery waiting in backoff;
terminal failures remain failed history. Storage failures stop the current drain
until the ordinary dispatcher tick. There is no error-classification policy,
jitter, per-trigger configuration, or manual delivery retry.

Delivery retries only create the immutable Run. Slipramp never implicitly
retries the resulting infrastructure command, even when that Run fails.
`GET /api/v1/git-trigger-deliveries/failed` lists terminal failures with ordinary
authenticated read permission, bounded limit/offset pagination, and
newest-failure-first ordering. It returns Repository identity, revision and commit,
Target/Action/trigger, attempt count, failure time, and a fixed non-sensitive
diagnostic (at most 512 characters); underlying error text and parameter values
are never persisted.
The operational Inbox shows these failures and links to the Target, retaining
its partial-source and result-limit notices. Additional trigger forms remain on
the [roadmap](./ROADMAP.md#git-trigger-extensions).

## Controller and execution protocol

A Controller has a stable Cluster name, a cluster-scoped enrollment credential,
and a separate Controller-owned bearer credential. Startup enrollment registers
only the latter's SHA-256 digest. Valid enrollment is auto-approved; repeating
the same credential is idempotent, while replacement is rejected. Subsequent
requests derive Cluster identity from that bearer credential. Authenticated
poll/report traffic updates last-seen time without a separate heartbeat
protocol. Cluster reachability becomes stale after one minute without traffic.

Delivery is at least once over a small versioned HTTP API. Long polls return
assignment or cancellation envelopes. Deterministic Run and Job identifiers
make retries idempotent without brokers, bidirectional RPC, distributed leases,
or leader election. Assignment remains `assigned` until the Controller reports
the materialized Job UID and `preparing` phase. Permanent rendering or admission
failures finish the Run as failed rather than blocking delivery.

The Server's Controller command scheduler selects Run cancellations, then due
Promotion creation, then due pre-callback Promotion reconciliation, then Run
assignments, composing existing Run and Promotion values. The HTTP adapter
owns authentication, long polling, and generated response mapping, including
assignment artifact credentials.

Controller polling dispatches Run work to one local worker per Run before Job
materialization begins. The Controller defaults to four concurrent new Run
workers and may be configured with another positive bound. The Server remains
authoritative for Cluster and Placement capacity; the local bound is defense in
depth, and assignments beyond it wait before Job materialization. Recovered Jobs
are always observed even when they exceed a newly lowered local bound. Each
worker owns materialization, observation, log completion, terminal reporting,
and cleanup. Duplicate assignments reuse the worker, and repeated cancellation
commands coalesce without blocking polling. Completed workers remain registered
through an in-flight poll response so a late assignment cannot recreate a
finished Job.

Promotion creation and pre-callback reconciliation have a 15-second Kubernetes
operation budget per delivered command. Server reporting retains the Controller
context, so an expired creation attempt can still report a retryable result.
An expired reconciliation attempt leaves its durable inspection schedule intact.
These commands run inline; polling resumes after the bounded operation and report.

Controller HTTP failures have explicit operational categories. Transport
failures, HTTP 408/429, and Server errors retry; invalid requests, unknown
resources, conflicts, and unusable log state stop the affected Run worker with
a diagnostic while retaining its Job. A stopped worker suppresses duplicate
commands until Controller restart. Credential rejection stops the Controller
and interrupts polling, including rejection from background log or inventory
requests. Malformed polling responses fail the Controller rather than silently
discarding work.

Kargo creation and reconciliation reports use the same failure categories.
Transport failures and HTTP 408/429/5xx leave durable Server scheduling to
redeliver the command. Permanent report rejections, including unknown Promotion
identity, conflicting Kargo name or UID, invalid requests, and malformed responses,
stop the Controller with the Promotion ID and rejection diagnostic. Kubernetes
resources remain available for inspection and recovery. Valid delayed or repeated
reports remain idempotent on the Server, including callback races.

Every execution namespace is exclusively owned by one Controller and its
control plane. A Controller may own several namespaces, rejects assignments
outside its configured list, and scans those namespaces individually before
polling after restart. Another installation may reuse a namespace only after
all previous Slipramp Jobs are removed. Duplicate Controllers or reused Cluster
names are configuration errors, not a failover mechanism.

Every Job operation verifies the owned namespace, deterministic name, Run and
Cluster labels, and admitted Job UID. Once a worker pins that UID, it never
adopts a replacement. Pod observation, log reads, and cancellation also require
matching labels and a controlling owner reference to that exact Job UID.
Identity mismatches stop the affected worker and retain resources for diagnosis.
Original owned Pods may still supply logs after their pinned Job disappears.

### Job and Harness contract

Each Job is `slipramp-run-<run-id>` with stable Run and Cluster labels,
`restartPolicy: Never`, `backoffLimit: 0`, and an execution deadline. A pinned
Harness image installs the static executable into an `emptyDir`. A pinned
checkout image containing shell, Git, and SSH then runs the Harness around
checkout of the exact commit into a second `emptyDir`. The Action container
mounts the repository at Runtime `workingDirectory` or `/workspace` and runs
from the Target's repository-relative path through the injected Harness.

Checkout makes the Pod-private tree writable across unrelated container user
IDs. Placement security context, including `fsGroup` where needed, provides
access to restrictive projected Secret files. Pod extensions cannot replace
Slipramp-owned containers, init containers, restart policy, service account,
node selector, or deadline.

The Harness downloads selected inputs before execution, connects the child to
standard streams, forwards signals, preserves exit status, and uploads declared
outputs. Its Run-scoped transfer credential is removed from the child
environment. Artifact-transfer failure makes an otherwise successful execution
fail and never hides a nonzero child exit code.

### Cancellation and recovery

Queued cancellation finishes immediately. Controllers receive cancellation
before new assignments. For a started Pod, a Controller-owned annotation is
projected into checkout and Action containers. The Harness sends `SIGTERM` to
the active child and force-stops it after a grace period. Ordinary terminal
observation then drains and closes logs, reports the outcome, and deletes the
Job with its observed UID as a precondition. Unstarted Jobs are deleted before
reporting an empty canceled Run.

For every terminal Job, log closure and accepted terminal reporting precede
UID-checked deletion and stopping observation. Complete logs seal with their
size and digest. If a terminal Job remains after its Pod and log source have
disappeared, the Controller marks a nonempty durable prefix failed, reports the
Job's terminal outcome, and then deletes the Job. Failed prefixes remain
streamable as explicitly incomplete logs but are not downloadable artifacts.
Both sealed and failed closure are idempotent and resume after restart.
Transient storage, reporting, or deletion failures keep the Job available for
recovery. Kubernetes TTL cleanup is disabled so outages cannot delete the only
remaining log source. A `404` for the Run or log violates namespace ownership:
warn once, stop observing, and leave the Job for diagnosis.

## Credentials and trust

Execution credential injection follows conventions tools already understand:

1. A Placement exposes logical sources backed by cluster-local Kubernetes
   Secrets or native workload identity.
2. A Runtime declares logical requirements and environment, `envFrom`, file,
   or directory injection conventions.
3. A Target binds those requirements to Placement sources; Actions inherit or
   extend those bindings.

Each Runtime credential has at least one injection. Environment names and
mount paths cannot collide. Secret file modes may use permission bits through
`0777`. Placement pod extensions support projected tokens, CSI volumes, and
other cluster-specific mechanisms; a service account may provide credentials
without container injection. Jobs reference Secrets directly; their values are
never copied into product state.

Git uses separate credential paths. Server refresh credentials resolve to
administrator-mounted Git configuration, not Kubernetes lookups. Controller
checkout mappings resolve logical names to local Secrets with conventional
`gitconfig`, `ssh-privatekey`, and `known_hosts` keys, mounted only for checkout.
Slack notification destinations similarly resolve administrator-mounted files.
The Server validates each file as an absolute HTTPS webhook URL at startup but
persists only the destination name and type. Webhook URLs and secret-file paths
are absent from PostgreSQL, structured logs, and the product API.

Repository maintainers execute code with the authority granted by a Placement.
Containers do not provide a complete security boundary. Safety policy is
opt-in: direct actions, including unreviewed `terraform apply`, remain valid.
Preview artifacts, repository-controlled Git-trigger review, and Kargo review
policy add optional controls to this model. A repository maintainer can remove
`review: required`; Run review is therefore a voluntary operational guard, not
a Server-owned security policy, separation-of-duties boundary, or source-wide
enforcement mechanism.

## Logs and artifacts

The Controller follows Pod output once, briefly coalesces small source reads,
and posts chunks of at most 256 KiB with monotonically increasing byte offsets.
The Server writes S3 objects before committing their digest and offset metadata
in PostgreSQL. Retrying the same bytes at the same offset is idempotent.
Recovery reads the durable cursor and replays only to verify and skip the
accepted prefix. The Controller closes logs before terminal observation:
complete logs seal with final byte size and SHA-256, while an irrecoverable
prefix enters the failed state without a digest.

Live logs use reconnectable SSE whose event ID is the next durable byte offset.
Live viewing and retained capture share the same ingestion path. Pod logs are
the source during execution, never the durable record.

The artifact service exposes a cancellable pull stream of bounded log-byte
batches and offsets, coalescing available durable chunks while owning polling
and cursor validation. The HTTP adapter encodes the generated log-event payload
and SSE framing, distinguishing sealed completion from a failed partial log.
Chunks are read as the response consumes them; the generated response writer
flushes output and closes the stream on completion or disconnection.

Assignments include a deterministic, narrowly scoped artifact credential
derived from a Server HMAC key. The Harness may download only explicit input
bindings and upload only frozen Action output names. The Server streams bytes
to/from S3; PostgreSQL stores producing Run, name, media type, lifecycle, size,
digest, sensitivity, object key, retention, and provenance.
Artifact declaration descriptions are presentation metadata read from the
frozen Run snapshot for output and input views; they do not alter artifact
identity, compatibility, retention, or transfer behavior.

Inputs explicitly select a particular retained Artifact. `fromAction` must
name an Action that declares the input's name as an output. The supported
compatibility rule is `exact-catalog-revision`, also the default. Consumers are
pinned to the producer's Target, commit, and catalog revision. A successful
preview exposes **Apply this plan** when that revision has a directly runnable
consumer. Direct `sync` or apply remains independently available.

One installation-wide retention period applies to sealed Artifacts. The Server
marks eligible metadata `expired`, deletes objects idempotently, then marks it
`deleted` only after all deletes succeed. Artifacts produced by or bound to
unfinished Runs, and protected by nonterminal Promotions, cannot expire.
Metadata and provenance survive deletion. The same reconciler removes orphaned
objects in the owned `runs/` prefix after a grace period and an immediate
PostgreSQL recheck. S3 lifecycle rules do not independently expire contents.

## Product identity and authorization

The Server owns product authentication. `/api/v1` uses product sessions or API
keys except the OIDC login/callback boundary; `/controller/v1` and
`/execution/v1` retain independent Cluster and Run credentials. Execution
credentials never become product Principals. `/healthz` and `/readyz` expose
only non-sensitive unauthenticated availability information.

Principals are users or service accounts with a stable UUID, display name,
global role, and disabled state. OIDC identities use `(issuer, subject)`;
email and profile names never link identities. Credentials authenticate a
Principal and restrict its role; they do not own grants. Domain services
consume the authentication result rather than parsing cookies or claims.

### OIDC and sessions

One configured OIDC provider is required at startup. The Server uses code flow
with PKCE, state, nonce, issuer, signature, audience, and time validation. An
explicit subject-to-role configuration enrolls users and reapplies their role
on login; unknown subjects are rejected and at least one administrator mapping
is required. Removing a mapping prevents new logins but does not revoke an
existing session before its fixed expiry or revocation.

Opaque local sessions store only SHA-256 digests. The HTTP-only
`slipramp_session` cookie has a default absolute lifetime of 12 hours with no
idle extension. Provider access and refresh tokens are not retained. One-time
login transactions live in memory, so an interrupted login restarts after a
Server restart. A matching HTTP-only SameSite=Lax state cookie accompanies the
flow. Cookie state changes require CSRF protection, including
`X-Slipramp-CSRF: 1` for logout.

The Web forwards only the Slipramp session for SSR calls, SSE, and artifact
downloads and proxies same-origin login/callback requests. It never uses a
deployment-wide privileged key on behalf of users. The Server remains the
authorization authority even when the UI hides unavailable actions.

### API keys and permissions

API keys are `slipramp_key_` plus unpadded Base64URL encoding of 32 random bytes,
displayed once. PostgreSQL retains a SHA-256 digest, display prefix, scopes,
expiry, last use, and revocation. Disabled Principals and expired or revoked
credentials cannot authenticate. User keys are created only from the user's
browser session. Administrators create service accounts and their keys through
normal authenticated operations. Rotation is create, update clients, revoke.
There is no bootstrap API key or anonymous bypass: the first administrator
signs in through OIDC.

| Operation                                                                        | Minimum role             | API-key scope                |
| -------------------------------------------------------------------------------- | ------------------------ | ---------------------------- |
| Read product state, history, logs, artifact metadata and non-sensitive downloads | viewer                   | `slipramp:read`              |
| Create, rerun, or cancel Runs                                                     | operator                 | `slipramp:runs:write`        |
| Decide Run reviews                                                               | operator                 | `slipramp:runs:review`       |
| Acknowledge Inbox problems                                                       | operator                 | `slipramp:inbox:write`       |
| Submit or poll Kargo Promotions                                                  | operator service account | `slipramp:promotions:submit` |
| Create or decide Promotions, or change Stage policy                              | operator                 | `slipramp:promotions:write`  |
| Administer Repositories, service accounts, and their keys                        | admin                    | `slipramp:admin`             |

Scope restrictions intersect with the Principal's role. `slipramp:admin` grants
all permissions subject to role; other scopes do not imply one another.
Sensitive metadata is readable by viewers, but contents require operator or
admin and, for API keys, read scope. Owners can revoke their keys; administrator
revocation requires the administrator role and scope. Historical Principals
and keys are disabled/revoked rather than deleted; expired sessions may be
removed.

Run source and actor are distinct. The Server derives `manual` for session
requests and `api` for API-key requests. Git sources have no human actor;
Promotion preparation uses a `promotion` backlink and snapshots the requesting
service account. Source cannot be chosen in the ordinary Run request body.

## Kargo Promotions

Kargo remains responsible for rendering, pushing candidate branches, pushing
the approved commit to the target branch, and Argo CD reconciliation. Slipramp
owns inventory, optional review policy, preparation artifacts, and decisions.
Approval records permission to proceed, not successful downstream execution.
Kargo remains authoritative for Freight availability, soak requirements,
verification, promotion windows, and Promotion admission. Slipramp does not
precompute those rules or cancel or delete Kargo Promotions after creation.

### Stage inventory and policy

An optional Controller observer lists every page of Stages in explicitly
configured namespaces and submits complete authenticated snapshots. Any list
failure prevents submission. Repeated snapshot IDs are idempotent; successful
snapshots archive missing Stages while retaining policy and history. Observation
failure ages Stage freshness without disconnecting ordinary Run delivery.
Stage identity is `(integration, project, name)`, with project equal to the
Kubernetes namespace; the authenticated Controller supplies Cluster identity.

Named integrations are Server startup configuration: Cluster, optional Kargo UI
URL, Repository allowlist, ordered environments, preparation Repository/Target/
Action, maximum observation age, pending-review retention, and optional
candidate-branch cleanup policy. The Server
matches normalized observed repository URLs against that allowlist. The
Controller derives application, environment, repository, target branch, source
paths, typed current Freight, requested-Freight origin, direct-Warehouse
allowance, declared source Stages, and successful-render acknowledgement from
Stage data. The adapter recognizes `app`, `argoApp`, `overlay`, `gitopsRepo`,
`kustomizePath`, `stageBranchKustomization`, and
`slipramp.com/promotion-source-paths` conventions. Environment order is only a
presentation setting and never implies Freight topology.

Stage policy defaults to `automatic`. `review-required` optionally expires,
after which new requests evaluate as automatic while the old policy remains
visible. Acceptance snapshots effective policy; later changes never alter an
existing Promotion. Policy edits retain actor attribution and reject archived
Stages.

Stage inventory reads apply filters, lexical ordering, total counts, and
pagination in PostgreSQL. The requested page includes stored policies and
pending Promotion counts without loading Promotion history or querying policies
individually. The Promotion domain evaluates every policy on that page at the
same request time, including the automatic default and expired policies.

### Request, creation, preparation, and decision

`POST /api/v1/kargo/{integration}/promotions` requires a service-account API key
with submit permission. Kargo provides external identity, project, Stage, full
base and candidate Git object IDs, and a candidate branch under
`kargo/promotion/`. The Server resolves the remaining context from the current
Stage and rejects unknown, archived, stale, or ineligible observations.

The unique `(integration, externalPromotionId)` plus canonical request digest
makes identical repeated submissions/polls recover one Promotion; conflicting
reuse returns `409`. Persisting the request, actor, and snapshots precedes Run
creation. Domain states, including terminal errors, return the same machine
response shape rather than masquerading as transport errors.

Repeated callbacks validate the request digest and expire the matching pending
Promotion at the acceptance time in the same row-locked transaction. Concurrent
insertion recovery retries through that path. Callback handling does not sweep
other Promotions; background and review expiry remain separate entry points.

Promotion Stage, Repository, policy, and selected Freight snapshots are typed
domain values. Stage inventory and frozen snapshots share typed Freight and
render acknowledgements. PostgreSQL encodes and decodes their JSONB at the
storage boundary; services and Inbox projections use typed facts directly.
OpenAPI defines the corresponding response shapes, and HTTP maps the domain
values without JSON round trips. Snapshot creation copies nested mutable data.

`Promotion` is the only user-facing lifecycle for both Kargo-originated and
Slipramp-originated requests. Its source records which path created it; no
parallel start or invocation resource exists. Kargo-originated Promotions begin
with callback data and retain service-account attribution. Slipramp-originated
Promotions are durable before the Kubernetes object exists and retain the
human operator as `requestedBy` throughout callback correlation.

`POST /api/v1/promotion-stages/{stageId}/promotions` accepts an exact observed
source Stage ID and Freight name from an operator with Promotion write
permission. The browser never supplies trusted integration, Cluster, namespace,
project, Stage name, or UID coordinates. In one transaction the Server resolves
those values from durable observations, requires fresh active source and target
Stages in the same integration, Cluster, and Kargo project, and confirms the
source currently contains the exact Freight. A cross-Stage request must match
the target's declared source Stage for that Freight origin; an in-place request
must use the target as its source. The transaction snapshots the target,
Repository, effective policy, selected Freight, and actor.

Each request creates one Kargo Promotion for one exact Freight and one target
Stage. The durable record uses source `slipramp`, snapshots the source Stage ID
and selected Freight, and uses
`<stage-name>-slipramp-<promotion-id>` as its provisional external name. Kargo
admission may replace that name; the Controller reports the admitted name and
Kubernetes UID, and the Server atomically makes that name authoritative before
callback correlation. The record starts in `creating`;
request digest, base and candidate revisions, and candidate branch remain absent
until callback hydration. Dispatch attempts, next and last attempt times,
successful dispatch time, Kargo UID, next and last reconciliation times, and
bounded diagnostics remain on the Promotion rather than a second public
resource.

The lifecycle is:

```text
creating --Kargo callback--> preparing --> pending --> approved/rejected/expired
    |                              `-----------------------------> error
    `--terminal creation failure or pre-callback Kargo phase---> error
```

An automatic-policy callback moves directly from `creating` to `approved`.
`creating` covers queued Controller delivery, bounded retries, and the period
after Kubernetes creation while waiting for Kargo to submit callback data.

Promotion creation and pre-callback reconciliation share Controller command
scheduling with Run delivery. Claiming creation work atomically advances its
attempt and next-attempt time under a fixed bounded backoff policy. The Server
never uses Kubernetes or Kargo clients; a Controller configured for the
integration and project namespace
verifies the snapshotted Stage UID and creates the Kargo resource with
`slipramp.com/promotion-id: <promotion-id>`, the target Stage, and the exact
Freight. Before creation or after an ambiguous result, it finds the object by
that stable annotation and adopts only an object whose annotation and spec
match. It durably reports `created`, `retryable`, or `failed` with the admitted
Kargo name and UID or a bounded diagnostic. Object creation and result
reporting are idempotent, so an ambiguous poll or report is safe. Transient
Kubernetes and transport failures back off without blocking later eligible
work; invalid or forbidden admissions and an exhausted retry budget move the
Promotion to `error`. The namespace Role permits Stage get/list/promote and
Promotion create/get/list only; no Kargo API
credential, impersonation, Job, broker, or second Controller channel is used.

Successful materialization starts a durable fixed-interval reconciliation
schedule while the Promotion remains `creating`. Each claim moves its next due
time before delivery, so a lost command, lost report, Server restart, or
Controller restart only delays another inspection. The responsible Controller
lists the project namespace, finds exactly one resource by the stable
`slipramp.com/promotion-id` annotation, and requires both its Kubernetes UID and
canonical admission-assigned name to match the durable identity. Nonterminal
and missing phases leave the Promotion scheduled. If the resource reaches
`Failed`, `Errored`, `Aborted`, or `Succeeded` before callback hydration, the
Controller reports the terminal phase and a bounded diagnostic over the same
authenticated outbound API.

The ordinary Kargo callback hydrates a matching Slipramp-originated Promotion
only while it is `creating`. It verifies the snapshotted project and Stage,
fills the request digest and Git facts, preserves the original actor and all
creation snapshots, clears reconciliation scheduling, and advances to
`preparing` or `approved` according to the snapshotted policy. Callback
hydration, creation reporting, and terminal reconciliation reporting lock the
same Promotion. If the callback wins, a late terminal report is an idempotent
no-op; if a terminal report wins, it atomically moves the still-unhydrated
Promotion to `error` and the callback conflicts. A late successful creation
report only adds the canonical Kargo name, UID, and delivery time after a
callback. Repeating an identical callback or terminal report is idempotent;
different callback content or a mismatched canonical name or UID conflicts. A
Kargo-originated callback with no existing record continues through the
ordinary insertion path.

Automatic requests become `approved` without a Run. Guarded requests become
`preparing` and dispatch exactly one restart-safe, revision-pinned ordinary Run.
Preparation dispatch and completed-output reconciliation isolate failures per
Promotion. Known invalid requests, configuration, or artifacts fail immediately
with fixed non-sensitive diagnostics. Other failures persist a retry count and
next attempt, using eight failures with exponential backoff from five seconds
to a five-minute maximum. Waiting items do not block later eligible work;
exhaustion moves the Promotion to `error`. Successful Run creation resets the
failure budget for output reconciliation. These retries never rerun an
infrastructure command.

The configured Action receives these string parameters, passed as named flags
to the tools command: `promotion-id`, `external-promotion-id`, `project`,
`stage`, `application`, `environment`, `repository-name`, `repository-url`,
`base-revision`, `candidate-revision`, `candidate-branch`, `target-branch`, and
`source-paths` (a JSON array). These values come from the accepted snapshots,
not mutable Stage context at execution time.

The first-party `slipramp-kargo-tools prepare` command uses pinned Git and Argo
CD tools with read-only credentials. It verifies the candidate ref, compares
exact revisions, and emits deterministic bounded files:

| Artifact    | Media type                                  | Default path                    |
| ----------- | ------------------------------------------- | ------------------------------- |
| `git-diff`  | `text/x-diff; charset=utf-8`                | `/slipramp/artifacts/git.diff`  |
| `argo-diff` | `application/vnd.slipramp.argocd-diff+json` | `/slipramp/artifacts/argo.json` |

Both are sensitive and limited to 16 MiB each during Server validation. An
empty Git diff is a zero-byte file; empty Argo output is `{"resources":[]}`.
Structured resources identify group, kind, namespace, name, diff, and whether
the change is version metadata only. The tools producer and Server validator
share one typed Argo diff format, including its media type and strict JSON
validation. Argo CD targets come from the Runtime
credential file `/run/slipramp/credentials/argocd/targets.json`, keyed by
normalized environment, with `server`, `authToken`, and optional `plaintext`
and `grpcWeb`. Unknown environments fail closed. Tokens use child environment
rather than command arguments. The tools image has no Slipramp API integration;
the Harness handles transfer.

Successful preparation and artifact validation transition to `pending`;
failed Runs or missing, malformed, oversized, expired, or deleted artifacts
transition to `error` without command retry. Only pending Promotions accept an
immutable operator `approved` or `rejected` decision. An identical retry by the
same actor returns that decision; conflicting outcome, actor, or comment is
`409`. Pending reviews expire durably to `expired` at their deadline. Automatic
approval and expiry do not invent human decisions or actors.

Kargo pushes the approved candidate to the target branch without forcing, then
continues `argocd-update`. If the target moved, Git rejects the stale push.
Preparation never mutates Git refs, and neither the Server nor preparation
executes Kubernetes operations. The only Server-side Git mutation is the
explicitly configured terminal candidate-branch cleanup described below.

### Candidate branch cleanup

Candidate branch cleanup is opt-in per Kargo integration. Each enabled
Repository selects a write-capable Git credential from the Server's mounted
repository credential pool and a retention period. On the repository refresh
interval, the Server lists remote `kargo/promotion/*` refs and joins that
inventory with durable Promotions. PostgreSQL remains authoritative for
Promotion state; the remote is authoritative for which refs currently exist.

A branch is eligible only when every referencing Promotion is terminal, the
latest terminal update plus retention has elapsed, and the current remote tip
equals a candidate revision recorded for that branch. A unique
`(repository, candidate branch)` constraint prevents a new Promotion from
reusing an old branch during cleanup. Immediately before deletion the Git
adapter rereads the exact ref, then pushes a deletion guarded by
`--force-with-lease=<ref>:<expected revision>`. An absent ref is an idempotent
success; a moved ref is retained. Transport failures are retried by the next
scan and never retry a Promotion command.

Untracked refs, refs whose tips do not match durable candidate revisions, refs
used by nonterminal Promotions, and refs belonging to an integration that did
not enable cleanup are never deleted automatically. Detailed branch decisions
are logged. PostgreSQL keeps one last-run status per Repository with start and
finish times, aggregate observed, deleted, and retained counts, and a bounded
failure diagnostic. A new scan replaces that summary instead of retaining a
branch-level deletion history.

### Operational views

The Web provides projects, Promotions, Stage inventory/policy, Freight pipeline
comparisons, and branch drift. Review detail links preparation Runs and renders
sensitive Git and resource-filtered Argo CD diffs, hiding version-only changes
by default.
Operators can promote eligible exact Freight from observed upstream Stages on
the pipeline view or rerun a drifted Stage in place from branch drift. Both
flows use one dialog, can open the durable Promotion immediately or leave it in
the background, and periodically refresh its detail while creation is in
progress. The Kargo resource link appears only after successful Controller
delivery is recorded.

Promotion history applies filters, pending-first ordering, counts, and pagination
in PostgreSQL. The count and page share one read snapshot, and only the requested
page's snapshots are decoded. Historical filters use the frozen Stage snapshot.
Pipelines obtain grouped pending Promotion counts for matching active Stages
without loading Promotion history; topology and Freight comparison stay in the
Promotion domain.

Pipeline choices come only from current Freight on the destination Stage's
declared upstream Stages whose origin matches its requested Freight. They do not
infer adjacency from environment order or browse direct Warehouse Freight that
has not been observed on an upstream Stage. Branch drift offers the drifted
Stage's current Freight back to that same Stage and warns that the action reruns
its Kargo promotion template. The shared dialog shows target and source Stage,
environments, exact Freight name and origin, and Git, image, and chart versions.
A single choice is selected automatically; multiple choices require an explicit
selection. **Promote and view** redirects to the durable detail page, while
**Promote in background** closes the dialog and links the new Promotion from a
toast. API errors remain in the dialog without losing page filters.

Pipelines align typed Freight across configured environment order, preserve
duplicate Stages and missing environments, and append unconfigured environments.
Comparison reports equality or difference only when Stages exist in at least two
environments; otherwise it reports absence. Status filters select pipelines with
at least one matching Freight artifact before pagination. Comparisons do not
infer version or Git ancestry ordering.

Branch drift groups active Stages by Repository. A background worker refreshes
expired or invalidated groups with bounded, concurrent read-only Git scans while
API reads serve cached evidence without waiting for Git. Each Repository has one
in-flight scan and a reusable bare object cache for incremental fetches. Relevant
Repository or Stage changes make the group unknown until its replacement scan
completes; expired evidence remains readable while it refreshes. Watched
source-path changes and unexpected target movement are independent. Missing
render acknowledgement is unknown; equal target/result revisions acknowledge a
successful no-op. Repository and Stage scan failures remain visible. This cache
is derived state rebuilt after restart and removed during clean shutdown.

Branch drift pagination counts matching Stages after all filters, with 25 rows
per Web page. Results retain Repository groups, which may continue across pages,
and sort by Repository name, integration, project, application, environment,
Stage name, and Stage ID. Pages without a status filter read only Repository
groups represented on the page; status filters evaluate cached evidence for all
matching Repositories to count results.

## Notification delivery

Notifications report selected durable domain transitions without owning or
altering them. A delivery failure never changes a Promotion, Run, Repository,
Cluster, or other subject. Domain state remains the audit and recovery source of
truth; notification records exist to operate outbound delivery, not to form a
second general event log.

A notification event is created only at the durable transition it describes.
Polling, listing, rendering the Inbox, and restarting the Server never recreate
one. The immutable, versioned event snapshots its subject, transition time, and
channel-neutral presentation so delivery workers do not reconstruct an old
message from mutable state. Its stable deduplication key makes repeated or
concurrent transition handling idempotent. When a transactional domain service
owns the transition, it persists the state change, event, and intended deliveries
in one PostgreSQL transaction. Events based on elapsed time or derived health
require their own durable detection, episode, recovery, deduplication, and noise
semantics before they can be added.

### Events and routing

Outbound notification configuration is installation-wide Server configuration.
It names provider-neutral destinations, with Slack as the first adapter, and
routes that match implemented event types exactly. Routes resolve once at
enqueue time and snapshot the destination name and type into each delivery, so a
later configuration change cannot redirect committed work. Removing a configured
destination preserves history; unresolved pending deliveries follow the bounded
retry policy and eventually fail with a non-sensitive diagnostic. Exact event
matching remains deliberately smaller than arbitrary predicates, labels, or user
subscriptions.

The initial contract is `review.requested` schema version 1 with a `promotion`
or `run` subject, bounded title, summary, ordered facts, optional expiry, and an
application-relative action path. Facts come from the already-authorized domain
snapshot; adapters may omit unsupported presentation details but do not re-read
the subject to enrich a delivery. The model rejects external action URLs.
Additional events or subject types require an implemented durable transition and
explicit deduplication, recovery, and noise-suppression semantics.

The enqueue service validates the payload and resolves all matching destinations
before writing. PostgreSQL stores one immutable event and one mutable delivery per
selected destination. The event deduplication key is unique, and
`(event_id, destination_name)` is the delivery uniqueness boundary. Reusing a key
with the same snapshot returns the existing event without adding destinations;
conflicting content is rejected. When no route selects a destination, no
notification record is written and the domain transition proceeds normally.

### Inbox boundary

The operational Inbox and outbound notifications share domain semantics, not
storage or lifecycle. The Inbox projects what requires attention now regardless
of notification configuration or delivery success; notification events preserve
point-in-time snapshots for external delivery. Resolving or expiring a subject
removes the current Inbox item without retracting a sent message or erasing
delivery history. Viewing or acknowledging the Inbox never enqueues, suppresses,
acknowledges, or retries a delivery.

The two taxonomies need not mirror each other. A point-in-time event may be
useful externally without remaining a current Inbox item, while an Inbox
condition cannot produce notifications until it has a durable transition and
episode boundary. A terminal delivery failure is exposed through delivery
history rather than a notification-specific Inbox item. If it later joins the
Inbox, it must be a Server-side projection with a stable failure-occurrence ID;
per-Principal acknowledgement must not reset delivery state.

### Delivery and operations

Delivery is at least once. PostgreSQL stores `pending`, `delivered`, or terminal
`failed` state, attempt count, retry timing, bounded error summaries, and attempt,
delivery, and failure timestamps. A Server worker claims the oldest eligible
pending delivery, durably advances its attempt metadata and jittered exponential
retry time, and only then contacts the adapter. A restart after provider
acceptance but before recording success may therefore send a duplicate, but it
cannot lose the delivery. Separate destinations make independent progress.

Timeouts, connection failures, rate limits, and retryable provider responses
remain pending until the bounded attempt policy is exhausted. Permanent
rendering, configuration, and provider rejections fail immediately. Rate-limit
delays honor `Retry-After` up to the configured maximum backoff. Provider bodies
and raw transport errors are never persisted; diagnostics are bounded and
scrubbed of configured webhook URLs. Terminal history has bounded retention,
while pending deliveries are never eligible for retention.

Slack destinations use administrator-mounted webhook files and an explicit
absolute public Slipramp URL. The renderer joins the event's relative action path
to that URL and sends bounded context plus a link back to the authenticated
product. Messages cannot approve or reject. Provider-side decisions would require
signature validation, replay protection, provider-identity mapping to a Slipramp
Principal, and the same authorization checks as the product API; a shared webhook
credential cannot act for a human.

When guarded Kargo Promotion preparation succeeds, route resolution precedes the
state change and the same transaction moves the Promotion to `pending`, inserts
one deduplicated `review.requested` event, and inserts all selected deliveries.
With no matching route, the transaction changes only the Promotion. Automatic
Promotions and preparation failures emit no review request, and repeated
reconciliation cannot enqueue another event. The shared event shape does not
create shared approval persistence: Promotions continue to own their decisions.

When a review-required Git-trigger delivery creates a Run, route resolution
precedes persistence and the same transaction inserts the immutable queued Run,
links its delivery, records its initial phase, and enqueues one deduplicated
`review.requested` event with a `run` subject. Its snapshot includes Repository,
Target, Action, trigger, and commit facts plus a link to Run detail. With no
matching route, the Run and delivery linkage still commit. A notification write
failure rolls the entire transition back, so ordinary bounded Git-delivery
backoff may retry Run creation; it never retries an infrastructure command.
Approval, rejection, supersession, polling, and Inbox rendering cannot enqueue a
second request for that Run.

The generated read-only `/api/v1/notification-deliveries` endpoint uses ordinary
product read authorization. It exposes event and subject identity, destination
name and type, state, attempt count, bounded diagnostics, and timestamps while
omitting payloads, deduplication keys, provider bodies, webhook URLs, and secret
paths. The dedicated `/notification-deliveries` Web view uses that generated
client to show paginated history, subject links, attempt timing, and failure
summaries. It offers no routing, template, destination-secret, or retry editor.

## API, operations, and delivery

[OpenAPI](../api/openapi.yaml) is the source of truth for Server endpoints used
by the Web and Controllers. A small root references domain-sized path and
component files. One repository command bundles, validates, and generates Go
request/response types, Server interfaces, Controller client, TypeScript types,
and frontend client. Generated files are never edited manually. Standard Web
requests use the generated client; the small handwritten SSE transport uses
generated event types. API errors use RFC 9457 `application/problem+json`.

Go implements the Server, Controller, Harness, and tools under module
`github.com/slipramp/slipramp`. PostgreSQL uses pgxpool, embedded Goose migrations
at startup, and sqlc-generated queries. S3 access uses a narrow MinIO-client
boundary; the pre-provisioned bucket is checked at startup without requiring
bucket-administration privileges. The separate Web process uses strict
TypeScript, React Router v8 Framework/SSR mode, and Mantine UI v9.

The homepage is a role-aware operational Inbox delivered through one generated
Server API. Its complete read model groups pending Promotion decisions and
current operational problems, uses one evaluation time and deterministic
occurrence identities, reports exact Run activity counts, and groups stale Runs
beneath unreachable Clusters. Derived problems remain owned by their source
domains rather than a second durable problem lifecycle table.

Operators and administrators may acknowledge an exact batch of rendered problem
occurrences. PostgreSQL stores the acknowledgement per Principal and occurrence,
so it survives browser and Web restarts, remains visible to other Principals,
and does not hide a later occurrence of the same condition. Repeated refresh
failures belong to one Repository failure episode, so periodic retries do not
resurface an acknowledged problem; a successful refresh ends the episode and a
later failure starts a new occurrence. Decisions cannot be acknowledged, and
newly concurrent problems are absent from the submitted batch. The capability
comes from the complete role-and-scope authorization result.

The Web reads this projection without domain pagination or a fallback composition
path. A required projection-read failure produces an unavailable state and cannot
be presented as **All clear**. Dependency health remains outside the Inbox and is
exposed through readiness for external monitoring and failure diagnosis.

Targets have their own searchable, repository-, Runtime-, and Placement-filtered,
paginated index. Product navigation addresses Targets directly as
`/targets/{targetId}`; repository pages provide filtered discovery rather than
owning Target URLs. The API follows the same resource boundary with
`/api/v1/targets/{targetId}`, Target-scoped Action explanation, Target-ID Run
creation, and Target-ID Run history filtering. Repository catalog reads remain
available as immutable definition collections. Target responses include the
desired relevance commit, separate last-attempted and last-successful sync Run
checkpoints, their frozen markers and lifecycle facts, and a factual
`matched`/`different`/`unknown` comparison. The Target page links those Runs and
commits, keeps a latest failure visible beside an earlier matching success, and
shows archived checkpoints without a convergence claim. The Target index shows
the current factual comparison only for active Targets that define the technical
`sync` Action; Targets without `sync` have no reconciliation status to present.
Its Placement filter uses each Action's effective Placement, applying an Action
override before the Target default. A Target therefore matches every Placement
used by at least one of its Actions and appears at most once in each filtered
result.
Repository, commit, Cluster, Run/log/artifact, Promotion, and Access screens use
the same authenticated contract.

The authenticated Placement inventory exposes the validated effective
configuration currently held by the Server through `/api/v1/placements`,
`/api/v1/placements/{placementName}`, and the corresponding Web pages. The index
keeps Placement name, Cluster, Controller connectivity, and active Target usage
scannable; the detail page carries namespace, effective service account, timeout
and resource defaults, logical credential source names, last contact, and the
deterministic SHA-256 checksum of the complete effective Placement set. Usage
counts each active Target once per Placement when at least one resolved Action
uses it; a Target whose Actions span Placements contributes to each. The Web
count links to the matching Target inventory.
The inventory joins existing Controller observations: a configured Cluster that
has never checked in or whose observation is stale is `unreachable`, and the
last contact is present when known. Kubernetes Secret references, node selectors,
arbitrary pod-template contents, and Secret values are excluded. This is a
Server configuration and observation view; it does not contact Kubernetes or
claim that referenced cluster objects exist.

The optional kawaii theme adds an animated axolotl beside status badges and on
detail pages. While a Run is queued, she uses a deterministic waiting pose such
as reading, sipping iced coffee, eating popcorn, listening to music, playing
on her phone, touching the lawn beneath her, slurping ramen, or passing time in
an airplane window seat by fastening a lap belt, saving a drink during stormy
turbulence, or tucking into a cabin blanket. After assignment, she uses a
deterministic working pose throughout preparation and execution, including
runway marshalling, flight control, and inflight laptop work. Promotions being
created in Kargo use the waiting selection; preparing Promotions use the working
selection, and Promotions
awaiting review return to the waiting selection.
Terminal mascots appear only on Run and Promotion detail pages: successful Runs choose a
deterministic celebration pose, including an affectionate server hug or a guitar
power chord with a broad strum, joyful singing expression, and golden sound
bursts, a floppy-disk insertion, a hard-drive spinup, a pixel heart on a CRT,
a raised oven-mitt triumph, a controlled parachute landing, or a formal flag
salute. Working variations
include dough flipping, dough kneading, cake piping, grillmaster flipping, wok
tossing, kebab turning, checking an inert shoulder-launcher training replica,
reading a consistently oriented field map, scanning through a handheld rangefinder,
inspecting a compact field-medic kit, operating a field radio, checking a belt-fed
training weapon, digging with a folding entrenching tool, testing a full-face
respirator seal, conducting small-drone reconnaissance, and carefully installing
RAM into a minimal motherboard.
Failed or timed-out Runs choose a deterministic
failure pose ranging from a laptop slump to server biting, tool fumbles,
rocket-boot misfires, server stomping, emergency controls, and harmless slapstick
retreats, including a collapsed parachute ending in a harmless cartoon splat;
canceled Runs use a neutral phone animation.
Approved Promotions celebrate, rejected or failed Promotions use the failure
selection, and expired Promotions use the neutral phone animation. These outcome
cards replace the terminal result alert. Successful Run cards show only the
mascot and the `Run succeeded` title. Failed, timed-out, and canceled Run cards
retain the original outcome message, reason, and process exit code, including
zero; failed or expired Promotion cards retain their original diagnostic. The
standard theme keeps result alerts, which also remain as a diagnostic fallback
when no terminal outcome is available. Unknown observations suppress activity
only for unfinished Runs. Promotion list rows show activity only while creating
or preparing; waiting and terminal Promotion mascots stay on the detail page.
Cancellation intent retains the corresponding live animation with stopping text
until the Run finishes. Promotion details refresh throughout creation and
preparation, and continue after the pending transition until both prepared diffs
are loaded. In the kawaii theme, visible lists refresh while Runs are unfinished
or Promotions are creating or preparing.
Animation timing varies by Run or Promotion identity and stays stable across
refreshes. Compact standing variations include a determined working bounce and a
rising-chart celebration, plus a silver-pistol salute that racks the slide before
one upward celebratory shot. Pose-specific frame holds retain reviewed jumps and
emotional reactions, including the rocket misfire's hopeful start and disappointed
ending, the server hug's squeeze, and the guitar's brief strum and sustained chord,
while sprite geometry and reduced-motion behavior remain shared.
Waiting, working, celebration, and failure mascot poses also vary
deterministically by that identity, so the same Run or Promotion keeps its
selected pose across refreshes. The unlinked, authenticated `/mascot` route
groups every production animation by state at its detail, mobile, and compact
display sizes for visual review. Its optional compact gallery view uses the
supported 104px mobile presentation and denser cards for scanning more poses at
once; it is the default gallery view and can be toggled off. Every pose has
descriptive tags and a dedicated `/mascot/:pose` page, and
the gallery's tag filter is encoded in its URL. A
separate static build reuses that gallery at `https://mascot.slipramp.com`
without authentication, product navigation, API access, or a connection to the
Slipramp deployment. Its public pose pages use `/poses/:pose`. Cloudflare Workers
Static Assets serves the public build from its own custom domain with a
single-page application fallback for direct pose links; the central Ingress
remains private. The custom-domain binding is provisioned once in Cloudflare;
routine CI deployments update only the Worker and its static assets, so the
deployment token does not need zone-route write access. Gallery cards load their
sprite sheets as they approach the viewport. Activity respects reduced motion
preferences and leaves textual status indicators available.

`/healthz` reports process liveness. `/readyz` checks PostgreSQL and artifact
storage concurrently under a two-second deadline, returning non-sensitive
per-dependency states and `503` when required dependencies are unavailable.
The Server emits structured JSON logs and handles graceful shutdown.

`GET /metrics` exposes Prometheus/OpenMetrics text from a private registry on a
dedicated listener. The Server and Controller share the listener implementation
and lifecycle behavior. Application instrumentation uses OpenTelemetry with
pinned semantic conventions; the separately served metrics endpoint does not
enter HTTP telemetry, which records matched route templates rather than raw
paths. A 15-second worker
publishes bounded current-state gauges from one repeatable-read PostgreSQL
snapshot under a two-second deadline. Scrapes only read the last complete
in-memory view, and a failed refresh retains it while exposing snapshot
freshness. Narrow domain observers record Repository refresh, Git-trigger
delivery, notification delivery, and Artifact retention outcomes without
identifiers or other unbounded labels. The complete contract and convention
status are documented in the [metric catalog](./METRICS.md).

Each Controller owns a separate private OpenTelemetry/Prometheus provider and
serves `GET /metrics` from a dedicated cluster-local listener. The initial
Controller surface contains Resource metadata, maintained Go runtime and native
process metrics, plus stable OTel HTTP client metrics around every request made
by the generated Slipramp client. Client metrics omit raw URL paths and
identifiers. It is scraped directly inside the execution Cluster; the Server
does not proxy, aggregate, or require Controller metrics. The Controller chart
creates an internal metrics Service and no public route.

Two focused [Helm charts](../deploy/charts) deploy one Server/web pair and one
Controller per Cluster. The central chart requires external PostgreSQL and S3,
gives the Server no RBAC or service-account token, and offers single-host
Ingress. Placement ConfigMap checksums roll the Server on configuration changes;
live reload is not part of the contract. The Controller chart grants only
namespaced Job/Pod access and optional Kargo Stage reads. Secret-bearing values
default to operator-managed Secret references. Images are pinned; a `vX.Y.Z`
tag publishes matching application images and versioned OCI charts to GHCR
with tag/chart/application/image versions required to agree.

Formatting, linting, strict type checks, generated-output freshness, unit/race,
PostgreSQL/S3 integration, chart render/package checks, and builds run in CI.
Versions are pinned and warnings fail. Go uses golangci-lint v2; frontend uses
Oxlint and Oxfmt. Disposable kind acceptance runs as an independent,
non-blocking CI job on the self-hosted runners and remains available through the
local task. Its Go suite provisions the environment through the real kind,
Helm, kubectl, Docker, and Git command boundaries, then exercises the built CLI
across Target discovery and explanation, Run creation and control, log streaming,
Artifact transfer, pagination, reruns, and review decisions. It also covers
operator-started Kargo creation through real Controller materialization and
callback hydration, and automatic/guarded preparation through real artifact
uploads, including empty Git diffs. Minimal Stage and Promotion CRDs exercise
the Kubernetes API boundary without installing Kargo controllers; broader
failure and live cutover coverage remains on the [roadmap](./ROADMAP.md).
