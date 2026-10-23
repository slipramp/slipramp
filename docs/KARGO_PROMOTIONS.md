# Kargo Promotions Plan

This document defines the intended design and delivery sequence for folding the
current `kargo-gate` promotion-review behavior into Slipramp. It is a planning
aid for incremental implementation, not an implemented or stable external
contract.

The feature is a focused **Promotions** vertical for operators. It adds native
promotion state, review policy, diff presentation, and decisions without
turning ordinary Runs into approval-gated workflows. Kargo remains responsible
for executing its Promotion and advancing the target branch after Slipramp
returns an approved decision.

## Goals

- Let a Kargo Promotion request and poll an idempotent review decision.
- Let operators choose automatic or review-required behavior per observed
  Kargo Stage, including a temporary review requirement with an expiry.
- Generate Git and Argo CD diffs through an ordinary Slipramp Run and retain
  them as durable artifacts.
- Attribute the machine request, policy changes, and human decision to durable
  Slipramp principals.
- Discover Kargo Stages without giving the Slipramp Server Kubernetes access.
- Preserve the current approval queue, Freight comparison, and branch-drift
  operator value through incremental milestones.
- Replace the standalone `kargo-gate` service after an explicit cutover.

## Non-goals

The first implementation does not include:

- mandatory approvals, previews, or policy checks for ordinary Slipramp Runs;
- a generic approval, policy-engine, workflow-graph, or provider-plugin model;
- direct Kubernetes access from the Slipramp Server;
- Git branch mutation from the Slipramp Server;
- a replacement for Kargo, Argo CD, or their reconciliation behavior;
- pull-request approval flows;
- automatic retry of a failed diff or promotion command;
- high availability for the Server, web process, or Controller;
- compatibility routes, data importers, or dual operation with the existing
  `kargo-gate` API after cutover.

## Accepted Design Decisions

### Promotion is the durable resource

A `Promotion` records one external Kargo Promotion request, its snapshotted
Stage policy, preparation Run, artifacts, state, and optional decision. There
is no separate one-to-one Gate and Review model. The gate is an interaction
pattern over the Promotion resource.

The initial lifecycle states are:

```text
                 preparation succeeds
preparing --------------------------------> pending
    |                                          |
    | preparation fails                        | approve
    v                                          v
  error                                     approved
                                               ^
                                               |
automatic policy ------------------------------+

pending -- reject --> rejected
pending -- retention deadline --> expired
```

`approved` means Slipramp granted the decision. It does not claim that Kargo
successfully pushed or reconciled the change. Kargo owns that outcome.

### Review policy is optional and snapshotted

Each observed Stage has one current policy:

- `automatic`: a valid request becomes approved without preparation;
- `review-required`: a valid request is prepared and waits for an operator;
- `review-required` with `expiresAt`: new requests require review until the
  expiry, after which the effective mode is automatic.

The default is `automatic`. This preserves Slipramp's opt-in safety policy.
The effective policy is copied onto a Promotion when the request is accepted.
Later policy changes do not alter existing Promotions.

### Kargo performs the target-branch push

The Kargo task continues to create and push its candidate branch before
calling Slipramp. After Slipramp returns `approved`, the task pushes the same
commit to the Stage target branch and continues its existing `argocd-update`
step.

The target push is non-forcing. If the target branch moved after review
preparation, Git rejects the stale push and the Kargo Promotion fails safely.
Slipramp therefore needs read-only Git and Argo CD credentials for preparation,
not Git write credentials for approval.

### Preparation is an ordinary Run

A guarded Promotion creates one idempotent preparation Run through the normal
Run service. The integration configuration selects a repository-defined
Target and Action. The Run is pinned to the catalog revision used when it is
created and has a structured `promotion` run source.

The Action uses a first-party `slipramp-kargo-tools` runtime image to compare
the exact base and candidate revisions and request the Argo CD diff. It emits:

- `git-diff`: `text/x-diff; charset=utf-8`;
- `argo-diff`: `application/vnd.slipramp.argocd-diff+json`.

Both artifacts are sensitive by default. A successful Run must seal both,
including an explicit empty result. Missing, malformed, oversized, expired, or
deleted artifacts make preparation fail. A pending Promotion prevents its
artifacts from expiring. Each artifact is limited to 16 MiB when the Server
validates the completed Run.

