# Slipramp Architecture

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. Git supplies versioned operational definitions, container images supply
tools, and Kubernetes supplies disposable execution capacity across one or more
Clusters. Slipramp coordinates tools such as Terraform/OpenTofu, Ansible,
pyinfra, and repository scripts rather than replacing them.

This document records the system shape and the decisions that constrain every
feature. Focused design documents hold current domain contracts:

- [Repositories and catalogs](./design/catalog.md)
- [Runs and scheduling](./design/runs.md)
- [Controller execution](./design/execution.md)
- [Logs and Artifacts](./design/artifacts.md)
- [Identity and authorization](./design/identity.md)
- [Kargo Promotions](./design/kargo.md)
- [Notifications and Inbox](./design/notifications.md)

[ROADMAP.md](./ROADMAP.md) contains unfinished work. [OpenAPI](../api/openapi.yaml)
is authoritative for HTTP contracts. The [README](../README.md) maps operator,
contributor, and client documentation. Git history records implementation
sequence; design documents describe only the current system.

Contracts remain pre-stable. Prefer deliberate breaking changes that update all
callers, fixtures, generated code, and documentation together. Do not add
compatibility aliases, legacy routes, migration shims, or dual behavior unless
explicitly requested.

## Governing decisions

- The Slipramp Server has no kubeconfig, Kubernetes client, service-account
  token, or direct Kubernetes API access.
- One Controller runs in each execution Cluster and reaches the Server through
  an outbound authenticated HTTP API. Execution Clusters need no inbound
  control-plane connection.
- Multiple execution Clusters are first-class. The Server, Controllers, and
  execution Jobs may all run in different Clusters.
- PostgreSQL is authoritative for product state, history, scheduling, identity,
  and Artifact metadata. S3-compatible storage holds Artifact bytes only.
- Every Run is an immutable execution snapshot. Jobs clone the exact Git commit
  directly; repository archives and source bundles are never copied through
  PostgreSQL or object storage.
- Controller delivery is at least once and idempotent through deterministic Run
  and Job identity. The design does not require a broker, bidirectional RPC,
  distributed leases, leader election, or implicit infrastructure-command
  retries.
- Active Runs for the same Target are serialized by default. Removing a Target
  archives product state and never means infrastructure destruction.
- Safety policy is opt-in. Direct actions, including unreviewed apply commands,
  remain valid. Preview Artifacts, Run review, and Kargo review add optional
  controls rather than a mandatory approval framework.
- Repository maintainers execute with the authority granted by a Placement.
  Containers are not treated as a complete security boundary.
- The Harness is an internal execution component. Tools and Runtime images do
  not need a Slipramp SDK.
- Secrets stay outside PostgreSQL. Slipramp stores logical references and uses
  tool-native environment, file, directory, service-account, and Kubernetes
  Secret conventions for injection.

## Components and authority

- **Slipramp Server:** repository refresh, catalog validation, scheduling,
  identity, Kargo coordination, notifications, Artifact ingestion, and product
  APIs. It has no Kubernetes client or credentials.
- **Controller:** outbound work polling, local Job management, Kargo observation
  and materialization, Run observation, and log forwarding. One replica owns
  namespace-scoped access in each execution Cluster.
- **Harness:** checkout and Action process wrapping, signal forwarding,
  exit-status preservation, and Artifact transfer. Slipramp injects it; tools do
  not integrate with it.
- **Web and clients:** operator workflows over generated product API clients.
  The Server remains the authorization and validation authority.
- **PostgreSQL:** authoritative product state and coordination. It stores
  credential digests and secret references, never injected secret values.
- **S3-compatible storage:** live and completed log bytes and other Artifact
  contents. Only the Server accesses it; RustFS is the local implementation.

The control plane is the central subsystem; the Server is its deployable Go
process. Kubernetes is an execution backend, not the product database.

## Domain model

| Name | Meaning |
| --- | --- |
| Repository | A Git clone URL and tracked branch supplying versioned definitions |
| Catalog revision | An immutable validated catalog from one exact Git commit |
| Target | A durable operational unit containing user-defined Actions |
| Action | One command with parameters, execution policy, triggers, and Artifacts |
| Runtime | A repository-defined container environment and process or credential conventions |
| Placement | Server policy for Cluster, namespace, service account, credentials, resources, and pod settings |
| Cluster | A Kubernetes execution destination represented by its Controller |
| Run | An immutable Action execution snapshot plus independently recorded observations |
| Artifact | Durable Run output, including logs, plans, reports, and diff files |
| Principal | A durable user or service-account actor authenticated by a session or API key |
| Promotion | A durable Kargo request, policy snapshot, preparation, optional decision, and separately observed Kargo outcome |
| Notification event | An immutable channel-neutral snapshot of a selected durable transition |
| Notification delivery | Per-destination operational state for one notification event |

Repository plus Target name is the stable catalog identity of a Target. Removing
a Target archives it and preserves history; restoring the same key reactivates
the durable identity. Renaming creates a new Target. Historical Runs retain
their frozen Target presentation and catalog revision.

