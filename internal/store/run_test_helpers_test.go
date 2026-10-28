package store

import "github.com/slipramp/slipramp/internal/run"

func testSchedulingCapacity() run.SchedulingCapacity {
	return run.SchedulingCapacity{
		ClusterMaximumActiveRuns:   run.DefaultMaximumActiveRuns,
		PlacementMaximumActiveRuns: map[string]int32{},
	}
}