The dispatcher supplies this stable set of string parameters to the configured
preparation Action:

| Parameter | Value |
| --- | --- |
| `promotion-id` | Slipramp Promotion UUID |
| `external-promotion-id` | Kargo Promotion identity |
| `project` | observed Kargo project/namespace |
| `stage` | observed Kargo Stage name |
| `application` | snapshotted application |
| `environment` | snapshotted environment |
| `repository-name` | matched Slipramp Repository name |
| `repository-url` | snapshotted repository URL |
| `base-revision` | validated full base Git object ID |
| `candidate-revision` | validated full candidate Git object ID |
| `candidate-branch` | validated candidate branch |
| `target-branch` | snapshotted target branch |
| `source-paths` | JSON array of snapshotted source paths |

The preparation command is read-only and bounded by the Action timeout. A
failed preparation Run moves the Promotion to `error`; it is not retried
implicitly. Kargo can start a new Promotion with a new external identity after
the underlying problem is corrected.

### Controllers observe Kargo Stages

The optional Kargo observer runs inside the Slipramp Controller. It lists
Stages in explicitly configured namespaces, normalizes the Kargo-specific
shape, and sends complete snapshots to the Server. The Server never receives a
kubeconfig, service-account token, or Kubernetes client.

The controller chart creates Kargo Stage read permissions only when the
observer is enabled. It creates namespaced Roles and RoleBindings for the
configured Stage namespaces rather than a cluster-wide binding.

Stage identity is `(integration, project, name)`, where the project is the
Kubernetes namespace. The stored Stage also records its Slipramp Cluster.
Removing a Stage from a successful snapshot archives the current observation;
it does not remove its policy or historical Promotions.

### Integration wiring is deployment configuration

Kargo integrations are named Server configuration loaded at startup, like
Placements. Each integration selects:

- one registered Slipramp Cluster;
- the logical repository names allowed for observed Stages;
- the ordered environments used by Freight pipeline comparisons;
- the repository, Target, and Action used for review preparation;
- the maximum accepted Stage observation age;
- pending-review retention and expiry settings.

The corresponding Controller configuration supplies the integration name and
the namespaces it observes. Secret values remain in placement-selected
cluster-local Kubernetes Secrets and are never stored in PostgreSQL.

An illustrative configuration is:

```yaml
# Slipramp Server configuration.
kargo:
  integrations:
    prod:
      cluster: prod
      repositories: [kcloud-ops]
      environmentOrder: [homelab, prod]
      preparation:
        repository: kcloud-ops
        target: kargo-promotions
        action: prepare-review
      maximumStageObservationAge: 5m
      pendingReviewRetention: 192h

# Slipramp Controller configuration in the prod cluster.
kargo:
  integrations:
    - name: prod
      stageNamespaces:
        - kargo-core
        - kargo-devops
        - kargo-platform
```

The exact configuration fields become part of the implementation milestone
that adds validation and Helm values.

## Core Model

### Promotion Stage

A Promotion Stage is the current normalized observation of one Kargo Stage. It
is not a Slipramp Target and does not create Runs by itself.

Its durable fields include:

- stable UUID;
- integration, Cluster, project, and Stage name;
- Kubernetes UID and resource version;
- application and environment;
- matched Slipramp Repository;
- repository URL, target branch, and source paths;
- current Freight summary;
- optional successful-render acknowledgement used by drift detection;
- first-observed, last-observed, and archived timestamps.

Application, environment, repository URL, target branch, and source paths are
derived by the Controller from the Stage's labels, annotations, and promotion
variables. The Server matches the observed repository URL to one of the
integration's allowed Slipramp Repositories and stores its logical name. The
initial adapter retains the established `app`, `argoApp`, `overlay`,
`gitopsRepo`, `kustomizePath`, and
`stageBranchKustomization` conventions. A
`slipramp.com/promotion-source-paths` annotation may add unusual paths.

The successful-render acknowledgement records the source commit rendered by
Kargo, the target-branch commit read before rendering, and the resulting
target commit. Kargo's `git-commit` step returns the existing target commit
when rendering produces no changes, so equal target and result revisions are
a successful no-op acknowledgement rather than missing progress.

### Promotion Policy

A Promotion Policy belongs to one Promotion Stage and contains:

