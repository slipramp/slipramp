package placement

import (
	"strings"
	"testing"
)

func TestDecodeRunCapacity(t *testing.T) {
	t.Parallel()

	configured, err := Decode(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
clusters:
  colo:
    maximumActiveRuns: 6
placements:
  infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
    maximumActiveRuns: 2
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	clusterMaximum, found := configured.ClusterMaximumActiveRuns("colo")
	if !found || clusterMaximum != 6 {
		t.Errorf("cluster maximum = (%d, %t), want (6, true)", clusterMaximum, found)
	}
	placement, found := configured.Lookup("infrastructure")
	if !found || placement.MaximumActiveRuns != 2 {
		t.Errorf("Placement maximum = (%d, %t), want (2, true)", placement.MaximumActiveRuns, found)
	}
}

func TestDecodeRejectsInvalidRunCapacity(t *testing.T) {
	t.Parallel()

	_, err := Decode(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
clusters:
  colo:
    maximumActiveRuns: 2
placements:
  infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
    maximumActiveRuns: 3
`))
	if err == nil {
		t.Fatal("Decode() error = nil, want an error")
	}
}
