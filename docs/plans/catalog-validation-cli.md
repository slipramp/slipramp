# Catalog Validation CLI Plan

Slipramp should make repository catalog validation available before a commit reaches the tracked branch. A
repository author should be able to run the same catalog checks used by the Slipramp Server from a local checkout,
a pre-commit hook, or CI, receive precise diagnostics, and understand which installation-specific checks were not
performed.

The first deliverable is a general `slipramp` CLI with `slipramp catalog validate`. Later catalog commands should
reuse the same parser, diagnostics, and resolution model to explain effective Actions and preview semantic catalog
changes.

## Goals

- Validate `.slipramp/**/*.yaml` from a local repository without requiring a running Slipramp installation.
- Keep the Server and CLI on one authoritative parsing and validation implementation.
- Report all recoverable problems with exact source locations and stable diagnostic codes.
- Distinguish repository-portable validity from validity against a particular set of Placements.
- Support human-readable local output and structured CI integrations, including SARIF.
- Preserve the current architecture: the Server gains no Kubernetes access, the Harness remains internal, and
  validation never requires secret values.
- Establish foundations for `catalog explain` and `catalog diff` without including them in the first milestone.

## Non-goals

- Executing Actions locally or emulating the Harness.
- Pulling Runtime images or proving that tools exist inside them.
- Contacting Kubernetes or validating live namespace, ServiceAccount, Secret, or image-pull readiness.
- Adding mandatory review, workflow graphs, or tool-specific Terraform, Ansible, or pyinfra configuration.
- Accepting obsolete catalog shapes, compatibility aliases, or separate CLI-only validation behavior.
- Incremental validation of only changed YAML files. Catalog references cross files, so validation always evaluates
  the complete catalog.

## User experience

### Offline validation

From a repository root:

```console
$ slipramp catalog validate
.slipramp/targets.yaml:75:13: error [catalog.artifact.source-missing]
Target "rustfs-buckets", action "apply", input "plan" references artifact "plan", but action "preview" does not
declare that artifact.

1 error, 0 warnings
```

An explicit path validates another checkout:

```console
$ slipramp catalog validate ../infrastructure
```

The default command performs portable validation. If the catalog references Placements, the final summary makes the
omitted deployment checks visible:

```text
Catalog is portable-valid. Deployment checks were not run for placements: infrastructure-admin, production.
```

This result means that the repository-owned contract is internally valid. It does not promise that the named
Placements exist in a particular Slipramp installation.

### Deployment-aware validation

Operators with access to the non-secret Placement configuration can run the complete Server-side checks:

```console
$ slipramp catalog validate --placements ./placements.yaml
Catalog is valid against 2 placements: 3 runtimes, 12 targets, 28 actions.
```

The CLI must load the same `PlacementConfig` format used by the Server. Placement files contain references to
cluster-local credentials, not their values. This mode verifies Placement names, offered credential sources,
effective resources, and other static Placement constraints. It does not contact the selected Clusters.

### Output formats

The initial command supports:

```text
--format text    Human-readable diagnostics; the default.
--format json    Stable structured output for custom automation.
--format sarif   SARIF 2.1.0 results for code-scanning and pull-request annotations.
--strict         Treat warnings as a failed validation.
```

Diagnostics go to standard output for all formats so callers can redirect one complete result. Invocation and
unexpected internal failures go to standard error.

Exit codes are stable:

| Code | Meaning |
| --- | --- |
| `0` | Validation completed without errors, and without warnings when `--strict` is used. |
| `1` | Validation completed and found catalog errors or strict warnings. |
| `2` | The command could not run because its arguments, root, Placement file, or output configuration were invalid. |
| `3` | An unexpected internal failure prevented a trustworthy validation result. |

## Validation contract

### Portable validation

Portable validation covers facts owned by repository content:

- Catalog discovery, YAML syntax, duplicate mapping keys, unknown fields, API versions, and resource kinds.
- Runtime and Target names, duplicate identities, repository paths, container paths, watch patterns, durations,
  resource quantities, file modes, media types, and reserved environment names.