- configured mode;
- optional expiry;
- optional reason;
- updating principal reference and immutable actor snapshot;
- update timestamp.

The API exposes both configured and effective mode. An expired policy remains
visible for audit context but evaluates to `automatic` for new requests.

### Promotion

A Promotion contains:

- stable UUID;
- integration and external Kargo Promotion ID;
- canonical request digest;
- Promotion Stage reference and immutable Stage snapshot;
- Repository reference and immutable repository snapshot;
- base revision, candidate revision, candidate branch, and target branch;
- snapshotted policy and `reviewRequired` result;
- lifecycle state and optional failure message;
- preparation Run ID;
- Git and Argo CD diff artifact IDs;
- requesting service-account reference and immutable actor snapshot;
- creation, preparation, decision, expiry, and update timestamps.

The uniqueness boundary is `(integration, external_promotion_id)`. Reusing
that identity with a different canonical request digest is a conflict.

### Promotion Decision

A Promotion has at most one human decision containing:

- `approved` or `rejected`;
- optional comment;
- deciding principal reference and immutable actor snapshot;
- decision timestamp.

Automatic approval and expiry are lifecycle transitions rather than human
decisions. They do not invent an actor.

## API Contract

OpenAPI remains the source of truth. Add the domain-sized files
`api/paths/promotions.yaml` and `api/components/promotions.yaml`, reference them
from `api/openapi.yaml`, and regenerate every Go and TypeScript target together.

### Kargo machine endpoint

```text
POST /api/v1/kargo/{integration}/promotions
```

This endpoint accepts only `ProductBearer` authentication. The caller must be
a service account with operator or admin role and the
`slipramp:promotions:submit` scope.

The request contains only facts that Kargo uniquely knows:

```json
{
  "externalPromotionId": "external-dns-01jbe4q8",
  "project": "kargo-core",
  "stage": "external-dns-prod",
  "baseRevision": "8b7f1d217a6b0000000000000000000000000000",
  "candidateRevision": "f4cb21a88e1d0000000000000000000000000000",
  "candidateBranch": "kargo/promotion/external-dns-01jbe4q8"
}
```

The Server resolves application, environment, repository, and target branch
from the current observed Stage. It rejects an unknown or archived Stage, an
unmatched repository, an invalid full Git object ID, a candidate branch
outside `kargo/promotion/`, or an observation older than the configured limit.

The response is intentionally the same shape for first submission and polls:

```json
{
  "id": "b21eaf3e-dced-4a48-b57a-7a5fb4469cbd",
  "externalPromotionId": "external-dns-01jbe4q8",
  "status": "pending",
  "reviewRequired": true,
  "updatedAt": "2026-10-21T18:34:31Z"
}
```

An optional `message` explains `rejected`, `expired`, or `error`. The status is
one of `preparing`, `pending`, `approved`, `rejected`, `expired`, or `error`.

HTTP behavior is:

- `200` for a newly accepted request, a repeated poll, or any persisted domain
  state, including terminal rejection or error;
- `400` for malformed input;
- `401` for a missing or invalid bearer credential;
- `403` for the wrong principal kind, role, or scope;
- `404` for an unknown integration or Stage;
- `409` when the external identity already belongs to a different request, or
  the Stage is known but archived, stale, or otherwise ineligible;
- `500` or `503` only when the Server cannot durably evaluate the request.

Persisting the request and policy snapshot precedes any Run creation. A retry
after an ambiguous response therefore recovers the same Promotion.

### Operator endpoints

```text
GET /api/v1/promotions
GET /api/v1/promotions/{promotionId}
PUT /api/v1/promotions/{promotionId}/decision
GET /api/v1/promotion-stages
PUT /api/v1/promotion-stages/{stageId}/policy
GET /api/v1/promotion-branch-drift
```

List Promotions supports `status`, `integration`, `cluster`, `project`,
`stageId`, `limit`, and `offset` filters. Results are ordered by creation time
descending and include `total`, `limit`, and `offset`.

Promotion detail includes Stage and Repository snapshots, preparation Run,
artifact metadata, requesting actor, policy snapshot, decision, failure
message, and timestamps. Artifact contents continue to use the existing
artifact download endpoint and sensitivity authorization.