Actions are independent executions. Multi-step procedures belong in the invoked
script, task runner, or container rather than a Slipramp workflow graph. A Run
records its source; there is no separate invocation resource.

`sync` is an optional recognized Action name. It supplies factual reconciliation
checkpoints when present, but Targets without it remain valid. Slipramp compares
immutable desired and successful markers and never claims continuous
infrastructure convergence.

## State and consistency

PostgreSQL transactions join state changes that must not diverge: catalog
activation and Target updates, Run creation and trigger linkage, terminal Run
observations and reconciliation checkpoints, Promotion transitions and
preparation linkage, and notification events with intended deliveries.

External effects use durable intent plus idempotent reconciliation. A database
commit never assumes that a Kubernetes request, Git operation, object-store
write, or provider delivery happened exactly once. Stable identifiers, content
digests, monotonic offsets, row locking, and bounded retry state make ambiguous
results recoverable.

Object storage is not queried as a product index. PostgreSQL metadata identifies
every owned object and its lifecycle. Bytes are written before authoritative
metadata advances; retention and orphan reconciliation are explicit Server
workflows.

## Core flows

### Repository to catalog

The Server fetches each active Repository, checks out one exact commit in
temporary storage, validates the complete catalog, and atomically activates a
new immutable revision. Invalid heads retain a safe diagnostic while the last
valid revision stays active. Target relevance advances only for watched paths
affected by an accepted head. See [Repositories and catalogs](./design/catalog.md).

### Run to execution

All Run sources use one creation service that resolves the catalog, validates
inputs, and freezes the execution contract. PostgreSQL schedules eligible work
under Target, Cluster, and Placement limits. A Controller claims work, creates a
deterministically named local Job, observes it through completion, streams logs,
reports terminal facts, and removes the Job only after durable recovery points
exist. See [Runs and scheduling](./design/runs.md),
[Controller execution](./design/execution.md), and
[Logs and Artifacts](./design/artifacts.md).

### Kargo Promotion

Kargo remains authoritative for Freight eligibility, rendering, target-branch
updates, and downstream reconciliation. Slipramp inventories Stages, snapshots
policy and request context, optionally prepares review Artifacts through an
ordinary Run, records decisions, and uses the responsible Controller for the
small set of Kubernetes operations it cannot perform directly. Approval is
permission to proceed; an independently recorded terminal Kargo phase describes
what happened afterward without implying additional Argo CD health. See
[Kargo Promotions](./design/kargo.md).

### Notification delivery

Selected domain transitions may atomically create immutable notification events
and per-destination delivery intent. Provider delivery is at least once and
never changes the subject's domain state. The operational Inbox is a separate
current-state projection, not a notification queue. See
[Notifications and Inbox](./design/notifications.md).

## Trust and network boundaries

Controllers require outbound access to the Server. Execution Jobs require
outbound access to Git and the Server's execution Artifact endpoints. Use HTTPS
across untrusted networks; plaintext HTTP is valid only when a trusted encrypted
network, such as a tailnet, already protects the transport.

Product sessions and API keys authenticate Principals. Controller and
Run-scoped credentials belong to separate boundaries and never become product
Principals. Authorization is enforced in Server domain services rather than
trusted to client UI. See [Identity and authorization](./design/identity.md).

Execution credentials resolve inside the selected Cluster. Server-side Git and
notification credentials resolve from administrator-mounted configuration.
Secret values, webhook URLs, and secret file paths are excluded from product
state, APIs, and structured logs.

## Interfaces

OpenAPI is the source of truth for every Server endpoint used by the Web,
generated clients, or Controllers. The repository bundles and validates the
specification and generates Go server types and interfaces, the Controller
client, TypeScript types and frontend client, and Swift client models. Generated
files are never edited manually.

The remote MCP adapter is a separate protocol boundary at `/mcp`. It maps scoped
API keys or OAuth access tokens to existing Principals and invokes the same
domain services as the product API; it does not loop back through HTTP or create
a second authorization model. See [MCP.md](./MCP.md).

The Web, CLI, and iOS application are projections over Server-owned contracts.
They may provide purpose-built transports for reconnectable log streaming, but
they do not reimplement catalog resolution, authorization, scheduling, or Run
validation. See [CLI.md](./CLI.md) and the [iOS client](../ios/README.md).

## Deployment and operations

The supported topology is one Server/Web pair and one Controller per execution
Cluster, backed by external PostgreSQL and S3-compatible storage. The central
deployment grants the Server no Kubernetes RBAC. Controller permissions are
limited to owned namespaces and optional configured Kargo resources. See
[INSTALLATION.md](./INSTALLATION.md) and [CONFIGURATION.md](./CONFIGURATION.md).

Health, readiness, metrics, logs, and graceful shutdown are operational
interfaces, not alternate state authorities. The metric contract is documented
in [METRICS.md](./METRICS.md); local services, generation, public sites, and
validation commands are documented in [DEVELOPMENT.md](./DEVELOPMENT.md).
