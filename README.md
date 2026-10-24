# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. Connect a Git repository, discover versioned Targets and Actions, and
execute exact commits as Kubernetes Jobs across one or more Clusters.

Slipramp supports manual/API Runs, named Git triggers, live and retained logs,
declared artifacts, pinned preview/apply flows, OIDC/API-key authentication, and
optional Kargo Stage inventory, operator-started Promotions, and Promotion
reviews with Git and Argo CD diffs. Its Server has no Kubernetes access;
in-cluster Controllers manage execution through outbound HTTP.

Named Git-trigger deliveries retry Run creation with durable exponential backoff:
five seconds initially, doubling to a five-minute cap, with terminal failure on
the eighth failed attempt. Other eligible deliveries continue during backoff,
and restart preserves attempt counts and due times. Latest-wins refreshes
supersede pending retries; terminal failures remain history. Creating the Run
and linking its delivery is one PostgreSQL transaction. Delivery retries only
create the immutable Run; Slipramp never implicitly retries the resulting
infrastructure command.

The Inbox shows terminal Git-trigger failures with Target/Action context and
safe diagnostics. The authenticated read API
`GET /api/v1/git-trigger-deliveries/failed?limit=25&offset=0` returns newest
failures first (`limit` 1–200). There is no manual delivery retry action.

## Documentation

- [Design](docs/DESIGN.md): implemented architecture, domain model, trust
  boundaries, and governing decisions.
- [Roadmap](docs/ROADMAP.md): unfinished product work, reliability, live Kargo
  acceptance, and deferred choices.
- [Catalog example](examples/infrastructure-ops/README.md): Runtime and Target
  definitions, typed parameters, credentials, triggers, and artifact flows.
- [Server/web chart](deploy/charts/slipramp/README.md) and
  [Controller chart](deploy/charts/slipramp-controller/README.md): Kubernetes
  installation and deployment configuration.
- [End-to-end acceptance](deploy/e2e/README.md): disposable kind coverage and
  diagnostics.

Contracts are pre-stable. Development favors clean breaking changes with all
callers and documentation updated together.

## Server configuration

The Server accepts these environment variables:

| Variable                                    | Default                          | Purpose                                                                                                                    |
| ------------------------------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `SLIPRAMP_SERVER_ADDRESS`                   | `:8080`                          | TCP address on which the Server listens                                                                                    |
| `SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT`          | `10s`                            | Graceful shutdown deadline                                                                                                 |
| `SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS`     | `{}`                             | JSON object mapping cluster names to their bootstrap enrollment credentials                                                |
| `SLIPRAMP_PLACEMENTS_FILE`                  | Unset                            | Path to the versioned YAML placement configuration                                                                         |
| `SLIPRAMP_REPOSITORY_REFRESH_CREDENTIALS`   | `{}`                             | JSON object mapping refresh credential names to mounted Git config files                                                   |
| `SLIPRAMP_REPOSITORY_REFRESH_INTERVAL`      | `1m`                             | Interval between repository and promotion branch-drift refreshes                                                           |
| `SLIPRAMP_REPOSITORY_FETCH_TIMEOUT`         | `30s`                            | Deadline for fetching one repository branch                                                                                |
| `SLIPRAMP_DATABASE_URL`                     | Local development PostgreSQL URL | PostgreSQL connection URL                                                                                                  |
| `SLIPRAMP_DATABASE_CONNECT_TIMEOUT`         | `10s`                            | PostgreSQL startup deadline                                                                                                |
| `SLIPRAMP_S3_ENDPOINT`                      | `http://127.0.0.1:9000`          | S3-compatible endpoint URL                                                                                                 |
| `SLIPRAMP_S3_REGION`                        | `us-east-1`                      | S3 bucket region                                                                                                           |
| `SLIPRAMP_S3_BUCKET`                        | `slipramp`                       | Artifact-content bucket                                                                                                    |
| `SLIPRAMP_S3_ACCESS_KEY_ID`                 | `slipramp`                       | S3 access key ID                                                                                                           |
| `SLIPRAMP_S3_SECRET_ACCESS_KEY`             | `slipramp-development`           | S3 secret access key                                                                                                       |
| `SLIPRAMP_S3_STARTUP_TIMEOUT`               | `10s`                            | S3 bucket availability check deadline                                                                                      |
| `SLIPRAMP_ARTIFACT_TOKEN_SECRET`            | Local development secret         | HMAC key for Run-scoped harness artifact credentials; use at least 32 characters                                           |
| `SLIPRAMP_ARTIFACT_RETENTION_PERIOD`        | `720h`                           | Age after sealing when an artifact becomes eligible for expiration                                                         |
| `SLIPRAMP_ARTIFACT_RECONCILIATION_INTERVAL` | `1h`                             | Interval between artifact expiration, deletion, and orphan reconciliation passes                                           |
| `SLIPRAMP_ARTIFACT_ORPHAN_GRACE_PERIOD`     | `24h`                            | Minimum object age before an unreferenced object may be deleted                                                            |
| `SLIPRAMP_OIDC_ISSUER`                      | Required                         | Issuer URL for the single browser-login OIDC provider                                                                      |
| `SLIPRAMP_OIDC_CLIENT_ID`                   | Required                         | OIDC relying-party client ID                                                                                               |
| `SLIPRAMP_OIDC_CLIENT_SECRET`               | Required                         | OIDC relying-party client secret; supply through deployment secret configuration                                           |
| `SLIPRAMP_OIDC_REDIRECT_URL`                | Required                         | Absolute provider callback URL ending at `/api/v1/auth/callback`                                                           |
| `SLIPRAMP_OIDC_SUBJECT_ROLES`               | Required                         | JSON object mapping explicitly enrolled OIDC subjects to `admin`, `operator`, or `viewer`; at least one admin is required  |
| `SLIPRAMP_PRODUCT_SESSION_LIFETIME`         | `12h`                            | Absolute lifetime of a local browser session                                                                               |
| `SLIPRAMP_KARGO_INTEGRATIONS`               | `{}`                             | JSON object containing optional named Kargo integrations for Stage inventory, operator creation, review preparation, and optional Kargo UI deep links |

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

