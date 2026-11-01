# Slipramp Configuration

This reference covers process-level configuration for the Slipramp Server,
Controllers, and Web. For Kubernetes values and Secret wiring, see the
[Server/Web chart](../deploy/charts/slipramp/README.md) and
[Controller chart](../deploy/charts/slipramp-controller/README.md).

## Server configuration

### Environment

The Server accepts these environment variables:

| Variable                                    | Default                          | Purpose                                                                                                                    |
| ------------------------------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `SLIPRAMP_SERVER_ADDRESS`                   | `:8080`                          | TCP address on which the Server listens                                                                                    |
| `SLIPRAMP_SERVER_METRICS_ADDRESS`           | `:9091`                          | Dedicated operational metrics listener address                                                                             |
| `SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT`          | `10s`                            | Graceful shutdown deadline                                                                                                 |
| `SLIPRAMP_SERVER_TRUST_PROXY_HOPS`          | `0`                              | Exact trusted proxy hops used to resolve client IPs for HTTP logs and OIDC login-start throttling                           |
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
| `SLIPRAMP_ARTIFACT_TOKEN_SECRET`            | Required                         | HMAC key for Run-scoped harness artifact credentials; use at least 32 random bytes                                         |
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

The Artifact token key must be supplied explicitly on every Server startup.
Rotating it invalidates credentials already issued to active Runs, including
credentials for Artifact uploads and selected Artifact inputs; cancel and rerun
affected Runs after a rotation.

The Server ignores `X-Forwarded-For` by default. Set trusted proxy hops only
when every request reaches the Server through that exact number of controlled
proxies. The Web preserves the incoming header when proxying browser OIDC
requests so the same resolved client IP is available to Server request logs and
login-start throttling.

### Identity and recovery

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

### Placements

Placements are loaded from the file named by `SLIPRAMP_PLACEMENTS_FILE` when the
Server starts. Each placement selects a cluster registered in
`SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS` and may define Kubernetes
execution defaults
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

### Kargo integrations

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
retention are described in the [design](DESIGN.md). Kargo integration and
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
