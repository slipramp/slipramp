# kcloud-ops catalog example

This directory sketches how three real homelab workloads can fit Slipramp's
current catalog model:

- `homelab-node-exporter` manages node_exporter on a pyinfra inventory group.
- `rustfs-homelab` manages RustFS buckets and bucket-scoped IAM users with
  Terraform.
- `homelab-kubernetes-upgrade` performs guarded kubeadm previews and rolling
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
| `ops-ci` | `SSH_PRIVATE_KEY`, `TF_VAR_access_key`, `TF_VAR_secret_key`, `TF_STATE_ACCESS_KEY`, `TF_STATE_SECRET_KEY` | Host SSH and both RustFS credential pairs |
| `homelab-known-hosts` | `known_hosts` | Strict host verification for Kubernetes upgrades |

Secret values stay in the execution cluster. The catalog contains only the
logical bindings and injection conventions.

The upgrade launcher derives a temporary kubeconfig from the Run Pod's
projected ServiceAccount token. The deployment must grant that ServiceAccount
the narrow node lifecycle, eviction, health-check, backup-read, and etcd/Ceph
exec permissions used by the script.

## Prototype gaps

The image digest is an illustrative private operations image and must contain
pyinfra, Terraform, kubectl, the MinIO client (`mc`), Bash, and SSH. The RustFS
state bucket must also be bootstrapped before the remote-backend workflow can
run. Those details, the omitted workload source, and action approvals are
outside this example; the checked-in catalog is covered by a Go test.