The branch-drift endpoint groups active Stages by matched Slipramp Repository.
One read-only Git scan resolves the configured source branch and every target
branch needed by a repository group. For each Stage it compares the current
source branch with the acknowledged source revision only under the Stage's
observed source paths, and compares the target-branch head with the
acknowledged render result. Source-path changes and unexpected target-branch
movement are reported independently. A Stage without a valid successful-render
acknowledgement is `unknown`; repository and per-Stage scan failures remain
visible instead of turning the entire view into a server error.

Repository scan results, including failures, are held in the Server's
in-memory cache for the configured repository refresh interval. A changed
Repository or relevant Stage observation invalidates its group immediately.
Concurrent requests share the same grouped scan, and each remote operation is
bounded by the repository fetch timeout. The cache is derived operational
state and is rebuilt after a Server restart; PostgreSQL remains authoritative
for Repository and Stage inventory.

The decision request is:

```json
{
  "decision": "approved",
  "comment": "Validated the new RBAC rules against the migration plan."
}
```

The actor always comes from authentication. Only `pending` Promotions are
decidable. Repeating the same decision by the same actor returns the existing
result; a different outcome, actor, or comment returns `409`. The endpoint
returns the updated Promotion.

The policy request is:

```json
{
  "mode": "review-required",
  "expiresAt": "2026-10-22T02:00:00Z",
  "reason": "Temporary review requirement during the DNS migration."
}
```

`expiresAt` is allowed only for `review-required` and must be in the future.
Policy updates on archived Stages return `409` while preserving the stored
policy.

### Controller endpoint

```text
PUT /controller/v1/kargo/{integration}/stages
```

The endpoint accepts `ControllerBearer` authentication. Cluster identity comes
from the credential and is not repeated in the body.

The request contains a unique snapshot ID, observation time, and a complete
list of normalized Stages. The Controller finishes every namespace listing
before submission and never sends a partial snapshot after any list failure.
Repeating a snapshot ID is idempotent. A successful complete snapshot archives
previously observed Stages that are absent and returns `204`.

The Server verifies that the integration selects the authenticated
Controller's Cluster and rejects Stages outside the integration's repository
allowlist.

### Run source

Extend the Run source enum with `promotion` and add:

```json
{
  "type": "promotion",
  "promotion": {
    "promotionId": "b21eaf3e-dced-4a48-b57a-7a5fb4469cbd"
  }
}
```

The preparation Run snapshots the requesting Kargo service account as its
creator. The Promotion ID provides a stable backlink without adding
Kargo-specific fields to the Run itself.

## Authorization

Add two API-key scopes:

- `slipramp:promotions:submit`: submit and poll the Kargo machine endpoint;
- `slipramp:promotions:write`: decide Promotions and update Stage policy.

The permission model is:

| Operation | Minimum role | API-key scope |
| --- | --- | --- |
| Read Stages and Promotions | viewer | `slipramp:read` |
| Submit or poll from Kargo | operator service account | `slipramp:promotions:submit` |
| Approve, reject, or change policy | operator | `slipramp:promotions:write` |
| Manage Kargo service accounts and keys | admin | `slipramp:admin` |

The administrator scope continues to grant every permission subject to the
principal's role. The Kargo service account receives only the submit scope, so
its credential cannot approve its own Promotions or read unrelated product
state.

## Persistence

Add one focused migration after the current schema and group SQL in
`internal/store/queries/promotions.sql`. The intended tables are:

```text
promotion_stages
  id
  integration
  cluster_name
  project
  name
  kubernetes_uid
  resource_version
  normalized observation fields
  freight_json
  first_observed_at
  last_observed_at
  archived_at
  UNIQUE (integration, project, name)

promotion_stage_policies
  stage_id
  mode
  expires_at
  reason
  updated_by_principal_id
  updated_by_snapshot
  updated_at

kargo_stage_snapshots
  snapshot_id
  integration
  cluster_name
  observed_at
  accepted_at

promotions
  id
  integration
  external_promotion_id
  request_digest
  stage_id
  stage_snapshot
  repository_id
  repository_snapshot
  base_revision
  candidate_revision
  candidate_branch
  target_branch
  policy_snapshot
  review_required
  status
  message
  preparation_run_id
  git_diff_artifact_id
  argo_diff_artifact_id
  requested_by_principal_id
  requested_by_snapshot
  timestamps
  UNIQUE (integration, external_promotion_id)

promotion_decisions
  promotion_id
  decision
  comment
  decided_by_principal_id
  decided_by_snapshot
  decided_at
```

