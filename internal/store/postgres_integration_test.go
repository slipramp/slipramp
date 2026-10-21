package store

import "testing"

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	ctx, cfg, database := openIntegrationDatabase(t)
	database.Close()

	database, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	database.Close()
}
