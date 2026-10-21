# Slipramp Plan

The accepted first implementation baseline is recorded in [MVP.md](./MVP.md).
Slipramp now evolves incrementally from that baseline; this document continues
to describe the product and architecture direction.

The focused design and delivery plan for the optional Kargo promotion-review
vertical is recorded in [KARGO_PROMOTIONS.md](./KARGO_PROMOTIONS.md).

## Vision

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small teams. A user connects a Git repository and gets a catalog of targets and actions defined by YAML in that repository.

Kubernetes clusters provide disposable execution capacity, while container images provide the tools and environment used by each operation. A single Slipramp installation can execute work across many clusters without requiring the control plane to run in any particular execution cluster. Slipramp coordinates execution without trying to replace Terraform/OpenTofu, pyinfra, Ansible, shell scripts, or other infrastructure tools.

The initial environment is trusted and single-operator, but the core model should remain suitable for eventual open-source use.

## Principles

- Keep the path from Git definition to execution short and understandable.
- Treat Git as the source of operational definitions, not as a workflow language.
- Allow direct execution without mandatory previews, approvals, or policy gates.
- Support previews and additional safeguards when explicitly configured.
- Execute every run from an exact Git commit and retain enough context to explain what happened.
- Treat live logs as durable run artifacts from the moment execution begins.
- Remain tool-agnostic; tool-specific behavior belongs in repository definitions and runtime images.
- Prefer a small number of durable abstractions over pipelines, plugins, and provider-specific integrations.
- Be explicit that trusted repository content is executable code.
- Adopt strict formatting, static analysis, type checking, and linting from the beginning.

## Core Model

### Repository

A repository contains versioned target, runtime, and action definitions. Slipramp tracks a configured branch and validates the repository as a unit. Each accepted Git commit produces an immutable catalog revision, and the repository exposes its latest valid catalog revision.

Repositories are accessed through generic Git over SSH or HTTPS. Repository refresh credentials used by the Slipramp Server are named sources mounted into its deployment, such as files or environment variables populated from Kubernetes Secrets; the Server does not read the Kubernetes API to resolve them. Execution-side checkout credentials use logical names that each controller maps to cluster-local Kubernetes Secrets. Each repository tracks one configured branch and is refreshed periodically. Every fetched Git commit is indexed atomically so an invalid commit cannot partially replace the active catalog.

### Target

A target represents a named system or operational scope, such as a Terraform stack, pyinfra deployment, Ansible inventory, custom service, or group of machines. It exposes one or more user-defined actions.

A target's durable identity is its repository plus `metadata.name`. Renaming a target therefore archives the old catalog entry and creates a new one; historical runs continue to reference their immutable catalog revisions. Removing a target archives it from the active catalog and never implies infrastructure destruction.

`sync` is an optional, recognized action name for targets that support reconciliation. When present, Slipramp may track the target's desired Git commit, last attempted commit, and last successfully reconciled commit. Targets without `sync` remain useful collections of one-off operations and run history.

Slipramp should describe reconciliation history accurately rather than claim that a generic target is continuously in sync.

### Action

An action is an operation available on a target. Actions may represent reconciliation, previews, checks, maintenance, tests, upgrades, restarts, or arbitrary scripts.

Actions are independent executions rather than steps in a Slipramp workflow. Multi-step procedures remain inside the invoked script, task runner, or container.

A target supplies default runtime, placement, credentials, timeout, and resource settings. Individual actions may override those defaults when an operation needs a different tool or execution environment.

### Runtime

A runtime is repository-defined and describes the portable execution environment for an action: its container image, working directory, command conventions, non-secret environment, and the process-level interface expected for credentials.

Runtimes are resolved from the same catalog revision as their targets so related changes can be reviewed and applied together.

### Cluster

A cluster is a named Kubernetes execution destination reported by a Slipramp controller running inside that cluster. The Slipramp Server stores the cluster's logical identity and last-seen observation but has no kubeconfig, Kubernetes client, or direct network access to its API.

Cluster-controller configuration may map logical Git credential names to cluster-local Kubernetes Secrets. Private-repository checkout Jobs resolve the repository's logical credential through their controller; public repositories require no mapping.