- Runtime references and effective Action selection of a Runtime.
- Action commands, intents, parameters, static Git-trigger values, and trigger constraints.
- Credential requirements, injection collisions, and whether effective Actions bind every Runtime requirement.
- Artifact declarations, input provenance, compatibility, and mount conflicts.
- Target and Action default resolution that does not require Placement contents.

Portable validation checks that a Placement reference is well formed and present where required. It does not require
the referenced Placement to be known.

### Deployment validation

Deployment validation accepts a validated catalog and a validated `placement.Set`, then additionally checks:

- Every effective Placement exists.
- Every bound credential source is offered by the selected Placement.
- Effective Action resources are valid after merging Target, Action, and Placement values.
- Effective requests do not exceed effective limits.
- Any future static constraint whose authority belongs to Placement configuration.

The Server always runs portable and deployment validation before accepting a catalog revision. The CLI runs
deployment validation only when it has Placement configuration. There must not be a mode in which an empty
`placement.Set` implicitly means that unknown Placements are accepted; the selected validation level is explicit.

### Warnings

Errors correspond to conditions that would prevent catalog acceptance at the selected validation level. Warnings
identify suspicious repository content without changing Server acceptance.

Initial repository-aware warnings should be limited to checks with low false-positive rates:

- A Target path does not exist in the checkout.
- A watch pattern currently matches no repository files.
- A relative command path does not exist beneath the Target path.
- A repository command file is not covered by the Target's watch patterns.
- A watch pattern is duplicated or completely subsumed by another pattern.

PATH-based commands, generated files, and intentionally future-facing watch patterns are valid. The diagnostic must
explain those possibilities, and only `--strict` promotes warnings to a failing CLI result. Warning policy is CLI and
CI behavior; it does not make the Server reject a catalog.

## Structured diagnostics

Replace validation's single wrapped `error` result with a structured result. The precise Go shape can evolve during
implementation, but it must carry these concepts:

```go
type Diagnostic struct {
	Code      string
	Severity  Severity
	Message   string
	Hint      string
	Location  SourceRange
	Related   []RelatedLocation
}

type SourceRange struct {
	Path      string
	Document  int
	Line      int
	Column    int
	EndLine   int
	EndColumn int
	FieldPath string
}
```

Diagnostic codes are stable machine interfaces. Messages and hints may improve without forcing CI consumers to
parse prose. Codes use domain-shaped names such as:

- `catalog.yaml.unknown-field`
- `catalog.runtime.not-found`
- `catalog.placement.not-found`
- `catalog.parameter.required`
- `catalog.credential.source-not-offered`
- `catalog.artifact.source-missing`
- `catalog.watch.no-matches`

Related locations point to definitions involved in a conflict, such as both duplicate resources or the producer
Action involved in an invalid input. Diagnostics are sorted by path, document, source position, severity, and code to
keep local and CI output reproducible.

Parsing should continue across independent files and documents when recovery is safe. A malformed YAML document may
prevent further decoding of its file, but it must not suppress diagnostics from other files. Semantic validation
should accumulate independent failures instead of returning after the first one.

The parser must retain a field-path-to-YAML-node index so semantic diagnostics can use the actual source location.
Resource-level fallback locations remain available when no narrower node exists. The existing `Source` path and
document number remain part of persisted catalog definitions; line and column information is diagnostic metadata,
not immutable product state.

## Internal package changes

Refactor catalog processing into explicit stages:

1. Discover and decode repository documents while retaining source-node metadata.
2. Validate repository-portable invariants.
3. Resolve Target and Action defaults that are independent of Placements.
4. Optionally validate against a `placement.Set`.
5. Return the catalog plus sorted diagnostics.

Suggested package ownership:

- `internal/catalog`: discovery, decoding, resolution, portable validation, deployment validation, and diagnostics.
- `internal/placement`: reusable decoding and validation of the versioned Placement file.
- `internal/cli`: command dispatch and output rendering, grouped into focused catalog command files as the CLI grows.
- `cmd/slipramp`: the thin executable entry point.