Kargo integration behavior, credential boundaries, Run execution, and artifact
retention are described in the [design](docs/DESIGN.md). Integration and
Placement configuration is loaded at Server startup.

## Controller configuration

The controller requires in-cluster Kubernetes configuration and these
environment variables:

| Variable                                   | Purpose                                                                                                          |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| `SLIPRAMP_CONTROLLER_CLUSTER`              | Stable logical name of this execution cluster                                                                    |
| `SLIPRAMP_CONTROLLER_SERVER_URL`           | Absolute HTTP or HTTPS URL of the Slipramp Server; use plaintext HTTP only over a trusted encrypted network      |
| `SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN`     | Cluster-scoped bootstrap credential used only to enroll the controller                                           |
| `SLIPRAMP_CONTROLLER_TOKEN`                | Controller-owned bearer credential registered during enrollment and used for ongoing requests                    |
| `SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE`       | Pinned image containing `/bin/sh`, Git, and SSH for the checkout init container                                  |
| `SLIPRAMP_CONTROLLER_HARNESS_IMAGE`        | Pinned image whose entrypoint is `slipramp-harness`                                                              |
| `SLIPRAMP_CONTROLLER_GIT_CREDENTIALS`      | JSON object mapping logical checkout credential names to cluster-local Kubernetes Secret names; defaults to `{}` |
| `SLIPRAMP_CONTROLLER_EXECUTION_NAMESPACES` | Required JSON array of exclusively owned execution namespaces                                                    |
| `SLIPRAMP_CONTROLLER_KARGO_INTEGRATIONS`   | JSON array of optional Kargo integration names and Stage namespaces; defaults to `[]`                            |

Each Controller owns its configured execution namespaces and uses
namespace-scoped RBAC. Git checkout and execution Secrets remain in that
Cluster. Optional Kargo observers submit complete Stage snapshots only after
all configured namespace listings succeed.

## Web configuration

The Web uses React Router v8 Framework/SSR mode and Mantine UI v9. It accepts
these environment variables:

| Variable           | Default                 | Purpose                            |
| ------------------ | ----------------------- | ---------------------------------- |
| `SLIPRAMP_API_URL` | `http://127.0.0.1:8080` | Server URL used by the web process |

The browser accesses login and callback paths through the web origin. The web
process forwards those requests to `SLIPRAMP_API_URL`, so local development and
deployments without path-based ingress routing still receive the Server's OIDC
redirects and cookies on the web origin.

## Kubernetes installation

[`deploy/charts/slipramp`](deploy/charts/slipramp/README.md) installs one Server
and one web process. It requires external PostgreSQL and S3-compatible storage,
uses operator-managed Secret references by default, rolls the Server when its
Placement ConfigMap changes, and gives the Server neither RBAC nor a mounted
service-account token. Its optional single-host Ingress routes API, Controller,
execution, and health paths to the Server and the remaining web path to the web
process.

