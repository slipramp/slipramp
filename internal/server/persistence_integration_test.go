package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/api"
)

func TestPersistenceIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	s3Endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if databaseURL == "" || s3Endpoint == "" {
		t.Skip("integration test environment is not configured")
	}

	cfg := DefaultConfig()
	cfg.Database.URL = databaseURL
	cfg.ObjectStore.Endpoint = s3Endpoint
	cfg.ObjectStore.Region = os.Getenv("TEST_S3_REGION")
	cfg.ObjectStore.Bucket = os.Getenv("TEST_S3_BUCKET")
	cfg.ObjectStore.AccessKeyID = os.Getenv("TEST_S3_ACCESS_KEY_ID")
	cfg.ObjectStore.SecretAccessKey = os.Getenv("TEST_S3_SECRET_ACCESS_KEY")
	cfg.ArtifactTokenSecret = testArtifactTokenSecret
	configureIntegrationOIDC(t, &cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(ctx, cfg, logger, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("readiness status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if responseBody := response.Body.String(); responseBody == "" {
		t.Fatal("readiness response body is empty")
	}
	httpResponse := response.Result()
	defer func() {
		_ = httpResponse.Body.Close()
	}()
	readinessResponse, err := api.ParseGetReadinessResponse(httpResponse)
	if err != nil {
		t.Fatalf("parse readiness response: %v", err)
	}
	if readinessResponse.JSON200 == nil || readinessResponse.JSON200.Status != api.Ready {
		t.Errorf("readiness response = %#v, want ready", readinessResponse.JSON200)
	}

	metricsRequest := httptest.NewRequestWithContext(ctx, http.MethodGet, "/metrics", nil)
	metricsResponse := httptest.NewRecorder()
	app.telemetry.Handler().ServeHTTP(metricsResponse, metricsRequest)
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", metricsResponse.Code, http.StatusOK)
	}
	metricsBody := metricsResponse.Body.String()
	if !strings.Contains(metricsBody, "target_info") ||
		!strings.Contains(metricsBody, `service_name="slipramp-server"`) {
		t.Errorf("metrics response does not contain the Slipramp resource: %s", metricsBody)
	}
	if strings.Contains(metricsBody, `http_route="/metrics"`) {
		t.Error("metrics endpoint instrumented itself")
	}
	if !strings.Contains(metricsBody, `http_route="/readyz"`) {
		t.Errorf("metrics response does not contain the readiness route template: %s", metricsBody)
	}
	if strings.Contains(metricsBody, `http_route="/"`) {
		t.Error("readiness metrics use the root fallback instead of the matched route template")
	}

	applicationMetricsResponse := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(applicationMetricsResponse, metricsRequest)
	if applicationMetricsResponse.Code != http.StatusNotFound {
		t.Fatalf(
			"application metrics status = %d, want %d",
			applicationMetricsResponse.Code,
			http.StatusNotFound,
		)
	}
}
