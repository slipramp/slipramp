# Slipramp MVP Baseline

This document records the accepted first usable Slipramp release derived from
[PLAN.md](./PLAN.md). The MVP is complete. Its scope remains here as the
baseline for ongoing development, not as an open delivery checklist.

Acceptance combines the repository's automated quality, integration, and
disposable-cluster suites. Further resilience drills, diagnostics, upgrades,
and operator guidance continue as normal product work without reopening the
MVP.

## Objective

The MVP established the complete operational loop:

1. Connect a Git repository and track one branch.
2. Discover runtimes, targets, and actions from versioned YAML.
3. Invoke an action manually through the UI or API.
4. Execute the exact Git commit as a Kubernetes Job through the Slipramp harness.
5. Stream logs live while retaining them as S3-backed artifacts.
6. Publish and download declared artifacts.
7. Reuse a preview artifact, such as a Terraform plan, in a later action.
8. Start eligible actions from newly accepted Git heads through durable,
   idempotent trigger delivery.

The accepted deployment model is a single trusted Slipramp installation
executing work across one or more Kubernetes clusters. Authentication and human
attribution were not included in this historical baseline. They are now built
in as described in [AUTH.md](./AUTH.md).

## Scope

### Repository catalog

- Add and remove generic SSH or HTTPS Git repositories.
- Reference repository refresh credentials used by the Slipramp Server through named sources mounted into its deployment, such as files or environment variables populated from Kubernetes Secrets; the Server does not resolve them through the Kubernetes API.
- Assign a logical checkout credential to private repositories and resolve it to a cluster-local Kubernetes Secret in each execution cluster.
- Refresh one configured branch per repository on an interval.
- Fetch and index exact commit SHAs.
- Never copy repository contents, source bundles, or Git source archives into S3; execution Jobs clone their exact commit directly from Git.
- Discover versioned `Runtime` and `Target` documents from a documented repository layout.
- Validate each fetched Git commit atomically and retain the previous valid catalog revision when a commit is invalid.
- Show repository health, active catalog revision, last refresh result, and validation errors.
- Compute changed paths between indexed Git commits for target `watch` evaluation.
- Record matching named Git-trigger deliveries atomically when a non-initial,
  changed repository head is accepted.

### Target and action model

- Support targets with arbitrary named actions; `sync` remains optional.
- Support target defaults for runtime, placement, credentials, timeout, and resources.
- Allow an action to override target execution defaults.
- Support explicit argument arrays without an implicit shell.
- Support typed string, boolean, number, and enum parameters.
- Map declared parameters to arguments or environment variables.
- Resolve every run request against an immutable catalog revision and Git commit SHA.
- Support named `git: {}` action triggers that use the target's `watch`
  patterns, optionally supply static action parameters, and reject triggers on
  actions with artifact inputs. Validate required, declared, typed, and enum
  parameter constraints while loading the catalog.

### Controllers, placements, and credentials

- Run a Slipramp controller inside every execution cluster.
- Run one controller replica per execution cluster with a stable cluster name, a cluster-scoped bootstrap enrollment credential, and a separate controller-owned bearer credential.
- Enroll the controller-owned credential at startup, persist only its SHA-256 digest in PostgreSQL, and derive cluster identity from that credential on all ongoing controller routes.
- Auto-approve a controller presenting the configured enrollment credential; explicit enrollment approvals and credential rotation workflows remain later work.
- Have controllers use a small versioned HTTP API and long-poll the Slipramp Server for command envelopes carrying a run assignment or cancellation. Require HTTPS across untrusted networks; allow plaintext HTTP when the transport is already protected by a trusted encrypted network such as a tailnet.
- Treat ordinary authenticated controller request traffic as the cluster heartbeat and store only a last-seen time; do not introduce a separate heartbeat protocol.
- Keep all Kubernetes clients, RBAC, Job creation, observation, and cancellation inside the corresponding cluster controller.
- Require no kubeconfig or Kubernetes API access on the Slipramp Server.
- Report controller and cluster health from the last successful poll or report.
- Require controllers and execution Jobs to reach the Slipramp Server; only the
  Server needs access to the configured S3-compatible endpoint.
