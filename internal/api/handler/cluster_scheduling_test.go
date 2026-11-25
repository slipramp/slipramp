package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/run"
)

func TestGetClusterScheduling(t *testing.T) {
	t.Parallel()
	runs := &recordingRunService{clusterScheduling: run.ClusterScheduling{
		MaximumActiveRuns: 4, Active: 1, Queued: 2,
		Placements: []run.PlacementScheduling{
			{Name: "batch", Namespace: "jobs", MaximumActiveRuns: 1, Active: 1, Queued: 2},
			{Name: "shared", Namespace: "shared-jobs"},
		},
	}}
	handler := newAdminTestHandler(nil, nil, nil, nil, runs, nil, nil)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/clusters/colo/scheduling", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || runs.schedulingCluster != "colo" {
		t.Fatalf("status = %d, requested cluster = %q", response.Code, runs.schedulingCluster)
	}
	var result api.ClusterScheduling
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.ActiveRuns != 1 || result.QueuedRuns != 2 || result.MaximumActiveRuns != 4 || len(result.Placements) != 2 {
		t.Fatalf("scheduling = %#v", result)
	}
	batch := result.Placements[0]
	if batch.Name != "batch" || batch.Namespace != "jobs" || batch.ActiveRuns != 1 || batch.QueuedRuns != 2 ||
		batch.MaximumActiveRuns == nil || *batch.MaximumActiveRuns != 1 || result.Placements[1].MaximumActiveRuns != nil {
		t.Errorf("Placements = %#v", result.Placements)
	}

	runs.loadError = cluster.ErrNotFound
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Errorf("unobserved cluster status = %d", response.Code)
	}
}
