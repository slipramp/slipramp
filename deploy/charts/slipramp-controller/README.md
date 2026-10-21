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

Every configured execution namespace is exclusively owned by this Controller
and Slipramp installation. Do not run another active Controller for the same
Cluster name, and do not reuse a namespace until all Jobs left by its prior
owner have been removed. The chart has no leader election, lease, fencing, or
failover behavior, and its schema rejects any replica count other than one.

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

```console
helm upgrade --install slipramp-controller \
  deploy/charts/slipramp-controller \
  --namespace slipramp-system --create-namespace \
  --values cluster-values.yaml
```
