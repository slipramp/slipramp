# Slipramp CLI

The `slipramp` binary provides local catalog tooling and a focused remote
operator workflow for an existing Slipramp Server. Versioned CLI artifacts
are not published yet, so local examples run from a source checkout.

## Catalog CLI

The `slipramp` CLI validates the complete `.slipramp` catalog in a local
checkout with the same decoder and validation stages used by Server repository
refresh. Portable validation does not require a running installation and makes
omitted Placement checks explicit:

```console
go run ./cmd/slipramp catalog validate
go run ./cmd/slipramp catalog validate ../infrastructure
```

Supply the Server's non-secret `PlacementConfig` file to include static
deployment checks such as Placement existence, offered credential sources, and
effective resource requests and limits. This does not contact Kubernetes:

```console
go run ./cmd/slipramp catalog validate --placements ./placements.yaml
```

Text is the default output. JSON has a versioned top-level schema for custom
automation, and SARIF 2.1.0 uses repository-relative locations for code-scanning
systems. Warnings describe checkout-aware issues such as unmatched watch
patterns and missing relative command files; `--strict` makes them fail the
command without changing their severity.

```console
go run ./cmd/slipramp catalog validate --format json
go run ./cmd/slipramp catalog validate --format sarif > slipramp.sarif
go run ./cmd/slipramp catalog validate --strict
go run ./cmd/slipramp version
```

Exit code `0` means validation passed, `1` means catalog errors or strict
warnings were found, `2` means the invocation, root, or Placement file was
invalid, and `3` reports an unexpected internal/output failure. Diagnostics are
always written to standard output; invocation and internal failures use
standard error. See [Catalog validation](CATALOG_VALIDATION.md) for
source-pinned installation, pre-commit and CI integration, validation scopes,
and YAML editor configuration. Versioned CLI artifacts are not published yet.

Inspect the effective execution contract for one Target Action in a working
tree before committing or registering it with a Server:

```console
go run ./cmd/slipramp catalog explain rustfs-buckets preview --root examples/infrastructure-ops
go run ./cmd/slipramp catalog explain rustfs-buckets apply --root examples/infrastructure-ops --format json
go run ./cmd/slipramp catalog explain kubernetes-upgrade upgrade-node --root examples/infrastructure-ops --set host=compute-01 --set reboot=false
```

`slipramp catalog explain TARGET ACTION [flags]` accepts `--root PATH` (current
directory by default), optional `--placements PATH`, repeatable `--set
NAME=VALUE`, and `--format text|json` (text by default). Relative file paths
resolve from the current directory. Both formats use one structured model of
effective values with field-level provenance and repository source file/document
locations. It includes commands, final argv, directories, Runtime environment,
Placement defaults, resources, credential references and injection mappings,
Action/Parameter/Artifact presentation metadata, artifact producers/consumers
and compatibility, watch patterns, and triggers.

The whole catalog must validate before an Action can be explained. Without
`--placements`, repository-owned contracts are validated and Placement-dependent
values and remaining resource defaults are explicitly unresolved. With the
Server's `PlacementConfig` file, the command performs full static deployment
validation and resolves those defaults and credential sources. Secret contents
and arbitrary Placement pod configuration are never printed. The command does
not contact the Server or Kubernetes, inspect images, or execute the Action.

Required parameters must be supplied; optional parameters stay omitted. Names
must be unique across `--set` flags. String and enum values are literal, booleans
use Go's `strconv.ParseBool` syntax (normally `true` or `false`), and numbers must
be finite. The same normalization and validation used for Run creation applies.
Controller execution shares argument/environment delivery: arguments append in
lexical parameter-name order, named booleans emit a flag only when true, and
positional/environment booleans emit `true` or `false`.

Explanation uses the same exit codes as validation. Unknown Targets/Actions and
invalid parameter values return `1` with an error on standard error; invalid
catalogs return `1` with the usual validation diagnostics on standard output.
Successful JSON explanations include `schemaVersion: 1`, `validationLevel`, and
effective fields carrying `value`, `resolved`, and `provenance`; unresolved
fields also carry `unresolvedReason`. Checkout warnings remain in the explanation
and do not prevent success.

## Remote operator CLI

The same `slipramp` binary supports a focused remote workflow against an
existing Slipramp Server. Pass the absolute Server URL with `--server`, or set
`SLIPRAMP_URL`. The CLI reads its credential only from `SLIPRAMP_API_KEY` and
sends it as a Bearer token. It never writes the key to disk or includes it in
output or errors. HTTPS uses the system trust store; there is no insecure TLS
mode. Plain HTTP should be used only when a trusted encrypted network already
protects the connection.