Cluster identity is part of every dispatched run and Kubernetes object reference. A disconnected cluster makes the observation of its active runs temporarily unknown rather than changing their lifecycle phase or failing them; observation resumes when the connection recovers.

### Controller

A controller runs inside each execution cluster and is the only Slipramp component that uses that cluster's Kubernetes API. A single controller replica uses a small, versioned HTTP API. HTTPS is required across untrusted networks, while plaintext HTTP is permitted when a trusted encrypted network such as a tailnet already protects the transport. At startup it presents a cluster-scoped enrollment credential to register the SHA-256 digest of a separate controller-owned bearer credential. The current implementation auto-approves a valid enrollment credential; explicit approvals and credential rotation remain later product work. The Server derives cluster identity from the enrolled bearer credential, so ordinary controller routes do not repeat a caller-supplied cluster name. The controller long-polls for command envelopes, creates and observes Jobs, and posts lifecycle observations, log chunks, and completion back through ordinary HTTP requests. Authenticated poll and report traffic updates the cluster's last-seen time, so there is no separate heartbeat protocol.

Delivery is at least once. Runs and Jobs use deterministic identifiers, allowing a repeated response or request to be handled idempotently without a distributed lease protocol. A Run remains assigned until the controller reports the materialized Job UID and preparing phase. After restarting, the controller scans its labeled Jobs and reports their current observations before requesting more work. For a terminal Job, it uploads and seals the complete log, successfully reports the terminal observation, deletes the Job with its observed UID as a Kubernetes precondition, and only then stops observation. Each accepted step is idempotent, so recovery can repeat log sealing and terminal reporting before finishing deletion. A `404` for the Run or its log violates the execution-namespace ownership contract; the controller warns once, stops that observer, and leaves the Job for manual diagnosis. Kubernetes TTL cleanup is not used because it could remove the log source while the Server or object storage is unavailable. Execution clusters require no inbound network path from Slipramp. The initial deployment requires one controller replica, a unique cluster name, and an explicit list of exclusively owned execution namespaces. The controller rejects assignments outside that list and scans each configured namespace individually during recovery, allowing namespace-scoped RBAC instead of cluster-wide access. Duplicate active controllers or reused names are configuration errors rather than a supported failover mechanism. Controller high availability, fencing, leader election, message brokers, bidirectional streaming, and a custom RPC protocol are not required for the initial design.

### Placement

A placement is a named deployment configuration that selects a cluster and describes where a run executes within it and what authority it receives. It may select a namespace, service account, named credential sources, node selection, resource limits, and other Kubernetes settings.

Every Kubernetes namespace selected by a Slipramp placement is exclusively owned by one Slipramp Controller and its control plane. A controller may own multiple execution namespaces, but another control plane must not reuse one until all Jobs created by the previous owner have been removed. This is an operational invariant rather than an identity-label or compatibility mechanism.

Placements are supplied to the Slipramp Server through its normal configuration, such as Helm values or a mounted ConfigMap managed by Argo CD. They are not Kubernetes custom resources. Placements form the boundary between portable repository behavior and cluster-specific capabilities, even when both definitions live in the same Git repository.

The initial implementation loads placement configuration at Server startup. Argo CD applies a change by rolling the Server deployment, normally through a ConfigMap checksum annotation. Live configuration reload is not required.

### Harness

The Slipramp harness wraps the configured action process inside each execution Job. It establishes the workspace and artifact contract, forwards signals, preserves the process exit status, streams output, and collects declared artifacts.

The harness is supplied by Slipramp rather than baked into runtime images. Infrastructure tools and repository scripts continue to use their normal commands, environment variables, files, and exit codes without integrating with a Slipramp SDK.

### Run

A run is an immutable execution of an action against a target. It captures the exact commit, catalog revision, resolved definitions, parameters, artifact inputs, run source, any available actor identity, and placement configuration used for the operation. Product authentication, authorization, and human attribution follow the incremental design in [AUTH.md](./AUTH.md).

### Artifact

An artifact is an output of a run. Every run creates a log artifact when execution starts, and actions may publish additional named artifacts such as Terraform plans, test results, generated manifests, or diagnostic bundles.

