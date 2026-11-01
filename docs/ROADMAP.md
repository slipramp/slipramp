# Slipramp Roadmap

This is the backlog for unfinished product, reliability, and operator work.
[DESIGN.md](./DESIGN.md) records the implemented architecture and governing
decisions. Completed milestones belong in Git history rather than this backlog.
When work lands, update the design and remove or narrow the corresponding item.
The sections below group related work; they are not release commitments or a
strict priority order.

## Kargo operations

- [ ] Enable candidate-branch cleanup per Repository after confirming retention
      and write credentials.
- [ ] Document Stage freshness, preparation and diff failures, review expiry,
      stale target pushes, and operational recovery.

Post-approval push and reconciliation results, same-identity preparation retry,
policy audit history beyond current attribution, and provider-neutral review
abstractions remain deferred until an operator need or second integration makes
their semantics concrete.

## Git-trigger extensions

- [ ] Design branch filters, trigger-specific path overrides, dynamic
      parameters, configurable delivery policies, per-commit delivery, and
      provider-specific integrations only as concrete use cases require them.
- [ ] Define missed-event and overlap semantics before adding scheduled or
      webhook triggers.

All trigger adapters must use ordinary Run creation and preserve manual and API
execution without requiring a trigger.

## Catalog tooling

- [ ] Publish versioned binaries, checksums, the schema, and a small CLI OCI
      image when Slipramp begins versioning releases.
- [ ] Reassess `catalog affected` for explaining which Targets and Git triggers
      match changed paths or two Git revisions.
- [ ] Reassess semantic `catalog diff` for comparing complete resolved catalogs
      rather than YAML text.

## Placement inventory and Controller preflight

Live preflight must preserve the Server's lack of Kubernetes credentials and
direct Kubernetes API access. The Controller performs cluster-local checks and
returns bounded, sanitized results through the existing outbound protocol.

- [ ] Derive catalog-aware Runtime credential compatibility from current
      accepted revisions. Show whether every required logical credential is
      offered by a Placement while keeping this static result distinct from live
      Secret checks.
- [ ] Add an explicit, operator-started preflight command. Define idempotency,
      authorization, timeouts, concurrency, durable result history, and behavior
      when the selected Controller is disconnected.
- [ ] Check namespace ownership, service-account and referenced Secret
      existence, required Secret keys, Controller and workload RBAC, and
      representative pod admission. Treat image pullability as an optional,
      explicitly cleaned-up active probe, and report unsupported or
      inconclusive checks separately from failures.

## Reliability and operator guidance

- [ ] Broaden acceptance across multiple execution Clusters, network
      interruptions, and Controller or Server restarts. Cover cancellation
      submission during a PostgreSQL outage, bounded Kargo command attempts,
      Promotion preparation retry recovery, and terminal diagnostics against
      real dependencies.
- [ ] Add focused operational signals only when the remaining acceptance cases
      reveal failure modes not covered by the existing metrics.
- [ ] Document upgrades, coordinated PostgreSQL and object-storage backups,
      restore and recovery drills, and troubleshooting beyond the existing
      installation and development guidance.
- [ ] Define PostgreSQL history retention and archival separately from
      artifact-content retention. Consider advanced Artifact policies or
      replication only for a concrete deployment requirement.
- [ ] Define contract versioning and upgrade behavior when the pre-stable API,
      catalog, and database need a stability policy.

## Security hardening

- [ ] Require Controller bearer and enrollment credentials to use one canonical
      encoding of at least 32 random bytes. Reject enrollment-token reuse across
      Clusters and add bounded throttling for failed enrollment and Controller
      authentication without weakening first-enrollment binding.

## Native iOS client

- [ ] Complete physical-device accessibility verification, signing, and
      remaining TestFlight metadata.
- [ ] Add APNs device registration and routed push delivery only after the
      native Inbox proves a concrete notification need.

## Optional Run review

- [ ] Design Server-owned policies, source-wide enforcement, separation of
      duties, and required preview or Artifact rules only when operators need a
      security boundary stronger than repository-controlled configuration.
- [ ] Add expiry and dedicated review queues only with explicit product
      semantics for recovery, noise, and operator ownership.
- [ ] Extract shared review persistence only if another production flow beyond
      Kargo demonstrates a genuinely common lifecycle rather than merely common
      vocabulary.

## Deferred product directions

Revisit these only when an operator need justifies focused design:

- Explicit Controller enrollment approvals and credential rotation.
- Server and Web high availability; Controller high availability, fencing,
  leader election, or multiple active replicas per Cluster name. The supported
  topology remains one of each; do not add partial distributed coordination.
- Pull-request and ephemeral environments; Actions outside Targets.
- Tool-specific parsing or provider integrations and richer drift detection
  beyond user Actions and existing Kargo branch drift.
- Additional outbound notification events and adapters, typed routing filters,
  explicit delivery retries, user subscriptions, or interactive provider
  decisions. Preserve the transition, deduplication, recovery, noise, and
  security boundaries in [notification delivery](./DESIGN.md#notification-delivery).
- Protected Targets, branch restrictions, maintenance windows, image policy, or
  broader authorization. Such controls must preserve the opt-in safety model.
- Workflow graphs and multi-step pipelines, only if independent Actions and
  scripts cease to meet actual requirements.
- Harness environment extensions beyond the current workspace, Artifact,
  logging, signal, and cancellation contracts.
