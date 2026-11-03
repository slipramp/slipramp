# First-class Ansible support

Status: in progress
Last updated: 2026-11-02

This document is the implementation plan for making Ansible a first-class
Slipramp tool integration. It records intended work, not current product
behavior. Update the current architecture documents as each contract lands and
remove this plan after the work is complete and the remaining backlog, if any,
has moved to [ROADMAP.md](../ROADMAP.md).

## Outcome

An operator can define an Ansible Runtime and Target using the ordinary catalog
model and run any repository-defined Ansible command. Operators who opt into a
recognized report Artifact can also inspect a native structured playbook
report. Ansible continues to own inventories, playbooks, collections,
connection plugins, check mode, and infrastructure behavior. Slipramp owns
immutable execution, Placement-selected authority, credential injection, logs,
Artifacts, history, and the bounded value-free projection of any report the
operator chooses to publish.

Ansible support must preserve these existing decisions:

- An Ansible execution is an ordinary Run, not a separate domain resource.
- A repository-defined Runtime is the Ansible control-node container.
- The Controller and Harness remain tool-neutral.
- The Server never receives Kubernetes credentials or SSH, Vault, cloud, or
  managed-node secret values.
- Direct Actions remain valid. Preview and review are optional.
- Slipramp never retries a playbook implicitly.
- Removing a Target never means undoing Ansible-managed changes.

## Paved path, not product policy

First-class support should make common Ansible workflows easy and observable
without narrowing the generic Action model. The reference example is a tested
starting point, not a required Ansible layout.

- Do not inspect command names or require `ansible-playbook`, Ansible Runner,
  the optional report helper, particular Action names, or a report Artifact.
- Do not reserve Runtime names, credential names, environment variables,
  inventory locations, image builders, or collection installation strategies.
- Continue accepting custom Runtime images, direct `ansible-playbook` and
  `ansible` commands, Ansible Runner, task runners, shell scripts, and other
  noninteractive entrypoints.
- Keep structured reporting opt-in through a declared recognized media type.
  Runs without it retain the same logs, status, history, cancellation, and
  Artifact capabilities as every other Action.
- Present secure and reproducible defaults in the example while documenting
  where an operator can deliberately choose different tradeoffs. Do not turn
  those defaults into catalog validation or Server policy.

## Current foundation

The existing generic execution model already provides most Ansible runtime
support:

- exact-commit Git checkout into an Action container;
- an explicit command array and repository-relative working directory;
- Placement-selected Cluster, namespace, service account, resources, network
  reachability, and pod policy;
- Runtime-declared environment, file, directory, and `envFrom` credential
  injection;
- streaming and retained logs;
- signal forwarding, cancellation, exit-status preservation, and Artifact
  upload after successful or failed commands;
- Target serialization, immutable Run history, Git triggers, and optional
  trigger review.

Ansible can therefore run today when a repository supplies an appropriate
Runtime image and Action command. The missing first-class surfaces are a tested
authoring convention, a complete example, a structured report adapter and Web
view, and end-to-end acceptance coverage.

## Scope

### In scope

- A tested reference Runtime image pattern.
- A versioned, product-owned optional report helper and base Runtime image.
- Reference SSH private-key and strict known-hosts configuration.
- Optional Ansible Vault and tool-native cloud credential conventions.
- Repository-owned inventories, dynamic inventory plugins, playbooks, roles,
  and configuration.
- Preview through Ansible check mode, without treating it as authoritative.
- Canary or subset execution through `--limit`.
- Direct reconcile Actions and optional `sync` Actions.
- Human-readable live logs.
- An optional sensitive structured source Artifact based on Ansible Runner
  events.
- A bounded native Ansible report summary and detail projection.
- OpenAPI, generated clients, Web presentation, metrics, migrations,
  documentation, and acceptance coverage for the report.

### Non-goals

- An `AnsibleTarget`, Ansible-specific Run type, or tool-specific scheduler.
- AWX or Ansible Automation Platform API integration.
- Workflow graphs, surveys, job templates, organizations, or inventories stored
  in PostgreSQL.
- `ansible-pull`; the Job already receives the exact repository commit.
- Product-managed nested Podman or Docker execution. An operator-defined Action
  may still use additional process isolation when its Placement permits it.