The log artifact is append-only and available as a live stream while its run is active. Clients can reconnect and continue reading from a known offset. When the run ends, Slipramp seals the log as an immutable artifact with its final size and digest. Log streaming and durable capture are the same data path so the live view and retained artifact cannot silently diverge.

Artifact metadata includes its producing run, name, media type, lifecycle state, size, digest, sensitivity, object key, and retention state. Artifact contents are stored only in S3-compatible object storage rather than in PostgreSQL or on the Server filesystem. RustFS provides the local object store.

The current implementation applies one installation-wide retention period to sealed artifacts. A
periodic Server reconciler changes eligible metadata from `retained` to
`expired`, deletes every corresponding object idempotently, and changes the
metadata to `deleted` only after all deletes succeed. Artifacts produced by an
unfinished Run or bound to an unfinished consuming Run are not eligible to
expire. Failed object deletes leave the artifact expired for a later retry,
while the artifact metadata and provenance remain in PostgreSQL in every
retention state.

The same reconciler lists the Server-owned `runs/` object prefix and removes
objects that have no non-deleted PostgreSQL metadata after a configurable grace
period. The grace period protects the interval between writing an upload or log
chunk object and committing its metadata. PostgreSQL is checked immediately
before each orphan delete and remains authoritative; S3 lifecycle rules do not
independently expire artifact contents.

Repository contents, source bundles, and Git source archives are not artifacts and are never copied into S3. Execution Jobs clone their exact commit directly from Git.

Actions may accept artifacts from earlier runs as inputs. An input binding identifies a specific artifact rather than implicitly selecting the latest one. Compatibility rules can require the artifact to come from a particular target, action, Git commit, catalog revision, or resolved execution environment.

A preview action can therefore produce a plan artifact and expose an immediate **Apply this plan** operation. The apply run uses the producing run's Git commit and catalog revision, then mounts the selected plan into the Job. Targets that define `sync` may still make it directly available without a preview.

## Refresh and Reconciliation

Slipramp keeps repository refresh separate from target reconciliation:

1. Repository refresh updates the catalog of targets and actions from Git.
2. Target reconciliation invokes the optional `sync` action on targets that define it.

Repository refresh does not imply target execution. Target actions may be started manually, through the API, from Git changes, or by other explicitly configured triggers.

The target-level `watch` field describes which repository paths affect that target. It advances the target's desired Git commit and can be reused by Git triggers, but it does not cause execution on its own.

Removing a target definition archives it from the active catalog; it must never imply infrastructure destruction. An invalid Git commit leaves the previous valid catalog revision active and records diagnostics for the rejected commit.

## Triggers

The implemented trigger baseline is deliberately limited to named Git
triggers declared on actions. A Git trigger has the form `name` plus `git: {}`
and an optional static `parameters` object, and uses the containing target's
existing `watch` patterns. Trigger names are catalog names and are unique
within their action. A target with a Git trigger must declare at least one
watch pattern, and the triggered action cannot have artifact inputs. Trigger
parameters are normalized and validated with the same string, boolean, number,
enum, required, and declaration rules as ordinary Run creation while loading
the catalog.

After initial indexing, each newly accepted repository head is compared with
the previously accepted head. Definitions from the new catalog revision are
evaluated against the changed paths, and each matching Git trigger records one
delivery in the same PostgreSQL transaction that activates the catalog
revision and updates target state. An unchanged head or rejected catalog
records no delivery. Delivery is per accepted head rather than per underlying
Git commit in a multi-commit fetch. Each delivery stores the normalized
parameter object from that accepted catalog revision, so later refreshes cannot
alter pending work.

Automatic Git-triggered work uses latest-wins coalescing per Repository and
Target. In the catalog-activation transaction, advancing an affected Target's
desired commit or archiving a removed Target marks older undispatched
deliveries superseded and finishes its queued Git Runs as canceled with reason
`SupersededByGitChange`. The transaction records finished phase transitions
and, for an affected active Target, creates matching deliveries for the new
revision. Superseded delivery rows remain as history but are excluded from
pending delivery selection. Assigned, preparing, and running Git Runs are not
automatically canceled. If an older Git Run is active, it finishes while only
the newest queued Git revision is retained behind it. Manual and API Runs are
never coalesced and retain ordinary FIFO scheduling among eligible Runs.

