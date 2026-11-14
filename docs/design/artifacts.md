# Logs and Artifacts

This document defines durable Run output, live log ingestion, declared Artifact
transfer, report projections, input provenance, and retention.

## Storage model

PostgreSQL is authoritative for Artifact identity, producing Run, declaration,
media type, sensitivity, lifecycle, size, digest, object key, retention, and
provenance. S3-compatible storage contains bytes only. The Server is the sole
object-store client; Controllers and the Harness transfer through authenticated
Server endpoints.

Assignments include a deterministic, narrowly scoped Run credential derived
from an installation signing key. It permits only the frozen input downloads and
declared output names for that Run. Rotating the signing key invalidates active
transfer credentials, so affected Runs must be canceled and started again.

## Live and retained logs

The Controller follows Pod output once and posts bounded chunks with monotonic
byte offsets. The Server writes object bytes before committing their digest and
offset metadata. Retrying identical bytes at the same offset is idempotent;
recovery starts from the durable cursor and verifies any replayed prefix.

Live viewing and retained logs share this ingestion path. Reconnectable SSE uses
the next durable byte offset as its event ID. Clients validate offsets, suppress
overlap after reconnect, and distinguish sealed completion from an explicitly
failed partial log. Pod output is the source during execution but never the
durable record.

Complete logs seal with final byte size and SHA-256 digest. If the source
disappears after a durable prefix, the log enters `failed` without a digest.
Failed prefixes remain streamable as incomplete evidence but are not offered as
complete downloads.

## Declared outputs and inputs

The Harness uploads only output names frozen into the Run snapshot. The Server
streams bytes to object storage and commits authoritative metadata only after
the write and validation succeed. Each upload attempt writes to its own object
key so rejected or concurrent requests cannot overwrite accepted contents. A
failed upload leaves no authoritative Artifact record; orphan reconciliation
removes abandoned objects after a grace period and a final PostgreSQL check.

An input explicitly selects one retained Artifact. Its declaration names a
producer Action and compatibility rule. The supported rule is
`exact-catalog-revision`: the consumer is pinned to the producer's Target,
commit, catalog revision, and relevance marker. Multiple inputs must agree on
that provenance. Artifact contents are never embedded in Run creation requests.

This contract supports preview/apply flows without making preview mandatory.
Direct Actions remain independently runnable when their catalog declarations
allow it.

## Structured reports

Declared media types may opt into registered Run report adapters. Report source
Artifacts are sensitive because tool output may contain secret values. An
adapter validates the source and produces a bounded, versioned, value-free
projection for product summaries and structured detail.

Current adapters cover Terraform plan JSON, OpenTofu plan JSON, pyinfra deploy
output, Ansible playbook event reports, and a generic step report for repository
scripts. Terraform and OpenTofu remain distinct tools and media types even when
they share a compatible projection model. Projections retain tool identity,
version, mode, aggregate counts, and normalized changes while excluding
provider values and other unnecessary source data. The Ansible projection
retains bounded play, task, host, and outcome structure, excludes arbitrary
module result values, and hard-redacts tasks whose results indicate `no_log`.

The generic step report is a final, descriptive hierarchy with at most one
level of child steps. It records bounded identifiers, display names, outcomes,
and elapsed durations without arbitrary result payloads. Array order preserves
the script's reported execution order; step relationships do not create a
Slipramp workflow, retry, dependency, or live progress protocol.

The Ansible runtime helper records the checkout's repository directory in its
version 2 source envelope. The adapter uses that directory to make task paths
repository-relative and omits paths outside it; it does not infer the checkout
location from container path names. The Server owns task redaction, so clients
render the validated projection directly. Host recap totals and host-task event
counts are separate: loops, rescued failures, and ignored failures can make
them differ. The projection schema remains version 1; source and projection
versions evolve independently.

The original source remains the immutable Artifact. PostgreSQL stores report
kind, projection version, bounded summary, and projection object metadata; the
object store holds source and projection bytes. Reading projection detail or
source content uses sensitive-Artifact authorization.

The Web presents Ansible recap totals and complete or partial status before
progressively disclosing play, task, and per-host outcomes in event order. It
supports bounded filtering and pagination, collapses successful unchanged
tasks by default for large reports, preserves redaction labels, and keeps batch
host results keyboard and screen-reader navigable. When projection detail or
source content has expired, retained summary metadata remains visible with an
explicit expiry notice; source download continues to require sensitive
Artifact authorization while retained.

Reports are descriptive output. They do not determine Run outcome, prove
infrastructure state, or create an approval gate.

## Retention and usage

One installation-wide retention period applies to sealed Artifacts and failed
partial logs. Sealed Artifact age starts at sealing; failed-log age starts when
the producing Run finishes. The Server first marks eligible metadata `expired`,
deletes every owned object idempotently, then marks metadata `deleted` and
clears object-location metadata, including log-chunk transfer records. Artifact
history, report summaries, and provenance remain after byte deletion.

Artifacts cannot expire while their producing or consuming Run is unfinished or
while a nonterminal Promotion protects them. S3 lifecycle rules do not expire
owned objects independently.

Storage usage is derived from authoritative metadata for retained and
not-yet-deleted content, including durable prefixes of open or failed logs. It
reports logical recorded bytes, not object-store capacity, physical allocation,
replication overhead, free space, or provider billing.
