# Slipramp MVP Plan

This document turns the direction in [PLAN.md](./PLAN.md) into the first usable Slipramp release.

## Objective

The MVP proves the complete operational loop:

1. Connect a Git repository and track one branch.
2. Discover runtimes, targets, and actions from versioned YAML.
3. Invoke an action manually through the UI or API.
4. Execute the exact Git commit as a Kubernetes Job through the Slipramp harness.
5. Stream logs live while retaining them as S3-backed artifacts.
6. Publish and download declared artifacts.
7. Reuse a preview artifact, such as a Terraform plan, in a later action.

The intended deployment is a single trusted Slipramp installation executing work across one or more Kubernetes clusters. Authentication and human attribution are not required for this release; the deployment must be protected externally when exposed beyond a trusted network.

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

### Target and action model

- Support targets with arbitrary named actions; `sync` remains optional.
- Support target defaults for runtime, placement, credentials, timeout, and resources.
- Allow an action to override target execution defaults.
- Support explicit argument arrays without an implicit shell.
- Support typed string, boolean, number, and enum parameters.
- Map declared parameters to arguments or environment variables.
- Resolve every run request against an immutable catalog revision and Git commit SHA.

### Controllers, placements, and credentials

- Run a Slipramp controller inside every execution cluster.
- Run one controller replica per execution cluster with a stable cluster name and static bearer credential.
- Have controllers use a small versioned HTTPS API and long-poll the Slipramp Server for command envelopes carrying a run assignment or cancellation.
- Treat ordinary authenticated controller request traffic as the cluster heartbeat and store only a last-seen time; do not introduce a separate heartbeat protocol.
- Keep all Kubernetes clients, RBAC, Job creation, observation, and cancellation inside the corresponding cluster controller.
- Require no kubeconfig or Kubernetes API access on the Slipramp Server.
- Report controller and cluster health from the last successful poll or report.
- Require each execution cluster to reach the configured S3-compatible endpoint.
- Require execution clusters to reach configured Git repositories directly.
- Support per-cluster mappings from logical Git credential names to checkout-only Kubernetes Secrets.
- Load named placements from Slipramp Server configuration.
- Require each placement to select a registered cluster name.
- Support namespace, ServiceAccount, resource requests and limits, node selection, and timeout defaults.
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
- Inject a static Slipramp harness without requiring it in the runtime image.
- Have the harness launch the configured process, forward signals, preserve its exit code, and collect artifacts.
- Use `backoffLimit: 0` unless retry behavior is explicitly introduced later.
- Apply a run deadline and return cancellation through the same long-poll API.
- Serialize active runs for the same target by default.
- Derive an unknown observation from controller connectivity and stale `lastObservedAt`; do not persist `unknown` as a Run lifecycle phase or terminal outcome.
- Recover active run observations when either the Slipramp Server or a controller restarts by scanning labeled Jobs before polling for more work.

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
- Expose artifact metadata, download, sensitivity, provenance, and retention state through the API and UI.

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
- Keep the run-creation boundary independent from HTTP so future Git, schedule, and webhook triggers can call the same service.
- Let repository refresh update the catalog and affected targets' desired Git commits without initiating actions.
- Reject unsupported trigger declarations explicitly rather than silently ignoring them.

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
  - Repository list and connection form
  - Repository status and validation errors
  - Controller and cluster health
  - Target catalog and target detail
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
- **PostgreSQL:** repositories, catalog revisions, targets, cluster observations, runs, phase transitions, run request idempotency, artifact metadata, and scheduler coordination.
- **S3-compatible storage:** log chunks and declared artifact contents only; RustFS is used locally.
- **Kubernetes clusters:** controllers, execution Jobs, Secrets, ServiceAccounts, and workload placement in each registered destination.

## Durable Model

The initial PostgreSQL schema should center on these records:

- repositories and repository refresh state
- immutable catalog revisions and validation results
- current target and runtime projections
- clusters, controller last-seen observations, and run assignments
- runs and run phase transitions
- run request idempotency keys
- target concurrency leases
- artifacts and ordered log chunks

Catalog revisions retain the resolved definitions needed to explain or reproduce a run even after Git advances. S3 object keys are derived from stable run and artifact IDs, while PostgreSQL remains authoritative for object metadata and ordering.

## Delivery Milestones

### 1. Project foundation

- Initialize the Go module and TypeScript web workspace.
- Establish the Server, cluster-controller, harness, and web entry points.
- Establish the split OpenAPI contract, bundling and validation, pinned Go and TypeScript generation, and a single regeneration command.
- Add PostgreSQL migrations and S3 client boundaries.
- Add local PostgreSQL and RustFS dependencies.
- Add versioned configuration loading for the Server, controllers, and placements.
- Establish the authenticated controller HTTPS API, cluster last-seen tracking, long polling, and idempotent work delivery.
- Establish structured logging, configuration validation, and graceful shutdown.
- Add the shared local and CI quality commands.