A background dispatcher drains pending deliveries through the ordinary Run
creation service. It pins each request to the delivery's exact catalog
revision, uses the stable delivery ID as the Run idempotency identity, and
passes the delivery's persisted parameters before recording the resulting Run
ID on the delivery. Retries and Server restarts can therefore repeat dispatch
without creating duplicate Runs. Trigger processing never creates Kubernetes
Jobs directly. At Run insertion, a Git request locks and reads the durable
Target state and requires its exact revision-pinned commit to equal the current
desired commit. A stale request marks its delivery superseded and creates no
Run, closing the refresh/dispatch race in PostgreSQL rather than relying on an
in-memory precheck.

Git Runs have a structured source containing only the trigger name and
delivery ID and have no human actor. Repository, commit, catalog revision,
branch snapshot, and creation time remain in their existing Run fields.
Manual and API Run creation are unchanged and actions remain invocable without
declaring a trigger.

Failed delivery-attempt metadata, bounded retry backoff, allowing later
eligible deliveries to proceed when one delivery fails permanently, and
restart-persistent retry scheduling remain later reliability work. Branch
filters, trigger-specific path overrides, dynamic parameter values,
configurable delivery policies, per-commit delivery, schedules, webhooks, and
provider-specific integrations also remain later work.

## Execution

The Slipramp Server queues a run for the cluster selected by the resolved placement. That cluster's controller claims the run and creates its Kubernetes Job. A checkout init container clones the repository directly from Git and checks out the exact commit recorded by the run. For private repositories, it uses the repository's logical checkout credential resolved to a Kubernetes Secret by the controller. The controller injects the Slipramp harness ahead of checkout so both checkout and the action are cancellable without requiring the harness in either image. The Job downloads any selected artifact inputs separately, then the harness executes the action in its runtime image.

Controllers require outbound access to the Slipramp Server, and execution Jobs require outbound access to their Git repositories and the Server's run-scoped artifact transfer endpoints. Only the Server accesses S3-compatible storage. The Server requires no Kubernetes API access and no inbound network path into execution clusters.

The controller follows the Kubernetes Pod log stream and posts bounded chunks with monotonically increasing byte offsets to the Slipramp Server. It does not repeatedly download the complete log. Repeated chunks are accepted idempotently, and recovery replays the Pod log only to verify and skip the durable prefix before continuing at the Server's cursor. The Server durably appends each new chunk to the run's log artifact before publishing it to connected clients. Kubernetes Pod logs are an execution source, not the retained record, and the loss or cleanup of a Pod must not remove already accepted output. The controller retains a terminal Job until the complete log is durably sealed and the terminal observation is accepted, then removes it with a UID-preconditioned delete. For cancellation, the controller marks the Pod through a controller-owned annotation projected into the checkout and action containers. The harness forwards `SIGTERM` to the active child process and force-stops it after a grace period; the Job remains recoverable while ordinary terminal observation drains and seals the log, reports the canceled outcome, and performs final cleanup.

Runs for the same target are serialized by default to avoid conflicting infrastructure operations. Automatic retries are explicit because transparently repeating a partially completed infrastructure command can be unsafe.

The execution contract should remain small and stable: workspace layout and permissions, standard environment variables, parameters, credential injection, artifact input and output directories, streaming logs, and exit status. The `SLIPRAMP_` environment namespace is reserved for this internal contract and is rejected in catalog-defined runtime, parameter, and credential environment names.

For artifact transfer, an assignment includes a Run-scoped credential derived
from a Server-held HMAC key. The harness removes this credential from the child
action environment. It downloads only the Run's explicit input bindings and
uploads only output names declared by the frozen action. PostgreSQL remains
authoritative for each binding and artifact's provenance, sensitivity,
retention, size, and digest; the Server streams the corresponding bytes to and
from S3-compatible storage.

