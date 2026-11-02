# Ansible authoring example

This directory is a runnable starting point for an ordinary Slipramp Target.
It keeps the inventory, playbook, and execution procedure in the repository;
Slipramp supplies the exact Git commit, Placement, credentials, logs, and Run
history without interpreting Ansible commands.

The example intentionally does not declare the planned native Ansible report
media type or invoke Ansible Runner yet. The image contains Runner so the next
delivery slice can add its tested, versioned event envelope together with the
Server adapter. The `direct-playbook` Action demonstrates that neither Runner,
a launcher, nor a report is required.

## Runtime image

[`runtime/Dockerfile`](./runtime/Dockerfile) is the control-node image pattern
used by the Target. It pins the base image by digest and selects exact
`ansible-core` and `ansible-runner` versions in
[`runtime/requirements.txt`](./runtime/requirements.txt). From the
`examples/infrastructure-ops` directory, build it with:

```console
docker build --file ansible/runtime/Dockerfile \
  --tag registry.example.com/ansible-operations:v1.0.0 \
  ansible
```

Push that image, resolve its registry digest, and replace the illustrative
Runtime reference in `.slipramp/runtimes.yaml` with the immutable
`repository@sha256:...` reference. The image runs as UID and GID 65532; the
example credential files are world-readable within their dedicated Secret
volumes so the non-root process can read them.

The playbook uses only `ansible.builtin` content. A repository that needs
collections should either install exact collection versions while building the
image or vendor them in Git and set `ANSIBLE_COLLECTIONS_PATH`. Installing from
Galaxy during each Run is also valid, but makes execution depend on network
availability and permits the external repository to affect reproducibility.
Pin collection versions even when installation happens at image-build time.
System packages and Python libraries required by modules or inventory plugins
belong in the same image.

Ansible Builder is an equally valid way to produce this control-node image.
Slipramp does not require a particular builder or an execution-environment
container. The later report-enabled form of this example will run Runner
locally inside the Action container rather than nesting another container.

## Inventory, playbook, and actions

[`inventory/hosts.yml`](./inventory/hosts.yml) contains placeholder DNS names
and an SSH user. Replace them with reachable managed nodes and update the
`canary` Action's fixed limit. [`site.yml`](./site.yml) writes a harmless marker
that gives check mode a concrete change to predict.

[`slipramp/ansible.py`](./slipramp/ansible.py) validates its mode and playbook
path, then invokes `ansible-playbook`. `preview` adds `--check --diff`; `apply`
does not. Standard output and error remain connected to the Run log. The
launcher starts Ansible in a separate process group, forwards termination
signals to that group, waits for it, and preserves its exit code.

Check mode is a preview rather than a proof of the next apply. Modules can omit
check-mode support, make incomplete predictions, or report different results
when the real environment changes. Diffs can also contain sensitive values, so
operators should control playbook output and use `no_log` where appropriate.
Slipramp does not turn preview into an approval requirement.

The catalog defines four independent choices:

- `preview` runs the whole playbook through the launcher in check mode.
- `canary` applies through the launcher with `--limit node01.example.com`.
- `sync` directly reconciles the complete inventory through the launcher.
- `direct-playbook` invokes `ansible-playbook` itself, with no launcher and no
  structured report declaration.

Action names and this layout are examples, not reserved conventions. A
repository may expose other playbooks, ad-hoc `ansible` commands, task runners,
shell scripts, or operator-defined entrypoints.

## SSH and other credentials

The Runtime declares separate `ssh` and `ssh-known-hosts` logical credentials.
The Target maps them to the example Placement's `ansible-ssh` and
`ssh-known-hosts` sources. At execution time they become:

```text
/run/slipramp/credentials/ssh/id_ed25519
/run/slipramp/credentials/ssh/known_hosts
```

Runtime environment variables point Ansible at both files and require strict
host-key checking. Populate `known_hosts` from a trusted provisioning channel;
do not use a Run-time `ssh-keyscan` of the same untrusted endpoint as a trust
decision. SSH certificates, agents, alternate connection plugins, and
inventory variables remain valid alternatives when expressed through the
Runtime and Placement mechanisms.

Vault is intentionally absent because this playbook does not use it. A Target
that needs Vault can add another Runtime credential which injects a password or
password-client script as a file, set `ANSIBLE_VAULT_PASSWORD_FILE` to that
path, and bind the logical credential to a Placement source. Do not put the
password in the catalog, inventory, image, or Git repository.

Dynamic inventory plugins work the same way: include the pinned plugin and its
dependencies in the image, keep inventory configuration in the repository, and
inject cloud authority using the provider's normal environment variables,
credential files, workload identity, or Kubernetes ServiceAccount conventions.
Secret values remain in the execution cluster and are never stored in the
catalog.
