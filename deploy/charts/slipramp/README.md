# Slipramp chart

This chart installs one Slipramp Server and one React Router web process. It
does not install PostgreSQL, RustFS, or any other S3-compatible service. The
operator must provision the PostgreSQL database and artifact bucket first.

The Server deliberately has `automountServiceAccountToken: false`; this chart
creates no ServiceAccount, Role, or RoleBinding for it. Placements are ordinary
Server configuration in a ConfigMap, never CRDs, and their checksum is attached
to the Server Pod template so a placement change rolls the Deployment.

## Required configuration

Set the external database URL, S3 endpoint and bucket, OIDC provider, explicit
OIDC subject-role enrollment map, Controller enrollment-token map, Artifact
token key, and at least one Placement. The Artifact token key must contain at
least 32 characters. Every Placement's Cluster must also be present in the
Controller enrollment-token map stored in the referenced Secret.

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

Repository-refresh credentials are mounted directly from operator-managed
Secrets. Each Secret can contain a `gitconfig` plus any files it references:

```yaml
server:
  repositoryRefresh:
    credentials:
      github-readonly:
        secretName: github-refresh
        gitConfigKey: gitconfig
```

The resulting Git config path is
`/var/run/slipramp/repository-credentials/github-readonly/gitconfig`.

Optional Kargo promotion-review integrations are static Server configuration.
Each integration selects a registered Cluster, a repository allowlist, and the
repository-defined Target and Action that will prepare review artifacts:

```yaml
server:
  kargo:
    integrations:
      prod:
        cluster: prod
        uiUrl: https://kargo.example.com
        repositories: [kcloud-ops]
        environmentOrder: [homelab, prod]
        preparation:
          repository: kcloud-ops
          target: kargo-promotions
          action: prepare-review
        maximumStageObservationAge: 5m
        pendingReviewRetention: 192h
```

The selected Cluster must also be present in `controllerEnrollmentTokens`.
The optional `uiUrl` enables direct Project, Stage, and Promotion links from the web UI.
`environmentOrder` is required and controls Freight comparison columns.
Observed environments that are absent from the list remain visible after the
configured columns. Changing Kargo integration configuration requires a Server
rollout.

## Ingress and operation

The optional Ingress uses one host. `/api`, `/controller`, `/execution`,
`/healthz`, and `/readyz` route to the Server; `/` routes to the web process.
This preserves the single-origin browser flow while keeping Controller and
execution endpoints on the Server. Terminate TLS before exposing Slipramp over
an untrusted network.

The Server's liveness probe uses `/healthz`. Readiness uses `/readyz` with a
three-second Kubernetes timeout because the application bounds its dependency
checks at two seconds. PostgreSQL and S3 Secret changes require an explicit
Server rollout. The chart fixes Server and web replicas at one and does not
claim high-availability support.

Released chart versions are published as OCI artifacts in GHCR. Pin the chart
version explicitly; the same release version is the default Server and web
image tag.

```console
helm upgrade --install slipramp \
  oci://ghcr.io/slipramp/slipramp-charts/slipramp \
  --version 0.1.0 \
  --namespace slipramp-system --create-namespace \
  --values production-values.yaml
```

For development from a source checkout, replace the OCI reference and
`--version` with `deploy/charts/slipramp`.
