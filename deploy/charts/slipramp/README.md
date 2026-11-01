# Slipramp chart

This chart installs one Slipramp Server and one React Router web process. It
does not install PostgreSQL, RustFS, or any other S3-compatible service. The
operator must provision the PostgreSQL database and artifact bucket first.

The Server deliberately has `automountServiceAccountToken: false`; this chart
creates no ServiceAccount, Role, or RoleBinding for it. Placements and Kargo
integrations are ordinary Server configuration in versioned files backed by
ConfigMaps, never CRDs. Each ConfigMap checksum is attached to the Server Pod
template so either configuration change rolls the Deployment.

## Required configuration

Set the external database URL, S3 endpoint and bucket, OIDC provider, explicit
OIDC subject-role enrollment map, Controller enrollment-token map, Artifact
token key, and at least one Placement. The Artifact token key must contain at
least 32 random bytes. Every Placement's Cluster must also be present in the
Controller enrollment-token map stored in the referenced Secret.

OAuth for remote MCP clients is optional. Set `server.mcp.oauth.issuer` to the
authorization server issuer and `server.mcp.oauth.resource` to the canonical
external URL ending in `/mcp`. The authorization server must issue JWT access
tokens with that exact URL in `aud` and grant the `slipramp:read` and
`slipramp:runs:write` scopes. Clients that register themselves, including Codex,
also require compatible dynamic client registration and S256 PKCE support from
the authorization server.
The chart routes the resource's path-aware RFC 9728 metadata endpoint to the
Server alongside `/mcp`.

Run scheduling defaults to four active Runs per Cluster. Override that limit
under `server.clusters.<name>.maximumActiveRuns`; a Placement may set a tighter
`maximumActiveRuns`. Both limits count assigned, preparing, and running Runs,
while one-active-Run-per-Target serialization and ordinary FIFO remain in
effect.

Secret-bearing settings use this shape:

```yaml
server:
  database:
    url:
      value: ""
      existingSecret:
        name: slipramp-server-secrets
        key: database-url
```

An external Secret reference is the default and renders no Secret value. To
ask Helm to create a Secret for one setting, put the secret in `value` and set
both `existingSecret.name` and `existingSecret.key` to empty strings. The
schema rejects setting both paths or neither path. Inline values are visible to
Helm and should be reserved for disposable environments.

Server and web image tags default to the chart application version and may be
overridden explicitly. Each image accepts an optional SHA-256 digest. When a
digest is set, the chart renders `repository:tag@sha256:...`, and Kubernetes
resolves the immutable digest:

```yaml
server:
  image:
    repository: ghcr.io/slipramp/slipramp-server
    tag: <version>
    digest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Repository-refresh credentials are mounted directly from operator-managed
Secrets. Each Secret can contain a `gitconfig` plus any files it references:

```yaml
server:
  repositoryRefresh:
    credentials:
      github-readonly:
        secretName: github-refresh
        gitConfigKey: gitconfig
      github-write:
        secretName: github-promotion-cleanup
        gitConfigKey: gitconfig
```

The resulting Git config path is
`/var/run/slipramp/repository-credentials/github-readonly/gitconfig`.

Optional Kargo integrations are static Server configuration. Each integration
selects a registered Cluster, a repository allowlist, ordered environments, and
the repository-defined Target and Action that prepare review artifacts. The
same configuration enables observed-Freight operator creation; Kargo remains
authoritative for admission and availability. The chart renders these values as
a strict `slipramp.com/v1alpha1` `KargoIntegrationConfig` document and sets
`SLIPRAMP_KARGO_INTEGRATIONS_FILE` to its mounted path:

```yaml
server:
  kargo:
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

The selected Cluster must also be present in `controllerEnrollmentTokens`.
The optional `uiUrl` enables direct Project, Stage, and Promotion links from the web UI.
The optional `argoCdUiUrls` map enables direct Application links for observed
Stages in each configured environment. Kargo supplies the Application identity;
the Server uses this map only to construct browser links and never contacts Argo CD.
`environmentOrder` is required and controls Freight comparison columns.
Observed environments that are absent from the list remain visible after the
configured columns. Changing Kargo integration configuration requires a Server
rollout.

