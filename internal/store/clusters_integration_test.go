package store

import (
	"testing"
	"time"
)

func TestClusterPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	firstSeen := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	if err := database.RecordClusterObservation(ctx, "colo", firstSeen); err != nil {
		t.Fatalf("RecordClusterObservation() error = %v", err)
	}

	cluster, err := database.GetCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("GetCluster() error = %v", err)
	}

	if !cluster.FirstSeenAt.Equal(firstSeen) ||
		!cluster.LastSeenAt.Equal(firstSeen) {
		t.Errorf(
			"cluster observation = (%s, %s), want (%s, %s)",
			cluster.FirstSeenAt,
			cluster.LastSeenAt,
			firstSeen,
			firstSeen,
		)
	}

	lastSeen := firstSeen.Add(time.Minute)
	if err := database.RecordClusterObservation(ctx, "colo", lastSeen); err != nil {
		t.Fatalf("second RecordClusterObservation() error = %v", err)
	}

	cluster, err = database.GetCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("second GetCluster() error = %v", err)
	}

	if !cluster.FirstSeenAt.Equal(firstSeen) ||
		!cluster.LastSeenAt.Equal(lastSeen) {
		t.Errorf(
			"updated observation = (%s, %s), want (%s, %s)",
			cluster.FirstSeenAt,
			cluster.LastSeenAt,
			firstSeen,
			lastSeen,
		)
	}

	otherSeen := firstSeen.Add(2 * time.Minute)
	if err := database.RecordClusterObservation(ctx, "backup", otherSeen); err != nil {
		t.Fatalf("record other cluster observation: %v", err)
	}

	clusters, err := database.ListClusterObservations(ctx)
	if err != nil {
		t.Fatalf("ListClusterObservations() error = %v", err)
	}

	if len(clusters) != 2 {
		t.Fatalf("cluster count = %d, want 2", len(clusters))
	}

	if clusters[0].Name != "backup" || clusters[1].Name != "colo" {
		t.Errorf("cluster order = (%q, %q), want (%q, %q)", clusters[0].Name, clusters[1].Name, "backup", "colo")
	}
}
