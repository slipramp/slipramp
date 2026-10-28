# Disposable kind acceptance test

The acceptance task creates a new single-node kind cluster, builds and loads the
Slipramp images, deploys PostgreSQL, RustFS, Dex, the Server, web application,
and one Controller, and serves a generated Git fixture over authenticated SSH.
The Server, web application, and Controller are installed through the supported
Helm charts; only disposable dependencies and acceptance fixtures use raw
manifests.
It then verifies the accepted baseline end to end:

- repository registration, authenticated refresh, and catalog indexing;
- OIDC administrator login through Dex followed by product API authentication
  with a key created through the authenticated management API;
- the built `slipramp` binary discovering every paginated Target, reading a
  durable Target, and explaining an Action against Server Placement defaults;
- complete operational Inbox projection through the Web and generated API,
  including per-Principal acknowledgement isolation, persistence across a Web
  restart, decision preservation, and recurrence after Controller recovery;
- typed, idempotent manual Run creation, waiting, inspection, paginated listing,
  exact reruns, durable log streaming, and exact-commit checkout through the CLI;
- review-required Git Runs remaining undispatched until approval, approved Runs
  executing their exact accepted commit, rejected Runs finishing as canceled
  without a Job, immutable CLI-submitted comments reaching the API and Web,
  CLI pending-review filtering, and review-only authorization independent from
  Run writes;
- preview logs streamed and a declared plan Artifact listed, downloaded, and
  digest-verified through the CLI;
- promotion of that plan through CLI Artifact input selection into an apply Run
  pinned to its commit and catalog revision;
- default per-Target serialization plus CLI queued and running cancellation,
  including the unsuccessful terminal-wait exit contract;
- controller and Server restart recovery while Runs are active;
- durable log recovery after RustFS is paused during an active Run, including
  Job completion while storage is down and exact sealed-log recovery;
- repeated cancellation delivery while RustFS is paused, including Controller
  responsiveness, terminal log recovery, and execution Job cleanup;
- active and queued Run recovery while PostgreSQL is paused, including truthful
  health and readiness, retained Job cleanup, exact logs, and resumed scheduling;
- automatic Kargo Promotion approval without a preparation Run;
- operator-started Kargo Promotion creation through Controller materialization
  and callback hydration of the same durable record;
- review-required Promotions prepared by the first-party Kargo tools image,
  with exact Git diffs and structured Argo CD diffs uploaded by the Harness,
  retained in RustFS, and downloaded through the artifact API;
- zero-byte Git diff uploads and idempotent Promotion polling.

The Kargo cases install minimal Stage and Promotion CRDs plus a Stage fixture.
The Controller observes that Stage, materializes an operator-started Promotion,
and reports its Kubernetes UID before a dedicated operator service account with
only `slipramp:promotions:submit` scope submits the existing callback. The suite
also uses real Git branches, execution Jobs, and artifact storage. A
repository-defined Argo CD command fixture validates the injected credential
and returns a fixed resource diff; the suite does not install Kargo or Argo CD
controllers or exercise their target-branch push and reconciliation. Those
behaviors remain part of operational cutover acceptance.

The cluster and all generated SSH keys are deleted when the test exits. Run it
from the repository root with Docker, Go, kubectl, Git, and `ssh-keygen`
available. The Task workflow installs pinned kind and Helm
binaries:

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
`SLIPRAMP_KIND_BIN`, `SLIPRAMP_HELM_BIN`, and `SLIPRAMP_KUBECTL_BIN`
environment variables read by the Go acceptance suite in
[`internal/acceptance/kind`](../../../internal/acceptance/kind/suite_test.go).

The `Checks` workflow also runs the suite as an independent, non-blocking job on
the self-hosted runners. Each CI run uses a unique cluster name and performs an
unconditional cleanup; the local task remains available for development and
failure diagnosis.
