# Slipramp Controller chart

This chart installs exactly one Controller for one stable Cluster name. The
Controller reaches the Slipramp Server outbound, enrolls a controller-owned
bearer credential, and manages Jobs only in `executionNamespaces`.

The chart creates one namespaced Role and RoleBinding in every configured
execution namespace. Its permissions are limited to creating, getting,
listing, and deleting Jobs; getting, listing, and updating Pods; and reading
Pod logs. It creates no ClusterRole. Set `rbac.create: false` only when those
permissions are managed separately.

Create every entry in `executionNamespaces` before installing the chart. The
chart binds permissions into those namespaces but does not own their lifecycle.

`maximumActiveRuns` defaults to four and bounds concurrent materialization and
observation workers for newly assigned Runs. Configure the Server's matching
Cluster capacity at the same or lower value. The Server scheduler remains
authoritative; this Controller limit is a local guardrail, and recovered Jobs
remain observable even if they exceed a newly lowered limit.

Every configured execution namespace is exclusively owned by this Controller
and Slipramp installation. Do not run another active Controller for the same
Cluster name, and do not reuse a namespace until all Jobs left by its prior
owner have been removed. The chart has no leader election, lease, fencing, or
failover behavior, and its schema rejects any replica count other than one.

## Metrics

The Controller exposes unauthenticated Prometheus/OpenMetrics text at
`GET /metrics` on a dedicated listener. The chart creates a cluster-internal
Service named for the release with a `metrics` port, defaulting to `9091`, and
does not create an Ingress. Keep it reachable only from the Cluster's trusted
monitoring network.

The endpoint contains `target_info` Resource metadata, stable outbound HTTP
metrics for the generated Slipramp client, maintained OpenTelemetry Go runtime
metrics, and native Prometheus process metrics. It does not yet expose
Controller-specific operation metrics. Configure annotation-based discovery
with:

```yaml
metrics:
  port: 9091
  service:
    annotations:
      prometheus.io/scrape: "true"
      prometheus.io/path: /metrics
      prometheus.io/port: "9091"
```

The complete metric names and convention status are documented in the
[metric catalog](../../../docs/METRICS.md).

## Credentials and images

Enrollment and bearer credentials default to keys in an operator-managed
Secret:

```yaml
credentials:
  enrollmentToken:
    value: ""
    existingSecret:
      name: slipramp-controller-credentials
      key: enrollment-token
  bearerToken:
    value: ""
    existingSecret:
      name: slipramp-controller-credentials
      key: controller-token
```

To ask Helm to create either key, set its `value` and clear both external
reference fields. The schema rejects ambiguous and missing credential paths.
Git checkout credentials always remain cluster-local Secrets; `gitCredentials`
only maps a logical name to a Secret name and never transmits its contents to
the Server.

Controller, checkout, and Harness images have explicit non-`latest` tags. Pin
them to a release tag or an immutable registry tag appropriate for the
installation. The checkout image must contain `/bin/sh`, Git, and SSH; the
Harness image entrypoint must be `slipramp-harness`.

Kargo Stage observation is optional and configured separately from execution
namespaces. For every distinct configured Stage namespace, the chart creates a
dedicated Role and RoleBinding. It can get, list, and promote
`kargo.akuity.io/v1alpha1` Stage resources and create, get, and list Promotion
resources for idempotent operator-started Promotions. No cluster-wide Kargo
permission is created.

```yaml
kargo:
  integrations:
    - name: prod
      stageNamespaces:
        - kargo-core
        - kargo-devops
        - kargo-platform
```

Released chart versions are published as OCI artifacts in GHCR. Pin the chart
version explicitly; the same release version is the default Controller,
checkout, and Harness image tag.

```console
helm upgrade --install slipramp-controller \
  oci://ghcr.io/slipramp/slipramp-charts/slipramp-controller \
  --version 0.1.0 \
  --namespace slipramp-system --create-namespace \
  --values cluster-values.yaml
```

For development from a source checkout, replace the OCI reference and
`--version` with `deploy/charts/slipramp-controller`.
