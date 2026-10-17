# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. The product direction is in [`docs/PLAN.md`](docs/PLAN.md), and the first
delivery scope is in [`docs/MVP.md`](docs/MVP.md).

## Current foundation

The current foundation establishes the four Slipramp processes: the Server,
cluster controller, harness, and web application. The split OpenAPI contract
defines the Server's JSON liveness probe at `GET /healthz`, the read-only
cluster health endpoint at `GET /api/v1/clusters`, and the authenticated
controller long poll at `POST /api/v1/controllers/{clusterName}/poll`. Go
request and response types, Server interfaces, the controller client, and the
TypeScript frontend client are generated together from that contract. API
errors use RFC 9457 problem details with the
`application/problem+json` media type.

The Server connects to PostgreSQL through pgxpool, applies embedded Goose
migrations at startup, and uses sqlc-generated query methods. Artifact contents
use a narrow S3-compatible storage boundary backed by the MinIO Go client. The
configured bucket is provisioned separately and checked at startup so the
Server does not require bucket-administration permissions. PostgreSQL remains
authoritative for product state and artifact metadata.

The Server accepts these environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SLIPRAMP_SERVER_ADDRESS` | `:8080` | TCP address on which the Server listens |
| `SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `SLIPRAMP_CONTROLLER_TOKENS` | `{}` | JSON object mapping registered cluster names to static bearer credentials |
| `SLIPRAMP_PLACEMENTS_FILE` | Unset | Path to the versioned YAML placement configuration |
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
kind: PlacementConfiguration
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

The controller process authenticates to the Server and continuously long-polls
for cluster work. Successful polls upsert the cluster's first-seen and
last-seen observations in PostgreSQL. The Server reports a cluster as
unreachable when it has not started a poll within one minute, and the web index
shows the resulting cluster health. Work delivery and Kubernetes reconciliation
will be added behind this boundary. Its required environment variables are:

| Variable | Purpose |
| --- | --- |
| `SLIPRAMP_CONTROLLER_CLUSTER` | Stable logical name of this execution cluster |
| `SLIPRAMP_CONTROLLER_SERVER_URL` | Absolute HTTPS URL of the Slipramp Server |
| `SLIPRAMP_CONTROLLER_TOKEN` | Static bearer credential used with the Server |

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

Go 1.27.2, Node.js 22.22.0 or newer, and [Task](https://taskfile.dev/) are
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
SLIPRAMP_CONTROLLER_CLUSTER=colo \
  SLIPRAMP_CONTROLLER_SERVER_URL=https://slipramp.example.com \
  SLIPRAMP_CONTROLLER_TOKEN=development-token \
  go run ./cmd/slipramp-controller
go run ./cmd/slipramp-harness command argument
task web:dev
```

`task local:down` stops PostgreSQL and RustFS while retaining their named
volumes. Database migrations live in `internal/store/migrations`, queries live
in `internal/store/queries`, and `task generate` refreshes both sqlc output and
the OpenAPI-generated clients.
