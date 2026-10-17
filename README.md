# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. The product direction is in [`docs/PLAN.md`](docs/PLAN.md), and the first
delivery scope is in [`docs/MVP.md`](docs/MVP.md).

## Current foundation

The current foundation establishes the four Slipramp processes: the Server,
cluster controller, harness, and web application. The split OpenAPI contract
defines the Server's JSON liveness probe at `GET /healthz`, the read-only
cluster health endpoint at `GET /api/v1/clusters`, and the authenticated
controller long poll at `POST /controller/v1/controllers/{clusterName}/poll`.
Repository registrations can be listed and created at
`GET|POST /api/v1/repositories` and removed at
`DELETE /api/v1/repositories/{repositoryId}`. Go request and response types,
Server interfaces, the controller client, and the TypeScript frontend client
are generated together from that contract. API errors use RFC 9457 problem
details with the
`application/problem+json` media type.

The Server connects to PostgreSQL through pgxpool, applies embedded Goose
migrations at startup, and uses sqlc-generated query methods. Artifact contents
use a narrow S3-compatible storage boundary backed by the MinIO Go client. The
configured bucket is provisioned separately and checked at startup so the
Server does not require bucket-administration permissions. PostgreSQL remains
authoritative for product state and artifact metadata.

Repository registrations persist an HTTPS, SSH, or SCP-style Git clone URL,
one tracked branch, and optional names for two distinct credential paths. The
refresh credential name refers to a secret source available to the Server,
while the checkout credential name is resolved to a Kubernetes Secret by each
execution cluster. Secret values and URL-embedded HTTPS credentials are not
accepted. Removing a repository hides it from the active list while retaining
its stable identity for future catalog and run history references.

The Server fetches each active repository when it starts and on the configured
interval. It materializes the exact commit in an isolated temporary checkout,
loads the catalog, and removes the checkout after indexing. Repository contents
are never stored in PostgreSQL or S3.

Repository catalog definitions live below `.slipramp` in the tracked Git tree.
Files ending in `.yaml` or `.yml` are discovered recursively and may each
contain one or more versioned `Runtime` or `Target` documents. The catalog
loader decodes these documents independently of repository refresh and rejects
unknown fields and duplicate mapping keys. It validates repository and
container paths, runtime credential injection, target defaults, Kubernetes
resource quantities, timeouts, action intents and commands, typed parameter
mapping, configured placement and credential-source references, artifact
declarations, and artifact input provenance. Deferred trigger declarations are
rejected. Effective actions deterministically merge target credential and
resource defaults without mutating the source catalog. A valid commit produces
one immutable PostgreSQL catalog revision containing its runtime and target
definitions. Repeated refreshes reuse that revision. A rejected commit records
its SHA and validation diagnostic while leaving the previous valid revision
active. Repository API responses include the latest refresh state; catalog read
access through `GET /api/v1/repositories/{repositoryId}/catalog` returns the
active immutable revision and its complete typed Runtime and Target
definitions. The web application lists repository refresh health and provides
a repository detail screen for browsing the active revision's targets and
actions.

`POST /api/v1/runs` resolves a requested action against the active catalog and
persists an immutable queued Run with validated parameters, an optional
idempotency key, and a frozen repository, target, action, runtime, and placement
snapshot. Controller polls claim the oldest eligible Run for their cluster,
enforce one active Run per target, and return a run-assignment command containing
that frozen snapshot through the generated API contract. Assigned Runs are
redelivered with the same identity until the controller reports the materialized
Job UID and `preparing` phase. Lifecycle phase, terminal outcome, cancellation
intent, Job identity, observation freshness, diagnostics, and exit code remain
separate persisted facts.

`POST /api/v1/runs/{runId}/cancel` records a cancel request idempotently.
Queued Runs finish as canceled immediately. Controllers receive cancel commands
ahead of new assignments, stop local observation, and
delete the deterministic Job with identity and UID checks before reporting the
Run as canceled. Repeated delivery and an already-missing Job are safe.

