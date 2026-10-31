// Package telemetry owns Slipramp process metrics construction and export.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const (
	instrumentationScope = "github.com/slipramp/slipramp/internal/telemetry"
	cardinalityLimit     = 256
)

type SnapshotSource interface {
	LoadMetricsSnapshot(context.Context, time.Time) (MetricsSnapshot, error)
	DatabasePoolMetrics() DatabasePoolMetrics
}

type Config struct {
	ServiceVersion string
	Source         SnapshotSource
	Logger         *slog.Logger
}

type RuntimeConfig struct {
	ServiceName    string
	ServiceVersion string
}

type Runtime struct {
	provider       *sdkmetric.MeterProvider
	registry       *prometheus.Registry
	handler        http.Handler
	shutdownOnce   sync.Once
	shutdownResult error
}

type Telemetry struct {
	*Runtime
	operations operationInstruments
	snapshots  *snapshotPublisher
}

func New(ctx context.Context, config Config) (*Telemetry, error) {
	if config.Source == nil {
		return nil, errors.New("telemetry snapshot source is required")
	}
	if config.Logger == nil {
		return nil, errors.New("telemetry logger is required")
	}

	runtimeTelemetry, err := newRuntime(ctx, RuntimeConfig{
		ServiceName:    "slipramp-server",
		ServiceVersion: config.ServiceVersion,
	}, metricViews())
	if err != nil {
		return nil, err
	}

	meter := runtimeTelemetry.provider.Meter(
		instrumentationScope,
		metric.WithSchemaURL(semconv.SchemaURL),
	)
	operations, err := newOperationInstruments(meter)
	if err != nil {
		_ = runtimeTelemetry.Shutdown(context.Background())

		return nil, err
	}
	snapshots, err := newSnapshotPublisher(meter, config.Source, config.Logger)
	if err != nil {
		_ = runtimeTelemetry.Shutdown(context.Background())

		return nil, err
	}

	return &Telemetry{
		Runtime:    runtimeTelemetry,
		operations: operations,
		snapshots:  snapshots,
	}, nil
}

func NewRuntime(ctx context.Context, config RuntimeConfig) (*Runtime, error) {
	return newRuntime(ctx, config, runtimeMetricViews())
}

func newRuntime(ctx context.Context, config RuntimeConfig, views []sdkmetric.View) (*Runtime, error) {
	if strings.TrimSpace(config.ServiceName) == "" {
		return nil, errors.New("telemetry service name is required")
	}

	telemetryResource, err := newResource(ctx, config.ServiceName, config.ServiceVersion)
	if err != nil {
		return nil, err
	}

	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, fmt.Errorf("register process metrics: %w", err)
	}

	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(registry),
		otelprometheus.WithProducer(otelruntime.NewProducer()),
		otelprometheus.WithoutScopeInfo(),
		otelprometheus.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
	)
	if err != nil {
		return nil, fmt.Errorf("create Prometheus metric reader: %w", err)
	}

	providerOptions := []sdkmetric.Option{
		sdkmetric.WithResource(telemetryResource),
		sdkmetric.WithReader(exporter),
		sdkmetric.WithCardinalityLimit(cardinalityLimit),
	}
	if len(views) > 0 {
		providerOptions = append(providerOptions, sdkmetric.WithView(views...))
	}
	provider := sdkmetric.NewMeterProvider(providerOptions...)
	if err := otelruntime.Start(otelruntime.WithMeterProvider(provider)); err != nil {
		_ = provider.Shutdown(context.Background())

		return nil, fmt.Errorf("start Go runtime metrics: %w", err)
	}

	return &Runtime{
		provider: provider,
		registry: registry,
		handler:  promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true}),
	}, nil
}

func newResource(ctx context.Context, serviceName, serviceVersion string) (*resource.Resource, error) {
	attributes := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
		semconv.ServiceNamespace("slipramp"),
	}
	if version := strings.TrimSpace(serviceVersion); version != "" {
		attributes = append(attributes, semconv.ServiceVersion(version))
	}

	telemetryResource, err := resource.New(
		ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(attributes...),
	)
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}

	return telemetryResource, nil
}

func (t *Runtime) Handler() http.Handler {
	return t.handler
}

func (t *Runtime) InstrumentHTTPTransport(transport http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(
		transport,
		otelhttp.WithMeterProvider(t.provider),
	)
}

