package run

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type SchedulingStore interface {
	GetRunSchedulingSnapshots(
		context.Context,
		[]uuid.UUID,
		[]byte,
		[]byte,
	) ([]SchedulingSnapshot, error)
}

func (s *Service) Scheduling(
	ctx context.Context,
	records []Run,
	connectedClusters map[string]bool,
	at time.Time,
) (map[uuid.UUID]Scheduling, error) {
	queued := make([]Run, 0, len(records))
	clusterLimits := make(map[string]int32)
	placementLimits := make(map[string]int32)
	for _, name := range s.placements.Names() {
		configured, _ := s.placements.Lookup(name)
		if configured.MaximumActiveRuns > 0 {
			placementLimits[name] = configured.MaximumActiveRuns
		}
	}
	for _, record := range records {
		if record.Phase != PhaseQueued {
			continue
		}

		queued = append(queued, record)
		maximum, configured := s.placements.ClusterMaximumActiveRuns(record.ClusterName)
		if !configured {
			maximum = DefaultMaximumActiveRuns
		}
		clusterLimits[record.ClusterName] = maximum

	}
	if len(queued) == 0 {
		return map[uuid.UUID]Scheduling{}, nil
	}

	clusterLimitsJSON, err := json.Marshal(clusterLimits)
	if err != nil {
		return nil, fmt.Errorf("encode cluster Run capacities: %w", err)
	}
	placementLimitsJSON, err := json.Marshal(placementLimits)
	if err != nil {
		return nil, fmt.Errorf("encode Placement Run capacities: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(queued))
	for _, record := range queued {
		ids = append(ids, record.ID)
	}
	snapshots, err := s.scheduling.GetRunSchedulingSnapshots(
		ctx,
		ids,
		clusterLimitsJSON,
		placementLimitsJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("load Run scheduling snapshots: %w", err)
	}
	byID := make(map[uuid.UUID]SchedulingSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		byID[snapshot.RunID] = snapshot
	}

	at = at.UTC()
	result := make(map[uuid.UUID]Scheduling, len(queued))
	for _, record := range queued {
		snapshot, found := byID[record.ID]
		if !found {
			continue
		}

		detail := Scheduling{
			State:            SchedulingStateEligible,
			QueueAge:         max(at.Sub(record.CreatedAt), 0),
			EligiblePosition: snapshot.EligiblePosition,
			SnapshotAt:       at,
		}
		switch {
		case record.Review.State == ReviewStatePending:
			detail.State = SchedulingStateAwaitingReview
			detail.EligiblePosition = nil
		case snapshot.BlockingRunID != nil:
			detail.State = SchedulingStateTargetBusy
			detail.BlockingRunID = snapshot.BlockingRunID
			detail.EligiblePosition = nil
		case !connectedClusters[record.ClusterName]:
			detail.State = SchedulingStateClusterUnreachable
		case snapshot.ClusterActiveRuns >= int64(clusterLimits[record.ClusterName]):
			detail.State = SchedulingStateClusterCapacity
			detail.EligiblePosition = nil
		case placementLimits[record.PlacementName] > 0 &&
			snapshot.PlacementActiveRuns >= int64(placementLimits[record.PlacementName]):
			detail.State = SchedulingStateClusterCapacity
			detail.EligiblePosition = nil
		}
		result[record.ID] = detail
	}

	return result, nil
}

func (s *Service) AssignmentCapacity(clusterName string) SchedulingCapacity {
	clusterMaximum, configured := s.placements.ClusterMaximumActiveRuns(clusterName)
	if !configured {
		clusterMaximum = DefaultMaximumActiveRuns
	}

	placementMaximums := make(map[string]int32)
	for _, name := range s.placements.Names() {
		configured, _ := s.placements.Lookup(name)
		if configured.ClusterName == clusterName && configured.MaximumActiveRuns > 0 {
			placementMaximums[name] = configured.MaximumActiveRuns
		}
	}

	return SchedulingCapacity{
		ClusterMaximumActiveRuns:   clusterMaximum,
		PlacementMaximumActiveRuns: placementMaximums,
	}
}
