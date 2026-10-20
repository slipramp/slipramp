# Disposable kind acceptance test

The acceptance task creates a new single-node kind cluster, builds and loads the
Slipramp images, deploys PostgreSQL, RustFS, the Server, web application, and one
Controller, and serves a generated Git fixture over authenticated SSH. It then
verifies the accepted baseline end to end:

- repository registration, authenticated refresh, and catalog indexing;
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

Set `SLIPRAMP_ACCEPTANCE_KEEP_CLUSTER=true` to retain a failed cluster for
interactive diagnosis. The context is `kind-slipramp-acceptance` by default.
The cluster name, forwarded Server and web ports, timeout, kind binary, and
kubectl binary can be overridden with the `SLIPRAMP_ACCEPTANCE_*`,
`SLIPRAMP_KIND_BIN`, and `SLIPRAMP_KUBECTL_BIN` environment variables used near
the top of [`kind-acceptance.sh`](../../../scripts/e2e/kind-acceptance.sh).

The `Checks` GitHub Actions workflow runs the same task for pushes to `main`,
pull requests, and manual workflow dispatches.