func (t *Telemetry) InstrumentHTTP(handler http.Handler) http.Handler {
	return otelhttp.NewHandler(
		handler,
		"HTTP",
		otelhttp.WithMeterProvider(t.provider),
	)
}

func (t *Telemetry) RunSnapshotWorker(ctx context.Context) {
	t.snapshots.Run(ctx)
}

func (t *Runtime) Shutdown(ctx context.Context) error {
	t.shutdownOnce.Do(func() {
		t.shutdownResult = t.provider.Shutdown(ctx)
	})

	return t.shutdownResult
}

func metricViews() []sdkmetric.View {
	httpServerAttributes := func(value attribute.KeyValue) bool {
		switch value.Key {
		case semconv.HTTPRequestMethodKey,
			semconv.URLSchemeKey,
			semconv.HTTPResponseStatusCodeKey,
			semconv.HTTPRouteKey,
			semconv.NetworkProtocolNameKey,
			semconv.NetworkProtocolVersionKey,
			semconv.ErrorTypeKey:
			return true
		default:
			return false
		}
	}
	resultAttribute := func(value attribute.KeyValue) bool {
		return value.Key == attribute.Key("result")
	}
	operationAttribute := func(value attribute.KeyValue) bool {
		return value.Key == attribute.Key("operation")
	}
	reportIngestionAttributes := func(value attribute.KeyValue) bool {
		switch value.Key {
		case attribute.Key("report_kind"), attribute.Key("result"):
			return true
		default:
			return false
		}
	}

	views := runtimeMetricViews()

	return append(views,
		histogramView(
			"http.server.request.duration",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10},
			httpServerAttributes,
		),
		histogramView(
			"http.server.request.body.size",
			[]float64{0, 1024, 4 * 1024, 16 * 1024, 64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024,
				16 * 1024 * 1024, 64 * 1024 * 1024},
			httpServerAttributes,
		),
		histogramView(
			"http.server.response.body.size",
			[]float64{0, 1024, 4 * 1024, 16 * 1024, 64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024,
				16 * 1024 * 1024, 64 * 1024 * 1024},
			httpServerAttributes,
		),
		histogramView(
			"slipramp.repository.refresh.duration",
			[]float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
			resultAttribute,
		),
		histogramView(
			"slipramp.git_trigger.delivery.duration",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			resultAttribute,
		),
		histogramView(
			"slipramp.notification.delivery.duration",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			resultAttribute,
		),
		histogramView(
			"slipramp.artifact.reconciliation.duration",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
			resultAttribute,
		),
		histogramView(
			"slipramp.artifact.report.duration",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			reportIngestionAttributes,
		),
		attributeView("slipramp.repository.refresh.attempts", resultAttribute),
		attributeView("slipramp.git_trigger.delivery.attempts", resultAttribute),
		attributeView("slipramp.notification.delivery.attempts", resultAttribute),
		attributeView("slipramp.artifact.reconciliation.runs", resultAttribute),
		attributeView("slipramp.artifact.reconciliation.items", operationAttribute),
		attributeView("slipramp.artifact.report.ingestions", reportIngestionAttributes),
	)
}

func runtimeMetricViews() []sdkmetric.View {
	httpClientAttributes := func(value attribute.KeyValue) bool {
		switch value.Key {
		case semconv.HTTPRequestMethodKey,
			semconv.URLSchemeKey,
			semconv.ServerAddressKey,
			semconv.ServerPortKey,
			semconv.HTTPResponseStatusCodeKey,
			semconv.NetworkProtocolNameKey,
			semconv.NetworkProtocolVersionKey,
			semconv.ErrorTypeKey:
			return true
		default:
			return false
		}
	}

	return []sdkmetric.View{
		histogramView(
			"http.client.request.duration",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10, 15, 30, 60},
			httpClientAttributes,
		),
		histogramView(
			"http.client.request.body.size",
			[]float64{0, 1024, 4 * 1024, 16 * 1024, 64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024,
				16 * 1024 * 1024, 64 * 1024 * 1024},
			httpClientAttributes,
		),
	}
}

func histogramView(name string, boundaries []float64, filter attribute.Filter) sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: name, Kind: sdkmetric.InstrumentKindHistogram},
		sdkmetric.Stream{
			Aggregation:     sdkmetric.AggregationExplicitBucketHistogram{Boundaries: boundaries},
			AttributeFilter: filter,
		},
	)
}

func attributeView(name string, filter attribute.Filter) sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: name},
		sdkmetric.Stream{AttributeFilter: filter},
	)
}
