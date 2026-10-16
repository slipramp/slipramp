# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. The product direction is in [`docs/PLAN.md`](docs/PLAN.md), and the first
delivery scope is in [`docs/MVP.md`](docs/MVP.md).

## Current foundation

The first implementation slice establishes the Go module and the Slipramp
Server, cluster-controller, and harness processes. The Server currently
provides only a liveness probe at `GET /healthz`; product and controller APIs
will be added from the generated OpenAPI contract in a later slice.

The Server accepts these environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SLIPRAMP_SERVER_ADDRESS` | `:8080` | TCP address on which the Server listens |
| `SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |

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

## Development

Go 1.27.2 and [Task](https://taskfile.dev/) are required. Development tool
versions are pinned by `Taskfile.yml` and installed into the ignored `.bin`
directory.

```console
task deps
task check
go run ./cmd/slipramp-server
SLIPRAMP_CONTROLLER_CLUSTER=colo \
  SLIPRAMP_CONTROLLER_SERVER_URL=https://slipramp.example.com \
  SLIPRAMP_CONTROLLER_TOKEN=development-token \
  go run ./cmd/slipramp-controller
go run ./cmd/slipramp-harness command argument
```
