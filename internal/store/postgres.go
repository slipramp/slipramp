// Package store owns Slipramp's PostgreSQL connection and schema migrations.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/store/storedb"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultConnectTimeout = 10 * time.Second

type Config struct {
	URL            string
	ConnectTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		URL:            "postgres://slipramp:slipramp-development@127.0.0.1:5432/slipramp?sslmode=disable",
		ConnectTimeout: defaultConnectTimeout,
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.URL) == "" {
		return errors.New("database URL must not be empty")
	}

	poolConfig, err := pgxpool.ParseConfig(c.URL)
	if err != nil {
		return fmt.Errorf("parse database URL: %w", err)
	}

	if strings.TrimSpace(poolConfig.ConnConfig.Database) == "" {
		return errors.New("database URL must select a database")
	}

	if c.ConnectTimeout <= 0 {
		return errors.New("database connect timeout must be positive")
	}

	return nil
}

type Database struct {
	pool    *pgxpool.Pool
	queries *storedb.Queries
}

func Open(ctx context.Context, cfg Config) (*Database, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate database configuration: %w", err)
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection pool: %w", err)
	}

	database := &Database{
		pool:    pool,
		queries: storedb.New(pool),
	}
	if err := database.initialize(ctx, cfg.ConnectTimeout); err != nil {
		pool.Close()

		return nil, err
	}

	return database, nil
}

func (d *Database) initialize(
	ctx context.Context,
	connectTimeout time.Duration,
) error {
	connectContext, cancel := context.WithTimeout(ctx, connectTimeout)
	err := d.Ping(connectContext)
	cancel()

	if err != nil {
		return err
	}

	if err := applyMigrations(ctx, d.pool); err != nil {
		return fmt.Errorf("apply schema migrations: %w", err)
	}

	return nil
}

func (d *Database) Ping(ctx context.Context) error {
	_, err := d.queries.Ping(ctx)
	if err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}

	return nil
}

func (d *Database) Close() {
	if d == nil || d.pool == nil {
		return
	}

	d.pool.Close()
}
