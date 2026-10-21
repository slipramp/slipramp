# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. The product direction is in [`docs/PLAN.md`](docs/PLAN.md), and the
accepted MVP baseline is recorded in [`docs/MVP.md`](docs/MVP.md).

## Project status

The MVP operational loop is implemented and accepted. Slipramp can discover a
versioned repository catalog, run exact commits through authenticated in-cluster
controllers, stream and retain logs, transfer declared artifacts, and promote a
preview artifact into a pinned follow-up Run. Development now continues
incrementally from that baseline; the MVP document is a record of the delivered
scope rather than an open checklist.

The repository keeps formatting, linting, generation, unit, integration, build,
and disposable-cluster acceptance checks in CI. The end-to-end suite and the
operator-reviewed homelab flow are documented in
[`deploy/e2e`](deploy/e2e/README.md).

## Current implementation

Slipramp consists of four processes: the Server, cluster controller, harness,
and web application. The split OpenAPI contract defines the Server's JSON
liveness probe at `GET /healthz`, the read-only
cluster health endpoint at `GET /api/v1/clusters`, and the authenticated
controller enrollment and long poll at `POST /controller/v1/enroll` and
`POST /controller/v1/poll`. Ongoing controller routes derive the cluster
identity from the enrolled bearer credential rather than repeating it in every
path.

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

Each successful refresh compares the newly accepted commit with the previous
active catalog commit. Changed repository paths are matched against every
target's `watch` patterns, including recursive `**` segments. Slipramp stores
the resulting desired commit in a durable target projection and shows it in
the repository catalog and target detail screens. New and reactivated targets
start at the commit where they become active, removed targets are archived,
and invalid catalogs do not change target state or initiate Runs.

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

Target detail screens provide manual Run forms for actions without artifact
inputs. The forms render string, boolean, number, and enum controls from the
catalog declarations, omit unset optional parameters, and submit through the
same Run-creation API used by programmatic clients. Actions requiring artifact
inputs are started from a compatible retained artifact on its producing Run.
The Run history screen lists up to 50 recent Runs and links back to their
repository, target, and detail views. `GET /api/v1/runs` exposes the same
latest-first history with optional repository and repository-scoped target
filters and a bounded result limit.

Authenticated controllers can append raw action output at
`POST /controller/v1/runs/{runId}/log-chunks?offsetBytes=N`. Each chunk is
limited to 256 KiB, written to S3-compatible storage before its digest and
offset metadata become visible in PostgreSQL, and accepted idempotently when
the same bytes are retried at the same offset. The enrolled credential must
identify the Run's assigned cluster. The controller first reads the durable
cursor, so a restarted controller replays only bytes the Server has not
accepted. Before reporting a terminal observation it seals the log with the
verified final byte size and SHA-256 digest.

Run logs stream as reconnectable SSE from
`GET /api/v1/runs/{runId}/logs`; the event ID is the next durable byte offset.
Run detail and artifact metadata are available at `GET /api/v1/runs/{runId}`
and `GET /api/v1/runs/{runId}/artifacts`, and sealed contents download from
`GET /api/v1/artifacts/{artifactId}/download`. The web Run screen consumes the
same stream and exposes retained downloads.

Assignments carry a narrowly scoped, deterministic Run artifact credential that
the harness uses only with the Server's execution artifact endpoints. Before
starting the action the harness downloads every explicitly selected input to
its declared `mountPath`; afterward it uploads each declared output path with
its size and digest. Retries are idempotent. Input selection is explicit in
the Run request and compatible consumers are pinned to the producing Run's
commit and catalog revision. PostgreSQL stores bindings, provenance,
sensitivity, and retention metadata while S3-compatible storage holds bytes.
Successful preview Runs expose an **Apply this plan** action when the frozen
catalog contains a directly runnable consumer for the retained plan.

`POST /api/v1/runs/{runId}/cancel` records a cancel request idempotently.
Queued Runs finish as canceled immediately. Controllers receive cancel commands
ahead of new assignments. A controller marks a started execution Pod through a
projected cancellation annotation so the harness can terminate its active child
process, drain and seal its log, report the canceled outcome, and only then
delete the deterministic Job with identity and UID checks. An unstarted Job is
deleted before the empty canceled Run is reported. Repeated delivery and an
already-missing Job are safe.

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
| `SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS` | `{}` | JSON object mapping cluster names to their bootstrap enrollment credentials |
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
| `SLIPRAMP_ARTIFACT_TOKEN_SECRET` | Local development secret | HMAC key for Run-scoped harness artifact credentials; use at least 32 characters |
| `SLIPRAMP_ARTIFACT_RETENTION_PERIOD` | `720h` | Age after sealing when an artifact becomes eligible for expiration |
| `SLIPRAMP_ARTIFACT_RECONCILIATION_INTERVAL` | `1h` | Interval between artifact expiration, deletion, and orphan reconciliation passes |
| `SLIPRAMP_ARTIFACT_ORPHAN_GRACE_PERIOD` | `24h` | Minimum object age before an unreferenced object may be deleted |
| `SLIPRAMP_OIDC_ISSUER` | Required | Issuer URL for the single browser-login OIDC provider |
| `SLIPRAMP_OIDC_CLIENT_ID` | Required | OIDC relying-party client ID |
| `SLIPRAMP_OIDC_CLIENT_SECRET` | Required | OIDC relying-party client secret; supply through deployment secret configuration |
| `SLIPRAMP_OIDC_REDIRECT_URL` | Required | Absolute provider callback URL ending at `/api/v1/auth/callback` |
| `SLIPRAMP_OIDC_SUBJECT_ROLES` | Required | JSON object mapping explicitly enrolled OIDC subjects to `admin`, `operator`, or `viewer`; at least one admin is required |
| `SLIPRAMP_PRODUCT_SESSION_LIFETIME` | `12h` | Absolute lifetime of a local browser session |