Each Run currently becomes a Job named `slipramp-run-<run-id>` with stable
Run and cluster labels, `restartPolicy: Never`, and `backoffLimit: 0`. An initial
init container invokes `slipramp-harness --install` from a pinned
controller-configured image to copy the static harness into `/slipramp/bin` on
an `emptyDir`. A following checkout init container uses a pinned image containing
`/bin/sh`, Git, and SSH. The harness launches its checkout script, which fetches
the Run's exact commit into another `emptyDir`; repository contents never pass
through the Server or object storage. The action container mounts that repository
at the runtime's `workingDirectory`, or `/workspace` when omitted, and uses the
Target path as its process working directory. It starts the same harness copy
with the resolved explicit command and parameter arguments, preserving the rule
that repository actions do not receive an implicit shell. Because checkout and
runtime images may use unrelated numeric users, the checkout script makes the
Pod-private repository tree writable by the action container. A placement uses
the Pod security context, including `fsGroup` when required, to make restrictive
projected Secret files readable by the runtime user.

Cluster-controller Git credential mappings resolve a logical checkout
credential to a Kubernetes Secret name. The checkout container mounts that
Secret read-only and recognizes the conventional `gitconfig`,
`ssh-privatekey`, and `known_hosts` keys. Runtime credentials continue to use
their declared environment, `envFrom`, file, or directory injection directly
from placement-selected Secrets.

## Credentials and Injection

Tools should not need Slipramp-specific credential support. Slipramp maps credentials into the conventional environment variables, files, volumes, or workload identity mechanisms already understood by each tool.

Credential configuration has three layers:

1. A placement exposes named credential sources backed by Kubernetes Secrets or other cluster-native identity mechanisms.
2. A runtime declares logical credential requirements and how their keys should be presented inside its container.
3. A target binds each runtime requirement to one of the credential sources offered by its placement.

This keeps Kubernetes Secret names and cluster identity out of portable runtimes while allowing a runtime to define tool-specific conventions such as AWS environment variables or SSH file locations. Actions inherit the target's bindings and may request additional bindings when needed.

Supported injection forms should include individual environment variables, `envFrom`, individual files, mounted secret directories, and placement-defined Kubernetes extensions such as projected service account tokens or CSI volumes. A placement may also provide credentials entirely through its service account and require no container injection.

When using Kubernetes Secrets, references are resolved in the placement's selected cluster. Slipramp should place `secretKeyRef`, secret volume, and projected volume references into the Job rather than copying secret values into PostgreSQL. Arbitrary pod-level mechanisms belong on the placement as an escape hatch, while runtime and target definitions remain portable.

## Safety and Trust

Product users and automation resolve to durable Slipramp principals through
OIDC sessions or API keys. Controllers and execution Jobs retain their separate
cluster-scoped and Run-scoped credentials rather than becoming product
principals. The initial authorization model remains installation-wide and
deliberately small; resource-level ACLs and a general policy engine are not
required. See [AUTH.md](./AUTH.md) for the implementation plan.

Policy should be optional. A user must be able to run an unreviewed `terraform apply` or arbitrary script when that is the desired operating model.

Optional controls may include previews, approvals, protected targets, branch restrictions, role-based access, maintenance windows, and image policies. These controls should layer onto the basic execution model rather than define it.

Execution correctness remains fundamental even without policy: immutable Git commits and catalog revisions, clear run sources, target concurrency, explicit retry behavior, durable run records, and visible cancellation or ambiguous termination.

Repository maintainers can execute code with the capabilities granted by a placement. Slipramp should document this trust boundary plainly rather than imply that containers provide a complete security boundary.

## Control Plane

The control plane owns repository refresh, catalog validation, run scheduling, controller coordination, artifacts, history, and the API/UI used to operate targets. The Slipramp Server is the central deployable process that implements these responsibilities; “control plane” refers to the overall central subsystem rather than a binary name.

Kubernetes clusters are execution backends rather than the source of truth for Slipramp's product state. In-cluster controllers own Job observation and report it to the Server. The Server stores lifecycle phase (`queued`, `assigned`, `preparing`, `running`, or `finished`), terminal outcome (`succeeded`, `failed`, `canceled`, or `timed-out`), cancellation intent, Job identity, diagnostic details, and observation freshness as separate facts. `unknown` is derived from stale observation and controller connectivity rather than stored as a phase. After either side restarts, stable run labels and idempotent observation reports allow central run state to be reconciled with Jobs that still exist in the cluster.

