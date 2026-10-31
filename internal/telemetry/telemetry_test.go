package telemetry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/runreport"
)

func TestTelemetryExportsPrivateSemanticMetrics(t *testing.T) {
	globalMarker := prometheus.NewGauge(prometheus.GaugeOpts{Name: "slipramp_test_global_registry_marker"})
	if err := prometheus.Register(globalMarker); err != nil {
		t.Fatalf("register global metric: %v", err)
	}
	t.Cleanup(func() {
		prometheus.Unregister(globalMarker)
	})
	globalMarker.Set(1)

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	oldest := now.Add(-90 * time.Second)
	lastSeen := now.Add(-10 * time.Second)
	snapshotSource := &testSnapshotSource{
		snapshot: MetricsSnapshot{
			ActiveRuns:                 map[string]int64{"running": 2},
			PendingRunReviews:          3,
			OldestEligibleRunCreatedAt: &oldest,
			ConnectedControllers:       1,
			ControllerLastSeenAt:       &lastSeen,
			RepositoryRefreshStates:    map[string]int64{"succeeded": 4},
			NotificationDeliveryStates: map[string]int64{"pending": 5},
		},
		pool: DatabasePoolMetrics{
			AcquiredConnections: 2,
			IdleConnections:     3,
			MaximumConnections:  10,
			AcquireWaits:        7,
			CanceledAcquires:    1,
			AcquireDuration:     4 * time.Second,
		},
	}
	metrics, err := New(t.Context(), Config{
		ServiceVersion: "test-version",
		Source:         snapshotSource,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := metrics.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	})
	metrics.snapshots.now = func() time.Time { return now }
	initialScrape := scrape(t, metrics.Handler())
	assertMetricLine(t, initialScrape, "slipramp_run_active", `phase="running"`, " 0")
	assertMetricLine(t, initialScrape, "slipramp_metrics_snapshot_success_ratio", " 0")
	metrics.snapshots.refresh(t.Context())
	if snapshotSource.snapshotCalls != 1 {
		t.Fatalf("snapshot refresh calls = %d, want 1", snapshotSource.snapshotCalls)
	}

	router := http.NewServeMux()
	router.Handle("GET /metrics", metrics.Handler())
	router.HandleFunc("GET /things/{thingID}", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	handler := metrics.InstrumentHTTP(router)

	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/things/unbounded-name", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("instrumented status = %d, want %d", response.Code, http.StatusNoContent)
	}

	metrics.ObserveRepositoryRefresh(t.Context(), repository.RefreshAttemptSucceeded, 150*time.Millisecond)
	metrics.ObserveGitTriggerDelivery(t.Context(), gittrigger.DeliveryAttemptRetryScheduled, 25*time.Millisecond)
	metrics.ObserveNotificationDelivery(t.Context(), notification.DeliveryAttemptDelivered, 50*time.Millisecond)
	metrics.ObserveArtifactReconciliation(
		t.Context(),
		artifact.ReconciliationSucceeded,
		2*time.Second,
		artifact.ReconciliationResult{ExpiredArtifacts: 2, DeletedArtifacts: 3, DeletedOrphans: 4},
	)
	metrics.ObserveReportIngestion(
		t.Context(),
		runreport.TerraformPlanKind,
		artifact.ReportIngestionSucceeded,
		75*time.Millisecond,
	)
	metrics.ObserveReportIngestion(
		t.Context(),
		runreport.OpenTofuPlanKind,
		artifact.ReportIngestionSucceeded,
		80*time.Millisecond,
	)

	firstScrape := scrape(t, handler)
	secondScrape := scrape(t, handler)
	if snapshotSource.snapshotCalls != 1 {
		t.Fatalf("snapshot calls after scrapes = %d, want 1", snapshotSource.snapshotCalls)
	}

	assertMetricLine(t, firstScrape, "target_info", `service_name="slipramp-server"`, `service_namespace="slipramp"`)
	assertMetricLine(t, firstScrape, "target_info", `service_version="test-version"`)
	assertMetricLine(
		t,
		firstScrape,
		"http_server_request_duration_seconds_count",
		`http_route="/things/{thingID}"`,
		`url_scheme="http"`,
		" 1",
	)
	assertMetricLine(t, firstScrape, "# TYPE http_server_request_duration_seconds histogram")
	assertMetricLine(t, firstScrape, "# TYPE slipramp_repository_refresh_attempts_total counter")
	assertMetricLine(t, firstScrape, "# TYPE slipramp_run_active gauge")
	assertMetricLine(t, firstScrape, "http_server_request_body_size_bytes_bucket", `le="1024"`)
	assertMetricLine(t, firstScrape, "http_server_response_body_size_bytes_bucket", `le="1024"`)
	assertMetricLine(t, secondScrape, "http_server_request_duration_seconds_count", `http_route="/things/{thingID}"`, " 1")
	if strings.Contains(firstScrape, "unbounded-name") {
		t.Fatal("HTTP metrics contain the raw URI path")
	}
	assertMetricLine(t, firstScrape, "slipramp_run_active", `phase="running"`, " 2")
	assertMetricLine(t, firstScrape, "slipramp_run_review_pending", " 3")
	assertMetricLine(t, firstScrape, "slipramp_run_queue_oldest_eligible_age_seconds", " 90")
	assertMetricLine(t, firstScrape, "slipramp_metrics_snapshot_success_ratio", " 1")
	assertMetricLine(
		t,
		firstScrape,
		"db_client_connection_count",
		`db_client_connection_pool_name="primary"`,
		`db_client_connection_state="used"`,
		" 2",
	)
	assertMetricLine(
		t,
		firstScrape,
		"db_client_connection_max",
		`db_client_connection_pool_name="primary"`,
		" 10",
	)
	assertMetricLine(t, firstScrape, "slipramp_repository_refresh_attempts_total", `result="succeeded"`, " 1")
	assertMetricLine(t, firstScrape, "slipramp_repository_refresh_duration_seconds_count", `result="succeeded"`, " 1")
	assertMetricLine(t, firstScrape, "slipramp_git_trigger_delivery_attempts_total", `result="retry_scheduled"`, " 1")
	assertMetricLine(t, firstScrape, "slipramp_notification_delivery_attempts_total", `result="delivered"`, " 1")
	assertMetricLine(t, firstScrape, "slipramp_artifact_reconciliation_runs_total", `result="succeeded"`, " 1")
	assertMetricLine(t, firstScrape, "slipramp_artifact_reconciliation_items_total", `operation="expired"`, " 2")
	assertMetricLine(t, firstScrape, "slipramp_artifact_reconciliation_items_total", `operation="deleted"`, " 3")
	assertMetricLine(t, firstScrape, "slipramp_artifact_reconciliation_items_total", `operation="orphan_deleted"`, " 4")
	assertMetricLine(
		t,
		firstScrape,
		"slipramp_artifact_report_ingestions_total",
		`report_kind="terraform-plan"`,
		`result="succeeded"`,
		" 1",
	)
	assertMetricLine(
		t,
		firstScrape,
		"slipramp_artifact_report_duration_seconds_count",
		`report_kind="terraform-plan"`,
		`result="succeeded"`,
		" 1",
	)
	assertMetricLine(
		t,
		firstScrape,
		"slipramp_artifact_report_ingestions_total",
		`report_kind="opentofu-plan"`,
		`result="succeeded"`,
		" 1",
	)
	assertMetricLine(t, firstScrape, "go_goroutine_count")
	assertMetricLine(t, firstScrape, "process_start_time_seconds")
	if strings.Contains(firstScrape, "slipramp_test_global_registry_marker") {
		t.Fatal("private registry included a collector from the Prometheus global registry")
	}

	lastSuccess := metricLine(t, firstScrape, "slipramp_metrics_snapshot_last_success_time_seconds")
	snapshotSource.err = errors.New("database unavailable")
	metrics.snapshots.now = func() time.Time { return now.Add(time.Minute) }
	metrics.snapshots.refresh(t.Context())
	failedScrape := scrape(t, handler)
	assertMetricLine(t, failedScrape, "slipramp_metrics_snapshot_success_ratio", " 0")
	assertMetricLine(t, failedScrape, "slipramp_run_active", `phase="running"`, " 2")
	if got := metricLine(t, failedScrape, "slipramp_metrics_snapshot_last_success_time_seconds"); got != lastSuccess {
		t.Errorf("last-success metric changed after failed refresh: %q, want %q", got, lastSuccess)
	}
}