Exit condition: all components build, connect to their dependencies, and pass formatting, linting, type checking, and baseline tests.

### 2. Git catalog

- Implement repository persistence and credential references.
- Refresh configured branches by fetching their latest commits.
- Define the discovery layout and parse `Runtime` and `Target` documents.
- Validate references, defaults, actions, parameters, credentials, and artifacts, and report trigger declarations as unsupported in this release.
- Store immutable catalog revisions and expose current catalog APIs.
- Build repository and target catalog screens.

Exit condition: connecting a repository produces a browsable target/action catalog, and an invalid commit leaves the previous catalog active with visible diagnostics.

### 3. Run execution

- Implement run creation and frozen definition resolution.
- Implement target concurrency and the scheduler.
- Resolve the placement's cluster and enqueue the frozen run for its controller.
- Have the controller render credentials, parameters, and action overrides into a local Job.
- Clone and materialize the exact Git commit directly inside that execution Job.
- Observe Job conditions and the action container state locally, and report lifecycle phase, terminal outcome, reason, message, exit code, Job UID, and observation freshness through the controller HTTP API.
- Implement cancellation, deadlines, restart recovery, and explicit no-retry behavior.
- Build action-run and run-status screens.

Exit condition: arbitrary repository scripts can be invoked from the UI and API, routed to different registered clusters, execute at the requested commit, receive cluster-local credentials and parameters, and produce durable outcomes.

### 4. Streaming logs and artifacts

- Implement controller log forwarding, ordered S3-backed log chunks, and PostgreSQL metadata.
- Add live streaming with reconnect cursors.
- Seal completed log artifacts with size and digest.
- Implement harness upload of declared artifacts.
- Implement artifact download and input materialization.
- Add artifact views to run detail.

Exit condition: logs appear live, survive Pod deletion and Server restart, and declared files can be uploaded, downloaded, and supplied to another run.

### 5. Preview promotion

- Implement artifact compatibility checks.
- Add the **Apply this plan** API and UI flow.
- Pin the apply run to the plan's original Git commit and catalog revision.
- Demonstrate the flow with the Terraform example.

Exit condition: a Terraform preview produces a plan artifact that can be selected and applied without generating a new plan, while direct apply remains available.

### 6. Operational hardening

- Evaluate `watch` paths after catalog refresh and show affected targets' desired Git commits without initiating actions.
- Exercise recovery from Server, controller, Pod, network, and individual cluster interruptions.
- Add retention controls and orphaned-object reconciliation.
- Add health, readiness, and dependency diagnostics.
- Complete installation and operator documentation.

Exit condition: the installation reports per-cluster health, preserves manual run request idempotency, and recovers cleanly from expected component and cluster interruptions.

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
- Frontend unit and component tests
- PostgreSQL and S3 integration tests
- Controller HTTP API, idempotent Job routing, and recovery tests against multiple disposable clusters

Warnings fail CI. Tool versions are pinned, generated files are reproducible, migrations are tested in both directions where practical, and changed code is expected to include proportionate tests.

## Definition of Done

The MVP is complete when a fresh installation can:

1. Configure PostgreSQL, an S3-compatible endpoint, deploy authenticated controllers into multiple Kubernetes clusters, and configure placements using their registered cluster names.
2. Connect a private Git repository and continuously index its configured branch.
3. Display valid targets and arbitrary actions from repository YAML.
4. Route actions through different placements to different Kubernetes clusters and run them at exact commits without modifying their runtime images for Slipramp.
5. Inject parameters and Kubernetes-backed credentials using standard tool conventions.
6. Show live reconnectable logs and retain them as downloadable S3-backed artifacts.
7. Upload and download declared artifacts.
8. Apply an exact Terraform plan artifact from its producing Git commit and catalog revision.
9. Cancel a run and recover run observation after either the Slipramp Server or cluster controller restarts.
10. Detect relevant Git changes without automatically executing actions, while preserving a trigger-ready run-creation boundary.
11. Generate the Go Server contract and TypeScript frontend client reproducibly from the validated split OpenAPI specification.
12. Pass all formatting, linting, type checking, unit, integration, and build checks.

## Deferred Work

- Built-in authentication, user management, authorization, and human attribution
- Approval workflows and required previews
- Git-change, scheduled, and webhook triggers
- High-availability Server and web deployments
- Controller high availability, leader election, and multiple active replicas for one cluster name
- Pull-request and ephemeral environments
- Standalone actions outside targets
- Tool-specific output parsing or provider integrations
- General workflow graphs and multi-step pipelines
- Policy engines and mandatory image restrictions
- Advanced artifact retention, archival, and cross-installation replication
- Rich drift detection beyond optional user-defined actions