- Require execution clusters to reach configured Git repositories directly.
- Support per-cluster mappings from logical Git credential names to checkout-only Kubernetes Secrets.
- Load named placements from Slipramp Server configuration.
- Require each placement to select a registered cluster name.
- Support namespace, ServiceAccount, resource requests and limits, node selection, and timeout defaults.
- Treat every namespace selected by a placement as exclusively owned by one Slipramp Controller and control plane. A controller may own multiple execution namespaces; before another control plane reuses one, operators must remove all Slipramp Jobs left by its previous owner. Do not add control-plane identity labels or compatibility behavior for namespace reuse.
- Configure each Controller with its owned execution namespaces, reject work
  outside that list, and recover Jobs namespace by namespace so installation
  RBAC remains namespace-scoped.
- Expose named Kubernetes Secret sources through placements.
- Bind target credential requirements to placement sources.
- Inject credentials as individual environment variables, `envFrom`, individual files, or mounted directories.
- Use native Kubernetes Secret references without persisting secret values in PostgreSQL.
- Resolve execution Secret references in the cluster selected by the placement.
- Permit placement-level pod configuration for cluster-specific workload identity or volume mechanisms.

### Kubernetes execution

- Queue each run for the cluster selected by its resolved placement.
- Return a run-assignment command from the controller's long poll and create one local Kubernetes Job with a deterministic name and stable labels.
- Make delivery at least once and make repeated run assignments idempotent instead of introducing a distributed lease protocol.
- Keep a run `assigned` until the controller reports the materialized Job UID and `preparing` phase. Reject permanent materialization failures as a terminal failed outcome so one bad assignment cannot block its cluster's delivery stream.
- Clone the repository and check out the run's exact commit into a shared workspace through an init container.
- Make the Pod-private checkout writable by the runtime container without requiring the checkout and runtime images to share a numeric user. Use placement pod security context, including `fsGroup` when needed, for restrictive Secret file modes.
- Resolve private-repository checkout credentials from the selected cluster without persisting their values in PostgreSQL.
- Inject a static Slipramp harness without requiring it in the checkout or runtime image.
- Have the harness launch cancellable checkout and action processes, forward signals, preserve exit status, and collect declared action artifacts.
- Use `backoffLimit: 0` unless retry behavior is explicitly introduced later.
- Apply a run deadline and return cancellation through the same long-poll API.
- Serialize active runs for the same target by default.
- Derive an unknown observation from controller connectivity and stale `lastObservedAt`; do not persist `unknown` as a Run lifecycle phase or terminal outcome.
- Recover active run observations when either the Slipramp Server or a controller restarts by scanning labeled Jobs before polling for more work.
- For every terminal Job, upload and seal its complete log, successfully report the terminal observation, delete the Job with its UID as a Kubernetes deletion precondition, and then stop its observer. Make each step idempotent so restart recovery repeats accepted sealing and reporting operations and completes deletion.
- Follow each active Pod log once and forward bounded chunks instead of repeatedly downloading the complete log. On recovery, replay only to validate and skip the Server's durable prefix. Request cancellation through a controller-owned Pod annotation projected into the checkout and action containers; have the harness terminate the active child process, then let ordinary terminal observation drain and seal the log, report the canceled outcome, and delete the Job.
- Do not set `ttlSecondsAfterFinished`; the controller must retain the Job and its Pod logs through Server or object-storage outages. Retry transient log, report, and delete failures without deleting prematurely.
- Treat a `404` Run or log rejection as a namespace-ownership invariant violation: warn once, stop observing, and leave the Job for manual diagnosis instead of retrying continuously.

### Runs and status

- Persist run metadata and phase transitions in PostgreSQL.
- Model lifecycle phase separately as `queued`, `assigned`, `preparing`, `running`, or `finished`. Record the terminal outcome separately as `succeeded`, `failed`, `canceled`, or `timed-out`, and record cancellation intent separately from both.
- Record the repository, commit, catalog revision, target, action, parameters, artifact inputs, cluster, placement configuration, run source, timestamps, Job UID, observation freshness, outcome reason and message, and exit code.
- Expose run history by repository and target.
- Never mutate a run after dispatch except for its lifecycle observations, cancellation request, timestamps, and produced artifact metadata.

### Logs and artifacts

- Create the log artifact when a run begins.
- Have the cluster controller tail action output from Kubernetes and post chunks with monotonically increasing byte offsets to the Slipramp Server.
- Accept repeated log chunks idempotently by run ID and offset.
- Persist received chunks as ordered S3 objects and publish them to connected clients only through the same ingestion path.
- Stream live logs to the browser over a reconnectable HTTP stream with cursor or offset support.
- Seal the log artifact with final size and digest when the run ends.
- Store all artifact contents in S3-compatible storage and all artifact metadata in PostgreSQL.
- Use RustFS as the documented local S3-compatible service.
- Let the harness upload declared files using run-scoped upload authorization.
- Let artifact-consuming runs download selected inputs into declared paths before execution.
- Keep run-scoped artifact credentials out of the child action environment and
  authorize only the Run's declared outputs and explicit input bindings.
