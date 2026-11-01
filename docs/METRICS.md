# Metrics

The telemetry path is deliberately short:

```text
Server HTTP/runtime/domain observers -> Server OTel SDK MeterProvider
Controller runtime/client instrumentation -> Controller OTel SDK MeterProvider
    -> in-process Prometheus Reader on a private registry
    -> GET /metrics -> Prometheus
```

The Slipramp Server exposes Prometheus/OpenMetrics text at `GET /metrics` on a
dedicated listener, defaulting to port `9091`. The endpoint is operational and
unauthenticated; expose it only to a trusted monitoring network. The central
Helm chart exposes the listener only on the internal Server Service and does not
route it through the public Ingress or HTTPRoute:

```yaml
scrape_configs:
  - job_name: slipramp-server
    metrics_path: /metrics
    static_configs:
      - targets: [slipramp-server.slipramp-system.svc:9091]
```

For annotation-based discovery, set the Server Service annotations shown in
the [chart operations guide](../deploy/charts/slipramp/README.md#ingress-httproute-and-operation).

Each Controller exposes its own Prometheus/OpenMetrics endpoint at
`GET /metrics` on the shared metrics-server implementation, defaulting to port
`9091`. The Controller chart creates an internal Service for that port; it has
no public Ingress. Scrape the Controller inside its execution Cluster so process
health remains observable when the Controller cannot reach the Server:

```yaml
scrape_configs:
  - job_name: slipramp-controller
    metrics_path: /metrics
    static_configs:
      - targets: [slipramp-controller.slipramp-system.svc:9091]
```

The Controller emits Resource metadata, outbound Slipramp HTTP client metrics,
and the Go runtime and native process metrics cataloged below.
Controller-specific operation metrics remain future work.

## Local Prometheus and Grafana

`task local:up` provisions Prometheus `v3.15.1` and Grafana `13.2.4` alongside
the other development services. Prometheus scrapes the host-running Server
metrics listener at `host.docker.internal:9091` every 15 seconds and retains
seven days of local data. Docker's host-gateway mapping makes the same target
work on Linux and Docker Desktop.

Prometheus is available at `http://localhost:9090`. Grafana is available at
`http://localhost:3000` with anonymous viewer access, and opens the versioned
[`Slipramp Server` dashboard](../deploy/local/grafana/dashboards/slipramp-server.json)
from the `Slipramp` folder. The dashboard covers snapshot health, current
state, worker outcomes and latency, HTTP traffic, the Go runtime, process
health, and the PostgreSQL pool. Its Prometheus data source and dashboard are
provisioned from `deploy/local/grafana`, so no interactive setup is required.

The monitoring services are developer-only. The integration task starts the
core PostgreSQL, RustFS, and Keycloak dependencies directly and does not start or
pull Prometheus or Grafana in CI.

The Server and each Controller use separate private Prometheus registries, so
imported libraries cannot silently add collectors to this contract.
OpenTelemetry is the application instrumentation API. Metric names below are
the Prometheus names after the OpenTelemetry Prometheus translator applies unit
suffixes. All OpenTelemetry series expose resource metadata through
`target_info`, including `service_name="slipramp-server"` or
`service_name="slipramp-controller"`, `service_namespace="slipramp"`, and the
build's `service_version` when available. Release image builds inject the
release version; source builds use `dev`.

The dependency baseline is OpenTelemetry Go `v1.47.0`, Go instrumentation
`v0.72.0`, and semantic conventions `v1.43.0`. The HTTP conventions are
stable. The Go runtime and database client connection conventions are
development status and are adopted deliberately here; process metrics are
native Prometheus collector names. Slipramp owns the `slipramp_` names. It does
not add an exporter namespace. The Resource and Slipramp instrumentation scope
record schema URL `https://opentelemetry.io/schemas/1.43.0`.

The Prometheus exporter is pinned at pre-1.0 version `v0.69.0`. Its Reader and
name translation are therefore not covered by the OTel Go 1.x stability
guarantee. Slipramp pins its underscore-escaping-with-suffixes translation
strategy and treats the names in this document as the deployment contract;
dependency upgrades must verify and update the catalog intentionally.

## HTTP server metrics

HTTP telemetry comes from the maintained OpenTelemetry `otelhttp` middleware.
The metrics listener is separate from the instrumented application listener,
so scraping cannot inflate traffic measurements. `http_route` contains the
matched Go route template, never the raw request path.

| OTel instrument | Prometheus metric | Type / UCUM unit | Attributes | Origin / status |
| --- | --- | --- | --- | --- |
| `http.server.request.duration` | `http_server_request_duration_seconds` | histogram / `s` | HTTP set below | OTel HTTP / stable |
| `http.server.request.body.size` | `http_server_request_body_size_bytes` | histogram / `By` | HTTP set below | OTel HTTP / stable |
| `http.server.response.body.size` | `http_server_response_body_size_bytes` | histogram / `By` | HTTP set below | OTel HTTP / stable |

The bounded HTTP set is `http.request.method`, `url.scheme`,
`http.response.status_code`, `http.route`, `network.protocol.name`,
`network.protocol.version`, and `error.type`. The exporter translates dots to
underscores in Prometheus label names. Method, scheme, protocol, status, and
error classifications come from the immediate request and response;
`http.route` is present only as a matched static route template. Slipramp does
not interpret forwarding headers as trusted client or scheme information.

The request-duration boundaries are 0.005, 0.01, 0.025, 0.05, 0.075, 0.1,
0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, and 10 seconds, matching the stable semantic
convention recommendation.

Both body-size histograms use explicit boundaries at 0, 1 KiB, 4 KiB,
16 KiB, 64 KiB, 256 KiB, 1 MiB, 4 MiB, 16 MiB, and 64 MiB.

## HTTP client metrics

The Controller instruments the generated Slipramp client at its HTTP transport
boundary. This covers enrollment, command polling, observations, log transfer,
and Kargo reports without coupling telemetry to generated client methods. Raw
paths are intentionally absent because those URLs can contain Run, Promotion,
and integration identifiers.

| OTel instrument | Prometheus metric | Type / UCUM unit | Attributes | Origin / status |
| --- | --- | --- | --- | --- |
| `http.client.request.duration` | `http_client_request_duration_seconds` | histogram / `s` | HTTP client set below | OTel HTTP / stable |
| `http.client.request.body.size` | `http_client_request_body_size_bytes` | histogram / `By` | HTTP client set below | OTel HTTP / stable |

The bounded client set is `http.request.method`, `url.scheme`,
`server.address`, `server.port`, `http.response.status_code`,
`network.protocol.name`, `network.protocol.version`, and `error.type`. Each
Controller has one statically configured Server destination. `error.type` is a
transport error classification, not error text.

Client duration boundaries are 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25,
0.5, 0.75, 1, 2.5, 5, 7.5, 10, 15, 30, and 60 seconds. The upper buckets cover
normal long polling and its 35-second client timeout. Request-body boundaries
match the HTTP server body-size boundaries above.

## Slipramp current-state metrics

A worker refreshes current state every 15 seconds under a two-second deadline.
All domain values in one refresh come from a read-only, repeatable-read
PostgreSQL transaction. The scrape callback only reads the last published
in-memory snapshot. A failed refresh sets
`slipramp_metrics_snapshot_success_ratio` to zero and retains the previous
values; it never publishes a partially refreshed view.

| OTel instrument | Prometheus metric | Type / UCUM unit | Attributes | Meaning |
| --- | --- | --- | --- | --- |
| `slipramp.run.active` | `slipramp_run_active` | observable gauge / `{run}` | `phase=assigned|preparing|running` | Active Runs in each bounded phase. |
| `slipramp.run.review.pending` | `slipramp_run_review_pending` | observable gauge / `{run}` | none | Queued Runs awaiting review. |
| `slipramp.run.queue.oldest_eligible.age` | `slipramp_run_queue_oldest_eligible_age_seconds` | observable gauge / `s` | none | Age of the oldest review-eligible queued Run not blocked by an active Run for its Target. Absent when none is eligible. |
| `slipramp.controller.connected` | `slipramp_controller_connected` | observable gauge / `{controller}` | none | Controllers whose latest contact is within the connection window. |
| `slipramp.controller.last_seen.time` | `slipramp_controller_last_seen_time_seconds` | observable gauge / `s` | none | Unix time of the latest Controller contact. Absent until one has been seen. |
| `slipramp.repository.refresh.state` | `slipramp_repository_refresh_state` | observable gauge / `{repository}` | `state=pending|succeeded|failed` | Active Repositories in each durable refresh state. |
| `slipramp.notification.delivery.state` | `slipramp_notification_delivery_state` | observable gauge / `{delivery}` | `state=pending|delivered|failed` | Retained notification deliveries in each durable state. |
| `slipramp.metrics.snapshot.success` | `slipramp_metrics_snapshot_success_ratio` | observable gauge / `1` | none | `1` when the latest refresh succeeded and `0` otherwise. |
| `slipramp.metrics.snapshot.last_success.time` | `slipramp_metrics_snapshot_last_success_time_seconds` | observable gauge / `s` | none | Unix time of the last successful refresh. Absent before the first success. |

Every metric in this table is a Slipramp-specific convention. Queue age uses
the same eligibility predicate and FIFO timestamp as dispatch: queued,
review-not-required or approved, and no active Run for the same Target.

## Slipramp operation metrics

Operation labels are fixed enums. They intentionally exclude identifiers,
names, commit SHAs, paths, URLs, diagnostics, and other unbounded values.

| OTel instrument | Prometheus metric | Type / UCUM unit | Bounded attributes |
| --- | --- | --- | --- |
| `slipramp.repository.refresh.attempts` | `slipramp_repository_refresh_attempts_total` | counter / `{repository}` | `result=succeeded|fetch_failed|catalog_rejected|persist_failed` |
| `slipramp.repository.refresh.duration` | `slipramp_repository_refresh_duration_seconds` | histogram / `s` | same `result` |
| `slipramp.git_trigger.delivery.attempts` | `slipramp_git_trigger_delivery_attempts_total` | counter / `{delivery}` | `result=created|superseded|retry_scheduled|terminal_failed|record_failed` |
| `slipramp.git_trigger.delivery.duration` | `slipramp_git_trigger_delivery_duration_seconds` | histogram / `s` | same `result` |
| `slipramp.notification.delivery.attempts` | `slipramp_notification_delivery_attempts_total` | counter / `{delivery}` | `result=delivered|retry_scheduled|terminal_failed|configuration_failed|record_failed` |
| `slipramp.notification.delivery.duration` | `slipramp_notification_delivery_duration_seconds` | histogram / `s` | same `result` |
| `slipramp.artifact.reconciliation.runs` | `slipramp_artifact_reconciliation_runs_total` | counter / `{run}` | `result=succeeded|failed` |
| `slipramp.artifact.reconciliation.duration` | `slipramp_artifact_reconciliation_duration_seconds` | histogram / `s` | same `result` |
| `slipramp.artifact.reconciliation.items` | `slipramp_artifact_reconciliation_items_total` | counter / `{item}` | `operation=expired|deleted|orphan_deleted` |
| `slipramp.artifact.report.ingestions` | `slipramp_artifact_report_ingestions_total` | counter / `{report}` | `report_kind=terraform-plan|opentofu-plan|pyinfra-deploy`, `result=succeeded|rejected|failed` |
| `slipramp.artifact.report.duration` | `slipramp_artifact_report_duration_seconds` | histogram / `s` | same `report_kind` and `result` |
| `slipramp.mcp.tool.calls` | `slipramp_mcp_tool_calls_total` | counter / `{call}` | `tool=list_targets|explain_target_action|create_run|get_run|read_run_log|list_run_artifacts|read_artifact|cancel_run`, `result=succeeded|rejected|failed` |
| `slipramp.mcp.tool.duration` | `slipramp_mcp_tool_duration_seconds` | histogram / `s` | same `tool` and `result` |

Every metric in this table is a Slipramp-specific convention. Background
operation attempts are recorded once after their durable result is known; idle
polls and normal shutdown cancellation do not increment those counters. An MCP
tool call is recorded once after its handler returns, including caller-visible
rejections and internal failures.

Repository refresh duration buckets are 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
30, 60, 120, and 300 seconds. Artifact reconciliation uses the same upper
range with additional 0.005, 0.01, and 0.025 second buckets. Delivery and
Artifact report ingestion and MCP tool duration buckets are 0.005, 0.01, 0.025,
0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, and 30 seconds.

## Go runtime and process metrics

The Server and Controller OpenTelemetry providers emit these development-status
Go runtime conventions by default:

| OTel instrument | Prometheus metric | Type / UCUM unit | Attributes or notes |
| --- | --- | --- | --- |
| `go.memory.used` | `go_memory_used_bytes` | observable up/down counter / `By` | `go.memory.type=stack|other` |
| `go.memory.limit` | `go_memory_limit_bytes` | observable up/down counter / `By` | Absent when no runtime memory limit exists. |
| `go.memory.allocated` | `go_memory_allocated_bytes_total` | observable counter / `By` | Cumulative heap bytes allocated. |
| `go.memory.allocations` | `go_memory_allocations_total` | observable counter / `{allocation}` | Cumulative heap allocations. |
| `go.memory.gc.goal` | `go_memory_gc_goal_bytes` | observable up/down counter / `By` | Current heap goal. |
| `go.goroutine.count` | `go_goroutine_count` | observable up/down counter / `{goroutine}` | Live goroutines. |
| `go.processor.limit` | `go_processor_limit` | observable up/down counter / `{thread}` | OS threads available to execute Go code. |
| `go.config.gogc` | `go_config_gogc_percent` | observable up/down counter / `%` | Configured GC target percentage. |
| `go.schedule.duration` | `go_schedule_duration_seconds` | histogram / `s` | Time runnable goroutines waited in the scheduler. |

All metrics in this table are OTel Go runtime development conventions supplied
by the maintained `go.opentelemetry.io/contrib/instrumentation/runtime` module.

Each private registry also registers the native Prometheus process collector:

| Prometheus metric | Type / unit | Notes |
| --- | --- | --- |
| `process_cpu_seconds_total` | counter / seconds | User and system CPU time. |
| `process_open_fds` | gauge / descriptors | Open file descriptors where supported. |
| `process_max_fds` | gauge / descriptors | File descriptor limit where supported. |
| `process_virtual_memory_bytes` | gauge / bytes | Virtual memory size. |
| `process_virtual_memory_max_bytes` | gauge / bytes | Maximum virtual memory where supported. |
| `process_resident_memory_bytes` | gauge / bytes | Resident memory size. |
| `process_start_time_seconds` | gauge / Unix seconds | Process start time. |
| `process_network_receive_bytes_total` | counter / bytes | Process network receive bytes where supported. |
| `process_network_transmit_bytes_total` | counter / bytes | Process network transmit bytes where supported. |

These are Prometheus-native metrics from `client_golang`'s maintained process
collector, not OTel semantic-convention instruments. Availability is
platform-dependent.

Neither process enables the OpenTelemetry runtime package's deprecated
`runtime.go.*` compatibility metrics.

## PostgreSQL pool metrics

Pool statistics come from pgx and are copied into the same periodic in-memory
snapshot. They therefore have the same bounded query-free scrape behavior and
retain their last good values when the state refresh fails.

| OTel instrument | Prometheus metric | Type / UCUM unit | Attributes | Origin / status |
| --- | --- | --- | --- | --- |
| `db.client.connection.count` | `db_client_connection_count` | observable up/down counter / `{connection}` | `db.client.connection.pool.name=primary`, `db.client.connection.state=used|idle` | OTel database pool / development |
| `db.client.connection.max` | `db_client_connection_max` | observable up/down counter / `{connection}` | `db.client.connection.pool.name=primary` | OTel database pool / development |
| `slipramp.database.pool.acquire.waits` | `slipramp_database_pool_acquire_waits_total` | observable counter / `{request}` | none | Slipramp-specific |
| `slipramp.database.pool.acquire.canceled` | `slipramp_database_pool_acquire_canceled_total` | observable counter / `{request}` | none | Slipramp-specific |
| `slipramp.database.pool.acquire.duration` | `slipramp_database_pool_acquire_duration_seconds_total` | observable counter / `s` | none | Slipramp-specific |

pgx reports cumulative acquisition waits, canceled acquisitions, and aggregate
acquisition time. Those values do not map to the OTel pool timeout and
per-acquisition wait-time instruments, so they retain explicit `slipramp.*`
names rather than borrowing incompatible `db.*` semantics.

## Export metadata and cardinality

`target_info` is the only additional exporter metadata metric. It is a
Prometheus gauge with value `1` and the OTel Resource attributes translated to
labels. Instrumentation scope metadata is disabled.

The SDK enforces a per-instrument cardinality limit of 256 as a final
guardrail. Product instruments use only the enum values listed above. Slipramp
does not emit IDs, Target or Repository names, action names, Git SHAs, URLs,
paths, notification destinations, diagnostics, or error text as metric
attributes. The HTTP server route attribute contains only matched route
templates. HTTP client metrics contain only the single configured Server
destination and may contain the OTel transport error classification. MCP
metrics contain only the eight registered tool names and three result classes;
Principal, credential, client, and resource identifiers remain log fields and
never become metric attributes.

## Example alerts

Thresholds are examples and should be tuned to the installation's refresh and
dispatch expectations.

Snapshot refresh has failed, never succeeded, or has been stale for a minute:

```promql
slipramp_metrics_snapshot_success_ratio == 0
or absent(slipramp_metrics_snapshot_last_success_time_seconds)
or time() - slipramp_metrics_snapshot_last_success_time_seconds > 60
```

No Controller is connected, or no Controller has checked in for a minute:

```promql
slipramp_controller_connected < 1
or absent(slipramp_controller_last_seen_time_seconds)
or time() - slipramp_controller_last_seen_time_seconds > 60
```

The oldest dispatch-eligible Run has waited more than five minutes:

```promql
slipramp_run_queue_oldest_eligible_age_seconds > 300
```

At least one active Repository is in the durable failed refresh state:

```promql
sum(slipramp_repository_refresh_state{state="failed"}) > 0
```