Candidate branch cleanup is opt-in per integration. It scans remote
`kargo/promotion/*` refs on the repository refresh interval and deletes a ref
only after every referencing Promotion is terminal, the retention window has
elapsed, and the remote tip still matches a recorded candidate revision. The
credential name must reference `server.repositoryRefresh.credentials` and must
have permission to delete branches. Untracked and moved refs are reported in
Server logs and are never deleted automatically. PostgreSQL retains only the
latest cleanup outcome and aggregate branch counts for each Repository.

Optional outbound notifications use an explicit public Slipramp URL, exact
event routes, and Slack webhook URLs mounted from operator-managed Secrets:

```yaml
server:
  notifications:
    publicUrl: https://slipramp.example.com
    destinations:
      operations:
        type: slack
        secretName: slipramp-slack
        secretKey: webhook-url
    routes:
      - events: [review.requested]
        destinations: [operations]
    retry:
      maximumAttempts: 5
      maximumBackoff: 1h
    retentionPeriod: 720h
```

The chart mounts each webhook URL as a file and passes only that file path to
the Server. The URL is not rendered into environment variables, generated
Secrets, PostgreSQL, logs, or API responses. Routes accept only implemented
exact event names. Retry and retention settings establish bounded policy for
the delivery workers; pending deliveries are never eligible for retention.

## Ingress, HTTPRoute, and operation

The chart can create a `networking.k8s.io/v1` Ingress, a Gateway API
`gateway.networking.k8s.io/v1` HTTPRoute, or both. Each route sends `/api`,
`/controller`, `/execution`, `/mcp`, `/healthz`, and `/readyz` to the Server and
`/` to the web process. The two exact browser OIDC paths under `/api/v1/auth`
route to the web process so it can preserve the single-origin login flow.

HTTPRoute support requires Gateway API CRDs and an operator-managed Gateway.
The chart does not install either. Attach the route through one or more
`parentRefs`; TLS belongs on the matching Gateway listener:

```yaml
httpRoute:
  enabled: true
  annotations: {}
  parentRefs:
    - name: public
      namespace: gateway-system
      sectionName: https
  hostnames:
    - slipramp.example.com
```

The referenced listener must allow HTTPRoutes from the release namespace.
Terminate TLS before exposing Slipramp over an untrusted network.

The Server's liveness probe uses `/healthz`. Readiness uses `/readyz` with a
three-second Kubernetes timeout because the application bounds its dependency
checks at two seconds. Server and web startup, liveness, and readiness timing is
configurable under each component's probe values. The default startup probes
give the Server five minutes and the web process two and a half minutes to
start before liveness checks begin. PostgreSQL, S3, and Slack webhook Secret
changes require an explicit Server rollout. The chart fixes Server and web
replicas at one and does not claim high-availability support.

The Server and web processes do not trust forwarded headers by default. When a
known reverse proxy terminates TLS, set `server.trustedProxyHops` and
`web.trustedProxyHops` to the exact number of trusted proxy hops between the
client and each process. The Web preserves `X-Forwarded-For` while proxying
browser OIDC requests so the Server can resolve the same client IP for
login-start throttling and request logs. For example, a Gateway that connects
directly to both Services normally uses:

```yaml
server:
  trustedProxyHops: 1
web:
  trustedProxyHops: 1
```

Do not set a larger value than the deployment topology requires; doing so can
allow a client to supply forwarded values that the processes treat as trusted.

The Server also exposes unauthenticated Prometheus/OpenMetrics text at
`/metrics` on its dedicated `metrics` Service port, defaulting to `9091`. The
Neither public route exposes that port. Configure an in-cluster scraper explicitly,
for example with Service annotations in an overlay:

```yaml
server:
  service:
    annotations:
      prometheus.io/scrape: "true"
      prometheus.io/path: /metrics
      prometheus.io/port: "9091"
```

See the [metric catalog](../../../docs/METRICS.md) for the complete emitted
contract, labels, bucket boundaries, convention status, and freshness model.

Released chart versions are published as OCI artifacts in GHCR. Pin the chart
version explicitly; the same release version is the default Server and web
image tag.

```console
VERSION=<release-version>
helm upgrade --install slipramp \
  oci://ghcr.io/slipramp/slipramp-charts/slipramp \
  --version "$VERSION" \
  --namespace slipramp-system --create-namespace \
  --values production-values.yaml
```

For development from a source checkout, replace the OCI reference and
`--version` with `deploy/charts/slipramp`.