State transitions use conditional updates so concurrent polls, dispatch, Run
completion, expiry, and decisions cannot overwrite one another. Foreign keys
preserve history, while immutable JSON snapshots keep display meaningful after
renames or configuration changes.

The artifact retention query treats artifacts referenced by a nonterminal
Promotion as retained. After a Promotion becomes terminal, ordinary artifact
retention applies.

## Server and Controller Responsibilities

### Server

The Server:

- validates static integration configuration;
- accepts Stage snapshots and Kargo requests;
- owns policies, Promotion state, authorization, and idempotency;
- creates exactly one preparation Run for each guarded Promotion;
- observes preparation completion and verifies its declared artifacts;
- expires old pending Promotions;
- scans Repository branches read-only and serves the cached drift view;
- serves operator and machine APIs.

It does not connect to Kubernetes, execute `argocd`, or push Git branches.

### Controller

The Controller:

- lists Kargo Stages in configured namespaces;
- normalizes Stage identity, Freight, repository, branch, application,
  environment, and source-path data;
- submits complete snapshots under its existing controller identity;
- continues ordinary Run assignment and observation unchanged.

Kargo observation failure affects Stage freshness but does not disconnect the
Controller or interfere with Run delivery.

### Kargo tools runtime

The first-party tools image:

- contains Git 2.53.0 and Argo CD 3.5.0 in an image pinned by digest;
- accepts explicit argument arrays and never requires an implicit shell;
- fetches and compares the exact revisions recorded on the Promotion;
- verifies that the candidate branch resolves to the candidate revision;
- requests the Argo CD diff for the observed application and environment;
- caps output size and emits deterministic artifact files;
- never advances or deletes a Git ref during preparation.

Git and Argo CD credentials use ordinary Runtime credential declarations and
Placement bindings. The image contains no Slipramp API integration; the Harness
owns artifact transfer and log streaming.

The image entrypoint is `slipramp-kargo-tools`. Its `prepare` command accepts
the complete stable parameter set above as named flags. It writes the Git diff
to `/slipramp/artifacts/git.diff` and the Argo CD diff to
`/slipramp/artifacts/argo.json`; the paths can be overridden for local tests.
An empty Git diff is a zero-byte file. An empty Argo CD diff is the canonical
JSON document `{"resources":[]}\n`. Structured Argo CD resources use this
shape:

```json
{
  "resources": [
    {
      "group": "apps",
      "kind": "Deployment",
      "namespace": "default",
      "name": "example",
      "diff": "@@ -1 +1 @@\n-image: old\n+image: new",
      "versionMetadataOnly": false
    }
  ]
}
```

The Argo CD credential source mounts a target file at
`/run/slipramp/credentials/argocd/targets.json`. Keys are normalized
environment names. Each value supplies `server`, `authToken`, and optional
`plaintext` and `grpcWeb` booleans. The command rejects an environment absent
from this file and passes the token through `ARGOCD_AUTH_TOKEN`, never through
the process argument list. Optional HTTPS Git basic authentication uses the
`SLIPRAMP_KARGO_GIT_USERNAME` and `SLIPRAMP_KARGO_GIT_PASSWORD` credential
environment variables. SSH and credential-helper access can use ordinary Git
environment and configuration files injected by the Runtime.

## Web Application

Add a **Promotions** navigation item with these initial routes:

```text
/promotions
/promotions/:promotionId
/promotion-stages
/promotion-pipelines
/promotion-branch-drift
```

The Promotions page shows pending work first, then recent terminal Promotions,
with integration, Stage, application, revisions, age, status, and requesting
actor filters. The detail page shows:

- Stage and repository context;
- base and candidate revisions;
- snapshotted policy;
- preparation Run status and logs;
- Git and Argo CD diff tabs;
- resource kind and name filters for structured Argo CD diffs;
- version-metadata-only changes hidden by default;
- decision comment and approve or reject controls;
- immutable decision attribution.