OIDC configuration is required for the Server to start. The subject-role map is
the installation's enrollment and role authority: unknown subjects cannot sign
in, at least one mapped subject must be an administrator, and a mapped role is
reapplied on each successful login. The first administrator signs in through
OIDC and can then create user API keys, service accounts, and service-account
keys through the authenticated management surface. There is no bootstrap API
key or anonymous product-route mode.

Browser sessions use an opaque, HTTP-only `slipramp_session` cookie. They have
a fixed absolute lifetime and no idle expiry. The callback validates
authorization code PKCE, state, nonce, issuer, signature, audience, and token
time claims; provider access and refresh tokens are not retained. Every product
API operation requires either a local session or a Slipramp API key and applies
the principal's global role and, for a key, its scopes. The web process forwards
only the session to SSR API calls, live-log streams, and artifact downloads; it
also passes same-origin login and callback requests to the Server.

Back up the PostgreSQL database and OIDC deployment configuration together.
PostgreSQL contains principals, OIDC identity mappings, API-key digests, and
session digests, while API-key plaintext is never stored and must remain with
its client. A restore keeps API keys usable when their client still has the
secret and the restored record is active. The configured provider must retain
the same issuer and subject identifiers. If administrator credentials are
lost, an operator with deployment access can explicitly map a known OIDC
subject to `admin`, restart the Server, sign in, and issue replacement keys;
there is no configuration credential that bypasses OIDC.

Placements are loaded from the file named by `SLIPRAMP_PLACEMENTS_FILE` when the
Server starts. Each placement selects a cluster registered in
`SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS` and may define Kubernetes execution defaults
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

The controller process runs inside its execution cluster. At startup it uses a
cluster-scoped enrollment credential to register the SHA-256 digest of its
separate controller-owned bearer credential. Repeating enrollment with the
same credential is idempotent; replacing an enrolled credential is rejected.
The controller then authenticates with that enrolled credential and
continuously long-polls for controller commands. Successful polls
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
are delivered to the harness through a projected Pod annotation so the action
can stop before the controller drains its final log and removes the Job.

The controller requires in-cluster Kubernetes configuration and these
environment variables:

| Variable | Purpose |
| --- | --- |
| `SLIPRAMP_CONTROLLER_CLUSTER` | Stable logical name of this execution cluster |
| `SLIPRAMP_CONTROLLER_SERVER_URL` | Absolute HTTP or HTTPS URL of the Slipramp Server; use plaintext HTTP only over a trusted encrypted network |
| `SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN` | Cluster-scoped bootstrap credential used only to enroll the controller |
| `SLIPRAMP_CONTROLLER_TOKEN` | Controller-owned bearer credential registered during enrollment and used for ongoing requests |
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

The harness materializes selected inputs, executes an action from an explicit
argument array without an implicit shell, and uploads declared output files.
It removes its Run-scoped artifact credential from the child process
environment, connects the action to its standard streams, forwards `SIGHUP`,
`SIGINT`, and `SIGTERM`, and preserves the action's exit status. An artifact
transfer failure changes an otherwise successful harness execution to failure;
it never hides a nonzero action exit code.

The web process is a React Router v8 Framework Mode application with server-side
rendering and a Mantine v9 application shell. Its index route uses the generated
API client to provide a searchable, repository-filtered, paginated view of
Targets across active catalogs. Observed execution clusters are available on
the dedicated Clusters screen. The web process accepts these environment
variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SLIPRAMP_API_URL` | `http://127.0.0.1:8080` | Server URL used by the web process |

The browser accesses login and callback paths through the web origin. The web
process forwards those requests to `SLIPRAMP_API_URL`, so local development and
deployments without path-based ingress routing still receive the Server's OIDC
redirects and cookies on the web origin.

## Development

Go 1.27.2, Git, Node.js 22.22.0 or newer, and [Task](https://taskfile.dev/) are
required. Docker is required for the local PostgreSQL and RustFS services.
Development tool and package versions are pinned by `Taskfile.yml`, `go.mod`,
`package.json`, and `pnpm-lock.yaml`; `task deps` installs the pinned Go tools
and pnpm release into the ignored repository cache. The development credentials
below are intentionally local-only. Configure the five required OIDC variables
from the Server table before running `slipramp-server`; the disposable kind
suite includes its own Dex provider and needs no external identity service.

```console
task deps
task local:up
task generate
task check
task integration
task acceptance:kind
SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS='{"colo":"development-enrollment-token"}' \
  go run ./cmd/slipramp-server
go run ./cmd/slipramp-harness command argument
task web:dev
```

The controller is exercised through unit tests locally and runs with a
Kubernetes service account in a cluster.

The first real-cluster walkthrough uses a laptop-hosted Server over the trusted
tailnet and a controller in the homelab cluster. See
[`deploy/e2e/README.md`](deploy/e2e/README.md) for the image, credential,
deployment, and `kcloud-ops` preview sequence.

`task local:down` stops PostgreSQL and RustFS while retaining their named
volumes. Database migrations live in `internal/store/migrations`, queries live
in `internal/store/queries`, and `task generate` refreshes both sqlc output and
the OpenAPI-generated clients. While the repository and database contracts are
still pre-stable, schema baselines may be rewritten instead of migrated
compatibly; recreate the local PostgreSQL volume after such a change.