PostgreSQL is the durable database for repositories, catalog revisions and
current projections, Git-trigger deliveries, run metadata, artifact metadata,
and scheduler state. S3-compatible object storage is used only for artifact
contents, including live and completed logs. Artifact metadata and object
references remain in PostgreSQL.

The Server exposes process-only liveness separately from dependency-aware
readiness. Readiness checks PostgreSQL and artifact storage concurrently under
a bounded deadline and reports only non-sensitive availability states. A
failed required dependency makes the Server unready without changing the
liveness result.

The design should favor a simple single-operator deployment while keeping boundaries around repository access, persistence, and execution clear enough to evolve independently.

## Deployment Packaging

Slipramp provides two focused Helm charts. The `slipramp` chart deploys exactly
one Server and one web process and requires separately managed PostgreSQL and
S3-compatible artifact storage. It supplies placements through a ConfigMap and
rolls the Server from that ConfigMap's checksum. Secret-bearing Server settings
default to operator-managed Secret references, and the Server receives neither
Kubernetes RBAC nor a service-account token.

The `slipramp-controller` chart installs exactly one Controller for one stable
Cluster name. It grants only namespaced Job and Pod permissions in the
configured execution namespaces. Controller enrollment and bearer credentials,
Git checkout credentials, and execution credentials remain cluster-local
Secrets. Both charts use deterministic resource names, standard labels,
non-floating images, focused scheduling and security settings, schema
validation, and optional explicit chart-managed Secrets for disposable uses.
The central chart offers a minimal single-host Ingress without bundling a
database or object store. Server, web, and Controller high availability remain
later work. A `vX.Y.Z` Git tag publishes the matching application images and
both versioned charts to GHCR as OCI artifacts. Release validation requires the
tag, chart versions, application versions, and default image tags to agree and
never publishes a floating `latest` tag.

## Technology Stack

The Slipramp Server, in-cluster controller, repository refresh, API, scheduler, and execution harness are written in Go using the latest stable Go release. The Go module path is `github.com/slipramp/slipramp`. PostgreSQL access should use explicit schema migrations and strongly typed query boundaries.

The web application uses TypeScript, React Router v8 in Framework Mode with server-side rendering, and Mantine UI v9. The React Router server runs as a web process and communicates with the Go API; browser access can be presented through a single origin by the deployment ingress or proxy.

New projects and dependencies use the latest stable releases available when introduced, with exact resolved versions committed through Go modules and the frontend lockfile. Updates are intentional and validated rather than floating at deployment time.

Go linting and formatting run through golangci-lint v2 using `golangci-lint run` and `golangci-lint fmt`, with the formatter and linter sets declared in its versioned configuration. Frontend code uses strict TypeScript checking plus the Ox toolchain: Oxlint for TypeScript, React, and accessibility rules, and Oxfmt for formatting TypeScript, TSX, CSS, JSON, YAML, and Markdown. ESLint, Prettier, and Stylelint are not part of the toolchain. The same checks run locally and in CI, and warnings are treated as failures.

## API Contract

OpenAPI is the source of truth for the Slipramp Server HTTP API, including the endpoints used by the web application and cluster controllers. The contract generates Go request and response types, Server interfaces, and the controller client, plus TypeScript types and the frontend client. Handwritten Server code implements the generated interfaces and keeps domain behavior outside generated files.

The specification is assembled from a small number of domain-sized files rather than one large document or one file per endpoint. A root document owns API metadata, versioning, tags, and security, then references grouped path and component files such as:

```text
api/
  openapi.yaml
  paths/
    repositories.yaml
    catalog.yaml
    runs.yaml
    artifacts.yaml
    controllers.yaml
  components/
    common.yaml
    catalog.yaml
    runs.yaml
    artifacts.yaml
    controllers.yaml
```

Files may contain several related paths or schemas. New files are introduced for coherent API areas, not individual operations or models. The build bundles the references into one validated OpenAPI document for code generation and publication.

Generated Go and TypeScript code is never edited by hand. Generator versions and configuration are pinned, one repository command regenerates every target, and CI validates the contract and fails when generated output is stale. Standard request-response operations use the generated frontend client. Live logs use a small reconnecting SSE transport while retaining generated event payload types from the same contract.

