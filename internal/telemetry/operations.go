package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/mcpserver"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/runreport"
)

type operationInstruments struct {
	repositoryRefreshAttempts      metric.Int64Counter
	repositoryRefreshDuration      metric.Float64Histogram
	gitTriggerDeliveryAttempts     metric.Int64Counter
	gitTriggerDeliveryDuration     metric.Float64Histogram
	notificationDeliveryAttempts   metric.Int64Counter
	notificationDeliveryDuration   metric.Float64Histogram
	artifactReconciliationRuns     metric.Int64Counter
	artifactReconciliationDuration metric.Float64Histogram
	artifactReconciliationItems    metric.Int64Counter
	artifactReportIngestions       metric.Int64Counter
	artifactReportDuration         metric.Float64Histogram
	mcpToolCalls                   metric.Int64Counter
	mcpToolDuration                metric.Float64Histogram
}

func newOperationInstruments(meter metric.Meter) (operationInstruments, error) {
	var instruments operationInstruments
	var err error

	instruments.repositoryRefreshAttempts, err = meter.Int64Counter(
		"slipramp.repository.refresh.attempts",
		metric.WithUnit("{repository}"),
		metric.WithDescription("Repository refresh attempts by durable result."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Repository refresh attempts metric: %w", err)
	}
	instruments.repositoryRefreshDuration, err = meter.Float64Histogram(
		"slipramp.repository.refresh.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Repository refresh attempts."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Repository refresh duration metric: %w", err)
	}
	instruments.gitTriggerDeliveryAttempts, err = meter.Int64Counter(
		"slipramp.git_trigger.delivery.attempts",
		metric.WithUnit("{delivery}"),
		metric.WithDescription("Git-trigger delivery attempts by durable result."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Git-trigger delivery attempts metric: %w", err)
	}
	instruments.gitTriggerDeliveryDuration, err = meter.Float64Histogram(
		"slipramp.git_trigger.delivery.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Git-trigger delivery attempts."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Git-trigger delivery duration metric: %w", err)
	}
	instruments.notificationDeliveryAttempts, err = meter.Int64Counter(
		"slipramp.notification.delivery.attempts",
		metric.WithUnit("{delivery}"),
		metric.WithDescription("Notification delivery attempts by durable result."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create notification delivery attempts metric: %w", err)
	}
	instruments.notificationDeliveryDuration, err = meter.Float64Histogram(
		"slipramp.notification.delivery.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of notification delivery attempts."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create notification delivery duration metric: %w", err)
	}
	instruments.artifactReconciliationRuns, err = meter.Int64Counter(
		"slipramp.artifact.reconciliation.runs",
		metric.WithUnit("{run}"),
		metric.WithDescription("Artifact reconciliation runs by result."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Artifact reconciliation runs metric: %w", err)
	}
	instruments.artifactReconciliationDuration, err = meter.Float64Histogram(
		"slipramp.artifact.reconciliation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Artifact reconciliation runs."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Artifact reconciliation duration metric: %w", err)
	}
	instruments.artifactReconciliationItems, err = meter.Int64Counter(
		"slipramp.artifact.reconciliation.items",
		metric.WithUnit("{item}"),
		metric.WithDescription("Items completed by Artifact reconciliation operation."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Artifact reconciliation items metric: %w", err)
	}
	instruments.artifactReportIngestions, err = meter.Int64Counter(
		"slipramp.artifact.report.ingestions",
		metric.WithUnit("{report}"),
		metric.WithDescription("Run report ingestion attempts by report kind and result."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Artifact report ingestions metric: %w", err)
	}
	instruments.artifactReportDuration, err = meter.Float64Histogram(
		"slipramp.artifact.report.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Run report ingestion attempts."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create Artifact report duration metric: %w", err)
	}
	instruments.mcpToolCalls, err = meter.Int64Counter(
		"slipramp.mcp.tool.calls",
		metric.WithUnit("{call}"),
		metric.WithDescription("MCP tool calls by tool and result."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create MCP tool calls metric: %w", err)
	}
	instruments.mcpToolDuration, err = meter.Float64Histogram(
		"slipramp.mcp.tool.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of MCP tool calls."),
	)
	if err != nil {
		return operationInstruments{}, fmt.Errorf("create MCP tool duration metric: %w", err)
	}

	return instruments, nil
}

func (t *Telemetry) ObserveMCPToolCall(
	ctx context.Context,
	tool string,
	outcome mcpserver.ToolCallOutcome,
	duration time.Duration,
) {
	options := metric.WithAttributes(
		attribute.String("tool", tool),
		attribute.String("result", string(outcome)),
	)
	t.operations.mcpToolCalls.Add(ctx, 1, options)
	t.operations.mcpToolDuration.Record(ctx, duration.Seconds(), options)
}

func (t *Telemetry) ObserveReportIngestion(
	ctx context.Context,
	kind runreport.ReportKind,
	result artifact.ReportIngestionOutcome,
	duration time.Duration,
) {
	options := metric.WithAttributes(
		attribute.String("report_kind", string(kind)),
		attribute.String("result", string(result)),
	)
	t.operations.artifactReportIngestions.Add(ctx, 1, options)
	t.operations.artifactReportDuration.Record(ctx, duration.Seconds(), options)
}

func (t *Telemetry) ObserveRepositoryRefresh(
	ctx context.Context,
	result repository.RefreshAttemptResult,
	duration time.Duration,
) {
	options := metric.WithAttributes(attribute.String("result", string(result)))
	t.operations.repositoryRefreshAttempts.Add(ctx, 1, options)
	t.operations.repositoryRefreshDuration.Record(ctx, duration.Seconds(), options)
}

func (t *Telemetry) ObserveGitTriggerDelivery(
	ctx context.Context,
	result gittrigger.DeliveryAttemptResult,
	duration time.Duration,
) {
	options := metric.WithAttributes(attribute.String("result", string(result)))
	t.operations.gitTriggerDeliveryAttempts.Add(ctx, 1, options)
	t.operations.gitTriggerDeliveryDuration.Record(ctx, duration.Seconds(), options)
}

func (t *Telemetry) ObserveNotificationDelivery(
	ctx context.Context,
	result notification.DeliveryAttemptResult,
	duration time.Duration,
) {
	options := metric.WithAttributes(attribute.String("result", string(result)))
	t.operations.notificationDeliveryAttempts.Add(ctx, 1, options)
	t.operations.notificationDeliveryDuration.Record(ctx, duration.Seconds(), options)
}

func (t *Telemetry) ObserveArtifactReconciliation(
	ctx context.Context,
	result artifact.ReconciliationOutcome,
	duration time.Duration,
	items artifact.ReconciliationResult,
) {
	resultOptions := metric.WithAttributes(attribute.String("result", string(result)))
	t.operations.artifactReconciliationRuns.Add(ctx, 1, resultOptions)
	t.operations.artifactReconciliationDuration.Record(ctx, duration.Seconds(), resultOptions)

	t.recordReconciliationItems(ctx, "expired", items.ExpiredArtifacts)
	t.recordReconciliationItems(ctx, "deleted", items.DeletedArtifacts)
	t.recordReconciliationItems(ctx, "orphan_deleted", items.DeletedOrphans)
}

func (t *Telemetry) recordReconciliationItems(ctx context.Context, operation string, count int) {
	if count <= 0 {
		return
	}

	t.operations.artifactReconciliationItems.Add(
		ctx,
		int64(count),
		metric.WithAttributes(attribute.String("operation", operation)),
	)
}