Move Placement-file decoding out of Server environment loading so both the Server and CLI can call the same function.
Environment lookup and selection of `SLIPRAMP_PLACEMENTS_FILE` remain Server configuration responsibilities.

Use the standard library `flag` package for the initial command unless command growth demonstrates a concrete need
for a CLI framework. The initial hierarchy is small and does not justify another application dependency.

The existing `catalog.Load` API may be replaced cleanly; the project does not require compatibility shims. All
Server callers, tests, and examples should move to the new staged result together.

## CLI implementation

Add `cmd/slipramp` as a general operator and repository-author binary. Its initial command hierarchy is:

```text
slipramp catalog validate [flags] [repository-root]
slipramp version
```

The executable should:

- Default the repository root to the current directory.
- Reject multiple positional roots.
- Resolve output paths relative to the invocation directory, while reporting catalog paths relative to the selected
  repository root.
- Avoid network access.
- Avoid logging frameworks and timestamps in validation output so results remain deterministic.
- Never print environment values, Secret contents, or other credential material.
- Include the Slipramp version and supported catalog API versions in `slipramp version`.

Text rendering uses concise compiler-style headers followed by wrapped messages and optional hints. JSON and SARIF
renderers consume the same diagnostics and summary rather than performing their own validation.

The JSON format receives an explicit schema version from its first release. SARIF uses `slipramp/catalog` as the tool
driver, diagnostic codes as rule IDs, repository-relative artifact locations, one-based regions, and error or warning
levels. Do not include absolute local paths in JSON or SARIF output.

## Editor schema

Publish a versioned JSON Schema for `slipramp.com/v1alpha1` catalog documents and include it in CLI release artifacts.
The schema provides YAML completion and immediate structural feedback for individual documents. It is not the
authoritative validator because JSON Schema cannot fully express cross-document references, default resolution,
credential bindings, artifact provenance, or Placement-aware constraints.

Schema generation should come from a maintained source that is checked for freshness in CI. Do not independently
hand-maintain field definitions in Go, OpenAPI, and JSON Schema. The implementation design must choose one generation
source and add a stale-output check before schema publication.

Document editor configuration for common YAML language servers. An optional root-level schema directive may be
considered later, but catalog files must not require editor-specific comments.

## Server integration

Repository refresh consumes the same staged catalog result as the CLI. A revision is accepted only when both portable
and deployment validation contain no errors. Warnings do not prevent acceptance.

Initially, the Server may continue persisting and exposing a bounded human-readable refresh failure summary. The
diagnostic model should nevertheless be designed so a later API can expose a bounded structured diagnostic list with
source ranges and codes. Do not make the CLI depend on that future API.

Server logging should emit a compact validation summary and stable codes rather than one log event per diagnostic.
Diagnostic messages must remain safe for product display and must not contain source file contents or parameter
values.

## CI and distribution

The CLI must be straightforward to pin to the same release as a Slipramp installation:

- Build and test `cmd/slipramp` with the existing repository checks.
- Publish versioned binaries for supported release platforms.
- Publish a small OCI image containing the CLI for container-based CI.
- Publish the catalog JSON Schema beside the CLI artifacts.
- Document checksum verification and version pinning.

Start with provider-neutral examples:

```console
slipramp catalog validate --format text
slipramp catalog validate --format sarif > slipramp.sarif
```

A dedicated GitHub Action or GitLab component is deferred until repetition justifies it. SARIF and the OCI image are
the initial interoperability surfaces.

## Testing and acceptance

### Unit coverage

- Every validation rule produces the expected code, severity, field path, and source position.
- Independent errors aggregate and sort deterministically.
- Related locations identify both sides of duplicate and reference errors.
- Portable validation accepts unknown but well-formed Placement names.
- Deployment validation rejects missing Placements and unavailable credential sources.
- Warning promotion under `--strict` changes the exit result without changing diagnostic severity.
- Text, JSON, and SARIF renderers produce deterministic golden output.
- JSON and SARIF never contain absolute checkout paths.
- Unsupported flags, formats, and unreadable roots return the documented invocation exit code.