## Illustrative YAML

These examples use the current `slipramp.com/v1alpha1` repository contract.
Catalog documents reject unknown fields and duplicate mapping keys so
misspelled execution settings cannot be accepted silently.

### Repository Catalog Layout

Catalog resources live under the `.slipramp` directory at the repository root.
Slipramp recursively reads files ending in `.yaml` or `.yml` from that
directory in lexical path order and ignores YAML elsewhere in the repository.
Each file may contain one or more YAML documents, and every non-empty document
defines one `Runtime` or `Target`. Resources may therefore be grouped into one
file or split across directories without changing their meaning.

Target paths and `watch` patterns are clean, slash-separated,
repository-relative paths. Runtime working directories, credential mount
paths, artifact paths, and input mount paths are clean, non-root absolute
container paths. Resource names and quantities use Kubernetes syntax, and
configured timeouts are positive Go duration strings such as `30m`.

Targets provide defaults for runtime, placement, timeout, credentials, and
resource requests and limits. An action's scalar values replace target
defaults, while credential and resource maps merge by key. The resolved action
is copied from the catalog so freezing it for a Run cannot mutate the catalog
revision. Every effective placement must exist in Server configuration, and
each credential required by the effective runtime must bind to a source that
the placement offers.

### Runtimes

Runtimes live with the repository content and define portable tool environments. Targets can share them while supplying their own paths and commands.

Each named runtime credential must declare at least one injection. Individual
Secret keys can become environment variables or files. `environmentFrom: true`
exposes every key using Kubernetes `envFrom`, while `directory` mounts every
key below one directory. File modes may use permission bits through `0777`.
Environment names and mount paths must not collide within a runtime.

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: terraform
spec:
  image: registry.slipramp.com/runtimes/terraform:1
  environment:
    TF_IN_AUTOMATION: "true"
  credentials:
    backend:
      inject:
        environment:
          AWS_ACCESS_KEY_ID:
            key: accessKeyId
          AWS_SECRET_ACCESS_KEY:
            key: secretAccessKey
---
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: pyinfra
spec:
  image: registry.slipramp.com/runtimes/pyinfra:3
  environment:
    PYTHONUNBUFFERED: "1"
  credentials:
    ssh:
      inject:
        files:
          - key: privateKey
            path: /home/ops/.ssh/id_ed25519
            mode: 0400
          - key: knownHosts
            path: /home/ops/.ssh/known_hosts
            mode: 0444
        directory:
          path: /var/run/ssh-extensions
          mode: 0444
---
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: operations
spec:
  image: registry.slipramp.com/runtimes/operations:2026.10
```

### Controller and Placements

Each cluster runs one controller replica configured with its stable cluster name, Slipramp Server address, bootstrap enrollment credential, controller-owned bearer credential, and local Git credential mappings. Placements remain part of Slipramp Server configuration and select a registered cluster by name.

```yaml
# Controller configuration deployed into the colo cluster.
controller:
  cluster: colo
  server: https://slipramp.com
  executionNamespaces:
    - slipramp-runs
  enrollmentCredential:
    secretRef:
      name: slipramp-controller
      key: enrollment-token
  credential:
    secretRef:
      name: slipramp-controller
      key: token
  gitCredentials:
    github-readonly:
      secretRef:
        name: github-readonly

# Slipramp Server configuration.
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  infrastructure-admin:
    cluster: colo
    jobNamespace: slipramp-runs
    serviceAccountName: infrastructure-admin
    credentials:
      terraform-backend:
        secretRef:
          name: terraform-backend
      ssh-deploy-key:
        secretRef:
          name: ssh-deploy-key
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: "2"
        memory: 2Gi
    podTemplate:
      metadata:
        labels:
          azure.workload.identity/use: "true"
      spec:
        runtimeClassName: kata
        securityContext:
          fsGroup: 65532