[`examples/kcloud-ops`](./examples/kcloud-ops) contains a larger catalog
example with a pyinfra deployment and a Terraform stack, including runtime
credentials, placement configuration, a canary action, and a saved-plan
artifact flow.

`SLIPRAMP_REPOSITORY_REFRESH_CREDENTIALS` maps logical credential names to
absolute paths of administrator-mounted Git config files. Each isolated fetch
uses only its selected config file and disables interactive prompts. The file
can use standard Git configuration such as a scoped credential helper for
HTTPS or `core.sshCommand` for SSH; any referenced credential and known-hosts
files should be mounted separately. For example:

```json
{
  "github-server": "/var/run/secrets/slipramp/github.gitconfig"
}
```

The Server accepts these environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SLIPRAMP_SERVER_ADDRESS` | `:8080` | TCP address on which the Server listens |
| `SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `SLIPRAMP_CONTROLLER_TOKENS` | `{}` | JSON object mapping registered cluster names to static bearer credentials |
| `SLIPRAMP_PLACEMENTS_FILE` | Unset | Path to the versioned YAML placement configuration |
| `SLIPRAMP_REPOSITORY_REFRESH_CREDENTIALS` | `{}` | JSON object mapping refresh credential names to mounted Git config files |
| `SLIPRAMP_REPOSITORY_REFRESH_INTERVAL` | `1m` | Interval between repository branch refreshes |
| `SLIPRAMP_REPOSITORY_FETCH_TIMEOUT` | `30s` | Deadline for fetching one repository branch |
| `SLIPRAMP_DATABASE_URL` | Local development PostgreSQL URL | PostgreSQL connection URL |
| `SLIPRAMP_DATABASE_CONNECT_TIMEOUT` | `10s` | PostgreSQL startup deadline |
| `SLIPRAMP_S3_ENDPOINT` | `http://127.0.0.1:9000` | S3-compatible endpoint URL |
| `SLIPRAMP_S3_REGION` | `us-east-1` | S3 bucket region |
| `SLIPRAMP_S3_BUCKET` | `slipramp` | Artifact-content bucket |
| `SLIPRAMP_S3_ACCESS_KEY_ID` | `slipramp` | S3 access key ID |
| `SLIPRAMP_S3_SECRET_ACCESS_KEY` | `slipramp-development` | S3 secret access key |
| `SLIPRAMP_S3_STARTUP_TIMEOUT` | `10s` | S3 bucket availability check deadline |

Placements are loaded from the file named by `SLIPRAMP_PLACEMENTS_FILE` when the
Server starts. Each placement selects a cluster registered in
`SLIPRAMP_CONTROLLER_TOKENS` and may define Kubernetes execution defaults
without giving the Server access to the Kubernetes API. For example, a
placements file may contain:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  infrastructure-admin:
    cluster: colo
    jobNamespace: slipramp-runs
    serviceAccountName: infrastructure-admin
    timeout: 30m
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: "2"
        memory: 2Gi
    nodeSelector:
      topology.kubernetes.io/zone: us-west-2a
    podTemplate:
      metadata:
        labels:
          azure.workload.identity/use: "true"
      spec:
        runtimeClassName: kata
    credentials:
      terraform-backend:
        secretRef:
          name: terraform-backend