- Expose artifact metadata, download, sensitivity, provenance, and retention state through the API and UI.
- Expire sealed artifacts after one installation-wide retention period, while
  retaining artifacts produced by or bound to unfinished Runs. Delete their S3
  objects idempotently before marking metadata deleted, and retry failed deletes.
- Reconcile unreferenced objects under the Server-owned `runs/` prefix after a
  grace period so interrupted uploads do not leak contents and in-flight
  metadata commits are not raced.

### Preview and apply

- Allow an action to declare a named output artifact such as `plan`.
- Allow another action to require an artifact produced by a specific source action.
- Provide an **Apply this plan** operation from a successful preview run.
- Pin the consuming run to the producing run's target, Git commit, and catalog revision.
- Make direct execution independent of the preview path; a target may still expose a direct `sync` or other apply action.

### Run creation boundary

- Treat manual and API run requests as always available unless the deployment later adds policy.
- Route UI and API requests through one run-creation service rather than dispatching Jobs directly.
- Record a structured run source and accept an optional idempotency key.
- Keep the run-creation boundary independent from HTTP so Git delivery and
  future schedule or webhook triggers call the same service.
- Dispatch persisted Git-trigger deliveries with their exact catalog revision
  and stable delivery ID as the Run idempotency identity. Persist the
  normalized trigger parameter object on the delivery and pass that frozen
  value through ordinary Run creation.
- Keep initial indexing, unchanged commits, rejected catalogs, and
  non-matching changed paths from producing deliveries.
- Record Git Run sources with only their trigger name and delivery ID and no
  human actor; keep manual and API behavior unchanged.
- Reject unsupported trigger sources explicitly rather than silently ignoring them.

## Incremental Behavior After the MVP

Automatic Git-triggered work now uses latest-wins coalescing per Repository and
Target. In the same transaction that activates a catalog revision and advances
or archives Target state, Slipramp preserves superseded delivery records,
excludes them from pending selection, finishes stale queued Git Runs as
canceled with reason `SupersededByGitChange`, records the corresponding phase
transitions, and creates matching deliveries for affected active Targets.
Assigned, preparing, and running Git Runs continue to completion, with only the
newest queued Git revision retained behind an active Run. Manual and API Runs
remain ordinary FIFO work and are never superseded.

Git Run insertion also locks and reads the durable Target state in PostgreSQL.
If the delivery's exact catalog-revision pin resolves to a commit other than
the Target's current desired commit, the delivery is marked superseded and no
Run is created. This database boundary closes the refresh/dispatch race while
preserving static trigger parameters and delivery-ID idempotency.

### API and web application

- Define the complete Slipramp Server HTTP API in OpenAPI, including web and controller endpoints.
- Keep a small root OpenAPI document with domain-sized path and component files for repositories, catalog, runs, artifacts, and controllers; do not split individual endpoints or models into separate files.
- Bundle and validate the referenced files as one versioned API contract.
- Generate Go request and response types, Server interfaces, and the controller client from the contract.
- Generate TypeScript types and the standard frontend API client from the same contract.
- Keep handwritten business logic behind generated Server interfaces and never edit generated code directly.
- Expose the generated versioned JSON API from the Slipramp Server.
- Expose a reconnectable server-sent event stream for live run logs.
- Use generated event payload types with a small handwritten frontend SSE transport for streaming and reconnection.
- Run the React Router v8 application in Framework Mode with SSR as a separate web process.
- Use Mantine UI v9 for application layout and components.
- Present the Go API and web application through one origin in the deployment.
- Provide these primary screens:
  - Cross-repository Target index as the homepage, with server-side search,
    repository filtering, pagination, and compact action summaries suitable for
    hundreds of targets
  - Repository list and connection form
  - Repository status and validation errors
  - Controller and cluster health on a dedicated screen
  - Target detail with collapsible action definitions and run forms
  - Action run form
  - Run history and run detail
  - Live and retained logs
  - Artifact list, download, and **Apply this plan** flow

## System Shape

The repository remains a single project with a Go module at `github.com/slipramp/slipramp` and a TypeScript web workspace. The deployable components are:

- **Server:** central Go process implementing the generated OpenAPI server interfaces, repository refresher, scheduler, controller coordination, log ingestion, and artifact metadata; it has no Kubernetes client.
- **Cluster controller:** Go process deployed into each execution cluster; it claims work outbound, manages local Jobs, and reports state and logs.
- **Harness:** statically linked Go executable injected into runtime Jobs.
- **Web:** React Router v8 SSR server and browser application using Mantine UI and the generated TypeScript API client.
- **PostgreSQL:** repositories, catalog revisions, targets, Git-trigger deliveries, controller credential digests, cluster observations, runs, phase transitions, run request idempotency, artifact metadata, and scheduler coordination.
- **S3-compatible storage:** log chunks and declared artifact contents only; RustFS is used locally.
- **Kubernetes clusters:** controllers, execution Jobs, Secrets, ServiceAccounts, and workload placement in each registered destination.

## Durable Model

The MVP PostgreSQL schema centers on these records:

- repositories and repository refresh state
- immutable catalog revisions and validation results
- current target and runtime projections
- clusters, enrolled controller credential digests, last-seen observations, and run assignments
- runs and run phase transitions
- run request idempotency keys
- Git-trigger delivery outbox records and resulting Run IDs
- target concurrency leases
- artifacts and ordered log chunks

Catalog revisions retain the resolved definitions needed to explain or reproduce a run even after Git advances. S3 object keys are derived from stable run and artifact IDs, while PostgreSQL remains authoritative for object metadata and ordering.

## Completed Delivery Milestones

All six milestones were accepted as a coherent operational baseline. The exit
conditions below record the outcomes used to shape the implementation.

### 1. Project foundation — complete

- Initialized the Go module and TypeScript web workspace.
- Established the Server, cluster-controller, harness, and web entry points.
- Established the split OpenAPI contract, bundling and validation, pinned Go and TypeScript generation, and a single regeneration command.
- Added PostgreSQL migrations and S3 client boundaries.
- Added local PostgreSQL and RustFS dependencies.
- Added versioned configuration loading for the Server, controllers, and placements.
- Established controller enrollment, the authenticated controller HTTP API, cluster last-seen tracking, long polling, and idempotent work delivery, with HTTPS required unless a trusted encrypted network protects plaintext transport.
- Established structured logging, configuration validation, and graceful shutdown.
- Added the shared local and CI quality commands.

Accepted outcome: all components build, connect to their dependencies, and pass formatting, linting, type checking, and baseline tests.

### 2. Git catalog — complete

- Implemented repository persistence and credential references.
- Refreshed configured branches by fetching their latest commits.
- Defined the discovery layout and parsed `Runtime` and `Target` documents.
- Validated references, defaults, actions, parameters, credentials, artifacts,
  and the focused Git-trigger declaration; unsupported trigger sources are
  rejected.
- Stored immutable catalog revisions and exposed current catalog APIs.
- Built repository and target catalog screens.

Accepted outcome: connecting a repository produces a browsable target/action catalog, and an invalid commit leaves the previous catalog active with visible diagnostics.

### 3. Run execution — complete

- Implemented run creation and frozen definition resolution.
- Implemented target concurrency and the scheduler.
- Resolved the placement's cluster and enqueued the frozen Run for its controller.
- Rendered credentials, parameters, and action overrides into a local Job.
- Cloned and materialized the exact Git commit directly inside that execution Job.
- Observed Job conditions and the action container state locally, and reported lifecycle phase, terminal outcome, reason, message, exit code, Job UID, and observation freshness through the controller HTTP API.
- Implemented cancellation, deadlines, restart recovery, and explicit no-retry behavior.
- Built action-run and run-detail screens.

Accepted outcome: arbitrary repository scripts can be invoked from the UI and API, routed to registered clusters, execute at the requested commit, receive cluster-local credentials and parameters, and produce durable outcomes.

### 4. Streaming logs and artifacts — complete

- Implemented controller log forwarding, ordered S3-backed log chunks, and PostgreSQL metadata.
- Added live streaming with reconnect cursors.
- Sealed completed log artifacts with size and digest.
- Implemented harness upload of declared artifacts.
- Implemented artifact download and input materialization.
- Added artifact views to run detail.

Accepted outcome: logs appear live, survive Pod deletion and Server restart, and declared files can be uploaded, downloaded, and supplied to another run.

### 5. Preview promotion — complete

