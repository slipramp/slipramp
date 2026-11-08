# Runs and scheduling

This document defines immutable Run creation, scheduling, reconciliation,
rerun, Git-trigger, and scheduled-trigger contracts. [Controller execution](./execution.md) defines local
Job behavior; [Logs and Artifacts](./artifacts.md) defines durable output.

## Immutable creation

Manual, API, MCP, Git-trigger, scheduled-trigger, and Promotion preparation
requests use one Run creation service. A request selects a durable Target UUID and Action name. The
service resolves the active catalog, validates parameters and Artifact bindings,
and freezes:

- Repository and checkout commit;
- catalog revision and independent Target relevance marker;
- Target, Action, Runtime, and effective Placement contract;
- normalized parameters and selected input Artifacts;
- Run source and optional actor snapshot.

For unpinned requests, catalog resolution uses the immutable revision ID read
with the Target relevance marker, so concurrent refresh cannot mix the two.

Authenticated idempotency keys are scoped to the submitting Principal. Creation
and any required parent linkage commit atomically, including Git delivery and
Promotion preparation linkage. An ambiguous response can therefore be retried
without producing a detached execution.

Run source and actor are separate facts. Browser requests use `manual`, ordinary
API-key requests use `api`, MCP uses `mcp`, Git deliveries and scheduled Runs
have no human actor, and Promotion preparation records its Promotion source.
Clients cannot choose a source value directly.

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

Run history is read from PostgreSQL in descending creation order. Filters may
combine Repository, commit, input Artifact, Target, Action, source, trigger,
Cluster, lifecycle phase, review state, and pagination. Trigger-name filtering
requires a source that carries a named automatic trigger, currently `git` or
`schedule`. This allows `limit=1` to select the exact latest Run for one named
trigger without maintaining a separate latest-result projection.

The Run domain owns allowed observation transitions. Persistence locks the Run,
validates the new observation, and writes current facts plus phase history in one
transaction. Duplicate terminal reports are idempotent and cannot move a Run or
Target checkpoint backward.

## Operator cancellation

Operator cancellation is derived from lifecycle phase, terminal outcome and
reason, and `cancellation_requested_at`; it is not another persisted status or
execution phase. The derived states are:

- `not-requested`: no operator intent exists, including queued Runs canceled by
  Git supersession or review rejection;
- `requested`: intent is durable while the Run remains assigned, preparing, or
  running;
- `completed`: the requested cancellation produced a terminal canceled outcome
  with reason `CancelRequested`;
- `overtaken`: another terminal outcome won the race before cancellation
  completed.

| Current phase / cancellation state | Authoritative operation | Resulting phase / cancellation state | Terminal outcome |
| --- | --- | --- | --- |
| queued / not-requested | Operator requests cancellation under the Run row lock | finished / completed | canceled with `CancelRequested`; no Controller command |
| assigned, preparing, or running / not-requested | Operator requests cancellation under the Run row lock | same phase / requested | none; intent becomes eligible for Controller delivery |
| assigned, preparing, or running / requested | Controller reports canceled with `CancelRequested` | finished / completed | canceled with `CancelRequested` |
| assigned, preparing, or running / requested | Controller reports succeeded, failed, or timed-out | finished / overtaken | reported outcome and reason |
| assigned, preparing, or running / not-requested | Controller reports an ordinary terminal outcome | finished / not-requested | reported outcome and reason |
| queued / not-requested | Git latest-wins supersedes the Run | finished / not-requested | canceled with `SupersededByGitChange` |
| queued / not-requested | Review is rejected | finished / not-requested | canceled with `ReviewRejected` |
| any phase / requested, completed, or overtaken | Operator repeats cancellation | unchanged | unchanged |
| finished / not-requested | Operator requests cancellation | unchanged; request is rejected | unchanged |

The request and an ordinary terminal observation serialize on the same Run row.
If completion commits first, the new request is rejected. If intent commits
first, the terminal outcome is accepted and cancellation becomes overtaken.
Equal request and observation timestamps are valid; row-lock order, not clock
precision, determines which operation established intent first.

Only a Controller observation backed by current cancellation intent may report
the canceled outcome, and it must use `CancelRequested`. Git supersession and
review rejection finish queued Runs with their own reasons and never set
operator cancellation intent.

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
normally. Manual, API, MCP, schedule, and Promotion Runs are never superseded.

Dispatch pins creation to the delivery revision and uses the delivery UUID as
the idempotency key. Run insertion locks current Target state and refuses work
whose commit is no longer desired, closing the refresh/dispatch race.

Delivery state is derived from the existing `run_id`, `superseded_at`,
`failed_at`, `failure_count`, and `next_attempt_at` fields; no persisted status
duplicates those facts. The terminal markers are mutually exclusive. Pending
eligibility is time-dependent: `next_attempt_at` is due at an inclusive
boundary.

| Current state | Authoritative fields | Guarded operation | Result |
| --- | --- | --- | --- |
| Pending, eligible | No terminal marker; `next_attempt_at <= now` | Link the matching Run | Run linked |
| Pending, eligible | No terminal marker; selected `failure_count` still matches | Record a retryable creation failure | Pending in retry backoff |
| Pending, eligible | No terminal marker; selected `failure_count` still matches | Record a permanent failure or exhaust the attempt limit | Terminally failed |
| Pending, eligible or in backoff | No terminal marker | Supersede for a newer desired commit | Superseded |
| Pending in retry backoff | No terminal marker; `next_attempt_at > now` | Time reaches `next_attempt_at` | Pending, eligible |
| Run linked | Matching `run_id` | Repeat the same linkage | Run linked, unchanged |

Creation failures retain bounded diagnostics, attempt count, and next due time.
Permanent frozen-definition failures terminate immediately. Operational
failures use bounded exponential backoff, isolating a failing delivery so later
eligible work continues in ordinary creation-time FIFO order. A failure update
compares the selected failure count and due time, so duplicate or concurrent
dispatchers record at most one transition for the attempt. Exhaustion produces
terminal failure history. Retrying a delivery may create its immutable Run; it
never retries the Run's infrastructure command.

## Scheduled Action triggers

A named Action trigger may instead declare `schedule.cron` with optional static
parameters. Cron uses exactly five fields—minute, hour, day of month, month, and
day of week—and is always evaluated in UTC. Timezone configuration and
seconds-level schedules are not supported. A trigger selects exactly one of
`git` or `schedule`; `review: required` remains Git-only. Scheduled Actions may
have any intent but cannot require Artifact inputs. Static parameters use the
ordinary catalog parameter normalization and validation rules.

The Server keeps the next future occurrence for each active named schedule. On
startup and whenever a Target's active catalog revision changes, it calculates
only the next occurrence after the current time. It never replays missed
occurrences. Removing or changing a schedule changes only future evaluation and
does not mutate or cancel Runs already created.

At an occurrence, the scheduler asks the ordinary Run creation service to
resolve the currently active Target and catalog and freeze the normal immutable
snapshot. The Run source records the trigger name and scheduled UTC timestamp;
there is no human actor. A deterministic occurrence idempotency key and the Run
insert transaction prevent duplicate creation across repeated evaluation or a
Server restart near the occurrence.

If the previous Run from the same Target, Action, and named schedule is still
unfinished, including queued, the occurrence is skipped. Queued or active Runs
from other sources do not suppress creation; normal Target serialization may
still keep the new scheduled Run queued. A creation failure is logged with
bounded, non-sensitive context and is not retried. The next cron occurrence is
the next attempt. Scheduled triggers add no delivery API, distributed lease,
leader election, Kubernetes scheduling, notifications, or retention behavior.

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