```

The loader rejects unknown fields, duplicate keys, unsupported versions, and
invalid Kubernetes names or resource quantities. The optional `podTemplate`
accepts Kubernetes pod metadata and spec extensions, but cannot replace
Slipramp-owned containers, init containers, restart policy, service account,
node selector, or run deadline.

The Server emits structured JSON logs and shuts down gracefully on `SIGINT` or
`SIGTERM`.

The controller process runs inside its execution cluster, authenticates to the
Server, and continuously long-polls for controller commands. Successful polls
and Run observation reports upsert the cluster's first-seen and last-seen
timestamps in PostgreSQL. The Server reports a cluster as unreachable after one
minute without either request, and the web index shows the resulting cluster
health. Each frozen
assignment is rendered as a deterministic `slipramp-run-<run-id>` Kubernetes
Job. Repeated delivery creates the Job once and then reuses it. Before polling,
the controller scans labeled Jobs across the cluster and resumes their
observers. It derives `preparing` and `running` from the action container,
records Job completion reasons and exit codes, maps deadline failures to the
`timed-out` outcome, and reports a permanent rendering or admission failure as
a terminal failed Run instead of blocking later assignments. Cancel commands
stop an active observer before deleting its Job so deletion cannot be
misreported as an unexpected missing Job.

The controller requires in-cluster Kubernetes configuration and these
environment variables:

| Variable | Purpose |
| --- | --- |
| `SLIPRAMP_CONTROLLER_CLUSTER` | Stable logical name of this execution cluster |
| `SLIPRAMP_CONTROLLER_SERVER_URL` | Absolute HTTPS URL of the Slipramp Server |
| `SLIPRAMP_CONTROLLER_TOKEN` | Static bearer credential used with the Server |
| `SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE` | Pinned image containing `/bin/sh`, Git, and SSH for the checkout init container |
| `SLIPRAMP_CONTROLLER_HARNESS_IMAGE` | Pinned image whose entrypoint is `slipramp-harness` |
| `SLIPRAMP_CONTROLLER_GIT_CREDENTIALS` | JSON object mapping logical checkout credential names to cluster-local Kubernetes Secret names; defaults to `{}` |

The checkout init container materializes the exact commit into an `emptyDir`.
The action container mounts it at the runtime's `workingDirectory`, or
`/workspace` by default, and runs from the target's repository-relative path.
The checkout is made writable inside the execution Pod so checkout and runtime
images need not share a numeric user. Placements can supply a Pod `fsGroup` for
runtime access to restrictive Secret file modes.
The harness image copies its static executable into a second `emptyDir`, so the
runtime image does not need Slipramp tooling. A checkout credential Secret may
provide `gitconfig`, `ssh-privatekey`, and `known_hosts` keys; it is mounted only
in the checkout init container.

The harness executes an action from an explicit argument array without an
implicit shell. It connects the action to its standard streams, forwards
`SIGHUP`, `SIGINT`, and `SIGTERM`, and exits with the action's exit status.

The web process is a React Router v8 Framework Mode application with server-side
rendering and a Mantine v9 application shell. Its index route uses the generated
API client to report whether the Slipramp Server is reachable and show observed
execution clusters. The web process accepts these environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SLIPRAMP_API_URL` | `http://127.0.0.1:8080` | Server URL used by SSR loaders |

## Development

Go 1.27.2, Git, Node.js 22.22.0 or newer, and [Task](https://taskfile.dev/) are
required. Docker is required for the local PostgreSQL and RustFS services.
Development tool and package versions are pinned by `Taskfile.yml`, `go.mod`,
`package.json`, and `pnpm-lock.yaml`; `task deps` installs the pinned Go tools
and pnpm release into the ignored repository cache. The development credentials
below are intentionally local-only.

```console
task deps
task local:up
task generate
task check
task integration
SLIPRAMP_CONTROLLER_TOKENS='{"colo":"development-token"}' \
  go run ./cmd/slipramp-server
go run ./cmd/slipramp-harness command argument
task web:dev
```

The controller is exercised through unit tests locally and runs with a
Kubernetes service account in a cluster.

`task local:down` stops PostgreSQL and RustFS while retaining their named
volumes. Database migrations live in `internal/store/migrations`, queries live
in `internal/store/queries`, and `task generate` refreshes both sqlc output and
the OpenAPI-generated clients. During the MVP, schema baselines may be rewritten
instead of migrated compatibly; recreate the local PostgreSQL volume after such
a change.