func TestRuntimeExportsControllerResourceAndProcessMetrics(t *testing.T) {
	runtimeTelemetry, err := NewRuntime(t.Context(), RuntimeConfig{
		ServiceName:    "slipramp-controller",
		ServiceVersion: "test-version",
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	t.Cleanup(func() {
		if err := runtimeTelemetry.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	})

	exposition := scrape(t, runtimeTelemetry.Handler())
	assertMetricLine(
		t,
		exposition,
		"target_info",
		`service_name="slipramp-controller"`,
		`service_namespace="slipramp"`,
		`service_version="test-version"`,
	)
	assertMetricLine(t, exposition, "go_goroutine_count")
	assertMetricLine(t, exposition, "process_start_time_seconds")
	if strings.Contains(exposition, "slipramp_run_active") {
		t.Fatal("runtime telemetry unexpectedly contains Server product metrics")
	}
}

func TestRuntimeInstrumentsHTTPClient(t *testing.T) {
	t.Parallel()

	runtimeTelemetry, err := NewRuntime(t.Context(), RuntimeConfig{
		ServiceName:    "slipramp-controller",
		ServiceVersion: "test-version",
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	t.Cleanup(func() {
		if err := runtimeTelemetry.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	})

	transport := runtimeTelemetry.InstrumentHTTPTransport(roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/failed" {
			return nil, errors.New("connection failed")
		}
		_, _ = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()

		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       http.NoBody,
			Request:    request,
		}, nil
	}))
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"http://slipramp.example.com:8443/controller/v1/runs/unbounded-run-id/observations",
		bytes.NewBufferString("hello"),
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	failedRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://slipramp.example.com:8443/failed",
		http.NoBody,
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	failedResponse, err := client.Do(failedRequest)
	if failedResponse != nil {
		_ = failedResponse.Body.Close()
	}
	if err == nil {
		t.Fatal("failed request error = nil")
	}

	exposition := scrape(t, runtimeTelemetry.Handler())
	assertMetricLine(
		t,
		exposition,
		"http_client_request_duration_seconds_count",
		`http_request_method="POST"`,
		`http_response_status_code="204"`,
		`server_address="slipramp.example.com"`,
		`server_port="8443"`,
		`url_scheme="http"`,
		" 1",
	)
	assertMetricLine(t, exposition, "http_client_request_body_size_bytes_count", " 1")
	assertMetricLine(t, exposition, "http_client_request_body_size_bytes_sum", " 5")
	assertMetricLine(
		t,
		exposition,
		"http_client_request_duration_seconds_count",
		`error_type="errors.errorString"`,
		" 1",
	)
	if strings.Contains(exposition, "unbounded-run-id") {
		t.Fatal("HTTP client metrics contain the raw URI path")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRuntimeRequiresServiceName(t *testing.T) {
	_, err := NewRuntime(t.Context(), RuntimeConfig{})
	if err == nil || !strings.Contains(err.Error(), "service name") {
		t.Fatalf("NewRuntime() error = %v, want service-name validation", err)
	}
}

func TestHTTPInstrumentationPreservesResponseBehavior(t *testing.T) {
	metrics, err := New(t.Context(), Config{
		ServiceVersion: "test-version",
		Source:         &testSnapshotSource{},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := metrics.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	})

	var canceledRequestObserved bool
	router := http.NewServeMux()
	router.Handle("GET /metrics", metrics.Handler())
	router.HandleFunc("GET /implicit", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("implicit"))
	})
	router.HandleFunc("GET /explicit", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
	})
	router.HandleFunc("GET /stream", func(writer http.ResponseWriter, _ *http.Request) {
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("instrumented response writer does not implement http.Flusher")

			return
		}
		_, _ = writer.Write([]byte("first\n"))
		flusher.Flush()
		_, _ = writer.Write([]byte("second\n"))
	})
	router.HandleFunc("GET /canceled", func(writer http.ResponseWriter, request *http.Request) {
		canceledRequestObserved = errors.Is(request.Context().Err(), context.Canceled)
		writer.WriteHeader(http.StatusNoContent)
	})
	handler := metrics.InstrumentHTTP(router)

	implicit := serveRequest(t, handler, "/implicit", t.Context())
	if implicit.Code != http.StatusOK || implicit.Body.String() != "implicit" {
		t.Errorf("implicit response = %d %q", implicit.Code, implicit.Body.String())
	}
	explicit := serveRequest(t, handler, "/explicit", t.Context())
	if explicit.Code != http.StatusTeapot {
		t.Errorf("explicit response status = %d, want %d", explicit.Code, http.StatusTeapot)
	}
	stream := serveRequest(t, handler, "/stream", t.Context())
	if stream.Code != http.StatusOK || !stream.Flushed || stream.Body.String() != "first\nsecond\n" {
		t.Errorf("stream response = status %d, flushed %t, body %q", stream.Code, stream.Flushed, stream.Body.String())
	}
	canceledContext, cancel := context.WithCancel(t.Context())
	cancel()
	canceled := serveRequest(t, handler, "/canceled", canceledContext)
	if canceled.Code != http.StatusNoContent || !canceledRequestObserved {
		t.Errorf("canceled response = status %d, observed %t", canceled.Code, canceledRequestObserved)
	}

	exposition := scrape(t, handler)
	assertMetricLine(
		t,
		exposition,
		"http_server_request_duration_seconds_count",
		`http_route="/implicit"`,
		`http_response_status_code="200"`,
		" 1",
	)
	assertMetricLine(
		t,
		exposition,
		"http_server_request_duration_seconds_count",
		`http_route="/explicit"`,
		`http_response_status_code="418"`,
		" 1",
	)
	assertMetricLine(t, exposition, "http_server_request_duration_seconds_count", `http_route="/stream"`, " 1")
}

