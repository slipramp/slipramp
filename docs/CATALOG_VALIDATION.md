# Catalog validation

The `slipramp` CLI validates every Runtime and Target below a repository's
`.slipramp` directory. It uses the same decoding, portable validation, and
deployment validation pipeline as Slipramp Server repository refresh.

Slipramp is not publishing versioned CLI artifacts yet. Until that begins,
build the CLI from a source checkout or install it from a full Git commit. A
source-built CLI reports `dev` from `slipramp version`; the selected Git commit
is the current pin.

## Install from source

Build from a checked-out commit:

```console
git clone https://github.com/slipramp/slipramp.git
cd slipramp
git checkout FULL_COMMIT_SHA
mkdir -p bin
go build -trimpath -o ./bin/slipramp ./cmd/slipramp
```

For an automated environment with Go installed, install the same commit
directly:

```console
SLIPRAMP_REVISION=FULL_COMMIT_SHA
mkdir -p .bin
GOBIN="$PWD/.bin" go install github.com/slipramp/slipramp/cmd/slipramp@"$SLIPRAMP_REVISION"
```

Use a full commit SHA rather than a branch name so local hooks and CI do not
silently change behavior.

## Validation levels

Validation has three deliberately distinct scopes:

| Scope | Command | Checks |
| --- | --- | --- |
| Portable | `slipramp catalog validate` | YAML structure, catalog names and references, resolved Actions, parameters, artifacts, triggers, repository paths, and watch patterns |
| Deployment | `slipramp catalog validate --placements placements.yaml` | All portable checks plus Placement existence, offered credential sources, and effective resource constraints |
| Live-cluster preflight | [Planned Controller-backed command](./ROADMAP.md#placement-inventory-and-controller-preflight) | Cluster reachability, Secret contents, service-account permissions, Runtime image availability, and Kubernetes admission |

Portable validation accepts unknown but well-formed Placement names because
Placements belong to a Slipramp installation rather than the repository. It
reports which referenced Placements were not checked. Deployment validation is
offline too: the supplied `PlacementConfig` contains references and static pod
settings, not Secret values, and the CLI never contacts Kubernetes.

Operator-facing Action and Parameter titles and descriptions and Artifact
descriptions are bounded catalog values. Action `documentationUrl` must be an
absolute `http` or `https` URL. These fields never replace Action, Parameter,
or Artifact map keys as technical identities.

Slipramp Server always performs portable and deployment validation with its
configured Placements. For the same checkout and `PlacementConfig`, a passing
deployment-aware CLI result has the same acceptance result as repository
refresh. A Runtime image without a content digest produces a warning because a
mutable tag can resolve to different bytes later; it remains valid, and the
Controller records the actual Kubernetes image identities on each Run.
Warnings do not reject Server refresh; `--strict` only makes warnings fail the
CLI invocation.

Run portable validation from the catalog repository root:

```console
slipramp catalog validate
slipramp catalog validate --strict
```

Run deployment validation when the non-secret Server configuration is
available:

```console
slipramp catalog validate --placements ./placements.yaml
```

Text is intended for terminals. JSON is a versioned Slipramp result document,
and SARIF 2.1.0 is suitable for code-scanning systems:

```console
slipramp catalog validate --format json
slipramp catalog validate --format sarif > slipramp.sarif
```

Exit code `0` means validation passed. Code `1` means catalog errors or strict
warnings were found, `2` means the invocation or its inputs were invalid, and
`3` means output or another internal operation failed. Diagnostics go to
standard output so structured output can be redirected without mixing it with
invocation failures from standard error.

## Git pre-commit hook

A plain Git hook keeps the integration independent of a particular hosting
provider. Install `slipramp` at a pinned commit, then save this as
`.git/hooks/pre-commit` in the catalog repository and make it executable:

```sh
#!/bin/sh

set -eu

repository_root=$(git rev-parse --show-toplevel)
exec slipramp catalog validate --strict "$repository_root"
```

The hook validates the working tree, including unstaged catalog changes. Teams
that intentionally permit warnings can omit `--strict`.

## CI

The same shell steps work in any CI system. Pin the CLI to a full Slipramp
commit, validate the checked-out catalog, and retain `slipramp.sarif` using the
CI provider's ordinary artifact or code-scanning support:

```sh
set -eu

SLIPRAMP_REVISION=FULL_COMMIT_SHA
mkdir -p .bin
GOBIN="$PWD/.bin" go install github.com/slipramp/slipramp/cmd/slipramp@"$SLIPRAMP_REVISION"
.bin/slipramp catalog validate --strict --format sarif > slipramp.sarif
```

Add `--placements path/to/placements.yaml` only when CI has the same non-secret
Placement configuration used by the Server. Do not place Kubernetes Secret
values in that file or the catalog repository.

## Editor schema

[`schemas/catalog-v1alpha1.json`](../schemas/catalog-v1alpha1.json) is a
generated JSON Schema for one `slipramp.com/v1alpha1` Runtime or Target YAML
document. It supplies structural validation and completion, but it is not the
authoritative validator: JSON Schema cannot enforce cross-document references,
resolved defaults, artifact provenance, credential availability, or Placement
constraints.

YAML Language Server clients, including the Red Hat YAML extension for VS Code,
accept a `yaml.schemas` mapping. Until the schema is published as a release
artifact, point the mapping at the file in a pinned Slipramp source checkout.
For example, when that checkout is a sibling of the catalog repository:

```json
{
  "yaml.schemas": {
    "../slipramp/schemas/catalog-v1alpha1.json": [
      ".slipramp/**/*.yaml",
      ".slipramp/**/*.yml"
    ]
  }
}
```

Relative schema paths are resolved from the workspace root in a single-folder
workspace. Other YAML Language Server clients use the same schema-to-glob map
inside their client-specific settings. Catalog files do not require modeline
comments or editor-only fields.

The schema is generated from the Go catalog types and checked for freshness by
the repository quality gate:

```console
task catalog:schema:generate
task catalog:schema:check
```

Change the catalog types first, regenerate the schema, and commit both changes
together.