Install [`deploy/charts/slipramp-controller`](deploy/charts/slipramp-controller/README.md)
once in each execution Cluster. It requires one stable Cluster name and one or
more exclusive execution namespaces, creates only namespaced Job/Pod RBAC, and
fixes the replica count at one. Git and execution Secrets stay in that Cluster.
Both charts use explicit non-`latest` image tags and expose resources, security
contexts, image pull Secrets, and focused scheduling settings.

Version tags publish matching application images and both charts to GHCR. OCI
installs pin the chart version; Helm infers the chart name from the final path
component and does not use `helm repo add` or an `index.yaml`.

```console
task charts:check
helm upgrade --install slipramp oci://ghcr.io/slipramp/slipramp-charts/slipramp --version 0.1.0 --namespace slipramp-system --create-namespace --values server-values.yaml
helm upgrade --install slipramp-controller oci://ghcr.io/slipramp/slipramp-charts/slipramp-controller --version 0.1.0 --namespace slipramp-system --values controller-values.yaml
```

Source checkouts remain installable from `deploy/charts/slipramp` and
`deploy/charts/slipramp-controller`. GHCR creates new container packages as
private by default. After the first release, a maintainer must verify that the
six image packages and two chart packages are public for anonymous installs.
Private packages require `helm registry login ghcr.io` and matching Kubernetes
image pull credentials.

To publish a release, update both charts' `version` and `appVersion` fields and
every default image tag to the same version, then run the package check and
push the matching Git tag. The release workflow publishes the six images
first and only pushes the two OCI charts after every image succeeds.

```console
task charts:package CHART_VERSION=0.1.0
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

## Development

Go 1.27.2, Git, Node.js 22.22.0 or newer, and [Task](https://taskfile.dev/) are
required. Docker is required for the local PostgreSQL, RustFS, and Dex services.
Development tool and package versions are pinned by `Taskfile.yml`, `go.mod`,
`package.json`, and `pnpm-lock.yaml`; `task deps` installs the pinned Go tools
and pnpm release into the ignored repository cache. The local stack includes a
Dex password database and development-only Server configuration for the
`kcloud-ops` repository and development homelab Controller. Run the Server and
web process in separate terminals:

```console
task deps
task local:server
task web:dev
task generate
task charts:check
task charts:package CHART_VERSION=0.1.0
task check
task integration
task acceptance:kind
go run ./cmd/slipramp-harness command argument
```

Open `http://localhost:5173` and sign in with
`admin@slipramp.local` / `slipramp`. These credentials and the static OIDC
client secret are only for local development. Dex stores its state in memory.

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
`slipramp-local-prod-sjc1-enrollment-token` and observe the `kargo-core`,
`kargo-devops`, `kargo-ivy`, `kargo-network`, `kargo-platform`, `kargo-play`,
`kargo-sandbox`, `kargo-storage`, and `kargo-virt` namespaces for the `homelab`
integration, because the centralized Kargo Stage objects live in that cluster.

The local `homelab-infrastructure` Placement exposes the `ops-ci`,
`homelab-known-hosts`, `kcloud-ops-git`, and `kargo-argocd` Secret sources in
the `slipramp-dev-runs` namespace. The Controller bootstrap remains
responsible for materializing those Secrets and the private-GHCR pull
credential. The configured Kargo integration is named `homelab`; configure the
Controller's Stage observer and Kargo PromotionTask to use that same name.
After signing in,
create an operator service account and API key with
`slipramp:promotions:submit` for the PromotionTask; there is intentionally no
bootstrap API key in source control.

The controller is exercised through unit tests locally and runs with a
Kubernetes service account in a cluster.

`task local:up` starts PostgreSQL, RustFS, and Dex without starting the Server.
`task local:down` stops them while retaining the PostgreSQL and RustFS named
volumes. Database migrations live in `internal/store/migrations`, queries live
in `internal/store/queries`, and `task generate` refreshes both sqlc output and
the OpenAPI-generated clients. `task local:server` rebuilds and gracefully
restarts the Server after server source, Go module, or local Placement changes.
Rapid file changes are coalesced, and a failed rebuild leaves the last good
Server binary available. While the repository and database contracts are still
pre-stable, schema baselines may be rewritten instead of migrated compatibly;
recreate the local PostgreSQL volume after such a change.
