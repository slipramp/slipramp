package cluster

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceRecordsClusterObservation(t *testing.T) {
	t.Parallel()

	clusters := &recordingStore{}
	service := NewService(clusters)
	location := time.FixedZone("test", -6*60*60)
	observedAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, location)
	service.now = func() time.Time {
		return observedAt
	}

	if err := service.RecordControllerContact(context.Background(), "colo"); err != nil {
		t.Fatalf("RecordControllerContact() error = %v", err)
	}

	if clusters.clusterName != "colo" {
		t.Errorf("cluster name = %q, want %q", clusters.clusterName, "colo")
	}

	if !clusters.observedAt.Equal(observedAt) {
		t.Errorf("observed at = %s, want %s", clusters.observedAt, observedAt)
	}

	if clusters.observedAt.Location() != time.UTC {
		t.Errorf("observation location = %s, want UTC", clusters.observedAt.Location())
	}
}

func TestServiceReturnsObservationFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	service := NewService(&recordingStore{err: want})

	err := service.RecordControllerContact(context.Background(), "colo")
	if !errors.Is(err, want) {
		t.Fatalf("RecordControllerContact() error = %v, want %v", err, want)
	}
}

func TestServiceReportsConnectionState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	clusters := &recordingStore{
		clusters: []Observation{
			{
				Name:        "connected",
				FirstSeenAt: now.Add(-time.Hour),
				LastSeenAt:  now.Add(-ConnectionWindow),
			},
			{
				Name:        "unreachable",
				FirstSeenAt: now.Add(-time.Hour),
				LastSeenAt:  now.Add(-ConnectionWindow - time.Nanosecond),
			},
		},
	}
	service := NewService(clusters)
	service.now = func() time.Time {
		return now
	}

	statuses, err := service.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}

	if len(statuses) != 2 {
		t.Fatalf("cluster count = %d, want 2", len(statuses))
	}

	if statuses[0].ConnectionState != ConnectionStateConnected {
		t.Errorf("connected state = %q, want %q", statuses[0].ConnectionState, ConnectionStateConnected)
	}

	if statuses[1].ConnectionState != ConnectionStateUnreachable {
		t.Errorf("unreachable state = %q, want %q", statuses[1].ConnectionState, ConnectionStateUnreachable)
	}
}

func TestDeriveConnectionStateUsesProvidedEvaluationTime(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	observation := Observation{LastSeenAt: at.Add(-ConnectionWindow)}
	if state := DeriveConnectionState(observation, at); state != ConnectionStateConnected {
		t.Errorf("state at freshness boundary = %q, want %q", state, ConnectionStateConnected)
	}
	if state := DeriveConnectionState(observation, at.Add(time.Nanosecond)); state != ConnectionStateUnreachable {
		t.Errorf("state after freshness boundary = %q, want %q", state, ConnectionStateUnreachable)
	}
}

type recordingStore struct {
	clusterName string
	observedAt  time.Time
	clusters    []Observation
	err         error
}

func (r *recordingStore) RecordClusterObservation(_ context.Context, clusterName string, observedAt time.Time) error {
	r.clusterName = clusterName
	r.observedAt = observedAt

	return r.err
}

func (r *recordingStore) ListClusterObservations(context.Context) ([]Observation, error) {
	return r.clusters, nil
}
