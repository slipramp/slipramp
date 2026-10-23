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
| Slipramp Server       | Repository refresh, catalog validation, scheduling, identity, Promotions, artifact ingestion, API  | No kubeconfig, Kubernetes client, or direct Kubernetes API access      |
| Controller            | Outbound command polling, local Jobs, observations, log forwarding, optional Kargo Stage discovery | One replica per execution Cluster; namespace-scoped Kubernetes access  |
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
| Target           | A repository-scoped named operational unit containing user-defined Actions                                     |
| Action           | One explicit command with execution settings, parameters, optional triggers, and artifact declarations         |
| Runtime          | Repository-defined portable container environment and credential/process conventions                           |
| Placement        | Server configuration selecting a Cluster, namespace, service account, credentials, resources, and pod settings |
| Cluster          | Named Kubernetes execution destination represented by its in-cluster Controller                                |
| Run              | Immutable action execution snapshot, with independently recorded lifecycle observations                        |
| Artifact         | Durable Run output, including logs, plans, and diff files                                                      |
| Principal        | Durable user or service-account actor authenticated by a session or API key                                    |
| Promotion        | Durable Kargo request, snapshotted policy, preparation, and optional operator decision                         |

A Target's identity is its Repository plus `metadata.name`. Renaming creates a
new identity and archives the old one. Removing a Target never destroys
infrastructure. Historical Runs retain their catalog revision.

`sync` is an optional recognized Action name. Targets without it remain useful
collections of operations. Track desired, last attempted, and last successful
reconciliation commits where applicable without claiming generic infrastructure
is continuously in sync.

Actions are independent executions. Multi-step procedures belong inside the
invoked script, task runner, or container rather than a Slipramp workflow graph.
A Run records its run source; there is no separate invocation domain resource.

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

Target defaults supply runtime, placement, timeout, credentials, and resources.
Action scalar overrides replace defaults; credential and resource maps merge
by key. Resolution copies definitions rather than mutating the catalog.
Runtimes and Targets always resolve from the same revision.

A valid commit creates or reuses an immutable catalog revision. An invalid
commit records its SHA and diagnostics while the last valid revision stays
active; it does not advance Target state or create Runs. Changed paths between
accepted heads match Target `watch` patterns, including recursive `**`, to
advance each affected Target's durable desired commit. New and reactivated
Targets begin at the commit where they become active; removed Targets archive.
Repository refresh indexes Git; target reconciliation executes optional `sync`.
Watch patterns alone never start execution.

### Parameters

Parameters are `string`, `boolean`, `number`, or `enum`, with required and enum
constraints validated before Run creation. Each maps to exactly one environment
variable or argument. An empty `argument: {}` is positional. Named string,
number, and enum arguments emit a name followed by the value; named booleans
emit a flag only when true. Positional and environment booleans use `true` or
`false`. Arguments append in lexical parameter-name order. Unset optional
parameters emit nothing. Commands are explicit argument arrays; shell behavior
must be requested explicitly.

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

## Run creation and scheduling

UI, API, Git delivery, and Promotion preparation use the same Run-creation
service. It validates parameters and artifact bindings, accepts an optional
idempotency key, and freezes the Repository, commit, catalog revision, Target,
Action, Runtime, Placement, source, and actor. Authenticated request idempotency
is scoped to the submitting Principal. Historical creator and cancellation
attribution includes both a Principal reference and an immutable actor snapshot.

A Controller claims the oldest eligible queued Run for its Cluster. PostgreSQL
coordinates one active Run per Target and ordinary FIFO scheduling among
eligible Runs. Infrastructure commands are never retried implicitly.

Lifecycle phase (`queued`, `assigned`, `preparing`, `running`, `finished`),
terminal outcome (`succeeded`, `failed`, `canceled`, `timed-out`), cancellation
intent, Job identity, exit code, diagnostics, and observation freshness are
separate facts. A disconnected Controller or stale observation derives an
unknown observation; it does not change phase or fail an active Run.

Finished Runs support two explicit rerun modes. Exact reruns copy the frozen
execution snapshot, commit, catalog revision, parameters, and retained inputs.
Latest reruns resolve the same Target, Action, and parameter values against the
active catalog and current Placement configuration. Latest reruns reject
artifact inputs because inputs pin consumers to their producing revision. Both
modes create a new Run linked to its immediate parent and derive source and
actor from the new request boundary.

### Git triggers

Named Action triggers use `git: {}`, optional static `parameters`, and the
Target's required `watch` patterns. Trigger names are unique within an Action.
Triggered Actions cannot have artifact inputs. Catalog loading validates and
normalizes static parameters with the ordinary Run parameter rules.

After initial indexing, accepting a changed head atomically activates the
catalog, updates Targets, and records one durable delivery for each matching
trigger. Unchanged or rejected heads create no deliveries. Delivery is per
accepted head, not per underlying commit in a multi-commit fetch. Each delivery
freezes the accepted revision and normalized parameters.

Automatic Git work uses latest-wins coalescing per Repository and Target. When
an affected Target's desired commit advances or the Target is archived, the
same transaction supersedes older undispatched deliveries and finishes queued
Git Runs as canceled with reason `SupersededByGitChange`, including phase
transitions. Superseded deliveries remain history. Assigned, preparing, and
running Git Runs finish normally; manual and API Runs are never superseded.

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