- Product-managed Galaxy installation into operator repositories or arbitrary
  Runtime images. Derived images and repository commands remain free to
  install dependencies at build or Run time.
- Parsing terminal output as the native report contract. Terminal output
  remains available as the Run log.
- Storing facts, module return values, diffs, or arbitrary event payload values
  in report summaries or projections.
- Making check mode, preview, or approval mandatory.
- Inferring infrastructure state or drift from a successful playbook.

## Reference authoring path

### Runtime image

Publish a tested `ansible-tools` base Runtime image containing selected
versions of:

- `ansible-core`;
- `ansible-runner`;
- Python and OpenSSH client tools;
- the product-owned `slipramp-ansible` report helper.

Operator images derive from that base to add pinned Galaxy collections and
their Python or system dependencies. The collection root is an explicit,
non-root-writable image-extension boundary; the product does not infer or
install repository dependencies. Ansible Builder, a conventional container
build, and an unrelated operator image remain valid alternatives.

The reference should pin collections in the derived image or install vendored
collection tarballs for a reproducible, offline-capable build. Run-time
`ansible-galaxy install` remains a supported repository choice. Document its
network, availability, and reproducibility tradeoffs without adding catalog
validation or warnings that reject the operator's choice.

Slipramp launches the selected Runtime image directly as the Action container.
The report helper launches Ansible Runner as a local subprocess without process
isolation because another execution-environment container is unnecessary. It
is not installed through the Harness or Controller, and operators do not copy
its source or report protocol into their repositories.

### Credential conventions

The example Runtime can demonstrate tool-native environment variables and
files:

```yaml
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: ansible
  title: Ansible operations image
spec:
  image: registry.example.com/ansible-operations@sha256:replace-me
  workingDirectory: /workspace
  environment:
    ANSIBLE_HOST_KEY_CHECKING: "true"
    ANSIBLE_PRIVATE_KEY_FILE: /run/slipramp/credentials/ssh/id_ed25519
    ANSIBLE_SSH_COMMON_ARGS: >-
      -o UserKnownHostsFile=/run/slipramp/credentials/ssh/known_hosts
      -o StrictHostKeyChecking=yes
    ANSIBLE_VAULT_PASSWORD_FILE: /run/slipramp/credentials/vault/password
  credentials:
    ssh:
      inject:
        files:
          - key: SSH_PRIVATE_KEY
            path: /run/slipramp/credentials/ssh/id_ed25519
            mode: 0444
          - key: known_hosts
            path: /run/slipramp/credentials/ssh/known_hosts
            mode: 0444
    vault:
      inject:
        files:
          - key: password
            path: /run/slipramp/credentials/vault/password
            mode: 0444
```

The final example may split the private key and known-hosts file into separate
logical Runtime credentials when that better demonstrates independent
Placement sources. The reference should keep host-key checking enabled and work
with a non-root Runtime user under the example Placement's security context.
These logical names, paths, modes, and SSH settings are examples rather than
reserved product conventions; operators may use inventory variables, SSH
agents, certificates, alternate connection plugins, or other Ansible-supported
mechanisms.

Vault is optional, and the example should omit it from Targets that do not use
it. Cloud inventory and module credentials can use provider-native environment,
file, workload identity, or service-account conventions through the same
Runtime and Placement mechanisms. Slipramp does not prescribe which Ansible
secret-management or connection mechanism an operator adopts.

### Target and Actions

Add a representative Ansible Target to the infrastructure operations example
without replacing the existing pyinfra Target. Coexistence is part of the
feature contract.

The report-enabled part of the example can demonstrate:

```yaml
spec:
  path: ansible
  runtime: ansible
  placement: infrastructure-admin
  credentials:
    ssh: operations
    vault: ansible-vault
  actions:
    preview:
      intent: preview
      command: [slipramp-ansible, preview, site.yml]
      artifacts:
        report:
          path: /slipramp/artifacts/ansible-playbook.json
          mediaType: application/vnd.slipramp.ansible-playbook+json
          sensitive: true
    canary:
      intent: reconcile
      command:
        - slipramp-ansible
        - apply
        - --limit
        - node01.example.com
        - site.yml
      artifacts:
        report:
          path: /slipramp/artifacts/ansible-playbook.json
          mediaType: application/vnd.slipramp.ansible-playbook+json
          sensitive: true
    sync:
      intent: reconcile
      command: [slipramp-ansible, apply, site.yml]
      artifacts:
        report:
          path: /slipramp/artifacts/ansible-playbook.json
          mediaType: application/vnd.slipramp.ansible-playbook+json
          sensitive: true
```

