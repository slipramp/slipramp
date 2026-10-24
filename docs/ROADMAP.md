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

## Reliability and operator guidance

- [ ] Broaden acceptance for multiple execution Clusters, Pod loss, network
      interruptions, Controller/Server restarts, and PostgreSQL/S3 outages, including
      retained log recovery, SSE reconnection, and cancellation under failure.
      Unit coverage includes responsive command polling during blocked Run work,
      coalesced cancellation, and bounded Kargo command attempts; exercise these
      paths against real dependencies as part of acceptance, along
      with Promotion preparation retry recovery and terminal diagnostics.
- [ ] Expand operational diagnostics beyond current readiness and observation
      freshness, guided by those failure cases.
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

Ordinary Runs remain directly executable by default. Required previews and Run
approval need their own explicit opt-in policy design, separate from Kargo.

- [ ] Define policy selection, snapshotted approval state, expiry/rejection,
      attribution, and races with cancellation, reruns, and Git coalescing.
      Extend the Run-owned transition rules under the existing PostgreSQL
      transaction boundary.
- [ ] Keep a waiting Run `queued` and exclude it from assignment until review is
      satisfied. Approval is orthogonal to phase and outcome, not a new execution
      phase or invocation resource. Rejection/expiry should cancel the undispatched
      Run with a stable reason.
- [ ] Reuse `review.requested` notifications with a Run subject once the state
      design is accepted. Extract shared review persistence only if two implemented
      flows demonstrate the need.

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
