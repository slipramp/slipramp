# Repositories and catalogs

This document defines the current Repository, catalog, Target, and Action
resolution contracts. [DESIGN.md](../DESIGN.md) owns the system-wide
architecture; [CATALOG_VALIDATION.md](../CATALOG_VALIDATION.md) covers developer
setup and validation workflows.

## Repository lifecycle

A Repository registration has a stable identity, one clone URL, and one tracked
branch. HTTPS, SSH, and SCP-style Git URLs are accepted; credentials embedded in
HTTPS URLs are rejected. Optional logical credential names select separate
Server refresh and Controller checkout credentials.

Administrators may change credential references, commit-link configuration, and
commit-indexing mode. Clone URL and branch are part of registration identity and
do not change in place. Removing a Repository hides the registration while
preserving its durable history.

Connection testing is a bounded, non-persisting probe of a proposed clone URL,
branch, and Server refresh credential. It resolves the remote branch head using
the normal isolated Git environment but does not register, refresh, validate, or
activate anything. The Server cannot test a checkout credential because that
logical name resolves independently in each execution Cluster.

## Refresh and activation

Startup, periodic, and administrator-requested refreshes use one serialized
per-Repository path:

1. Fetch the tracked branch with the configured Server credential.
2. Check out the exact head in temporary storage.
3. Discover, decode, and validate the complete catalog.
4. Create or reuse the immutable catalog revision for that commit.
5. Atomically activate the revision, update Targets, and enqueue matching Git
   trigger deliveries.
6. Remove the temporary checkout.

Repository contents, archives, and source bundles are never persisted in
PostgreSQL or object storage. Execution Jobs clone their exact commit directly.

Each refresh records an immutable attempt with source, timing, fetched head when
known, outcome, and a bounded non-sensitive diagnostic on failure. Fetch failure
and catalog rejection remain distinct. Revalidating an already accepted head
records another attempt but does not change Target relevance or create trigger
deliveries. Invalid heads never replace the last valid active revision.

## Catalog format and validation

Catalog resources use `slipramp.com/v1alpha1`. YAML files below `.slipramp` are
discovered recursively in lexical path order and may contain multiple Runtime or
Target documents. Strict decoding rejects unknown fields and duplicate mapping
keys.

One staged catalog implementation is shared by Server refresh,
`slipramp catalog validate`, and `slipramp catalog explain`:

- Discovery and decoding retain source positions and aggregate independent
  diagnostics.
- Portable validation checks repository-owned contracts while accepting
  unknown, well-formed Placement names.
- Deployment validation resolves the configured Placement set and verifies
  existence, credential offerings, and resource constraints.

Server refresh always performs deployment validation. An empty Placement set
does not implicitly select portable validation. Validation is static: it does
not contact Kubernetes, inspect registries, or resolve Secret values.

Diagnostic codes are the stable machine contract. Messages and hints may
improve without requiring callers to parse them. Diagnostics sort by source,
position, severity, and code for reproducible text, JSON, and SARIF output.
Warnings do not prevent Server activation; CLI strict mode may treat them as a
failing local result.

A generated Draft 2020-12 JSON Schema supplies per-document editor feedback.
Cross-document references, resolved defaults, repository-aware checks, and
deployment rules remain in the shared Go validator.

## Target identity and relevance

Repository plus `metadata.name` is a Target's stable catalog key. The Server
assigns a UUID that remains stable while the key is active or later reactivated.
Renaming creates a new Target and archives the old one. Removal archives the
Target with its last accepted definition and never destroys infrastructure.

Accepting a changed head compares paths between accepted revisions against each
Target's `watch` patterns. Affected, new, and reactivated Targets advance their
desired relevance commit. An unaffected Target may therefore retain a desired
commit older than the active catalog revision. Watch patterns update relevance;
they do not execute an Action by themselves.

Catalog revision reads contain immutable definitions and revision metadata. The
active catalog view joins that revision with mutable Target state from one
repeatable-read PostgreSQL snapshot so a refresh cannot produce a mixed view.

## Action resolution and parameters

Target defaults supply Runtime, Placement, timeout, credentials, and resources.
Action scalar overrides replace defaults; credential and resource maps merge by
key. Resolution copies definitions rather than mutating the catalog, and Runtime
and Target definitions always come from the same revision.

Parameters are `string`, `boolean`, finite `number`, or `enum`, with required and enum
constraints validated before Run creation. Each parameter maps to exactly one
environment variable or argument. Arguments append in lexical parameter-name
order. Unset optional parameters emit nothing. Commands are explicit argument
arrays; shell behavior must be requested explicitly.

One explanation model records effective values and provenance from Action,
Target, Runtime, Placement, supplied parameters, and built-in defaults. Offline
explanation may leave Placement-dependent values unresolved; Server explanation
uses current validated Placement configuration. Explanations do not render
Jobs, inspect Secrets or images, select Artifact contents, or execute Actions.
Run creation remains the authoritative validation boundary.

## Commit metadata

Commit metadata is presentation data, separate from exact execution SHAs.
Repositories may disable indexing, retain observed heads, or index tracked-branch
history. History indexing is asynchronous and does not block catalog refresh or
turn historical commits into catalog revisions or trigger deliveries.

Commit detail is scoped by Repository and full SHA. It may show Runs pinned to
the commit and Promotions that use it as a base or candidate revision. Equal Git
object IDs in different Repositories never merge product history.