The implementation may use a string or enum catalog parameter for `--limit`
instead of a fixed canary Action when the example can validate a useful input.
No product behavior should assume that an Ansible Target has `preview`,
`canary`, or `sync`; these remain repository-defined Actions.

Include at least one ordinary Ansible Action that invokes an operator-selected
entrypoint without declaring a structured report. This proves that the paved
path does not become a requirement.

### Optional report helper behavior

Ship `slipramp-ansible` as a product-owned CLI in the optional base Runtime
image. It owns the stable boundary between ordinary operator content and the
Slipramp report source contract. Actions that do not request a structured
report can invoke `ansible-playbook`, `ansible`, Ansible Runner, or another
repository command directly. When an Action declares the Ansible report media
type, the reference helper must:

1. Validate the requested mode and arguments.
2. Invoke Ansible Runner as a local subprocess without process isolation.
3. Add `--check --diff` only for preview mode.
4. Leave normal Ansible output connected to stdout and stderr for live logs.
5. Forward `SIGTERM` and other relevant termination signals to Runner, wait for
   it, and preserve its exit code.
6. Collect Runner job events into the declared report file after success,
   playbook failure, unreachable hosts, or cancellation when partial events are
   available.
7. Fail a successful playbook if report construction fails. Never replace a
   nonzero playbook exit status with a report-packaging status.

Keep the CLI narrow and test its argument handling, signal forwarding, exit
status, and report construction independently. Version it with Slipramp's
published image. Do not add it to the Harness or Controller, make it a
prerequisite for executing Ansible, or require an operator to embed its
implementation in a workflow.

## Optional structured report contract

### Source Artifact

Register this opt-in media type:

```text
application/vnd.slipramp.ansible-playbook+json
```

An Action opts into native reporting only by declaring an Artifact with this
media type and producing the corresponding file. The source file uses this
version 2 envelope:

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

Its fields contain:

- source schema version;
- absolute repository directory used to resolve task paths, discovered from the
  nearest Git root or falling back to the project directory for standalone use;
- `preview` or `apply` mode selected by the helper;
- optional Ansible Core and Ansible Runner versions when their version commands
  return recognized bounded output;
- terminal Runner status (`successful`, `failed`, `timeout`, or `canceled`) and
  process exit code as descriptive source fields;
- Runner events ordered by their event counter.

Runner's event payloads remain verbatim in the sensitive source Artifact. The
Server adapter, not the helper, owns normalization into the product report.
The source may have no events and is partial when it has no final
`playbook_on_stats` event. Completed events require positive integer counters
that match their Runner filenames. The producer rejects filename/content
counter mismatches and duplicate counters; the adapter must enforce the same
rules without trusting producer validation. Unknown well-formed Runner event
types remain in the sensitive source and are ignored by projection.

The helper writes one event at a time, caps the completed event stream at
200,000 events, and caps the complete source at the existing 32 MiB report
source limit. Incomplete Runner `*-partial.json` files are not source events.
Packaging failure changes a successful playbook exit into helper exit 70 but
never replaces a nonzero Runner exit. The adapter may impose lower projection
bounds than the sensitive source permits.

### Projection

Add report kind `ansible-playbook` with modes `preview` and `apply`.

The summary should include:

- whether a final playbook stats event was observed;
- unique hosts processed;
- per-host aggregate totals for `ok`, `changed`, `failed`, `unreachable`,
  `skipped`, `rescued`, and `ignored`;
- total plays and tasks observed;
- total host-task results grouped by outcome.

The structured detail should retain only the minimum useful identity and
outcome fields:

- stable event order;
- play identity and display name;
- task identity, display name, action/module name, and repository-relative
  source path when present;
- host name;
- normalized outcome: `ok`, `changed`, `failed`, `unreachable`, or `skipped`;
- whether the event represents a loop item, handler, rescue, or ignored
  failure when Runner exposes that fact reliably.

Do not project:

