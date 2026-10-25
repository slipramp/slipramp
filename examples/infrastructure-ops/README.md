# Infrastructure operations catalog example

This directory sketches how three representative infrastructure workloads can
fit Slipramp's current catalog model:

- `node-exporter` manages node_exporter on a pyinfra inventory group.
- `rustfs-buckets` manages RustFS buckets and bucket-scoped IAM users with
  Terraform.
- `kubernetes-upgrade` performs guarded kubeadm previews and rolling
  upgrades.

It is shaped like the relevant parts of an operations repository. Slipramp
would discover the resources below [`.slipramp`](./.slipramp) after cloning
that repository. The deployment and Terraform source files are intentionally
not copied into this example; the two launchers only document the expected
process boundary.

## Actions

The node_exporter target offers:

- `preview`, a pyinfra dry run against the complete `exporter_nodes` group.
- `canary`, a reconcile against one host.
- `sync`, a direct reconcile against the complete group.

The RustFS target offers:

- `preview`, which publishes a sensitive Terraform plan artifact.
- `apply`, which consumes an explicitly selected `plan` artifact from a
  `preview` run at the same catalog revision.
- `sync`, which directly applies the current configuration without requiring a
  preview.

The direct `sync` actions are deliberate. Slipramp's safety policy is opt-in;
previews and canaries are useful operations rather than mandatory gates.

Action map keys such as `preview` and `apply` remain the stable names used by
the CLI, API, triggers, and artifact references. Optional `title`, `description`,
and `documentationUrl` fields add operator-facing context without changing
those identities. Parameter `title` and `description` fields provide form
guidance while names such as `host` remain visible, and artifact descriptions
explain what an output contains wherever that output is produced or selected
as a later input.

The Kubernetes upgrade target offers a read-only `preview` for a selected
node, a single-node action, and separate ordered control-plane and compute
rolls. Its independent `apt-upgrade` and `reboot` boolean parameters can be
selected together; the underlying script rejects an apt upgrade without the
required reboot.

## Placement and credentials

[`placements.yaml`](./placements.yaml) is Server configuration rather than a
repository catalog resource. It is kept here so the example can be read and
validated as one unit. A real deployment would mount this file into the
Slipramp Server and arrange for these Kubernetes Secrets in the selected
execution cluster:

| Credential source | Secret keys | Runtime use |
| --- | --- | --- |
| `operations` | `SSH_PRIVATE_KEY`, `TF_VAR_access_key`, `TF_VAR_secret_key`, `TF_STATE_ACCESS_KEY`, `TF_STATE_SECRET_KEY` | Host SSH and both RustFS credential pairs |
| `ssh-known-hosts` | `known_hosts` | Strict host verification for Kubernetes upgrades |

Secret values stay in the execution cluster. The catalog contains only the
logical bindings and injection conventions.

From the Slipramp source root, run both validation levels against the example:

```console
go run ./cmd/slipramp catalog validate examples/infrastructure-ops
go run ./cmd/slipramp catalog validate \
  --placements examples/infrastructure-ops/placements.yaml \
  examples/infrastructure-ops
```

The omitted workload source is intentional, so the validator reports its
missing paths as warnings while accepting the catalog. Deployment validation
also checks the example's Placement and credential bindings without reading
Secret values or contacting Kubernetes.

The upgrade launcher derives a temporary kubeconfig from the Run Pod's
projected ServiceAccount token. The deployment must grant that ServiceAccount
cluster-admin access because the example's infrastructure actions intentionally
have full control of the selected cluster. Use a narrower placement and
ServiceAccount for repositories or actions that should not have that access.

## Prototype gaps

The runtime image is an illustrative placeholder and must be replaced with an
image containing pyinfra, Terraform, kubectl, the MinIO client (`mc`), Bash,
and SSH. The RustFS state bucket must also be bootstrapped before the
remote-backend workflow can run. Those details, the omitted workload source,
and action approvals are outside this example; the checked-in catalog is
covered by a Go test.
