package store

import (
	"context"
	"fmt"
	"time"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/store/storedb"
	"github.com/jackc/pgx/v5/pgtype"
)

func (d *Database) RecordClusterObservation(
	ctx context.Context,
	clusterName string,
	observedAt time.Time,
) error {
	err := d.queries.RecordClusterObservation(
		ctx,
		storedb.RecordClusterObservationParams{
			Name: clusterName,
			ObservedAt: pgtype.Timestamptz{
				Time:  observedAt,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("record cluster observation: %w", err)
	}

	return nil
}

func (d *Database) GetCluster(
	ctx context.Context,
	clusterName string,
) (cluster.Observation, error) {
	row, err := d.queries.GetCluster(ctx, clusterName)
	if err != nil {
		return cluster.Observation{}, fmt.Errorf("get cluster: %w", err)
	}

	return cluster.Observation{
		Name:        row.Name,
		FirstSeenAt: row.FirstSeenAt.Time.UTC(),
		LastSeenAt:  row.LastSeenAt.Time.UTC(),
	}, nil
}

func (d *Database) ListClusterObservations(
	ctx context.Context,
) ([]cluster.Observation, error) {
	rows, err := d.queries.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}

	observations := make([]cluster.Observation, 0, len(rows))
	for _, row := range rows {
		observations = append(observations, cluster.Observation{
			Name:        row.Name,
			FirstSeenAt: row.FirstSeenAt.Time.UTC(),
			LastSeenAt:  row.LastSeenAt.Time.UTC(),
		})
	}

	return observations, nil
}
