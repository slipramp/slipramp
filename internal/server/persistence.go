package server

import (
	"context"
	"fmt"

	"github.com/slipramp/slipramp/internal/objectstore"
	"github.com/slipramp/slipramp/internal/store"
)

// persistence owns the Server's durable storage clients. PostgreSQL remains
// authoritative for product state and artifact metadata; the object store
// contains artifact bytes only.
type persistence struct {
	database         *store.Database
	artifactContents *objectstore.Store
}

func openPersistence(ctx context.Context, cfg Config) (*persistence, error) {
	database, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	artifactContents, err := objectstore.New(cfg.ObjectStore)
	if err != nil {
		database.Close()

		return nil, fmt.Errorf("configure artifact storage: %w", err)
	}

	if err := artifactContents.Check(ctx); err != nil {
		database.Close()

		return nil, fmt.Errorf("check artifact storage: %w", err)
	}

	return &persistence{
		database:         database,
		artifactContents: artifactContents,
	}, nil
}

func (p *persistence) Close() {
	if p == nil {
		return
	}

	p.database.Close()
}