The Stage page shows current Freight, observation freshness, policy, pending
count, and archived state. Policy controls are hidden when the principal lacks
write permission, while the Server remains authoritative. The pipeline page
groups active Stages by integration, repository, project, and application. It
aligns normalized Freight artifacts across the integration's configured
environment order, preserves duplicate Stages in one environment, shows
missing environments explicitly, and appends newly observed unconfigured
environments without rejecting the Controller snapshot. Comparisons report
only exact equality, difference, or absence; they do not infer semantic version
or Git ancestry ordering.

Branch drift becomes an additional view after the Stage inventory and review
flow are accepted. It presents one section per Repository, the cached scan
time and any repository-level failure, and per-Stage source and target drift.
Successful no-op renders are labeled explicitly so an unchanged target branch
does not look stale after Kargo acknowledged a newer source revision.

## Delivery Milestones

Each milestone should leave the repository generated, formatted, tested, and
documented. Do not land handwritten generated-code changes.

### 1. Contract, configuration, and persistence

- [x] Add and validate Server and Controller Kargo integration configuration.
- [x] Add promotion OpenAPI path and component files with machine, operator,
      policy, Stage observation, and Run-source schemas.
- [x] Add promotion scopes and permission checks.
- [x] Add the migration, SQL queries, generated store code, and integration
      tests for Stages, policies, Promotions, decisions, and snapshots.
- [x] Add `internal/promotion` domain types and validation without HTTP or
      Kubernetes dependencies.
- [x] Update `PLAN.md`, `AUTH.md`, Helm values, schemas, and chart documentation
      for the accepted configuration and permissions.

Accepted outcome: generated clients compile and PostgreSQL durably enforces
Promotion identity, policy, decision, and transition invariants.

### 2. Controller Stage observation

- [x] Add optional namespaced Kargo Stage discovery to the Controller.
- [x] Normalize the established Stage labels, annotations, variables, Freight,
      and successful-render acknowledgement.
- [x] Add the authenticated complete-snapshot handler and atomic persistence.
- [x] Archive missing Stages while preserving policy and history.
- [x] Add optional namespaced RBAC to the controller Helm chart.
- [x] Cover pagination, partial-list failure, repeated snapshots, stale
      snapshots, namespace restrictions, and multi-cluster identity.

Accepted outcome: the Server maintains a restart-safe, multi-cluster Stage
inventory without Kubernetes access.

### 3. Kargo request and preparation dispatch

- [x] Implement the Kargo machine endpoint and exact request-digest
      idempotency behavior.
- [x] Snapshot the observed Stage, repository, policy, and requesting actor in
      one durable request boundary.
- [x] Approve automatic requests immediately.
- [x] Add the restart-safe dispatcher that creates one revision-pinned Run for
      a guarded Promotion.
- [x] Add the `promotion` Run source and UI/API backlink.
- [x] Reconcile Run completion into `pending` or `error` without implicit
      retries.
- [x] Protect nonterminal Promotion artifacts from retention expiry.

Accepted outcome: repeated Kargo polls recover one durable Promotion, and a
guarded request reaches `pending` through an ordinary Slipramp Run.

### 4. Diff runtime and artifacts

- [x] Move the reusable read-only Git and Argo CD diff logic from
      `kargo-gate` into the first-party Kargo tools command.
- [x] Build and pin its runtime image with Git and Argo CD versions.
- [x] Define the `kargo-promotions` Runtime, Target, and `prepare-review` Action
      in the operational repository.
- [x] Emit bounded Git and structured Argo CD diff artifacts, including
      deterministic empty results.
- [x] Validate candidate ref identity and fail closed for an unknown
      environment or Argo CD target.
- [x] Add unit tests for output parsing and command execution against a real
      Git repository.
- [x] Add an execution acceptance test using real artifact upload.

Accepted outcome: a real guarded Promotion presents exact Git and Argo CD
diffs retained through the normal artifact path.

The disposable kind acceptance suite covers automatic approval without a Run
and guarded preparations with both changed and empty Git diffs. It executes
the first-party tools image, verifies revision pinning and actor attribution,
downloads the retained sensitive artifacts after Job deletion, and checks
that repeated machine polls preserve one preparation Run. Stage inventory is
seeded through the authenticated Controller snapshot API; an Argo CD command
fixture supplies deterministic resource diffs. Live Kargo and Argo CD cutover
coverage remains in milestone 6.

### 5. Operator review experience