- Implemented artifact compatibility checks.
- Added the **Apply this plan** API and UI flow.
- Pinned the apply Run to the plan's original Git commit and catalog revision.
- Demonstrated the flow with the Terraform example.

Accepted outcome: a Terraform preview produces a plan artifact that can be selected and applied without generating a new plan, while direct apply remains available.

### 6. Operational hardening — complete baseline

- Evaluated `watch` paths after catalog refresh, showed affected targets'
  desired Git commits, and atomically recorded deliveries for matching declared
  Git triggers.
- Exercised Server and controller restart recovery while Runs were active.
- Configured the artifact retention, reconciliation, and orphan grace periods;
  exercised expiration, retryable deletion, and orphaned-object reconciliation.
- Reported process liveness and per-cluster connection health.
- Documented local development and disposable-cluster acceptance.

Accepted outcome: the installation reports per-cluster health, preserves manual run request idempotency, and recovers active Runs across Server and controller restarts.

## Quality Gates

The repository provides a single set of commands used by developers and CI:

- Go formatting through `golangci-lint fmt`
- Go linting through `golangci-lint run`
- Go unit and race tests
- Oxlint with TypeScript, React, and accessibility rules
- Oxfmt verification
- Strict TypeScript checking
- OpenAPI bundling and validation
- Reproducible Go and TypeScript API generation with a stale-output check
- Frontend unit tests
- PostgreSQL and S3 integration tests
- Controller HTTP API, idempotent Job routing, and recovery tests, including a
  disposable kind acceptance environment
- Helm chart linting, schema validation, deterministic rendering checks, and
  focused positive and negative render tests
- Release-version validation and OCI packaging for both supported charts

Warnings fail CI. Tool versions are pinned, generated files are reproducible, migrations are tested in both directions where practical, and changed code is expected to include proportionate tests.

## Accepted Capabilities

The accepted MVP baseline can:

1. Configure PostgreSQL, an S3-compatible endpoint, authenticated cluster controllers, and placements using their registered cluster names.
2. Connect a private Git repository and continuously index its configured branch.
3. Display valid targets and arbitrary actions from repository YAML.
4. Route actions through placements to registered Kubernetes clusters and run them at exact commits without modifying their runtime images for Slipramp.
5. Inject parameters and Kubernetes-backed credentials using standard tool conventions.
6. Show live reconnectable logs and retain them as downloadable S3-backed artifacts.
7. Upload and download declared artifacts.
8. Apply an exact Terraform plan artifact from its producing Git commit and catalog revision.
9. Cancel a run and recover run observation after either the Slipramp Server or cluster controller restarts.
10. Detect relevant Git changes and durably create ordinary, revision-pinned
    Runs for matching named Git triggers after the initial index.
11. Generate the Go Server contract and TypeScript frontend client reproducibly from the validated split OpenAPI specification.
12. Pass all formatting, linting, type checking, unit, integration, and build checks.
13. Install the Server, web process, and one Controller through supported Helm
    charts without giving the Server Kubernetes access or bundling production
    data services, with versioned release charts and matching images published
    to GHCR from a single release tag.

## Ongoing Work Beyond the MVP

The following work is intentionally incremental and does not change the
accepted status of the MVP:

- Broader multi-cluster, Pod-loss, network-interruption, and dependency-outage
  acceptance coverage
- Richer operational status beyond the implemented dependency-aware Server readiness
- Upgrade, backup, recovery, and troubleshooting guidance beyond the supported
  Helm installation and existing development and kind flows
- Evolution and compatibility policy for the pre-stable API, catalog, and
  database contracts
- Further identity administration, audit history, and MCP authentication beyond
  the implemented model in [AUTH.md](./AUTH.md)
- Approval workflows and required previews
- Git trigger filtering overrides, dynamic parameter values, per-commit
  delivery, configurable delivery policies, and provider-specific integrations
- Git-trigger failed delivery-attempt metadata, bounded retry backoff,
  preventing one permanently failing delivery from blocking later eligible
  deliveries, and restart-persistent retry scheduling
- Scheduled and webhook triggers
- High-availability Server and web deployments
- Controller high availability, leader election, and multiple active replicas for one cluster name
- Explicit controller enrollment approvals and credential rotation workflows
- Pull-request and ephemeral environments
- Standalone actions outside targets
- Tool-specific output parsing or provider integrations
- General workflow graphs and multi-step pipelines
- Policy engines and mandatory image restrictions
- Advanced artifact retention, archival, and cross-installation replication
- Rich drift detection beyond optional user-defined actions
