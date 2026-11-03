# Ansible authoring example

This directory is a runnable starting point for an ordinary Slipramp Target.
The operator repository contains the inventory, playbook, Ansible configuration,
and catalog Actions. Slipramp supplies the exact Git commit, Placement,
credentials, logs, Run history, and the optional report helper without
interpreting the playbook.

The `preview`, `canary`, and `sync` Actions declare the helper's source envelope
as a sensitive `application/vnd.slipramp.ansible-playbook+json` Artifact. The
Server projects only bounded, value-free play, task, host, and outcome data for
structured display. The `direct-playbook` Action demonstrates that neither
Runner nor structured reporting is required.

## Runtime image

Slipramp publishes `ghcr.io/slipramp/slipramp-ansible-tools`, a versioned base
Runtime containing pinned Ansible Core and Ansible Runner releases, OpenSSH,
and the product-owned `slipramp-ansible` helper. The image runs as UID and GID
65532 and has a writable collection root at
`/usr/share/ansible/collections`.

[`runtime/Dockerfile`](./runtime/Dockerfile) derives an operator Runtime from
that base. Build from the `examples/infrastructure-ops` directory:

```console
docker build --file ansible/runtime/Dockerfile \
  --build-arg ANSIBLE_TOOLS_IMAGE=ghcr.io/slipramp/slipramp-ansible-tools@sha256:replace-me \
  --tag registry.example.com/ansible-operations:v1.0.0 \
  ansible
```

Push the result, resolve its registry digest, and replace the illustrative
Runtime reference in `.slipramp/runtimes.yaml` with the immutable
`repository@sha256:...` reference.

The checked-in playbook uses only `ansible.builtin` content. A repository that
needs Galaxy collections can extend the same image without copying the report
helper into its workflow:

```dockerfile
ARG ANSIBLE_TOOLS_IMAGE=ghcr.io/slipramp/slipramp-ansible-tools@sha256:replace-me
FROM ${ANSIBLE_TOOLS_IMAGE}

COPY --chown=65532:65532 requirements.yml /tmp/requirements.yml
RUN ansible-galaxy collection install \
    --requirements-file /tmp/requirements.yml \
    --collections-path "${ANSIBLE_COLLECTIONS_PATH}"
```

Pin every collection version in `requirements.yml`. Requirements may also
refer to collection tarballs vendored in the build context for an offline
build. System packages and Python libraries required by modules or inventory
plugins belong in the derived image; switch to `USER 0` only for system-package
installation and restore `USER 65532:65532` afterward. Installing collections
during every Run is still an operator choice, but it makes execution depend on
network availability and weakens reproducibility.

Ansible Builder is an equally valid way to produce a derived Runtime. Slipramp
does not require a particular image builder or a nested execution-environment
container.

## Inventory, playbook, and actions

[`inventory/hosts.yml`](./inventory/hosts.yml) contains placeholder DNS names
and an SSH user. Replace them with reachable managed nodes and update the
`canary` Action's fixed limit. [`site.yml`](./site.yml) writes a harmless marker
that gives check mode a concrete change to predict.

The `preview`, `canary`, and `sync` Actions invoke the product-owned helper from
the Runtime image. It runs Ansible Runner against the checked-out repository,
keeps normal output attached to the Run log, forwards termination signals, and
preserves the action exit status. `preview` adds `--check --diff`; `apply` does
not.

The four Actions are independent examples:

- `preview` runs the whole playbook in check mode.
- `canary` applies with `--limit node01.example.com`.
- `sync` directly reconciles the complete inventory.
- `direct-playbook` invokes `ansible-playbook` itself without Runner or a
  structured report.

Action names and this layout are not reserved conventions. A repository may
expose other playbooks, ad-hoc `ansible` commands, task runners, shell scripts,
or operator-defined entrypoints.

Check mode is a preview rather than a proof of the next apply. Modules can omit
check-mode support, make incomplete predictions, or report different results
when the real environment changes. Diffs can contain sensitive values, so
operators should control playbook output and use `no_log` where appropriate.
Slipramp does not turn preview into an approval requirement.

## Source report contract

After Runner exits, `slipramp-ansible` writes
`/slipramp/artifacts/ansible-playbook.json` atomically. Its `--report` option can
select another path for local tests. The schema version 2 envelope is:

```json
{
  "schemaVersion": 2,
  "repositoryDirectory": "/workspace",
  "mode": "preview",
  "versions": {
    "ansibleCore": "2.21.5",
    "ansibleRunner": "2.4.3"
  },
  "runner": {
    "status": "successful",
    "exitCode": 0
  },
  "events": []
}
```

`mode` is `preview` or `apply`. `runner.status` is `successful`, `failed`,
`timeout`, or `canceled`; its exit code is descriptive and does not replace the
helper's exit status. `events` contains the completed Runner JSON events ordered
by their positive integer `counter`. A missing final `playbook_on_stats` event
represents a partial stream rather than a packaging failure.

`repositoryDirectory` records the nearest Git repository root above the project
directory, or the project directory for a standalone playbook. The Server uses
it to resolve repository-relative task paths, including with custom Runtime
working directories, and omits paths outside that directory.

The helper rejects malformed event JSON, filename/content counter mismatches,
duplicate counters, more than 200,000 completed events, and a source document
larger than 32 MiB. It ignores Runner's incomplete `*-partial.json` files and
streams one completed event at a time into the report. Packaging failure changes
a successful playbook exit to helper exit 70, but never hides a nonzero Runner
exit.

Runner events may contain module results, diffs, facts, output, and secret
values. The example therefore declares this source file sensitive. The Server
projection allowlists structural identifiers, normalized outcomes, counters,
and host totals; it excludes arbitrary module values and treats `no_log` as a
hard task-level redaction boundary. Only that projection is suitable for
ordinary structured display.

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
inventory variables remain valid alternatives.

Vault is intentionally absent because this playbook does not use it. A Target
that needs Vault can inject a password or password-client script through
another Runtime credential and set `ANSIBLE_VAULT_PASSWORD_FILE`. Do not put
the password in the catalog, inventory, image, or Git repository.

Dynamic inventory plugins work the same way: add the pinned collection and its
dependencies to the derived image, keep inventory configuration in the
repository, and inject cloud authority using the provider's normal environment,
file, workload-identity, or Kubernetes ServiceAccount conventions. Secret
values remain in the execution cluster.
