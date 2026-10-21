package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
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
	configureIntegrationOIDC(t, &cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()
}
