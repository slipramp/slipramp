# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. The product direction is in [`docs/PLAN.md`](docs/PLAN.md), and the first
delivery scope is in [`docs/MVP.md`](docs/MVP.md).

## Current foundation

The current foundation establishes the four Slipramp processes: the Server,
cluster controller, harness, and web application. The split OpenAPI contract
currently defines the Server's JSON liveness probe at `GET /healthz`; product
and controller APIs will be added in later slices. Go server interfaces and the
TypeScript frontend client are generated together from that contract.

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
| `SLIPRAMP_DATABASE_URL` | Local development PostgreSQL URL | PostgreSQL connection URL |
| `SLIPRAMP_DATABASE_CONNECT_TIMEOUT` | `10s` | PostgreSQL startup deadline |
| `SLIPRAMP_S3_ENDPOINT` | `http://127.0.0.1:9000` | S3-compatible endpoint URL |
| `SLIPRAMP_S3_REGION` | `us-east-1` | S3 bucket region |
| `SLIPRAMP_S3_BUCKET` | `slipramp` | Artifact-content bucket |
| `SLIPRAMP_S3_ACCESS_KEY_ID` | `slipramp` | S3 access key ID |
| `SLIPRAMP_S3_SECRET_ACCESS_KEY` | `slipramp-development` | S3 secret access key |
| `SLIPRAMP_S3_STARTUP_TIMEOUT` | `10s` | S3 bucket availability check deadline |

The Server emits structured JSON logs and shuts down gracefully on `SIGINT` or
`SIGTERM`.

The controller process currently establishes its configuration and lifecycle
boundary. Polling and Kubernetes reconciliation will be added with the
generated controller API. Its required environment variables are:

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
API client to report whether the Slipramp Server is reachable. The web process
accepts these environment variables:

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
