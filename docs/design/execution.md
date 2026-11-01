# Controller execution

This document defines the outbound Controller protocol, Kubernetes ownership,
Job materialization, Harness behavior, credential injection, cancellation, and
recovery contracts.

## Controller identity and delivery

A Controller has one stable Cluster name, a cluster-scoped enrollment
credential, and a separate Controller-owned bearer credential. Enrollment
registers only the bearer digest. Repeating the same enrollment is idempotent;
credential replacement is rejected. Subsequent requests derive Cluster identity
from that credential. Authenticated traffic updates last contact without a
separate heartbeat protocol.

Delivery uses a small versioned HTTP API and is at least once. Long polls return
Run assignment, Run cancellation, or configured Kargo work. Deterministic
resource identity makes repeated commands safe without a broker, bidirectional
RPC, distributed lease, or leader election.

The Server schedules cancellation before new assignment and interleaves due
Kargo coordination without creating another delivery channel.

The Server remains authoritative for scheduling and capacity. The Controller
also bounds concurrent new Run workers as local defense in depth. Recovered Jobs
are always observed even when they exceed a newly lowered local worker limit.
Duplicate assignments reuse the existing worker; repeated cancellations
coalesce.

Retryable transport and Server failures leave durable Server work eligible for
redelivery. Invalid requests, conflicts, identity mismatches, and unusable
durable log state stop the affected worker while retaining its Kubernetes
resources for diagnosis. Credential rejection or malformed polling responses
stop the Controller rather than silently discarding work.

## Namespace and object ownership

Every execution namespace is exclusively owned by one Controller and one
Slipramp control plane. A Controller may own several configured namespaces and
rejects assignments outside them. It scans each owned namespace before polling
after restart.

Duplicate active Controllers, reused Cluster names, and shared namespaces are
configuration errors, not failover mechanisms. Another installation may reuse a
namespace only after the previous installation's Jobs are removed.

Every Job operation verifies namespace, deterministic name, Run and Cluster
labels, and admitted Job UID. Once a worker pins the UID, it never adopts a
replacement. Pod observation, log access, cancellation, and deletion also
require matching labels and an owner reference to that exact Job. Mismatches
retain resources for diagnosis.

## Job contract

Each execution uses a deterministically named Job with:

- stable Run and Cluster labels;
- `restartPolicy: Never` and `backoffLimit: 0`;
- an execution deadline;
- a pinned Harness installer image;
- a pinned checkout image with Git, shell, and SSH support that runs the mounted
  Harness executable;
- an Action container from the resolved Runtime;
- private volumes for checkout, credentials, and declared Artifact transfer.

The checkout container clones the exact commit into a Pod-local volume. The
Action container mounts it at the Runtime working directory, or `/workspace`,
and starts from the Target's repository-relative path through the injected
Harness.

Placement policy owns service account, resources, security context, node
selection, and permitted pod extensions. Extensions cannot replace
Slipramp-owned containers, init containers, restart policy, service account,
node selector, or deadline.

The Controller records the first Kubernetes `status.imageID` observed for the
Harness, checkout, and Action roles. A conflicting later identity is rejected.
Observed image identity records what Kubernetes executed; it does not impose a
digest-only image policy.

## Harness contract

The Harness:

1. downloads explicitly selected input Artifacts;
2. starts the Action command as an explicit argument array;
3. connects the child process to standard streams;
4. forwards termination signals and enforces the cancellation grace period;
5. preserves the child exit status;
6. uploads declared output Artifacts.

The Harness checks cancellation before starting the Action, including after
downloading input Artifacts, and skips execution when cancellation is already
requested.

The Run-scoped transfer credential is removed from the child environment.
Artifact transfer failure makes an otherwise successful execution fail and
never hides a nonzero child exit code. Runtime images and tools require no
Slipramp libraries or protocol support.

## Credential injection

Execution credentials are resolved in three layers:

1. A Placement exposes logical sources backed by cluster-local Kubernetes
   Secrets, service accounts, or workload identity.
2. A Runtime declares logical requirements and environment, `envFrom`, file, or
   directory injection conventions.
3. A Target binds Runtime requirements to Placement sources; Actions may extend
   inherited bindings.

Jobs reference Kubernetes Secrets directly. Values never pass through product
state. Environment names and mount paths cannot collide. Placement pod
extensions may use projected tokens, CSI volumes, and other cluster-native
mechanisms.

Git checkout uses its own mapping from logical credential names to local Secrets
with conventional Git configuration, SSH private key, and known-hosts entries.
These credentials are mounted only for checkout.

## Placement inventory

The Server exposes its validated effective Placement configuration as a
sanitized product view. It includes Cluster, namespace, effective service
account, timeout and resource defaults, logical credential source names,
configuration checksum, Controller reachability, last contact, and active
Target usage by resolved Action Placement.

The inventory excludes Secret references, Secret values, node selectors, and
arbitrary pod-template content. It reports Server configuration plus Controller
observation; it does not contact Kubernetes or claim that referenced cluster
objects exist.

## Cancellation and recovery

Queued cancellation finishes in PostgreSQL without Controller work. Once a Job
exists, cancellation is delivered before new assignment. The Controller marks
intent for the checkout and Action containers; the Harness sends `SIGTERM` and
force-stops the child after its grace period.

For terminal Jobs, durable log closure and accepted terminal reporting precede
UID-checked Job deletion. Complete logs seal with size and digest. If the log
source disappears after a nonempty durable prefix, that prefix becomes an
explicitly incomplete failed log before terminal reporting and cleanup.

When Kubernetes has marked a Job with `FailureTarget`, a terminated Action
retains that failure or timeout outcome even if its signal handler exited
successfully. The pending failure does not finish an Action that is still
running.

Storage, reporting, or deletion failures retain the Job for recovery. Kubernetes
TTL cleanup is disabled because it could remove the only remaining log source
during an outage. All closure, reporting, and deletion steps are idempotent and
resume after Controller restart.
