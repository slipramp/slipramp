# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small
teams. Connect a Git repository, discover versioned Targets and Actions, and
execute exact commits as Kubernetes Jobs across one or more Clusters.

Slipramp supports manual/API Runs, named Git triggers, optional Run review,
live and retained logs, declared artifacts, pinned preview/apply flows,
OIDC/API-key authentication, and optional Kargo Stage inventory,
operator-started Promotions, and Promotion reviews with Git and Argo CD diffs.
Its Server has no Kubernetes access; in-cluster Controllers manage execution
through outbound HTTP.

Repository refresh runs at Server startup and on the configured interval.
Administrators can also choose **Refresh now** from Repository detail. Both
sources use the same serialized path and retain the newest 50 durable attempts,
including source, duration, fetched commit when available, result, and a bounded
safe diagnostic. The UI and generated API distinguish a Git fetch failure from
a successful fetch whose catalog was rejected. Rechecking an already accepted
head is harmless and does not recreate Git-trigger deliveries. The read endpoint
is `GET /api/v1/repositories/{repositoryId}/refresh-attempts`; administrators
start a requested attempt with `POST` to the same path.

The Connect and Edit repository screens also provide **Test connection**. The
operator-started probe checks the proposed clone URL, tracked branch, and
optional Server refresh credential before registration or before changed
settings are saved. It uses the configured repository fetch timeout, does not
create or mutate a Repository, and returns only the observed head SHA or a
bounded non-sensitive diagnostic. Repository editing atomically updates the
optional refresh credential reference, checkout credential reference, commit
URL template, and commit-indexing mode. Credential fields contain configured
names only; Slipramp never accepts or persists their secret values through this
API. Checkout credentials resolve in execution Clusters and therefore are not
covered by the Server-side connection probe.

Named Git-trigger deliveries retry Run creation with durable exponential backoff:
five seconds initially, doubling to a five-minute cap, with terminal failure on
the eighth failed attempt. Other eligible deliveries continue during backoff,
and restart preserves attempt counts and due times. Latest-wins refreshes
supersede pending retries; terminal failures remain history. Creating the Run
and linking its delivery is one PostgreSQL transaction. Delivery retries only
create the immutable Run; Slipramp never implicitly retries the resulting
infrastructure command.

A named Git trigger may set `review: required`. Its Run remains queued and
cannot produce a Job until an operator approves it from the Inbox or Run
detail. Approval executes the existing Run at its exact accepted commit;
rejection finishes it as canceled. The setting applies only to that trigger:
manual and API Runs remain directly runnable, and omitting `review` preserves
automatic execution. API keys deciding reviews require
`slipramp:runs:review`; that scope does not grant Run creation or cancellation.

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
- [Catalog validation](docs/CATALOG_VALIDATION.md): source-pinned CLI setup,
  portable and deployment checks, hooks, CI, and editor schema configuration.
- [Server/web chart](deploy/charts/slipramp/README.md) and
  [Controller chart](deploy/charts/slipramp-controller/README.md): Kubernetes
  installation and deployment configuration.
- [Metrics](docs/METRICS.md): Prometheus endpoint, complete metric catalog,
  semantic-convention status, labels, and scrape guidance.
- [End-to-end acceptance](deploy/e2e/README.md): disposable kind coverage and
  diagnostics.