- `res` objects or arbitrary module return values;
- stdout or stderr fragments;
- task arguments;
- diffs;
- facts;
- registered values;
- exception messages or provider responses.

Treat `no_log` as a hard redaction boundary. The first version should replace
the affected task's presentation fields with a redacted marker instead of
trying to decide which strings are safe. Summary counts may still include the
event.

Define explicit upper bounds for source bytes, events, plays, tasks, unique
hosts, host-task results, names, paths, and projection bytes. Unknown event
types and unknown event fields should be ignored when they are well formed;
malformed required fields, unsafe strings, inconsistent counters, and bound
violations should reject report ingestion without changing the Run outcome.

The projection is descriptive. Runner status, event contents, and recap counts
must never determine or revise the Run's phase or exit status.

### OpenAPI and persistence

Add `AnsiblePlaybookReportSummary` and `AnsiblePlaybookReport` to the report
unions and discriminator mappings in `api/components/run-reports.yaml`. Generate
all Go, TypeScript, and Swift outputs with the repository generation command;
never edit generated clients directly.

Add a migration that extends the `artifact_reports` kind and mode constraints
for `ansible-playbook`. Existing generic report metadata and projection storage
should be reused. Do not add Ansible-specific report tables unless the bounded
projection demonstrably cannot fit the current model.

Add the report kind to ingestion telemetry and update `docs/METRICS.md`.

## Web presentation for report-enabled Runs

The Run page should present:

- compact recap badges for changed, failed, unreachable, skipped, rescued, and
  ignored counts;
- whether the report is complete or partial;
- play and task grouping in event order;
- normalized per-host task outcomes;
- filtering by play, task, action/module, host, and outcome;
- collapsed successful/no-change detail by default for large reports;
- the existing sensitive-detail authorization and source Artifact download
  behavior;
- a clear notice when source or projection detail has expired while retained
  summary metadata remains.

Reuse general Run-report loading and presentation primitives, but keep the
Ansible schema and view distinct from pyinfra. Do not introduce a generalized
configuration-management report abstraction merely because the two tools both
have hosts and operations.

## Delivery slices

### Slice 1: runnable example and authoring guidance

- [x] Add the Ansible Runtime and Target to
      `examples/infrastructure-ops/.slipramp`.
- [x] Add a minimal inventory, playbook, and Action fixture.
- [x] Extend the example Placement and credential documentation.
- [x] Document image construction, dependency strategies, check-mode limits,
      the reference host-verification setup, Vault, dynamic inventory, cloud
      credentials, and the operator's available alternatives.
- [x] Include and test a direct Ansible Action without Runner or a report.
- [x] Extend catalog example tests.
- [x] Verify both portable and deployment catalog validation.

This slice should require no Server, Controller, Harness, API, or database
contract change.

### Slice 2: report source and backend projection

- [x] Add the optional product-owned helper and published base Runtime image;
      keep the Harness, Controller, and operator repository free of the report
      protocol implementation.
- [x] Finalize the versioned source envelope and helper packaging behavior.
- [x] Add success, preview, failure, unreachable, loop, handler, rescue,
      ignored, `no_log`, and partial-run source fixtures.
- [x] Implement the bounded `ansible-playbook` adapter under
      `internal/runreport`.
- [x] Add adapter registry, projection encoding, persistence, and rejection
      tests.
- [x] Add the report-kind database constraint migration.
- [x] Extend OpenAPI report schemas and regenerate all clients.
- [x] Extend API report conversion and handler tests.
- [x] Extend report-ingestion metrics and documentation.

### Slice 3: Web report experience

- [x] Add Ansible summary and detail view models.
- [x] Add recap, grouping, filtering, redaction, partial-report, and expired
      detail presentation.
- [x] Connect the report view to the Run page report-kind switch.
- [x] Add focused TypeScript tests for summaries, grouping, filtering, and
      redaction.
- [ ] Verify keyboard navigation, screen-reader labels, narrow layouts, and
      large-report behavior.

### Slice 4: end-to-end acceptance and final documentation

- [ ] Add a pinned acceptance Runtime image with Ansible Core and Runner.
- [ ] Add a small SSH-managed host fixture separate from the Git SSH service.
- [ ] Exercise the reference strict known-host verification and private-key
      injection path without enforcing it as a product policy.
