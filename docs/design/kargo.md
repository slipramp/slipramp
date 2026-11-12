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
selects a Cluster and Stage freshness limit. A cohesive optional review
configuration adds a Repository allowlist, preparation Target and Action, and
review retention. Optional Kargo and Argo CD links and candidate-branch cleanup
policy add only their corresponding product features. Controller configuration
assigns Stage namespaces to integrations. Every Stage in an assigned namespace
belongs to that integration, and a namespace may be assigned to exactly one
integration in a Controller configuration.

An optional Controller observer reads every Stage page from each assigned
namespace. Stage labels do not affect enrollment or integration ownership.
Optional Stage annotations enrich the inventory with application, environment,
Repository URL, target branch, and source paths; the observer does not infer
those coordinates from promotion-template variables or repository layout.
See the [Stage metadata contract](../CONFIGURATION.md#kargo-stage-metadata).

The observer submits complete authenticated snapshots of every Stage in the
assigned namespaces. Listing, pagination, and structural normalization must all
succeed before a snapshot is complete. The Controller retries its current
pending snapshot identity until the Server accepts it, and retrying that current
latest snapshot is idempotent. Successful snapshots archive Stages that were
deleted, moved to another namespace owner, or belonged to a namespace removed
from the integration while retaining policy and history. Inventory failure
affects Stage freshness without archiving Stages or interrupting ordinary Run
delivery.

Slipramp retains the latest accepted complete Stage snapshot checkpoint for each
integration and Cluster, not a history of observations. The checkpoint preserves
the snapshot identity, observation time, and original acceptance time for
freshness, ordering, and retry idempotency. It is also the durable
integration-health signal. Each configured integration appears on its Cluster
as never observed, current, or stale using that integration's Stage freshness
limit. This signal does not claim to diagnose a Kubernetes-side listing failure
the Server never observed. Installations with no configured integration omit
Kargo-specific product navigation; an empty or stale configured integration
remains visible for recovery.

Stage identity is `(integration, project, name)`, with project equal to the
Kubernetes namespace and Cluster supplied by the authenticated Controller.
Stored observations contain typed current and candidate Freight, native
requested-Freight origins, availability strategy, required soak time, requested
source Stages, status conditions, current and last Promotion references,
aggregate health, optional Slipramp enrichment, and successful-render
acknowledgement. Missing enrichment disables only the Slipramp capability that
needs it; the Stage remains visible in its Kargo Project.

Each Stage exposes independent `promotion`, `review`, and `branchDrift`
capabilities. Each capability is `ready`, `not-configured`,
`not-applicable`, or `unavailable`. Missing optional enrichment is
`not-configured` and is a neutral native-only state. `unavailable` is
reserved for a configured capability whose required context no longer matches;
control-flow Stages use `not-applicable` for native promotion and review.
Native promotion is ready for a freshly observed non-control-flow Stage with
addressable native identity; exact eligible Freight is selected under Kargo's
observed availability, verification, and soak rules. Review additionally
requires the complete Stage enrichment, an active Repository whose URL matches
the observed URL, and the integration's review configuration. Branch drift
retains its own Git mapping and acknowledgement requirements. Review or
branch-drift limitations never disable native promotion.

Stage policy defaults to `automatic`. `review-required` may have an expiry and
may be configured only while review is ready. Promotion acceptance snapshots
the effective policy; later policy changes do not alter existing Promotions. A
configured `review-required` policy is preserved if enrichment later
disappears. New Promotions then fail with review context unavailable until an
operator restores the context or explicitly changes the policy to `automatic`;
Slipramp never silently changes the policy.

## Request sources and idempotency

Kargo may submit an external Promotion callback through a service-account API
key. The request supplies external identity, project, and Stage. Base and
candidate Git object IDs plus a candidate branch under `kargo/promotion/` are
an all-or-none optional audit context and are required when review preparation
is required. Slipramp resolves the remaining context from fresh durable Stage
inventory without inventing Repository or Git facts.

An operator may instead start a Promotion from exact observed Freight on an
upstream Stage or a directly requested Warehouse. The Server resolves trusted
integration, Cluster, namespace, target Stage identity, source topology, and
policy facts from one durable transaction. It snapshots Repository and review
facts only when complete review context exists. The browser never supplies
those coordinates as authority.

`Promotion` is the only public lifecycle for both sources. Kargo-originated
requests retain service-account attribution. Slipramp-originated requests are
durable before their Kubernetes object exists and retain the requesting human
Principal throughout callback correlation.

External identity plus a canonical request digest makes identical retries
idempotent and rejects conflicting reuse. Durable request state is committed
before asynchronous creation or preparation begins.

## Orchestration state machines

```text
automatic: approved + awaiting delivery --> delivered --> Kargo terminal outcome
                                  `-------> delivery failed

review-required: creating --callback--> preparing --> pending --> approved/rejected/expired
                       `-------------------------> error
```

The durable Promotion aggregate exposes four related state machines. They share
one row so guarded updates remain atomic, but they are not one linear workflow:

| Machine | Authoritative fields | Meaning |
| --- | --- | --- |
| Public lifecycle | `status`, `decided_at`, `expires_at` | Creation, review preparation, pending review, decision, expiry, or orchestration error |
| Creation dispatch | attempt, next/last attempt, delivered, failed, and diagnostic fields | Slipramp-originated Kargo object delivery and its bounded retry budget |
| Review preparation | preparation Repository, catalog revision, Run, retry, completion, and Artifact columns | Revision reservation, the single idempotent Run, validated review Artifacts, and coordination failure |
| Kargo execution | `kargo_promotion_uid`, reconciliation timestamps, and Kargo outcome columns | Kubernetes identity binding, repeated observation, and the first terminal Kargo phase |

Phases within the latter three machines are derived from these existing fields;
Slipramp does not persist parallel phase columns. Domain transition functions
define legal changes and cross-field invariants, while SQL applies the matching
row locks and guarded updates atomically.

An operator-created automatic Promotion is `approved` as soon as its exact
Freight and policy are durably snapshotted. Its independent creation dispatch
may still be awaiting delivery, delivered, or terminally failed. A callback is
not part of that path. Review-required operator creation uses `creating` while
queued Controller delivery and callback hydration proceed. `preparing` owns one
ordinary revision-pinned Run. `pending` accepts one operator decision or
durable expiry. Guarded orchestration failures use `error` with a bounded
non-sensitive diagnostic.

The public lifecycle and Kargo execution are separate facts. An approved
Promotion without a terminal outcome remains `approved` while its Kargo
execution machine remains reconciling. A terminal report adds an immutable
outcome without rewriting the approval decision or review status. Failed,
errored, and aborted outcomes are therefore visible on an approved Promotion
rather than being collapsed into a review failure.

Product views derive one overall operator-facing state from review lifecycle,
creation delivery, and Kargo outcome. The list uses that state for quick
scanning while retaining filters over the separate facts. Promotion detail uses
the same overall state and shows the three underlying facts explicitly. Terminal
creation-delivery failures and unsuccessful Kargo outcomes also enter the
operational Inbox even when the public review lifecycle remains approved.

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
retry budget terminally fail creation dispatch. Automatic approval remains an
approved review decision and the delivery failure is presented separately. No
separate Kargo client channel, impersonation layer, broker, or Job is
introduced.

The same Controller command scheduler performs fixed-interval reconciliation.
For a Slipramp-originated request it inspects the exact annotated object after
delivery and continues until a terminal Kargo phase is recorded, regardless of
whether an automatic callback occurs. For a Kargo-originated request it resolves
the exact snapshotted namespace, name, and Stage, binds the first observed
Kubernetes UID durably, and requires that UID on later observations. Conflicting
namespace, name, Stage, annotation, or UID evidence is rejected rather than
guessed.

Missing and nonterminal state remains scheduled. A terminal Kargo phase is
recorded independently from review state; automatic Promotions are never
reclassified as callback failures. Callback hydration, approval, identity
binding, and terminal reports lock the same durable Promotion. Identical
observations are idempotent; a later conflicting phase or identity cannot
replace the first accepted fact. Only observation and reporting retry: the
Controller never repeats Kargo Promotion steps.

Callbacks remain required for review-required preparation. They are optional
for automatic Promotions: an identical callback returns the automatic approval
result, preserves conflict detection, and records Git audit associations only
when a resolved Repository and a complete Git context are both present.
Metadata-free callbacks skip Repository lookup, preparation, commit
association, candidate-branch cleanup, and Git or Argo CD preview work.
Repository allowlists authorize those Repository operations; they do not enroll
Stages or gate native Kargo execution.

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
Stage in an assigned namespace. Relationships are resolved only from Kargo's
native requested-Freight source references. Slipramp does not reconstruct
pipelines, infer environment adjacency, or merge Projects across integrations or
Clusters.
Project list summaries derive Stage count, unhealthy and stale Stage counts,
pending reviews, Freight comparison counts, and latest observation time from
the same Stage observations. Stale Stages are not also counted as unhealthy.
Server-side Project filters select Projects with current non-ready Stages,
stale observations, pending reviews, or a requested Freight alignment state
before pagination. Unpaginated summary totals drive operational Inbox counts.
Top-level Stage and pipeline views page by this canonical Project boundary and
reuse the same Project detail projection. Pipelines remain disconnected graphs
within one Project; the aggregate view never joins Stages across Projects,
integrations, or Clusters.

Freight alignment is a read-time projection over active durable Stage
observations. Slipramp creates one comparison for each exact
`source Stage -> requested Freight origin -> target Stage` relationship in
Kargo's `requestedFreight.sourceStages`. Multiple source Stages remain
independent even when Kargo applies `OneOf` or `All`; a direct Warehouse request
without a source Stage creates no comparison. Source resolution never crosses
the Project's integration, Cluster, or Kargo Project boundary.

A comparison is `aligned` when both Stages report the same current Kargo Freight
identity `(origin, name)`. It is `different` when fresh observations report
different names or current Freight on exactly one side. It is `unknown` when
the source Stage cannot be resolved, either observation is stale, or neither
Stage reports usable current Freight for the requested origin. Freight and
Artifact details explain the observed identity without ordering image tags,
chart versions, or Git revisions. Project reads derive this projection without
querying Kargo or Argo CD and do not persist separate alignment state. A Freight
difference is an ordinary possible intermediate promotion state, independent
of candidate readiness, promotion eligibility, Stage health, and Git branch
drift.

Freight eligibility is conservative. Directly requested Warehouse Freight is
eligible. Upstream Freight requires observed verification and required soak-time
evidence unless Kargo records explicit approval for the target Stage. `OneOf`
requires one eligible source and `All` requires every requested source. Missing
evidence is `unknown`, never assumed ready. Every configured Stage remains in
the topology. Every freshly observed non-control-flow Stage can use native
automatic promotion when exact eligible Freight exists, including a Stage with
no Slipramp annotations. A stale Stage remains visible but non-promotable until
a current complete snapshot arrives. Control-flow Stages remain visible but
non-promotable.

Project and Promotion reads do not query Kargo or Argo CD. Branch drift uses
cached read-only Git evidence. Project and branch-drift responses report the
same Stage capabilities. Both views evaluate review and branch drift against
the current Repository allowlist and clone URL. A changed URL leaves drift
unknown until the Stage mapping matches again.

Visible Promotion lists, Project inventory, Project detail, branch-drift,
integration-health, and Inbox views revalidate on a slower operational cadence.
Promotion lists use a faster cadence while visible work is active. Project
view, attention and Freight-alignment filters, and selected comparison Stages
remain in the URL so refreshes and cross-page links preserve operator scope.

Branch drift compares watched source paths and target movement independently.
It preserves the last successful render acknowledgement and reports stale,
failed, or unknown evidence.
