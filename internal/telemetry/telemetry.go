// Package telemetry owns Slipramp Server metrics construction and export.
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

	"github.com/slipramp/slipramp/internal/store"
)

const (
	instrumentationScope = "github.com/slipramp/slipramp/internal/telemetry"
	cardinalityLimit     = 256
)

type SnapshotStore interface {
	LoadMetricsSnapshot(context.Context, time.Time) (store.MetricsSnapshot, error)
	DatabasePoolMetrics() store.DatabasePoolMetrics
}

type Config struct {
	ServiceVersion string
	Store          SnapshotStore
	Logger         *slog.Logger
}

type Telemetry struct {
	provider       *sdkmetric.MeterProvider
	registry       *prometheus.Registry
	handler        http.Handler
	operations     operationInstruments
	snapshots      *snapshotPublisher
	shutdownOnce   sync.Once
	shutdownResult error
}

func New(ctx context.Context, config Config) (*Telemetry, error) {
	if config.Store == nil {
		return nil, errors.New("telemetry snapshot store is required")
	}
	if config.Logger == nil {
		return nil, errors.New("telemetry logger is required")
	}

	telemetryResource, err := newResource(ctx, config.ServiceVersion)
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

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(telemetryResource),
		sdkmetric.WithReader(exporter),
		sdkmetric.WithCardinalityLimit(cardinalityLimit),
		sdkmetric.WithView(metricViews()...),
	)
	if err := otelruntime.Start(otelruntime.WithMeterProvider(provider)); err != nil {
		_ = provider.Shutdown(context.Background())

		return nil, fmt.Errorf("start Go runtime metrics: %w", err)
	}

	meter := provider.Meter(
		instrumentationScope,
		metric.WithSchemaURL(semconv.SchemaURL),
	)
	operations, err := newOperationInstruments(meter)
	if err != nil {
		_ = provider.Shutdown(context.Background())

		return nil, err
	}
	snapshots, err := newSnapshotPublisher(meter, config.Store, config.Logger)
	if err != nil {
		_ = provider.Shutdown(context.Background())

		return nil, err
	}

	return &Telemetry{
		provider:   provider,
		registry:   registry,
		handler:    promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true}),
		operations: operations,
		snapshots:  snapshots,
	}, nil
}

func newResource(ctx context.Context, serviceVersion string) (*resource.Resource, error) {
	attributes := []attribute.KeyValue{
		semconv.ServiceName("slipramp-server"),
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

func (t *Telemetry) Handler() http.Handler {
	return t.handler
}

func (t *Telemetry) InstrumentHTTP(handler http.Handler) http.Handler {
	return otelhttp.NewHandler(
		handler,
		"HTTP",
		otelhttp.WithMeterProvider(t.provider),
		otelhttp.WithFilter(func(request *http.Request) bool {
			return request.URL.Path != "/metrics"
		}),
	)
}

func (t *Telemetry) RunSnapshotWorker(ctx context.Context) {
	t.snapshots.Run(ctx)
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	t.shutdownOnce.Do(func() {
		t.shutdownResult = t.provider.Shutdown(ctx)
	})

	return t.shutdownResult
}

func metricViews() []sdkmetric.View {
	httpAttributes := func(value attribute.KeyValue) bool {
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

	return []sdkmetric.View{
		histogramView(
			"http.server.request.duration",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10},
			httpAttributes,
		),
		histogramView(
			"http.server.request.body.size",
			[]float64{0, 1024, 4 * 1024, 16 * 1024, 64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024,
				16 * 1024 * 1024, 64 * 1024 * 1024},
			httpAttributes,
		),
		histogramView(
			"http.server.response.body.size",
			[]float64{0, 1024, 4 * 1024, 16 * 1024, 64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024,
				16 * 1024 * 1024, 64 * 1024 * 1024},
			httpAttributes,
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
		attributeView("slipramp.repository.refresh.attempts", resultAttribute),
		attributeView("slipramp.git_trigger.delivery.attempts", resultAttribute),
		attributeView("slipramp.notification.delivery.attempts", resultAttribute),
		attributeView("slipramp.artifact.reconciliation.runs", resultAttribute),
		attributeView("slipramp.artifact.reconciliation.items", operationAttribute),
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
