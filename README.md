# Slipramp

Slipramp is a GitOps-driven infrastructure control plane for homelabs and
small teams. It coordinates tools such as Terraform/OpenTofu, Ansible,
pyinfra, and repository scripts as durable Actions executed from exact Git
commits across one or more Kubernetes Clusters.

## Features

- **Git-backed operational catalog.** Repositories define portable Runtimes,
  durable Targets, typed parameters, explicit Actions, credentials, triggers,
  and Artifact contracts. Every accepted catalog revision is tied to an exact
  commit.
- **Flexible, immutable Runs.** Operators can start Actions through the Web,
  CLI, or API, while named Git triggers provide target-scoped latest-wins
  automation and optional review. Safety policy and preview flows remain
  opt-in rather than mandatory gates.
- **Multi-cluster execution.** One Controller per execution Cluster creates
  short-lived Kubernetes Jobs from Placement policy. Controllers connect
  outbound, so the Slipramp Server has no kubeconfig or direct Kubernetes API
  access.
- **Durable logs and Artifacts.** Live logs and declared outputs share the same
  retained Artifact path. Plan and preview outputs can be selected explicitly
  by later Runs, pinning consumers to the producing commit and catalog
  revision.
- **Catalog validation and explanation.** The CLI and Server share strict YAML
  decoding and validation. Local tooling provides source-addressed
  diagnostics, JSON and SARIF output, editor schema support, and resolved
  Action explanations with field-level provenance.
- **Identity and credential boundaries.** OIDC sessions, role-aware users and
  service accounts, and scoped API keys protect product operations. Slipramp
  stores credential references rather than injected secret values.
- **Optional Kargo operations.** Slipramp can inventory Kargo Stages, start
  Promotions, prepare Git and Argo CD diffs, apply review policy, and surface
  operational health without making Kargo a requirement for ordinary Runs.
- **Operator-focused interfaces.** The Web UI, remote CLI, generated API
  clients, and native iOS preview cover Run discovery, execution, review,
  logs, Artifacts, and promotion operations.

## Project status

Slipramp is in early, ongoing development. Its contracts are pre-stable, and
development favors clean breaking changes that update callers, generated
code, fixtures, and documentation together.

## Documentation

- [Kubernetes installation and releases](docs/INSTALLATION.md): deployment
  topology, Helm installation, and release publishing.
- [Configuration](docs/CONFIGURATION.md): Server, Controller, Web, Placement,
  identity, and optional Kargo settings.
- [CLI reference](docs/CLI.md): local catalog validation and explanation, plus
  the remote operator workflow.
- [Catalog example](examples/infrastructure-ops/README.md): complete Runtime
  and Target definitions, typed parameters, credentials, triggers, and
  Artifact flows.
- [Catalog validation](docs/CATALOG_VALIDATION.md): source-pinned setup,
  validation scopes, hooks, CI, and editor integration.
- [Design](docs/DESIGN.md): implemented architecture, domain model, trust
  boundaries, and governing decisions.
- [Roadmap](docs/ROADMAP.md): unfinished product, reliability, and operator
  work.
- [Development](docs/DEVELOPMENT.md): local services, common tasks, private
  development integrations, generation, and testing.
- [Metrics](docs/METRICS.md): Prometheus endpoints, metric catalog, labels,
  and scrape guidance.
- [End-to-end acceptance](deploy/e2e/README.md): disposable kind coverage and
  diagnostics.
- [iOS preview](ios/README.md): native SwiftUI setup, connection, and tests.
- [Project site](https://slipramp.com) and
  [mascot gallery](https://mascot.slipramp.com): public project and artwork
  sites, published independently from the private control plane.

## License

Slipramp software, documentation, and configuration are licensed under the [Apache License 2.0](LICENSE). The production artwork identified in [LICENSE-ARTWORK.md](LICENSE-ARTWORK.md) is licensed separately under [Creative Commons Attribution-ShareAlike 4.0 International](https://creativecommons.org/licenses/by-sa/4.0/).

Neither license grants trademark rights in the Slipramp name, logo, or mascot. See [TRADEMARKS.md](TRADEMARKS.md) for the trademark policy.
