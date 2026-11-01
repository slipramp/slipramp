# Slipramp Development

Run the commands in this guide from the repository root.

## Prerequisites

Go 1.27.2, Git, Node.js 22.22.0 or newer, and [Task](https://taskfile.dev/) are
required. Docker is required for the local PostgreSQL, RustFS, Dex, Prometheus,
and Grafana services.
Canonical source artwork is maintained in the sibling `slipramp-assets`
repository, normally checked out beside this repository.
ImageMagick and the libwebp command-line tools are required only when packaging
new mascot source sheets or regenerating the optimized native iOS mascot pack.
Development tool and package versions are pinned by `Taskfile.yml`, `go.mod`,
`package.json`, and `pnpm-lock.yaml`; `task deps` installs the pinned Go tools
and pnpm release into the ignored repository cache.

## Local workflow

The local stack includes a Dex password database and development-only Server
configuration for the
`kcloud-ops` repository and development homelab Controller. Run the Server and
web process in separate terminals:

```console
task deps
task local:server
task web:dev
```

Other useful entry points include:

```console
task ios:test
task ios:build
task mascot:dev
task mascot:build
task mascot:generate
task mascot:check
task mascot:deploy
task site:dev
task site:build
task site:deploy
task generate
task charts:check
task charts:package CHART_VERSION=<version>
task release VERSION=<version>
task check
task integration
task acceptance:kind
go run ./cmd/slipramp-harness command argument
```

## Public sites and mascot

The `mascot.slipramp.com` custom domain is attached to the `slipramp-mascot`
Worker once in Cloudflare. Routine `mascot:deploy` and CI deployments update the
Worker and its static assets without rewriting that zone binding.

The public landing page and privacy policy use the same static deployment model.
The `slipramp.com` custom domain is attached to the `slipramp-site` Worker once
in Cloudflare, and routine `site:deploy` and CI deployments update its assets
without rewriting that zone binding. Cloudflare provides the site's hosting-level
traffic analytics; no other analytics service, advertising network, forms, or
accounts are part of the site. Website data is separate from self-hosted
Slipramp installations. Slipramp does not currently include product analytics;
if product analytics are added, they will exclude repository contents,
credentials or secrets, catalog definitions, run inputs or outputs, logs, and
artifacts. Cloudflare Network Error Logging may also collect browser reports
about failures reaching the site.

## Local identity and observability

Open `http://localhost:5173` and sign in with
`admin@slipramp.local` / `slipramp`. These credentials and the static OIDC
client secret are only for local development. Dex stores its state in memory.
Prometheus is available at `http://localhost:9090`, and the provisioned
Slipramp Server dashboard is available anonymously at
`http://localhost:3000/d/slipramp-server/slipramp-server`. Grafana's local
administrator credentials are `slipramp` / `slipramp-development`.

## Private development integrations

Register the private operational repositories with:

- `kcloud-ops`: clone URL `https://github.com/ianunruh/kcloud-ops`, branch
  `main`, refresh credential `kcloud-ops`, and checkout credential
  `kcloud-ops`;
- `universe`: clone URL `https://gitlab.kcloud.io/watchivy/universe.git`,
  branch `main`, refresh credential `universe`, and no checkout credential.

The local refresh credentials rewrite the HTTPS URLs to SSH and use the
developer's existing SSH agent or default key and `known_hosts`. Confirm that
`ssh -T git@github.com` and `ssh -T git@gitlab.kcloud.io` can authenticate
before registering the repositories. The Kargo integration allows both
repositories because the `kargo-ivy` project promotes `universe`; its review
preparation still runs from the `kcloud-ops` catalog.

The development homelab Controller must use
`slipramp-local-homelab-enrollment-token` as its enrollment token and continue
mapping checkout credential `kcloud-ops` to Secret `kcloud-ops-git`. It owns
the `slipramp-dev-runs` execution namespace and runs promotion preparation
Jobs without sharing the production Controller's `slipramp-runs` namespace.
The development `prod-sjc1` Controller must use
`slipramp-local-prod-sjc1-enrollment-token`, own its cluster-local
`slipramp-dev-runs` namespace, and observe the `kargo-core`,
`kargo-devops`, `kargo-ivy`, `kargo-network`, `kargo-platform`, `kargo-play`,
`kargo-sandbox`, `kargo-storage`, and `kargo-virt` namespaces for the `homelab`
integration, because the centralized Kargo Stage objects live in that cluster.

The local `homelab-infrastructure` and `prod-infrastructure` Placements expose
their cluster-local `ops-ci`, host-known-hosts, and `kcloud-ops-git` Secret
sources in `slipramp-dev-runs`; both Placements also expose `kargo-argocd` for
promotion preparation Runs. The Controller bootstrap remains responsible for
materializing those Secrets and the private-GHCR pull credential in each
cluster. The configured Kargo integration is named `homelab`; configure the
Controller's Stage observer and Kargo PromotionTask to use that same name.
After signing in, create an operator service account and API key with
`slipramp:promotions:submit` for the PromotionTask; there is intentionally no
bootstrap API key in source control.

## Local services and generated code

The controller is exercised through unit tests locally and runs with a
Kubernetes service account in a cluster.

`task local:up` starts PostgreSQL, RustFS, Dex, Prometheus, and Grafana without
starting the Server. `task integration` intentionally starts only PostgreSQL,
RustFS, and Dex, so the monitoring images are not part of CI. `task local:down`
stops the stack while retaining its named volumes. Database migrations live in
`internal/store/migrations`, queries live in `internal/store/queries`, and
`task generate` refreshes both sqlc output and the OpenAPI-generated clients.
`task local:server` rebuilds and gracefully restarts the Server after server
source, Go module, or local Placement changes.
Rapid file changes are coalesced, and a failed rebuild leaves the last good
Server binary available. While the repository and database contracts are still
pre-stable, schema baselines may be rewritten instead of migrated compatibly;
recreate the local PostgreSQL volume after such a change.
