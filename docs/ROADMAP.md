# Slipramp Roadmap

This is the backlog for unfinished product, reliability, and operator work.
[DESIGN.md](./DESIGN.md) records the implemented architecture and governing
decisions. Completed milestones belong in Git history rather than this backlog.
When work lands, update the design and remove or narrow the corresponding item.
The sections below group related work; they are not release commitments or a
strict priority order.

## Operational Inbox

The Web homepage already composes pending decisions and operational problems.
The dedicated Server projection and durable acknowledgements are unimplemented.
The accepted detailed proposal is [INBOX.md](./INBOX.md).

- [ ] Define `GET /api/v1/inbox` and the exact-batch
      `POST /api/v1/inbox/problem-acknowledgements` contract; generate all clients.
- [ ] Add `inbox:write` permission and `slipramp:inbox:write` scope across
      identity validation, database constraints, Access UI, and authorization tests.
- [ ] Persist per-Principal acknowledgements keyed by opaque problem-occurrence
      ID, with idempotent batch insertion and complete internal domain reads.
- [ ] Implement `internal/inbox` projection using one evaluation time,
      deterministic occurrence identity, capabilities, activity counts, Promotion
      expiry, and stale-Run grouping beneath unreachable Clusters.
- [ ] Track dependency outage transitions so a new outage is a new occurrence.
      Keep readiness available separately when PostgreSQL or the Inbox API fails.
- [ ] Add generated handlers and fail-closed authorization; reject partial
      source reads instead of presenting an incomplete **All clear** result.
- [ ] Replace the Web's domain fan-out and pagination limits with the dedicated
      API and readiness fallback. Add **Acknowledge** and **Acknowledge all** using
      only rendered problem IDs, with pending and error states.
- [ ] Verify two-Principal isolation, recurrence, concurrent resolution/new
      problems, role/scope behavior, database persistence, and Web cutover through
      focused unit, HTTP, integration, and acceptance coverage.

Completion means acknowledgements survive reloads, affect only the acting
Principal and exact occurrence, and cannot hide decisions or concurrent unseen
problems. Do not add a durable problem lifecycle table or retain both Web and
Server composition paths.

Later decisions, driven by actual scale or operator need: an acknowledged view
and unacknowledge API; team acknowledgement or assignment; durable occurrence
history; acknowledgement retention; pagination and whole-query batch semantics;
server-pushed updates; and broader audit or escalation behavior. Cleanup must
never silently resurface an active acknowledged occurrence.

## Notifications

Outbound delivery is unimplemented. [NOTIFICATIONS.md](./NOTIFICATIONS.md)
contains the accepted provider-neutral design with Slack as the first adapter.

- [ ] Add startup configuration for destinations, exact-event routes, public Web
      URL, bounded retry, retention, and secret-file mounts, including Helm
      validation. Keep credentials and destination secret URLs out of PostgreSQL.
- [ ] Add versioned immutable events and per-destination deliveries, grouped
      SQL/migrations, an enqueue boundary, and a generated delivery-status API.
- [ ] Implement Slack rendering and delivery with restart-persistent attempts,
      exponential backoff with jitter, rate-limit handling, bounded errors, terminal
      failure, and independent progress for separate destinations.
- [ ] Atomically enqueue one `review.requested` event and routed deliveries when
      a guarded Promotion becomes `pending`. Repeated reconciliation or machine
      polls must not enqueue duplicates.
- [ ] Expose non-sensitive delivery status and exhausted failures in operator
      views/Inbox. Add deterministic dispatcher tests and acceptance coverage for
      message content, authenticated links, restart recovery, and provider outage.

Delivery is at least once. Notification failure never changes a Promotion or
Run outcome. Messages link to authenticated Slipramp; they contain no logs,
sensitive diffs, artifact bodies, or credentials. Slack cannot approve or reject.

Introduce more events only with transition, deduplication, recovery, and noise
semantics: review resolution, failed Runs/refreshes, unreachable Controllers,
and stale observations are candidates. Signed webhooks and then email are
possible adapters when required. Defer user subscriptions, routing expressions,
template editors, interactive provider decisions, and a general event bus.

Implementation must settle delivery/terminal-record retention, route filters
needed by actual deployments, explicit operator retries, outage notification
noise, and whether delivery history later participates in broader audit views.
Pending deliveries cannot be pruned by retention.

## Kargo completion and operations

Stage discovery, policies, durable requests, ordinary preparation Runs, diff
artifacts, decisions, expiry, Freight comparisons, and cached branch drift with
Stage-level pagination are implemented. Remaining work concerns live acceptance
and operations rather than rebuilding that vertical.

### Live cutover and acceptance

The recorded 2026-10-22 `ops-ci-homelab` exercise covered a guarded request,
real Git/Argo CD artifacts including an empty Git diff, operator approval,
target-branch push, and healthy/synced Argo CD reconciliation. Preparation
failures were observed during bring-up and recovered through new Promotions.
Disposable kind coverage also exercises automatic approval and guarded
preparation, with authenticated Stage snapshots and deterministic Argo CD
fixtures. These do not close the entire live cutover matrix.

- [ ] Complete the noncritical live Stage matrix: automatic, approved, rejected,
      expired, preparation-failed, and stale-target outcomes; retain reproducible
      evidence for rejection, expiry, and stale-target cases still outstanding.
- [ ] Confirm/recreate any active per-Stage policies explicitly.
- [ ] Confirm removal of the standalone `kargo-gate` Deployment, Service, routes,
      PVC, service account, RBAC, machine key, and obsolete Argo CD/Git credentials
      after acceptance. Do not add legacy importers or compatibility routes.
- [ ] Inventory existing candidate branches and retain or deliberately clean
      them only after verifying that no pending Promotion depends on them.

The last two items were open in the previous cutover record and require live
deployment verification before they can be marked complete.

### Remaining capabilities

- [ ] Add explicit, observable candidate-branch expiry and cleanup with protection
      for pending Promotions.
- [ ] Deliver newly pending review notifications through the shared notification
      work above.
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
      retained log recovery and cancellation under failure.
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
- Protected Targets, branch restrictions, maintenance windows, image policy, or
  broader authorization. Such controls must preserve the opt-in safety model.
- Workflow graphs and multi-step pipelines, only if independent Actions and
  scripts cease to meet actual requirements.