Every execution namespace is exclusively owned by one Controller and its
control plane. A Controller may own several namespaces, rejects assignments
outside its configured list, and scans those namespaces individually before
polling after restart. Another installation may reuse a namespace only after
all previous Slipramp Jobs are removed. Duplicate Controllers or reused Cluster
names are configuration errors, not a failover mechanism.

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
observation then drains and seals logs, reports the outcome, and deletes the
Job with its observed UID as a precondition. Unstarted Jobs are deleted before
reporting an empty canceled Run.

For every terminal Job, log sealing and accepted terminal reporting precede
UID-checked deletion and stopping observation. Each step is idempotent and
resumes after restart. Transient storage, reporting, or deletion failures keep
the Job available for recovery. Kubernetes TTL cleanup is disabled so outages
cannot delete the only remaining log source. A `404` for the Run or log violates
namespace ownership: warn once, stop observing, and leave the Job for diagnosis.

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

Repository maintainers execute code with the authority granted by a Placement.
Containers do not provide a complete security boundary. Safety policy is
opt-in: direct actions, including unreviewed `terraform apply`, remain valid.
Preview artifacts and Kargo review policy add optional controls to this model.

## Logs and artifacts

The Controller follows Pod output once and posts chunks of at most 256 KiB with
monotonically increasing byte offsets. The Server writes S3 objects before
committing their digest and offset metadata in PostgreSQL. Retrying the same
bytes at the same offset is idempotent. Recovery reads the durable cursor and
replays only to verify and skip the accepted prefix. The Controller seals logs
with final byte size and SHA-256 before terminal observation.

Live logs use reconnectable SSE whose event ID is the next durable byte offset.
Live viewing and retained capture share the same ingestion path. Pod logs are
the source during execution, never the durable record.

Assignments include a deterministic, narrowly scoped artifact credential
derived from a Server HMAC key. The Harness may download only explicit input
bindings and upload only frozen Action output names. The Server streams bytes
to/from S3; PostgreSQL stores producing Run, name, media type, lifecycle, size,
digest, sensitivity, object key, retention, and provenance.

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
| Create or cancel Runs                                                            | operator                 | `slipramp:runs:write`        |
| Submit or poll Kargo Promotions                                                  | operator service account | `slipramp:promotions:submit` |
| Decide Promotions or change Stage policy                                         | operator                 | `slipramp:promotions:write`  |
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
Action, maximum observation age, and pending-review retention. The Server
matches normalized observed repository URLs against that allowlist. The
Controller derives application, environment, repository, target branch, source
paths, typed Freight, and successful-render acknowledgement from Stage data.
The adapter recognizes `app`, `argoApp`, `overlay`, `gitopsRepo`, `kustomizePath`,
`stageBranchKustomization`, and `slipramp.com/promotion-source-paths` conventions.

Stage policy defaults to `automatic`. `review-required` optionally expires,
after which new requests evaluate as automatic while the old policy remains
visible. Acceptance snapshots effective policy; later changes never alter an
existing Promotion. Policy edits retain actor attribution and reject archived
Stages.

### Request, preparation, and decision

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

Automatic requests become `approved` without a Run. Guarded requests become
`preparing` and dispatch exactly one restart-safe, revision-pinned ordinary Run.
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
the change is version metadata only. Argo CD targets come from the Runtime
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
Neither the Server nor preparation mutates Git refs or executes Kubernetes
operations.

### Operational views

The Web provides projects, Promotions, Stage inventory/policy, Freight pipeline
comparisons, and branch drift with shared integration, project, and application
filters. Review detail links preparation Runs and renders sensitive Git and
resource-filtered Argo CD diffs, hiding version-only changes by default.

Pipelines align typed Freight across configured environment order, preserve
duplicate Stages and missing environments, and append unconfigured environments.
Comparison reports equality, difference, or absence without inferring version
or Git ancestry ordering.

Branch drift groups active Stages by Repository and shares one bounded read-only
Git scan, cached for the refresh interval. Relevant Repository or Stage changes
invalidate the group; concurrent reads share work. Watched source-path changes
and unexpected target movement are independent. Missing render acknowledgement
is unknown; equal target/result revisions acknowledge a successful no-op.
Repository and Stage scan failures remain visible. This cache is derived state
rebuilt after restart.

Branch drift pagination counts matching Stages after all filters, with 25 rows
per Web page. Results retain Repository groups, which may continue across pages,
and sort by Repository name, integration, project, application, environment,
Stage name, and Stage ID. Pages without a status filter scan only Repositories
represented on the page; status filters evaluate all matching Repositories to
count results.

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

The current homepage is a role-aware operational Inbox composed by the Web
from domain APIs and readiness. It groups pending Promotion decisions and
operational problems; it has no dedicated Inbox API or durable acknowledgement
state yet. Targets have their own searchable, repository-filtered, paginated
index. Repository, commit, Cluster, Run/log/artifact, Promotion, and Access
screens use the same authenticated contract. The proposed Server projection is
in [INBOX.md](./INBOX.md).

`/healthz` reports process liveness. `/readyz` checks PostgreSQL and artifact
storage concurrently under a two-second deadline, returning non-sensitive
per-dependency states and `503` when required dependencies are unavailable.
The Server emits structured JSON logs and handles graceful shutdown.

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
PostgreSQL/S3 integration, chart render/package checks, builds, and disposable
kind acceptance run in CI. Versions are pinned and warnings fail. Go uses
golangci-lint v2; frontend uses Oxlint and Oxfmt. Existing kind acceptance covers
execution and automatic/guarded Kargo preparation through real artifact
uploads, including empty Git diffs; broader failure and live cutover coverage
remains on the [roadmap](./ROADMAP.md).
