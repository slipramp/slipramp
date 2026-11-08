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
pushed or reconciled successfully. Slipramp separately records Kargo's exact
terminal Promotion phase, observation time, and bounded sanitized diagnostic.
`Succeeded` means the Kargo Promotion completed; it does not independently
claim Argo CD health beyond the steps enforced by that Promotion.

The Server never receives Kubernetes or Kargo credentials. The Controller for
the configured Cluster performs the small set of required Kargo API operations
through the existing outbound command protocol.

## Integration configuration and inventory

Named integrations are strict versioned Server startup configuration. Each
selects a Cluster, Repository allowlist,
preparation Target and Action, freshness and review retention, optional Kargo
and Argo CD links, and optional candidate-branch cleanup policy. Controller
configuration selects the Stage namespaces observed for an integration.

An optional Controller observer reads every Stage page from configured
namespaces and selects Stages whose `slipramp.com/integration` label equals the
integration name. Optional Stage annotations enrich the inventory with
application, environment, Repository URL, target branch, and source paths; the
observer does not infer those coordinates from promotion-template variables or
repository layout.
See the [Stage metadata contract](../CONFIGURATION.md#kargo-stage-metadata).

The observer submits complete authenticated snapshots of participating Stages.
A partial list is never accepted as complete;
unselected Stages do not affect inventory. Repeated snapshot identity is
idempotent; successful snapshots archive missing or deselected Stages while
retaining policy and history. Inventory failure affects Stage freshness without
interrupting ordinary Run delivery.

Stage identity is `(integration, project, name)`, with project equal to the
Kubernetes namespace and Cluster supplied by the authenticated Controller.
Stored observations contain typed current and candidate Freight, native
requested-Freight origins, availability strategy, required soak time, requested
source Stages, status conditions, current and last Promotion references,
aggregate health, optional Slipramp enrichment, and successful-render
acknowledgement. Missing enrichment disables only the Slipramp capability that
needs it; the Stage remains visible in its Kargo Project.

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

## Orchestration state machines

```text
creating --callback--> preparing --> pending --> approved/rejected/expired
    |                       `---------------------------> error
    `--terminal creation or pre-callback result--------> error

approved --Controller observation--> Kargo Succeeded/Failed/Errored/Aborted
```

The durable Promotion aggregate exposes four related state machines. They share
one row so guarded updates remain atomic, but they are not one linear workflow:

| Machine | Authoritative fields | Meaning |
| --- | --- | --- |
| Public lifecycle | `status`, `decided_at`, `expires_at` | Creation, review preparation, pending review, decision, expiry, or orchestration error |
| Creation dispatch | `dispatch_attempt_count`, `next_dispatch_at`, `last_dispatch_at`, `dispatched_at` | Slipramp-originated Kargo object delivery and its bounded retry budget |
| Review preparation | preparation Repository, catalog revision, Run, retry, completion, and Artifact columns | Revision reservation, the single idempotent Run, validated review Artifacts, and coordination failure |
| Kargo execution | `kargo_promotion_uid`, reconciliation timestamps, and Kargo outcome columns | Kubernetes identity binding, repeated observation, and the first terminal Kargo phase |

Phases within the latter three machines are derived from these existing fields;
Slipramp does not persist parallel phase columns. Domain transition functions
define legal changes and cross-field invariants, while SQL applies the matching
row locks and guarded updates atomically.

An automatic-policy callback advances directly to `approved`. The public
`creating` state allows queued Controller delivery, bounded materialization
retries, and the wait for callback hydration. `preparing` owns one ordinary
revision-pinned Run. `pending` accepts one operator decision or durable expiry.
Terminal orchestration failures use `error` with a bounded non-sensitive
diagnostic.

The public lifecycle and Kargo execution are separate facts. An approved
Promotion without a terminal outcome remains `approved` while its Kargo
execution machine remains reconciling. A terminal report adds an immutable
outcome without rewriting the approval decision or review status. Failed,
errored, and aborted outcomes are therefore visible on an approved Promotion
rather than being collapsed into a review failure.

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

The Controller submits only the Stage and Freight identity. Kargo's enabled
admission and defaulting webhooks populate the Stage promotion template and
expand any `PromotionTask` references. Slipramp does not copy, interpret, or
partially reimplement that Kargo-owned behavior.

Creation uses durable bounded backoff. Transient Kubernetes and transport
failures do not block later work; invalid or forbidden admission and exhausted
retry budget move the Promotion to `error`. No separate Kargo client channel,
impersonation layer, broker, or Job is introduced.

The same Controller command scheduler performs fixed-interval reconciliation.
For a Slipramp-originated request it inspects the exact annotated object before
callback hydration and, after approval, continues until a terminal Kargo phase
is recorded. For a Kargo-originated request it resolves the exact snapshotted
namespace, name, and Stage, binds the first observed Kubernetes UID durably,
and requires that UID on later observations. Conflicting namespace, name,
Stage, annotation, or UID evidence is rejected rather than guessed.

Missing and nonterminal state remains scheduled. A terminal Kargo phase before
callback hydration still moves the unhydrated creation lifecycle to `error`,
while retaining the Kargo outcome separately. Callback hydration, approval,
identity binding, and terminal reports lock the same durable Promotion.
Identical observations are idempotent; a later conflicting phase or identity
cannot replace the first accepted fact. Only observation and reporting retry:
the Controller never repeats Kargo Promotion steps.

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
invent a human actor. Approval is permission for Kargo to proceed, not a claim
of downstream success. Kargo performs the approved target-branch update without
force and continues its own reconciliation; the separately observed terminal
outcome records what happened afterward.

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

The Project inventory is the canonical product topology. Its identity is the
exact `(integration, cluster, project)` boundary, and it retains every observed
participating Stage. Relationships are resolved only from Kargo's native
requested-Freight source references. Slipramp does not reconstruct pipelines,
infer environment adjacency, or merge Projects across integrations or Clusters.
Project list summaries derive Stage count, unhealthy and stale Stage counts,
pending reviews, and latest observation time from the same Stage observations.

Freight eligibility is conservative. Directly requested Warehouse Freight is
eligible. Upstream Freight requires observed verification and required soak-time
evidence unless Kargo records explicit approval for the target Stage. `OneOf`
requires one eligible source and `All` requires every requested source. Missing
evidence is `unknown`, never assumed ready. Control-flow Stages remain in the
topology, but Slipramp does not offer a Freight-based Promotion when no concrete
requested Freight is available.

Project and Promotion reads do not query Kargo or Argo CD. Branch drift uses
cached read-only Git evidence and remains a separate workflow. It compares
watched source paths and target movement independently, preserves the last
successful render acknowledgement, and reports stale, failed, or unknown
evidence rather than treating absence as agreement.
