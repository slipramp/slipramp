package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const migrationTable = "public.slipramp_schema_migrations"

//go:embed migrations
var embeddedMigrations embed.FS

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) (returnErr error) {
	if pool == nil {
		return errors.New("PostgreSQL pool is required")
	}

	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	database := stdlib.OpenDBFromPool(pool)
	defer func() {
		if err := database.Close(); err != nil {
			closeErr := fmt.Errorf("close Goose database adapter: %w", err)
			returnErr = errors.Join(returnErr, closeErr)
		}
	}()

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		database,
		migrations,
		goose.WithTableName(migrationTable),
	)
	if errors.Is(err, goose.ErrNoMigrations) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("create Goose provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply Goose migrations: %w", err)
	}

	return nil
}
