# Disposable kind acceptance test

The acceptance task creates a new single-node kind cluster, builds and loads the
Slipramp images, deploys PostgreSQL, RustFS, Dex, the Server, web application,
and one Controller, and serves a generated Git fixture over authenticated SSH.
It then verifies the accepted baseline end to end:

- repository registration, authenticated refresh, and catalog indexing;
- OIDC administrator login through Dex followed by product API authentication
  with a key created through the authenticated management API;
- idempotent manual Run creation and exact-commit checkout;
- preview logs and a declared plan artifact downloaded through the web process;
- promotion of that plan into an apply Run pinned to its commit and catalog
  revision;
- default per-Target serialization plus queued and running cancellation;
- controller and Server restart recovery while Runs are active.

The cluster and all generated SSH keys are deleted when the test exits. Run it
from the repository root with Docker, kubectl, curl, Git, jq, and `ssh-keygen`
available:

```console
task acceptance:kind
```

Dex uses its development-only mock connector, so the suite exercises discovery,
authorization redirects, callback validation, session creation, and API-key
creation without storing test passwords. To use a retained default cluster as
an interactive demo, temporarily resolve
`acceptance-dex.slipramp-system.svc.cluster.local` to `127.0.0.1`, then run these
forwards in separate terminals:

```console
kubectl --context kind-slipramp-acceptance -n slipramp-system port-forward service/acceptance-dex 5556:5556
kubectl --context kind-slipramp-acceptance -n slipramp-system port-forward service/slipramp-server 18080:8080
kubectl --context kind-slipramp-acceptance -n slipramp-system port-forward service/slipramp-web 13000:3000
```

Open `http://127.0.0.1:13000`, sign in, and choose **Acceptance Identity** on
the Dex page. Remove the temporary host mapping after the demo. Custom
acceptance ports require matching forwards.

Set `SLIPRAMP_ACCEPTANCE_KEEP_CLUSTER=true` to retain the cluster for an
interactive demo or failure diagnosis. The context is
`kind-slipramp-acceptance` by default.
The cluster name, forwarded Server and web ports, timeout, kind binary, and
kubectl binary can be overridden with the `SLIPRAMP_ACCEPTANCE_*`,
`SLIPRAMP_KIND_BIN`, and `SLIPRAMP_KUBECTL_BIN` environment variables used near
the top of [`kind-acceptance.sh`](../../../scripts/e2e/kind-acceptance.sh).

The `Checks` GitHub Actions workflow runs the same task for pushes to `main`,
pull requests, and manual workflow dispatches.