An API key needs `slipramp:read` for discovery, Run and Artifact reads, logs,
and non-sensitive Artifact downloads; `slipramp:runs:write` to create, rerun,
or cancel Runs; and `slipramp:runs:review` to approve or reject Run reviews.
Sensitive Artifact contents additionally require an operator or administrator
Principal. For example:

```console
export SLIPRAMP_URL=https://slipramp.example.com
export SLIPRAMP_API_KEY='replace-with-an-api-key'

slipramp target list --query storage --limit 25
slipramp target list --repository-id 018f90ff-9fd8-7b5f-b7e1-f32ff54c1201 --format json
slipramp target list --all --format json
slipramp target get 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202
slipramp target explain 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 preview
slipramp run list --target-id 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 --phase running
slipramp run list --review-state pending --all --format json
slipramp run create --target 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 --action preview --set region=us-west-2
slipramp run create --target 018f90ff-9fd8-7b5f-b7e1-f32ff54c1202 --action apply \
  --input plan=018f90ff-9fd8-7b5f-b7e1-f32ff54c1206
slipramp run get 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp run logs 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp run wait 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203 --timeout 30m
slipramp run rerun 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203 --mode exact
slipramp run review approve 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203 --comment 'Plan reviewed'
slipramp run cancel 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp artifact list 018f90ff-9fd8-7b5f-b7e1-f32ff54c1203
slipramp artifact download 018f90ff-9fd8-7b5f-b7e1-f32ff54c1206 --output plan.tfplan
```

`target list` also accepts exact `--runtime` and `--placement` filters plus
`--offset`. `target get` returns a durable active or archived Target and its most
recently accepted definition. `target explain` returns the effective Action
contract resolved against the Server's current Placement configuration;
required parameters may remain omitted for inspection, and parameter simulation
stays with the offline `catalog explain` command. `run list` supports the
Server's exact Repository, Target, commit, input Artifact, Cluster, phase, and
review-state filters plus offset pagination. Both list commands accept `--all`
to fetch every matching record at or after `--offset`; `--limit` remains the
request page size. Aggregate JSON retains the normal list envelope with all
fetched records in `items`, the page size in `limit`, the initial offset, and the
Server's matching total. `run create` requires an exact Target UUID and Action
name. Its repeatable
`--set NAME=VALUE` flags use the same typed parsing, required-field, enum, and
normalization rules as catalog explanation and Server Run creation. Repeatable
`--input NAME=ARTIFACT_UUID` flags select exact retained Artifact inputs; the
Server verifies compatibility and pins the Run to their producing commit and
catalog revision. Because that historical revision's parameter declarations are
not exposed to the CLI, `--input` cannot be combined with `--set`. This supports
the same directly runnable, parameter-free preview/apply transitions exposed by
the producing Run in the Web UI. Every create and rerun sends a fresh random
idempotency key; automation may supply `--idempotency-key` to recover the same
request safely. Reruns require an explicit `--mode exact` or `--mode latest`.

`run review approve` and `run review reject` accept an optional `--comment` and
return the canonical updated Run. `artifact list RUN_ID` returns the log and
declared outputs for one Run. `artifact download` requires `--output PATH`, or
`--output -` for standard output. File downloads never replace an existing path,
use mode `0600`, verify the Server's SHA-256 digest, and remove partial or invalid
outputs.

Text is the default for `target list`, `target get`, `target explain`, `run
list`, `run create`, `run get`, `run wait`, `run rerun`, both Run review
decisions, `run cancel`, and `artifact list`; each accepts `--format json` to
emit the generated API response shape. Structured and text results go to
standard output. Errors and operational diagnostics go to standard error. `run
wait` polls until the Run finishes, prints
its canonical terminal state, and exits distinctly when the outcome is not
`succeeded`. It waits without a deadline unless `--timeout` is set;
`--poll-interval` defaults to one second. `run logs` is intentionally
unformatted: it writes decoded log bytes exactly to standard output, follows
live output, and reconnects from the last durable byte offset without repeating
content. It exits successfully only after the log is sealed, independently of
the Run outcome. A failed/incomplete log leaves its retained prefix on standard
output and exits distinctly.

Remote commands use exit code `0` for success, `1` for local Run validation
failures, `2` for an invalid invocation or missing configuration, `3` for an
unexpected local/output failure, `4` for a network or Server/API failure, and
`5` for an explicitly incomplete log. `run wait` returns `6` after writing the
terminal Run when its outcome is `failed`, `canceled`, or `timed-out`.
Authentication, authorization, validation, conflict, and not-found problem
responses include the HTTP status and the Server's safe problem detail. Ctrl-C
cancels an active log request or Run wait and returns promptly with a nonzero
status.