- [x] Implement Promotion list, detail, Stage list, policy, and decision
      handlers behind generated interfaces.
- [x] Add the Promotions and Stage React Router routes using the generated
      frontend client.
- [x] Render structured resource diffs with filtering and version-only hiding.
- [x] Enforce one immutable decision with actor attribution and conflict-safe
      retries.
- [x] Add pending-review expiry and clear terminal messages.
- [x] Cover role and scope behavior, sensitive artifact access, concurrent
      decisions, expiry races, and Server restart recovery.

Accepted outcome: an authenticated operator can inspect, approve, reject, and
temporarily change policy without using the standalone service.

### 6. Kargo cutover

- [x] Update the Kargo promotion tasks to use Slipramp bearer authentication
      and the new machine endpoint.
- [x] Add the target-branch `git-push` after approval and before
      `argocd-update`.
- [ ] Exercise automatic, approved, rejected, expired, preparation-failed, and
      stale-target cases against a noncritical Stage.
- [ ] Recreate any currently active per-Stage policies explicitly; do not add a
      legacy JSON importer.
- [ ] Remove the standalone `kargo-gate` Deployment, Service, routes, PVC,
      service account, RBAC, machine API key, and obsolete Argo CD and Git
      credentials after acceptance.
- [ ] Retain or deliberately clean existing candidate branches only after
      verifying that no pending Promotion still depends on them.

On 2026-10-22, the guarded path was exercised end to end with the noncritical
`ops-ci-homelab` Stage. Kargo submitted the request with its shared Slipramp
credential, Slipramp ran the repository-defined preparation Action, retained
the real Git and Argo CD diff artifacts (including an empty Git diff), accepted
an operator approval, and Kargo resumed through the target-branch push and
`argocd-update`. The Promotion succeeded and the homelab Argo CD application
was healthy and synced. Preparation failures were also observed during
bring-up and recovered by starting a new Promotion after correcting the tools
image and empty-artifact upload path. Rejection, expiry, and stale-target
coverage remain outstanding, so the full cutover test-matrix item stays open.

Accepted outcome: production Kargo Promotions use Slipramp exclusively and no
standalone gate state or credential remains authoritative.

### 7. Operational views and notifications

- [x] Add configurable environment ordering and Freight pipeline comparisons;
      do not hard-code `homelab` and `prod`.
- [x] Add cached, repository-grouped branch drift scanning with no-op render
      acknowledgements.
- [ ] Add candidate-branch expiry and cleanup as an explicit, observable
      operation with protected pending branches.
- [ ] Add notification delivery for newly pending reviews, beginning with
      Slack, without making notification success part of gate state.
- [ ] Add status and troubleshooting documentation for Stage freshness, diff
      failures, expired reviews, and stale target pushes.

Accepted outcome: the operator views cover the remaining standalone
`kargo-gate` behavior without weakening the core execution boundaries.

## Required Tests

The implementation is not complete without focused coverage of:

- request validation and canonical digest stability;
- same-ID same-body recovery and same-ID different-body conflict;
- policy expiry and snapshot immutability;
- automatic approval without creating a Run;
- one preparation Run across concurrent polls and Server restarts;
- exact catalog-revision pinning for preparation;
- preparation success, missing artifact, failed Run, and deleted artifact;
- concurrent approve and reject attempts;
- actor attribution for submit, policy update, and decision;
- controller identity, namespace restrictions, pagination, and full-snapshot
  archival behavior;
- disconnected Controllers and stale Stage observation behavior;
- artifact sensitivity and retention protection;
- stale target-branch rejection in the Kargo task;
- OpenAPI and SQL generated-output freshness;
- Helm schema, RBAC, and deterministic rendering;
- a disposable-cluster acceptance path covering one automatic and one guarded
  Promotion.

## Deferred Decisions

These choices remain deliberately outside the first review slice:

- whether Kargo reports post-approval push and reconciliation results back to
  Slipramp;
- whether a failed preparation may be retried under the same external
  Promotion identity through an explicit operator action;
- whether policy changes need a general audit-event table beyond their current
  actor snapshot and Promotion history;
- whether notification delivery becomes a reusable Slipramp facility;
- whether a second integration justifies extracting provider-neutral review
  abstractions from the Kargo-specific contract.

Do not introduce abstractions for these decisions until a milestone requires
them.
