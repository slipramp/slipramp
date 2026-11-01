# Kargo Promotions

This document defines Slipramp's optional Kargo integration. Ordinary Targets,
Actions, and Runs do not depend on Kargo.

## Authority boundary

Kargo remains authoritative for Freight availability, soak and verification
requirements, promotion windows, rendering, candidate and target branch updates,
Promotion admission, and Argo CD reconciliation.

Slipramp owns observed Stage inventory, optional review policy, durable request
context, preparation Runs and Artifacts, operator decisions, and operational
projections. Approval means permission to proceed; it does not claim that Kargo
pushed or reconciled successfully.

The Server never receives Kubernetes or Kargo credentials. The Controller for
the configured Cluster performs the small set of required Kargo API operations
through the existing outbound command protocol.

## Integration configuration and inventory

Named integrations are strict versioned Server startup configuration. Each
selects a Cluster, Repository allowlist, ordered presentation environments,
preparation Target and Action, freshness and review retention, optional Kargo
and Argo CD links, and optional candidate-branch cleanup policy. Controller
configuration selects the Stage namespaces observed for an integration.

An optional Controller observer reads every Stage page from configured
namespaces and selects Stages whose `slipramp.com/integration` label equals the
integration name. Explicit Stage annotations supply application, environment,
Repository URL, target branch, and source paths; the observer does not infer
those coordinates from promotion-template variables or repository layout.
See the [Stage metadata contract](../CONFIGURATION.md#kargo-stage-metadata).

The observer submits complete authenticated snapshots of participating Stages.
A partial list or malformed participating Stage is never accepted as complete;
unselected Stages do not affect inventory. Repeated snapshot identity is
idempotent; successful snapshots archive missing or deselected Stages while
retaining policy and history. Inventory failure affects Stage freshness without
interrupting ordinary Run delivery.

Stage identity is `(integration, project, name)`, with project equal to the
Kubernetes namespace and Cluster supplied by the authenticated Controller.
Stored observations contain typed current Freight, requested-Freight origins,
declared source Stages, direct-Warehouse eligibility, aggregate health,
application identity, repository and branch context, and successful-render
acknowledgement.

Stage policy defaults to `automatic`. `review-required` may have an expiry.
Promotion acceptance snapshots the effective policy; later policy changes do
not alter existing Promotions.

## Request sources and idempotency

Kargo may submit an external Promotion callback through a service-account API
key. The request supplies external identity, project, Stage, base and candidate
Git object IDs, and a candidate branch under `kargo/promotion/`. Slipramp
resolves the remaining context from fresh durable Stage inventory.

An operator may instead start a Promotion from exact observed Freight on an
upstream Stage or a directly requested Warehouse. The Server resolves trusted
integration, Cluster, namespace, target Stage, Repository, source topology, and
policy facts from one durable transaction. The browser never supplies those
coordinates as authority.

`Promotion` is the only public lifecycle for both sources. Kargo-originated
requests retain service-account attribution. Slipramp-originated requests are
durable before their Kubernetes object exists and retain the requesting human
Principal throughout callback correlation.

External identity plus a canonical request digest makes identical retries
idempotent and rejects conflicting reuse. Durable request state is committed
before asynchronous creation or preparation begins.

## Lifecycle

```text
creating --callback--> preparing --> pending --> approved/rejected/expired
    |                       `---------------------------> error
    `--terminal creation or pre-callback result--------> error
```

An automatic-policy callback advances directly to `approved`. `creating`
includes queued Controller delivery, bounded materialization retries, and the
wait for callback hydration. `preparing` owns one ordinary revision-pinned Run.
`pending` accepts one operator decision or durable expiry. Terminal failures use
`error` with a bounded non-sensitive diagnostic.

Domain states return as domain responses; expected lifecycle errors are not
disguised as transport failures. Callback hydration, Controller reports,
decisions, and expiry lock the same durable Promotion so races resolve
atomically and identical reports remain idempotent.

## Controller materialization and reconciliation

For a Slipramp-originated request, the Server schedules creation through the
responsible Controller. The command snapshots target Stage UID and exact
Freight. The Controller creates a Kargo Promotion with a stable Slipramp
annotation, adopts only a matching existing object after ambiguity, and reports
the admitted Kargo name and UID.

Creation uses durable bounded backoff. Transient Kubernetes and transport
failures do not block later work; invalid or forbidden admission and exhausted
retry budget move the Promotion to `error`. No separate Kargo client channel,
impersonation layer, broker, or Job is introduced.

After materialization, fixed-interval pre-callback reconciliation inspects the
annotated object until callback hydration. Missing or nonterminal state remains
scheduled. A terminal Kargo phase before callback moves the durable Promotion to
`error`. Callback hydration and reconciliation reports are serialized so a late
report cannot overwrite the winning terminal fact.

## Guarded preparation and decisions

A guarded request dispatches exactly one ordinary Run pinned to the candidate
revision. The configured Action receives snapshotted Promotion, Stage,
Repository, revision, branch, and source-path parameters. The first-party Kargo
tools command uses read-only Git and Argo CD credentials and emits two sensitive
bounded Artifacts:

| Artifact | Media type | Meaning |
| --- | --- | --- |
| `git-diff` | `text/x-diff; charset=utf-8` | Exact base-to-candidate Git diff |
| `argo-diff` | `application/vnd.slipramp.argocd-diff+json` | Validated structured Argo CD resource changes |

The tools image has no Slipramp API integration; the Harness transfers declared
Artifacts. Preparation never mutates Git refs or Kubernetes resources.

Argo CD text diffs do not reliably include a changed field's enclosing YAML
structure. The first-party tools therefore leave all resource changes visible,
including changes to keys commonly used for version labels; those keys can also
control selectors or application data.

Successful Run completion plus Artifact validation moves the Promotion to
`pending`. A failed Run or missing, malformed, oversized, expired, or deleted
Artifact moves it to `error` without retrying the infrastructure command.
Dispatch and result reconciliation may retry their own durable coordination
work, but never create a second preparation Run.

Only pending Promotions accept `approved` or `rejected`. The immutable decision
records Principal, time, outcome, and optional normalized comment. Identical
retries are idempotent; conflicting decisions are rejected. Expiry does not
invent a human actor. Kargo performs the approved target-branch update without
force and continues its own reconciliation.

## Candidate branch cleanup

Cleanup is opt-in per integration and Repository. The Server lists remote
`kargo/promotion/*` refs using a configured write credential and joins them with
authoritative Promotion state. This explicit cleanup is the only Server-side Git
mutation.

A branch is eligible only when every referencing Promotion is terminal, the
retention period has elapsed, and the current remote tip equals the recorded
candidate revision. Deletion rereads the exact ref and uses force-with-lease for
that expected tip. Missing refs are idempotent success; moved, untracked,
nonterminal, mismatched, and disabled refs are retained.

Cleanup records one bounded latest-run summary per Repository rather than a
second branch-history model. Failures wait for the next scan and never retry a
Promotion command.

## Operational projections

Pipeline, Freight availability, health, and branch drift are derived from Stage
observations and cached read-only Git evidence. They do not create a second
Promotion lifecycle or query Kargo and Argo CD during ordinary product reads.

Freight choices come from exact observed upstream Stage or directly requested
Warehouse evidence, not inferred environment adjacency. Environment order is a
presentation setting only. Drift compares watched source paths and target
movement independently, preserves the last successful render acknowledgement,
and reports stale, failed, or unknown evidence rather than treating absence as
agreement.