### Integration coverage

- The infrastructure-ops example passes offline validation.
- The same example passes deployment validation with its example Placement file.
- Representative invalid catalogs produce multiple diagnostics across several files.
- The Server and CLI accept and reject the same catalog when given the same Placements.
- A catalog that is portable-valid but invalid for the supplied Placements is reported distinctly.
- The published schema accepts valid example documents and catches representative structural errors.

### Completion criteria

The first milestone is complete when:

- A repository author can run one offline command from the repository root and receive precise, aggregated feedback.
- CI can consume stable JSON or SARIF without parsing text.
- Passing deployment-aware CLI validation provides the same acceptance result as Server refresh for the same checkout
  and Placement configuration.
- Server refresh and the CLI demonstrably share parsing and validation code.
- Documentation clearly distinguishes portable, deployment, and future live-cluster preflight checks.

## Follow-on commands

These commands should reuse the diagnostic and resolution foundation but remain separate milestones.

### `slipramp catalog explain`

Resolve one Target Action and display its final command, working directory, Runtime image, Placement, timeout,
resources, parameter delivery, credential environment names and mount paths, artifacts, inputs, watch patterns, and
triggers. It never displays credential values. Optional sample parameter values show final argument ordering and
environment delivery.

### `slipramp catalog affected`

Given changed paths or two Git revisions, report affected Targets and the Git triggers that would be eligible for
delivery. This makes root-relative watch behavior and latest-wins automation visible before merge.

### `slipramp catalog diff`

Compare two complete catalogs semantically rather than as YAML text. Highlight added, archived, or renamed Targets;
Action contract changes; Runtime, Placement, credential, resource, and timeout changes; trigger additions; watch-scope
changes; parameter changes; and artifact-flow changes. Output should support text and JSON before provider-specific
pull-request integrations.

### Connected Placement capabilities

A later authenticated Server endpoint may expose non-sensitive Placement capabilities needed for deployment
validation: names, Clusters, credential source names, resource defaults, and static limits. The CLI would fetch that
description and validate the local checkout without uploading repository contents. Live Kubernetes readiness remains
a Controller-reported operational capability, not catalog validation and not direct Server access.

## Implementation sequence

1. Introduce structured diagnostic types, source indexing, aggregation, and deterministic sorting.
2. Split portable validation from Placement-dependent deployment validation.
3. Extract reusable Placement-file decoding from Server configuration.
4. Migrate repository refresh and existing catalog tests to the staged validation result.
5. Add `cmd/slipramp` and text output for `catalog validate`.
6. Add repository-aware warnings and `--strict`.
7. Add versioned JSON output and its contract tests.
8. Add SARIF output and golden tests.
9. Generate and publish the catalog JSON Schema with a stale-output check.
10. Add release binaries, the CLI OCI image, and local, pre-commit, and CI documentation.
11. Reassess `catalog explain`, `catalog affected`, and `catalog diff` as separate roadmap work.

Each step should leave Server refresh behavior correct and tests passing. The diagnostic refactor and validation split
may land together if preserving an intermediate single-error API would create temporary compatibility machinery.

## Open decisions

- Whether text diagnostics should include source excerpts and carets by default or only when attached to a terminal.
- Whether JSON diagnostics should be streamed or emitted as one document. One deterministic document is preferred for
  the initial bounded catalog size.
- Whether the first release should support reading catalog YAML from standard input. Repository-wide discovery makes
  this low value, so the default is to omit it.
- Which maintained source generates the catalog JSON Schema without duplicating the Go and OpenAPI contracts.
- Which operating systems and architectures receive release binaries initially.
- Whether warnings belong in Server refresh views after CLI adoption demonstrates that their signal-to-noise ratio is
  acceptable.
