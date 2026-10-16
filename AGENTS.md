# Slipramp Contributor Instructions

Slipramp is a GitOps-driven infrastructure control plane for homelabs and small teams. Read `docs/PLAN.md` for the product and architecture direction and `docs/MVP.md` for the current delivery scope. Keep those documents consistent when changing a decision they describe.

## Project Phase

Slipramp is in the MVP/prototype phase. Do not preserve backwards compatibility or introduce legacy behavior unless explicitly requested. Prefer a clean breaking change over compatibility aliases, deprecated fields, dual code paths, migration shims, legacy API routes, or support for obsolete YAML and database shapes. Update all callers, fixtures, generated code, and documentation together.

## Core Model and Naming

Use the established names consistently:

- A `Repository` supplies versioned definitions.
- A `Target` is a named operational unit containing user-defined `Action`s. `sync` is optional.
- A `Runtime` defines the portable container environment and process conventions.
- A `Placement` is Server configuration selecting a cluster, namespace, service account, credentials, resources, and other Kubernetes settings. Do not call it an executor or execution profile.
- A `Cluster` is a Kubernetes execution destination represented by its in-cluster `Controller`.
- The `Slipramp Server` is the central deployable process. The `control plane` is the overall central subsystem, not a binary name.
- A `Run` is an immutable action execution. Record its `run source`; do not introduce invocation as a separate domain concept.
- An `Artifact` is durable run output, including logs and plans.
- A `catalog revision` is the immutable validated catalog produced from a Git commit.
- `repository refresh` means fetching and indexing Git. Reserve `sync` and target reconciliation for the optional target action.
- The `Harness` is an internal execution component and should not become a requirement for user tools or runtime images.

## Architecture Boundaries

- The Slipramp Server must not have a kubeconfig, Kubernetes client, or direct Kubernetes API access.
- Run one Controller replica in each execution cluster. It reaches out to the Server over a small authenticated HTTPS long-poll API, manages local Jobs, and reports status and logs.
- Keep Controller delivery at least once and idempotent through deterministic Run and Job identifiers. Do not add message brokers, leader election, bidirectional RPC, or distributed leases for the MVP.
- Treat multiple Kubernetes clusters as a first-class requirement. Never assume the Server and execution Jobs share a cluster.
- Jobs clone their exact Git commit directly from the repository. Never copy repository contents, source bundles, or Git source archives into S3.
- PostgreSQL is authoritative for product state and artifact metadata. S3-compatible storage is only for artifact contents, including streaming and completed logs. RustFS is the local implementation.
- The Slipramp Harness wraps the action process, forwards signals, preserves exit status, streams logs, and transfers declared artifacts without requiring tools to know about Slipramp.
- Inject credentials using conventions tools already understand: environment variables, files, mounted directories, Kubernetes Secret references, service accounts, or placement-defined pod mechanisms. Never persist secret values in PostgreSQL.
- Stream logs from day one and retain them through the same artifact ingestion path used for live viewing.
- Allow artifacts such as Terraform plans to be selected explicitly as inputs to later Runs and pin consumers to the producing Git commit and catalog revision.

## Product Behavior

- Safety policy is opt-in. Direct actions such as an unreviewed `terraform apply` must remain valid when that is the operator's chosen workflow.
- Preview and plan flows are optional capabilities, not mandatory approval gates.
- Do not assume every Target has a `sync` Action.
- Manual and API Run creation are in scope. Git, schedule, and webhook triggers are deferred; keep the Run creation boundary ready for them without implementing them prematurely.
- Never interpret removing a Target as infrastructure destruction.
- Serialize active Runs for the same Target by default and do not retry infrastructure commands implicitly.

## API Contract

- OpenAPI is the source of truth for every Slipramp Server HTTP endpoint used by the web application or Controllers.
- Keep `api/openapi.yaml` small and split paths and schemas into a handful of domain-sized files. Group related operations and models; do not create one huge file or one file per endpoint or schema.
- Generate Go request/response types and Server interfaces, plus TypeScript types and the frontend client, from the same bundled contract.
- Never edit generated files by hand. Change the OpenAPI sources and regenerate all targets with the repository command.
- Pin generator versions, validate and bundle the specification in CI, and fail CI when generated output is stale.
- Standard requests use the generated frontend client. A small handwritten SSE transport may handle live-log reconnection while using generated event payload types.

## Technology and Quality

- Use the Go module path `github.com/slipramp/slipramp` and the latest stable Go release selected by the project.
- Use React Router v8 in Framework/SSR mode, TypeScript, and Mantine UI v9 for the web application.
- Use PostgreSQL directly for durable state and S3-compatible storage for artifact contents.
- Format and lint Go through golangci-lint v2 (`golangci-lint fmt` and `golangci-lint run`).
- Keep Go code spacious and readable rather than dense. Prefer short lines, focused functions, named intermediate values, and distinct chunks of logic over compressed expressions or large functions that mash unrelated work together.
- Use strict TypeScript, Oxlint, and Oxfmt. Do not add ESLint, Prettier, or Stylelint.
- Pin tool and dependency versions. Treat warnings as failures.
- Keep generated output reproducible and include proportionate unit and integration tests with changes.
- Prefer explicit argument arrays over an implicit shell. Shell behavior must be requested explicitly by the definition.