```

### Terraform Target

This target permits both a direct apply and an optional preview. Slipramp does not require the preview before `sync`.

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: network
  title: Home network
spec:
  path: infrastructure/network
  watch:
    - infrastructure/network/**
    - infrastructure/modules/**
  runtime: terraform
  placement: infrastructure-admin
  credentials:
    backend: terraform-backend
  actions:
    sync:
      intent: reconcile
      command: [terraform, apply]
      parameters:
        environment:
          type: enum
          values: [staging, production]
          required: true
          argument:
            name: --environment
        auto-approve:
          type: boolean
          argument:
            name: --auto-approve
      triggers:
        - name: reconcile-on-change
          git: {}
          parameters:
            environment: production
            auto-approve: true
    preview:
      intent: preview
      command:
        - terraform
        - plan
        - -out=/slipramp/artifacts/plan.tfplan
      artifacts:
        plan:
          path: /slipramp/artifacts/plan.tfplan
          mediaType: application/vnd.hashicorp.terraform.plan
          sensitive: true
    apply:
      intent: reconcile
      command:
        - terraform
        - apply
        - /slipramp/inputs/plan.tfplan
      inputs:
        plan:
          artifact:
            fromAction: preview
            compatibility: exact-catalog-revision
          mountPath: /slipramp/inputs/plan.tfplan
```

### pyinfra Target

The same target model works for an agentless deployment. The runtime image supplies pyinfra and any Python dependencies, while the placement supplies SSH credentials.

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: colo-servers
  title: Colo servers
spec:
  path: infrastructure/colo
  watch:
    - infrastructure/colo/**
    - infrastructure/python/**
  runtime: pyinfra
  placement: infrastructure-admin
  credentials:
    ssh: ssh-deploy-key
  actions:
    sync:
      intent: reconcile
      command: [pyinfra, inventory.py, deploy.py]
    preview:
      intent: preview
      command: [pyinfra, inventory.py, deploy.py, --dry]
```

### Upgrade Script Target

Arbitrary repository scripts use the same execution path as infrastructure tools. This action-only target exposes an upgrade without claiming to support reconciliation.

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: storage-cluster
  title: Storage cluster
spec:
  path: operations/storage
  watch:
    - operations/storage/**
  runtime: operations
  placement: infrastructure-admin
  actions:
    upgrade:
      intent: operation
      command: [./upgrade.sh]
      parameters:
        version:
          type: string
          required: true
          environment: UPGRADE_VERSION
        strategy:
          type: enum
          values: [rolling, immediate]
          argument:
            name: --strategy
        max-unavailable:
          type: number
          argument:
            name: --max-unavailable
        force:
          type: boolean
          argument:
            name: --force
```

Parameters use the `string`, `boolean`, `number`, or `enum` type. Enum
parameters declare their allowed `values`. Every parameter maps to exactly one
environment variable or argument. An empty `argument: {}` is positional. A
named string, number, or enum argument emits the name followed by its value; a
named boolean argument is a flag emitted only when true. Positional and
environment boolean values use `true` or `false`. Supplied arguments are
appended to the action command in lexical parameter-name order, and omitted
optional parameters emit nothing.

Artifact inputs use the input name as the artifact name on `fromAction`. The
source action must declare that output. The only currently supported
compatibility policy is `exact-catalog-revision`, and it is the default when
`compatibility` is omitted.

## Ongoing Design Work

The accepted MVP settled the core architecture. These details remain open as
Slipramp evolves:

- Versioning and compatibility behavior beyond the pre-stable `v1alpha1` repository contract.
- Additional standard harness environment variables beyond the implemented
  workspace, artifact-transfer, log, signal, and cancellation contract.
- PostgreSQL history retention and archival.
- Upgrade, backup, recovery, and broader failure-mode acceptance guidance.

The following product areas remain intentionally later work:

- MCP integration and further identity-administration or audit capabilities
  beyond the implemented OIDC, API-key, authorization, and attribution model.
- Git trigger branch/path overrides, dynamic parameter values, delivery
  policies, per-commit delivery, and provider-specific integrations; schedule
  and webhook triggers, including missed-event and overlap semantics.
- Git-trigger delivery failure metadata, bounded retry backoff, isolation of a
  permanently failing delivery from later eligible work, and restart-persistent
  retry scheduling.
- Explicit controller enrollment approvals and credential rotation workflows.
- Controller high availability, fencing, leader election, and multiple active replicas for one cluster name.
