# Slipramp Roadmap

This is the backlog for unfinished product, reliability, and operator work.
[DESIGN.md](./DESIGN.md) records the implemented architecture and governing
decisions. Completed milestones belong in Git history rather than this backlog.
When work lands, update the design and remove or narrow the corresponding item.
The sections below group related work; they are not release commitments or a
strict priority order.

## Kargo completion and operations

Stage discovery, policies, durable external and operator-started requests,
Controller materialization, restart-safe pre-callback terminal reconciliation,
callback hydration, ordinary preparation Runs, diff artifacts, decisions,
expiry, Freight comparisons, promotion actions, and background-refreshed branch
drift with Stage-level pagination are implemented. Opt-in candidate-branch
cleanup scans remote inventory, protects nonterminal Promotions, exact-tip
guards deletions after retention, and records the latest outcome per Repository.
Live Kargo acceptance and the standalone Kargo Gate retirement are complete.
Remaining work concerns operations rather than rebuilding that vertical.

### Live cutover and acceptance

On 2026-10-23, all 109 Stages in the eight observed Kargo projects cut over from
Kargo Gate to Slipramp-backed tasks, completed the live acceptance matrix, and
retained their configured automatic or review-required policies. The standalone
Kargo Gate service and credentials were then retired. The intentionally
unobserved `kargo-ivy` project retains its direct-push tasks.

- [ ] Enable candidate-branch cleanup per Repository after confirming retention
      and write credentials.

### Remaining capabilities

- [ ] Document Stage freshness, preparation and diff failures, review expiry,
      stale target pushes, and operational recovery.

Deferred choices: whether Kargo reports post-approval push/reconciliation results;
explicit same-identity preparation retry; policy audit history beyond current
attribution; and provider-neutral review abstractions if a second integration
actually needs them. Current preparation failures require a new external
Promotion identity; approval does not imply downstream success.

## Git-trigger extensions

Trigger extensions remain separate design work: branch filters, trigger-specific
path overrides, dynamic parameters, configurable delivery policies, per-commit
rather than per-accepted-head delivery, and provider-specific integrations.
Scheduled and webhook triggers also need explicit missed-event and overlap
semantics. All adapters must use ordinary Run creation and preserve manual/API
execution without requiring a trigger.

## Catalog tooling

The shared diagnostic pipeline, portable and deployment validation levels,
checkout-aware warnings, `slipramp catalog validate`, deterministic diagnostic
text, JSON, and SARIF output, and a generated versioned catalog JSON Schema with a
freshness check are implemented. `slipramp catalog explain` provides offline
effective Action inspection with provenance in text and JSON, portable unresolved
Placement values, optional deployment validation, and shared parameter simulation.
The authenticated Server API and Web Action Run page reuse the same explanation
model against current Placement configuration while allowing required form
parameters to remain omitted during initial display.
Source-pinned local, pre-commit, CI, and YAML
language-server workflows are documented. Remaining distribution work is
intentionally deferred until Slipramp begins versioning releases:

- [ ] Publish versioned binaries, checksums, the schema, and a small CLI OCI
      image when Slipramp begins versioning releases.
- [ ] Reassess `catalog affected` for explaining which Targets and Git triggers
      match changed paths or two Git revisions.
- [ ] Reassess semantic `catalog diff` for comparing complete resolved catalogs
      rather than YAML text.

## Reliability and operator guidance

- [ ] Broaden acceptance for multiple execution Clusters, network interruptions,
      Controller/Server restarts, and PostgreSQL/S3 outages, including retained
      log recovery, SSE reconnection, and cancellation under failure. Terminal
      Pod loss with a partial durable log has unit and persistence coverage;
      exercise it against real dependencies with responsive command polling
      during blocked Run work, coalesced cancellation, bounded Kargo command
      attempts, Promotion preparation retry recovery, and terminal diagnostics.
- [ ] Expand operational diagnostics beyond the implemented Server HTTP,
      process, runtime, PostgreSQL-pool, current-state, and core worker metrics
      plus the Controller process/runtime foundation. Add focused signals only
      when the remaining failure cases justify them.
