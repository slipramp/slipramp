package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	ctx, cfg, database := openIntegrationDatabase(t)
	database.Close()

	database, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	database.Close()
}

func openIntegrationDatabase(t *testing.T) (context.Context, Config, *Database) {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	databaseName := fmt.Sprintf("slipramp_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{databaseName}.Sanitize()

	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		if _, err := adminPool.Exec(cleanupContext, "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	cfg := DefaultConfig()
	cfg.URL = databaseURLWithName(t, databaseURL, databaseName)

	database, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return ctx, cfg, database
}

func databaseURLWithName(t *testing.T, databaseURL, databaseName string) string {
	t.Helper()

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}

	parsedURL.Path = "/" + databaseName

	return parsedURL.String()
}