- [iOS preview](ios/README.md): native SwiftUI setup, connection, and tests.
- [Project site](https://slipramp.com): the public landing page and privacy policy.
- [Mascot gallery](https://mascot.slipramp.com): the production axolotl
  animations, published independently from the private control plane.

Contracts are pre-stable. Development favors clean breaking changes with all
callers and documentation updated together.

## Catalog CLI

The `slipramp` CLI validates the complete `.slipramp` catalog in a local
checkout with the same decoder and validation stages used by Server repository
refresh. Portable validation does not require a running installation and makes
omitted Placement checks explicit:

```console
go run ./cmd/slipramp catalog validate
go run ./cmd/slipramp catalog validate ../infrastructure
```

Supply the Server's non-secret `PlacementConfig` file to include static
deployment checks such as Placement existence, offered credential sources, and
effective resource requests and limits. This does not contact Kubernetes:

```console
go run ./cmd/slipramp catalog validate --placements ./placements.yaml
```

Text is the default output. JSON has a versioned top-level schema for custom
automation, and SARIF 2.1.0 uses repository-relative locations for code-scanning
systems. Warnings describe checkout-aware issues such as unmatched watch
patterns and missing relative command files; `--strict` makes them fail the
command without changing their severity.

```console
go run ./cmd/slipramp catalog validate --format json
go run ./cmd/slipramp catalog validate --format sarif > slipramp.sarif
go run ./cmd/slipramp catalog validate --strict
go run ./cmd/slipramp version
```

Exit code `0` means validation passed, `1` means catalog errors or strict
warnings were found, `2` means the invocation, root, or Placement file was
invalid, and `3` reports an unexpected internal/output failure. Diagnostics are
always written to standard output; invocation and internal failures use
standard error. See [Catalog validation](docs/CATALOG_VALIDATION.md) for
source-pinned installation, pre-commit and CI integration, validation scopes,
and YAML editor configuration. Versioned CLI artifacts are not published yet.

Inspect the effective execution contract for one Target Action in a working
tree before committing or registering it with a Server:

```console
go run ./cmd/slipramp catalog explain rustfs-buckets preview --root examples/infrastructure-ops
go run ./cmd/slipramp catalog explain rustfs-buckets apply --root examples/infrastructure-ops --format json
go run ./cmd/slipramp catalog explain kubernetes-upgrade upgrade-node --root examples/infrastructure-ops --set host=compute-01 --set reboot=false
```

`slipramp catalog explain TARGET ACTION [flags]` accepts `--root PATH` (current
directory by default), optional `--placements PATH`, repeatable `--set
NAME=VALUE`, and `--format text|json` (text by default). Relative file paths
resolve from the current directory. Both formats use one structured model of
effective values with field-level provenance and repository source file/document
locations. It includes commands, final argv, directories, Runtime environment,
Placement defaults, resources, credential references and injection mappings,
Action/Parameter/Artifact presentation metadata, artifact producers/consumers
and compatibility, watch patterns, and triggers.

The whole catalog must validate before an Action can be explained. Without
`--placements`, repository-owned contracts are validated and Placement-dependent
values and remaining resource defaults are explicitly unresolved. With the
Server's `PlacementConfig` file, the command performs full static deployment
validation and resolves those defaults and credential sources. Secret contents
and arbitrary Placement pod configuration are never printed. The command does
not contact the Server or Kubernetes, inspect images, or execute the Action.

Required parameters must be supplied; optional parameters stay omitted. Names
must be unique across `--set` flags. String and enum values are literal, booleans
use Go's `strconv.ParseBool` syntax (normally `true` or `false`), and numbers must
be finite. The same normalization and validation used for Run creation applies.
Controller execution shares argument/environment delivery: arguments append in
lexical parameter-name order, named booleans emit a flag only when true, and
positional/environment booleans emit `true` or `false`.

Explanation uses the same exit codes as validation. Unknown Targets/Actions and
invalid parameter values return `1` with an error on standard error; invalid
catalogs return `1` with the usual validation diagnostics on standard output.
Successful JSON explanations include `schemaVersion: 1`, `validationLevel`, and
effective fields carrying `value`, `resolved`, and `provenance`; unresolved
fields also carry `unresolvedReason`. Checkout warnings remain in the explanation
and do not prevent success.

## Remote operator CLI

The same `slipramp` binary supports a focused remote workflow against an
existing Slipramp Server. Pass the absolute Server URL with `--server`, or set
`SLIPRAMP_URL`. The CLI reads its credential only from `SLIPRAMP_API_KEY` and
sends it as a Bearer token. It never writes the key to disk or includes it in
output or errors. HTTPS uses the system trust store; there is no insecure TLS
mode. Plain HTTP should be used only when a trusted encrypted network already
protects the connection.

An API key needs `slipramp:read` for discovery, Run and Artifact reads, logs,
and non-sensitive Artifact downloads; `slipramp:runs:write` to create, rerun,
or cancel Runs; and `slipramp:runs:review` to approve or reject Run reviews.
Sensitive Artifact contents additionally require an operator or administrator
Principal. For example:

```console
export SLIPRAMP_URL=https://slipramp.example.com
export SLIPRAMP_API_KEY='replace-with-an-api-key'

slipramp target list --query storage --limit 25
slipramp target list --repository-id 018f90ff-9fd8-7b5f-b7e1-f32ff54c1201 --format json
slipramp target list --all --format json
slipramp target get 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202
slipramp target explain 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 preview
slipramp run list --target-id 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 --phase running
slipramp run list --review-state pending --all --format json
slipramp run create --target 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 --action preview --set region=us-west-2
slipramp run create --target 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 --action apply \
  --input plan=018f90ff-9fd8-7b5f-b7e1-f32ff54c1206
slipramp run get 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp run logs 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp run wait 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203 --timeout 30m
slipramp run rerun 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203 --mode exact
slipramp run review approve 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203 --comment 'Plan reviewed'
slipramp run cancel 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp artifact list 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp artifact download 018f90ff-9fd8-7b5f-b7e1-f32ff54c1206 --output plan.tfplan
```

`target list` also accepts exact `--runtime` and `--placement` filters plus
`--offset`. `target get` returns a durable active or archived Target and its most
recently accepted definition. `target explain` returns the effective Action
contract resolved against the Server's current Placement configuration;
required parameters may remain omitted for inspection, and parameter simulation
stays with the offline `catalog explain` command. `run list` supports the
Server's exact Repository, Target, commit, input Artifact, Cluster, phase, and
review-state filters plus offset pagination. Both list commands accept `--all`
to fetch every matching record at or after `--offset`; `--limit` remains the
request page size. Aggregate JSON retains the normal list envelope with all
fetched records in `items`, the page size in `limit`, the initial offset, and the
Server's matching total. `run create` requires an exact Target UUID and Action
name. Its repeatable
`--set NAME=VALUE` flags use the same typed parsing, required-field, enum, and
normalization rules as catalog explanation and Server Run creation. Repeatable
`--input NAME=ARTIFACT_UUID` flags select exact retained Artifact inputs; the
Server verifies compatibility and pins the Run to their producing commit and
catalog revision. Because that historical revision's parameter declarations are
not exposed to the CLI, `--input` cannot be combined with `--set`. This supports
the same directly runnable, parameter-free preview/apply transitions exposed by
the producing Run in the Web UI. Every create and rerun sends a fresh random
idempotency key; automation may supply `--idempotency-key` to recover the same
request safely. Reruns require an explicit `--mode exact` or `--mode latest`.

`run review approve` and `run review reject` accept an optional `--comment` and
return the canonical updated Run. `artifact list RUN_ID` returns the log and
declared outputs for one Run. `artifact download` requires `--output PATH`, or
`--output -` for standard output. File downloads never replace an existing path,
use mode `0600`, verify the Server's SHA-256 digest, and remove partial or invalid
outputs.

Text is the default for `target list`, `target get`, `target explain`, `run
list`, `run create`, `run get`, `run wait`, `run rerun`, both Run review
decisions, `run cancel`, and `artifact list`; each accepts `--format json` to
emit the generated API response shape. Structured and text results go to
standard output. Errors and operational diagnostics go to standard error. `run
wait` polls until the Run finishes, prints
its canonical terminal state, and exits distinctly when the outcome is not
`succeeded`. It waits without a deadline unless `--timeout` is set;
`--poll-interval` defaults to one second. `run logs` is intentionally
unformatted: it writes decoded log bytes exactly to standard output, follows
live output, and reconnects from the last durable byte offset without repeating
content. It exits successfully only after the log is sealed, independently of
the Run outcome. A failed/incomplete log leaves its retained prefix on standard
output and exits distinctly.

Remote commands use exit code `0` for success, `1` for local Run validation
failures, `2` for an invalid invocation or missing configuration, `3` for an
unexpected local/output failure, `4` for a network or Server/API failure, and
`5` for an explicitly incomplete log. `run wait` returns `6` after writing the
terminal Run when its outcome is `failed`, `canceled`, or `timed-out`.
Authentication, authorization, validation, conflict, and not-found problem
responses include the HTTP status and the Server's safe problem detail. Ctrl-C
cancels an active log request or Run wait and returns promptly with a nonzero
status.

## Server configuration

The Server accepts these environment variables:

| Variable                                    | Default                          | Purpose                                                                                                                    |
| ------------------------------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `SLIPRAMP_SERVER_ADDRESS`                   | `:8080`                          | TCP address on which the Server listens                                                                                    |
| `SLIPRAMP_SERVER_METRICS_ADDRESS`           | `:9091`                          | Dedicated operational metrics listener address                                                                             |
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
| `SLIPRAMP_KARGO_INTEGRATIONS_FILE`          | Unset                            | Path to the versioned YAML Kargo integration configuration used for Stage inventory, operator creation, review preparation, health, and optional UI deep links |

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

Administrators can inspect users and service accounts in the Principal
inventory, disable or enable a Principal, revoke all of its API keys, and revoke
all of its local browser sessions. These controls are independent: disabling
does not revoke credentials, and removing a subject from the OIDC role map
prevents new logins but does not revoke an existing session. Use the explicit
session-revocation operation when immediate browser logout is required.

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
clusters:
  colo:
    maximumActiveRuns: 4
placements:
  infrastructure-admin:
    cluster: colo
    jobNamespace: slipramp-runs
    maximumActiveRuns: 2
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
Cluster scheduling defaults to four active Runs. A configured Cluster maximum
replaces that default, and a Placement maximum must be no larger than its
configured Cluster maximum.

Kargo integrations are loaded from the file named by
`SLIPRAMP_KARGO_INTEGRATIONS_FILE` when the Server starts. The file is optional;
when it is unset, no Kargo integrations are configured. For example:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations:
  prod:
    cluster: prod
    uiUrl: https://kargo.example.com
    argoCdUiUrls:
      homelab: https://argocd.homelab.example.com
      prod: https://argocd.example.com
    repositories: [kcloud-ops]
    environmentOrder: [homelab, prod]
    preparation:
      repository: kcloud-ops
      target: kargo-promotions
      action: prepare-review
    candidateBranchCleanup:
      retention: 168h
      credentials:
        kcloud-ops: github-write
    maximumStageObservationAge: 5m
    pendingReviewRetention: 192h
```

The loader requires exactly one document and rejects unknown fields, duplicate
keys, unsupported versions, invalid durations, and invalid integration values.
The selected Cluster must be registered in
`SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS`. Candidate branch cleanup credential
names must select entries in `SLIPRAMP_REPOSITORY_REFRESH_CREDENTIALS`.
Kargo integration behavior, credential boundaries, Run execution, and artifact
retention are described in the [design](docs/DESIGN.md). Kargo integration and
Placement configuration are both loaded once at Server startup.

## Controller configuration

The controller requires in-cluster Kubernetes configuration and these
environment variables:

| Variable                                    | Purpose                                                                                                          |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `SLIPRAMP_CONTROLLER_CLUSTER`               | Stable logical name of this execution cluster                                                                    |
| `SLIPRAMP_CONTROLLER_SERVER_URL`            | Absolute HTTP or HTTPS URL of the Slipramp Server; use plaintext HTTP only over a trusted encrypted network      |
| `SLIPRAMP_CONTROLLER_METRICS_ADDRESS`       | Metrics listener address; defaults to `:9091`                                                                    |
| `SLIPRAMP_CONTROLLER_MAXIMUM_ACTIVE_RUNS`   | Positive local worker limit for new assignments; defaults to `4`                                                |
| `SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN`      | Cluster-scoped bootstrap credential used only to enroll the controller                                           |
| `SLIPRAMP_CONTROLLER_TOKEN`                 | Controller-owned bearer credential registered during enrollment and used for ongoing requests                    |
| `SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE`        | Pinned image containing `/bin/sh`, Git, and SSH for the checkout init container                                  |
| `SLIPRAMP_CONTROLLER_HARNESS_IMAGE`         | Pinned image whose entrypoint is `slipramp-harness`                                                              |
| `SLIPRAMP_CONTROLLER_GIT_CREDENTIALS`       | JSON object mapping logical checkout credential names to cluster-local Kubernetes Secret names; defaults to `{}` |
| `SLIPRAMP_CONTROLLER_EXECUTION_NAMESPACES`  | Required JSON array of exclusively owned execution namespaces                                                    |
| `SLIPRAMP_CONTROLLER_KARGO_INTEGRATIONS`    | JSON array of optional Kargo integration names and Stage namespaces; defaults to `[]`                            |

Each Controller owns its configured execution namespaces and uses
namespace-scoped RBAC. Git checkout and execution Secrets remain in that
Cluster. Optional Kargo observers submit complete Stage snapshots only after
all configured namespace listings succeed.
The metrics listener is unauthenticated and should remain reachable only from
the Cluster's trusted monitoring network. It exposes outbound Slipramp HTTP
client, Go runtime, and process metrics.

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
Placement or Kargo integration ConfigMap changes, and gives the Server neither
RBAC nor a mounted service-account token. Its optional single-host Ingress
routes API, Controller, execution, and health paths to the Server and the
remaining web path to the web process. The Server also exposes `GET /metrics`
on a dedicated port for trusted in-cluster scraping; the chart deliberately
leaves that port out of its Ingress.

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
required. Docker is required for the local PostgreSQL, RustFS, Dex, Prometheus,
and Grafana services.
Canonical source artwork is maintained in the sibling `../slipramp-assets` repository.
ImageMagick and the libwebp command-line tools are required only when packaging
new mascot source sheets or regenerating the optimized native iOS mascot pack.
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
task charts:package CHART_VERSION=0.1.0
task check
task integration
task acceptance:kind
go run ./cmd/slipramp-harness command argument
```

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

Open `http://localhost:5173` and sign in with
`admin@slipramp.local` / `slipramp`. These credentials and the static OIDC
client secret are only for local development. Dex stores its state in memory.
Prometheus is available at `http://localhost:9090`, and the provisioned
Slipramp Server dashboard is available anonymously at
`http://localhost:3000/d/slipramp-server/slipramp-server`. Grafana's local
administrator credentials are `slipramp` / `slipramp-development`.

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
After signing in,
create an operator service account and API key with
`slipramp:promotions:submit` for the PromotionTask; there is intentionally no
bootstrap API key in source control.

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
