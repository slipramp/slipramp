# Runs and scheduling

This document defines immutable Run creation, scheduling, reconciliation, rerun,
and Git-trigger contracts. [Controller execution](./execution.md) defines local
Job behavior; [Logs and Artifacts](./artifacts.md) defines durable output.

## Immutable creation

Manual, API, MCP, Git-trigger, and Promotion preparation requests use one Run
creation service. A request selects a durable Target UUID and Action name. The
service resolves the active catalog, validates parameters and Artifact bindings,
and freezes:

- Repository and checkout commit;
- catalog revision and independent Target relevance marker;
- Target, Action, Runtime, and effective Placement contract;
- normalized parameters and selected input Artifacts;
- Run source and optional actor snapshot.

Authenticated idempotency keys are scoped to the submitting Principal. Creation
and any required parent linkage commit atomically, including Git delivery and
Promotion preparation linkage. An ambiguous response can therefore be retried
without producing a detached execution.

Run source and actor are separate facts. Browser requests use `manual`, ordinary
API-key requests use `api`, MCP uses `mcp`, Git deliveries have no human actor,
and Promotion preparation records its Promotion source. Clients cannot choose a
source value directly.

## Scheduling and lifecycle

PostgreSQL is the scheduling authority. A Controller claims the oldest eligible
queued Run for its Cluster while enforcing:

- one active Run per Target by default;
- the installation's Cluster concurrency limit;
- any tighter Placement concurrency limit;
- ordinary FIFO order among Runs eligible under those constraints.

Capacity is current deployment policy, not part of the immutable Run snapshot.
Infrastructure commands are never retried implicitly.

Queue explanation is derived at one read time and is not persisted as mutable
state. Pending review takes precedence, followed by an active Run for the same
Target, Controller reachability, Cluster or Placement capacity, and ordinary
eligibility. Queue age and FIFO position are facts at that read time, not
execution estimates.

Lifecycle phase, terminal outcome, review state, cancellation intent, Job
identity, observed container image identities, exit code, diagnostics, and
observation freshness are separate facts. A disconnected Controller or stale
observation does not rewrite phase or fail an active Run.

The Run domain owns allowed observation transitions. Persistence locks the Run,
validates the new observation, and writes current facts plus phase history in one
transaction. Duplicate terminal reports are idempotent and cannot move a Run or
Target checkpoint backward.

## Reconciliation markers

`sync` is an optional recognized Action name. Each new Run freezes the Target's
desired relevance commit independently from its checkout commit. An input-bound
Run inherits the producing Run's marker, and multiple inputs must agree.

Assignment of a `sync` Run advances the Target's last-attempted checkpoint.
Successful completion advances its last-successful checkpoint. Failed,
canceled, and timed-out attempts remain factual attempts without replacing an
earlier success.

An active Target with `sync` is:

- `matched` when its last-successful marker equals the desired commit;
- `different` when both markers are known and differ;
- `unknown` when no trustworthy successful marker exists.

Archived Targets always report `unknown`. This comparison describes recorded
Runs only; it is not a claim of continuous infrastructure convergence.

## Reruns

Finished Runs support two explicit modes:

- An exact rerun copies the frozen execution snapshot, checkout commit, catalog
  revision, relevance marker, parameters, Placement, and retained inputs.
- A latest rerun resolves the same Target and Action against the active catalog,
  reapplies parameter values, and captures current Placement configuration and
  Target relevance.

Latest reruns reject Artifact inputs because those inputs pin the consumer to
the producing revision. Both modes create a new Run linked to its immediate
parent and derive source and actor from the new request.

## Git-trigger delivery

A named Action trigger uses `git: {}`, optional static parameters, and optional
`review: required`. The containing Target supplies required `watch` patterns.
Triggered Actions cannot require Artifact inputs. Catalog validation normalizes
static parameters with the ordinary Run rules.

After initial indexing, an accepted changed head atomically activates the
catalog, updates Targets, and creates durable deliveries for matching triggers.
Delivery is per accepted head rather than per commit in a multi-commit fetch.
Each delivery freezes revision, parameters, and review requirement.

Automatic Git work uses Target-scoped latest-wins coalescing. When desired
relevance advances or a Target is archived, older undispatched deliveries are
superseded and older queued Git Runs finish as canceled. Active Git Runs finish
normally. Manual, API, MCP, and Promotion Runs are never superseded.

Dispatch pins creation to the delivery revision and uses the delivery UUID as
the idempotency key. Run insertion locks current Target state and refuses work
whose commit is no longer desired, closing the refresh/dispatch race.

Creation failures retain bounded diagnostics, attempt count, and next due time.
Bounded exponential backoff isolates a failing delivery so later eligible work
continues. Exhaustion produces terminal failure history. Retrying a delivery may
create its immutable Run; it never retries the Run's infrastructure command.

## Optional Run review

A review-required Git Run remains queued and ineligible while its review state
is `pending`. Review state is orthogonal to execution lifecycle:
`not-required`, `pending`, `approved`, or `rejected`.

Approval records one immutable Principal-attributed decision and makes the
existing Run eligible for normal scheduling. It does not assign the Run.
Rejection records the decision and finishes the undispatched Run as canceled.
An optional normalized comment is part of the immutable decision.

Decisions are accepted only for queued pending Runs. Repeating the same decision
by the same Principal is idempotent; conflicting content or a decision after
another lifecycle transition is rejected. Supersession applies equally to
pending and approved-but-still-queued Git Runs. Exact and latest reruns are
ordinary new requests and do not inherit review requirements.

Repository-controlled review is a voluntary operational guard. A repository
maintainer can remove it, so it is not a Server-owned security policy,
separation-of-duties boundary, or source-wide enforcement mechanism.