type testSnapshotSource struct {
	snapshot      MetricsSnapshot
	pool          DatabasePoolMetrics
	err           error
	snapshotCalls int
}

func (s *testSnapshotSource) LoadMetricsSnapshot(context.Context, time.Time) (MetricsSnapshot, error) {
	s.snapshotCalls++

	return s.snapshot, s.err
}

func (s *testSnapshotSource) DatabasePoolMetrics() DatabasePoolMetrics {
	return s.pool
}

func scrape(t *testing.T, handler http.Handler) string {
	t.Helper()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", response.Code, http.StatusOK)
	}

	return response.Body.String()
}

func serveRequest(t *testing.T, handler http.Handler, path string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()

	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	handler.ServeHTTP(response, request)

	return response
}

func assertMetricLine(t *testing.T, exposition string, fragments ...string) {
	t.Helper()

	for line := range strings.SplitSeq(exposition, "\n") {
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}

	t.Errorf("metrics output has no line containing %q", fragments)
}

func metricLine(t *testing.T, exposition string, name string) string {
	t.Helper()

	for line := range strings.SplitSeq(exposition, "\n") {
		if strings.HasPrefix(line, name+" ") || strings.HasPrefix(line, name+"{") {
			return line
		}
	}

	t.Fatalf("metrics output has no %s line", name)

	return ""
}