- [ ] Document upgrades, coordinated PostgreSQL/object-storage backups, restore,
      recovery drills, and troubleshooting beyond existing identity recovery,
      Helm installation, local development, and kind instructions.
- [ ] Define PostgreSQL history retention and archival separately from existing
      artifact-content retention. Consider advanced artifact policies or replication
      only for a concrete deployment requirement.
- [ ] Define contract versioning and upgrade behavior when the pre-stable API,
      catalog, and database actually need a stability policy. Current development
      continues to prefer clean breaking changes.
- [ ] Extend the Harness environment contract only when tools demonstrate a need
      beyond current workspace, artifacts, logging, signals, and cancellation.

## Identity and MCP

OIDC, local sessions, API keys, service accounts, role/scope enforcement, and Run
attribution are implemented.

- [ ] Add further identity-administration capabilities when concrete operations
      require them; introduce a general administrative audit model only with a
      coherent set of events.
- [ ] Design a remote MCP adapter against the current transport and authorization
      specification at implementation time. Verify issuer, resource/audience,
      scopes, and client-registration support with the chosen authorization server.
- [ ] Map validated MCP identity to existing Principals and reuse authorization
      and Run services. Derive an MCP Run source at that boundary; add scopes only
      when the actual tool surface needs them.

Slipramp should act as an OAuth resource server, not an authorization server.
Browser OIDC support alone does not establish suitable MCP token issuance.
Static-header clients may use a Slipramp API key, but that does not replace
standard OAuth discovery for general remote clients. Avoid separate MCP users,
resource ACL frameworks, or an identity platform without a product requirement.

## Optional Run review

Repository-controlled `review: required` on named Git triggers is implemented.
The delivery and immutable Run snapshot the requirement; pending Runs remain
queued but ineligible for assignment, approval restores ordinary FIFO
eligibility, rejection cancels with stable attribution, and latest-wins
coalescing continues before and after approval. Direct execution remains the
default. This is a voluntary repository-owned operational guard, separate from
Kargo review and not a Server-enforced security policy. The Web Run detail now
exposes permission-aware Approve and Reject controls, decision attribution, and
review-aware activity and history labels. Pending Run reviews appear in the
operational Inbox only for Principals authorized to decide them, and Run history
supports exact review-state filtering. Optional decision comments are trimmed,
bounded, persisted and displayed as part of the immutable decision. A dedicated
`runs:review` permission and `slipramp:runs:review` API-key scope authorize Run
decisions and Inbox visibility independently from Run creation and cancellation.
Review-required Git Run creation reuses the routed `review.requested`
notification contract with a Run subject; the Run, delivery linkage, event, and
outbound deliveries commit atomically.

- [ ] Design Server-owned policies, source-wide enforcement, separation of
      duties, and required preview or artifact rules only when operators need a
      security boundary stronger than repository-controlled configuration.
- [ ] Add expiry and dedicated review queues only with explicit product
      semantics for recovery, noise, and operator ownership.
- [ ] Extract shared review persistence only if another implemented flow beyond
      Kargo demonstrates a genuinely common lifecycle rather than merely common
      vocabulary.

## Deferred product directions

These are possibilities, not accepted implementation scope. Revisit them when
an operator need justifies a focused design:

- Explicit Controller enrollment approvals and credential rotation.
- Server/Web high availability; Controller high availability, fencing, leader
  election, or multiple active replicas per Cluster name. The supported topology
  remains one of each; do not add partial distributed coordination.
- Pull-request and ephemeral environments; Actions outside Targets.
- Tool-specific parsing/provider integrations and richer drift detection beyond
  user Actions and implemented Kargo branch drift.
- Additional outbound notification events and adapters, typed routing filters,
  explicit delivery retries, user subscriptions, or interactive provider
  decisions. Add them only for a concrete operator need and preserve the
  transition, deduplication, recovery, noise, and security boundaries in
  [notification delivery](./DESIGN.md#notification-delivery).
- Protected Targets, branch restrictions, maintenance windows, image policy, or
  broader authorization. Such controls must preserve the opt-in safety model.
- Workflow graphs and multi-step pipelines, only if independent Actions and
  scripts cease to meet actual requirements.