- [ ] Exercise preview with predicted changes and successful apply.
- [ ] Exercise failed and unreachable hosts while retaining the report.
- [ ] Exercise cancellation, signal forwarding, partial report upload, and Job
      cleanup.
- [ ] Verify `no_log` values and injected secrets never enter the projection,
      API summary, structured logs, or diagnostics.
- [ ] Verify no implicit retry and ordinary Target serialization.
- [x] Update `docs/design/artifacts.md` when the adapter lands.
- [ ] Update other current design, example, installation, and development
      documentation affected by the final implementation.
- [ ] Remove the completed Ansible roadmap entry and delete this plan once all
      lasting decisions are represented by current design documentation.

The slices may land as separate pull requests, but Slice 2 must not advertise a
report media type until the source producer and decoder agree on a tested
versioned contract. Keep intermediate roadmap wording accurate.

## Acceptance criteria

First-class Ansible support is complete when all of the following are true:

- A documented example validates and executes without special Controller or
  Server behavior.
- A direct Ansible Action without Runner, the helper, or a report remains valid
  and receives ordinary Run behavior.
- The same repository can contain independent Ansible and pyinfra Targets.
- Runtime and Placement credential compatibility validation covers every
  declared Ansible credential requirement.
- SSH host-key verification is enabled in the reference example and acceptance
  test, while the product does not enforce a connection policy.
- Preview and direct apply/reconcile Actions both work; neither is mandatory.
- Live logs remain readable Ansible output rather than machine JSON.
- Successful, failed, unreachable, and canceled playbooks retain the expected
  source Artifact when Runner produced usable events.
- The native summary and detail view work for preview and apply modes.
- Raw module values, diffs, facts, secrets, and `no_log` presentation do not
  enter projections.
- Report ingestion enforces documented resource bounds and fails safely.
- Playbook exit status, cancellation, scheduling, and Target serialization
  remain governed by ordinary Run behavior.
- OpenAPI and all generated clients are current.
- Unit, integration, Web, and kind acceptance checks pass.
- Current design and operator documentation describe the shipped behavior, and
  this temporary plan is no longer needed.

## Risks and mitigations

### Runner event compatibility

Runner is a better integration boundary than terminal output, but its event
documents can still grow new fields and event types. Decode only the narrow
field set required for the projection, ignore well-formed unknowns, pin tested
Runner and Ansible versions in acceptance, and version the source and adapter
contracts independently.

### Secret-bearing event data

Runner events may contain arbitrary module results. Always mark the source
Artifact sensitive, project from an allowlist instead of a denylist, omit result
objects entirely, and treat `no_log` as a hard redaction boundary.

### Large playbooks

Large inventories and loops can create many events. Bound source and projection
sizes and counts, aggregate summaries while decoding, and make the Web view
filterable and progressively disclosed. Do not silently truncate a projection;
reject it with a bounded diagnostic so the source Artifact and normal Run logs
remain the evidence.

### Cancellation through the report helper

The Harness signals its direct child. The report helper waits on Runner, so it
must forward signals explicitly and still attempt bounded report finalization.
Cover this with focused helper tests and a real Job cancellation acceptance
case.

### Check-mode overstatement

Not all modules support check mode or predict changes accurately. Label output
as an Ansible preview, retain module outcomes as descriptive facts, and never
present it as proof of the exact apply result.

## References

- [Slipramp architecture](../DESIGN.md)
- [Catalog design](../design/catalog.md)
- [Controller execution](../design/execution.md)
- [Logs and Artifacts](../design/artifacts.md)
- [Infrastructure operations example](../../examples/infrastructure-ops/README.md)
- [Ansible Runner documentation](https://docs.ansible.com/projects/runner/en/latest/)
- [Ansible Runner event artifacts](https://docs.ansible.com/projects/runner/en/devel/intro/)
- [Installing collections from requirements files](https://docs.ansible.com/projects/ansible/latest/collections_guide/collections_installing.html#installing-multiple-collections-with-a-requirements-file)
- [Ansible default callback output](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/default_callback.html)
- [Ansible SSH connection settings](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/ssh_connection.html)
- [Ansible Vault password sources](https://docs.ansible.com/projects/ansible/latest/vault_guide/vault_using_encrypted_content.html)
