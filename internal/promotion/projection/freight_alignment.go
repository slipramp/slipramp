package projection

import (
	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
)

type FreightAlignmentStatus string

const (
	FreightAlignmentAligned   FreightAlignmentStatus = "aligned"
	FreightAlignmentDifferent FreightAlignmentStatus = "different"
	FreightAlignmentUnknown   FreightAlignmentStatus = "unknown"
)

type FreightAlignment struct {
	Origin               string
	SourceStageName      string
	SourceStageID        *uuid.UUID
	TargetStageID        uuid.UUID
	Status               FreightAlignmentStatus
	SourceCurrentFreight *promotion.FreightItem
	TargetCurrentFreight *promotion.FreightItem
	Reason               string
}

type freightAlignmentStageKey struct {
	integration string
	cluster     string
	project     string
	name        string
}

func (s FreightAlignmentStatus) Valid() bool {
	return s == "" || s == FreightAlignmentAligned || s == FreightAlignmentDifferent ||
		s == FreightAlignmentUnknown
}

func freightAlignments(
	stages []promotion.Stage,
	observationStale map[uuid.UUID]bool,
) map[uuid.UUID][]FreightAlignment {
	byIdentity := make(map[freightAlignmentStageKey]promotion.Stage, len(stages))
	for _, stage := range stages {
		if stage.ArchivedAt == nil {
			byIdentity[newFreightAlignmentStageKey(stage)] = stage
		}
	}

	byTarget := make(map[uuid.UUID][]FreightAlignment, len(stages))
	for _, target := range stages {
		if target.ArchivedAt != nil {
			continue
		}
		for _, request := range target.RequestedFreight {
			for _, sourceName := range request.SourceStages {
				alignment := FreightAlignment{
					Origin:          request.Origin,
					SourceStageName: sourceName,
					TargetStageID:   target.ID,
				}
				sourceKey := newFreightAlignmentStageKey(target)
				sourceKey.name = sourceName
				source, resolved := byIdentity[sourceKey]
				if !resolved {
					alignment.Status = FreightAlignmentUnknown
					alignment.Reason = "The requested source Stage is not active in this Project."
					byTarget[target.ID] = append(byTarget[target.ID], alignment)
					continue
				}

				alignment.SourceStageID = new(source.ID)
				alignment.SourceCurrentFreight = currentFreightForOrigin(source.Freight, request.Origin)
				alignment.TargetCurrentFreight = currentFreightForOrigin(target.Freight, request.Origin)

				sourceStale := observationStale[source.ID]
				targetStale := observationStale[target.ID]
				switch {
				case sourceStale && targetStale:
					alignment.Status = FreightAlignmentUnknown
					alignment.Reason = "The source and target Stage observations are stale."
				case sourceStale:
					alignment.Status = FreightAlignmentUnknown
					alignment.Reason = "The source Stage observation is stale."
				case targetStale:
					alignment.Status = FreightAlignmentUnknown
					alignment.Reason = "The target Stage observation is stale."
				case alignment.SourceCurrentFreight == nil && alignment.TargetCurrentFreight == nil:
					alignment.Status = FreightAlignmentUnknown
					alignment.Reason = "Neither Stage reports current Freight for this origin."
				case alignment.SourceCurrentFreight == nil:
					alignment.Status = FreightAlignmentDifferent
					alignment.Reason = "The target has current Freight for this origin, but the source does not."
				case alignment.TargetCurrentFreight == nil:
					alignment.Status = FreightAlignmentDifferent
					alignment.Reason = "The source has current Freight for this origin, but the target does not."
				case alignment.SourceCurrentFreight.Name == alignment.TargetCurrentFreight.Name:
					alignment.Status = FreightAlignmentAligned
				default:
					alignment.Status = FreightAlignmentDifferent
					alignment.Reason = "The source and target currently use different Freight for this origin."
				}

				byTarget[target.ID] = append(byTarget[target.ID], alignment)
			}
		}
	}

	return byTarget
}

func newFreightAlignmentStageKey(stage promotion.Stage) freightAlignmentStageKey {
	return freightAlignmentStageKey{
		integration: stage.Integration,
		cluster:     stage.ClusterName,
		project:     stage.Project,
		name:        stage.Name,
	}
}

func currentFreightForOrigin(freight promotion.Freight, origin string) *promotion.FreightItem {
	for _, item := range freight.Items {
		if item.Origin == origin {
			return new(item)
		}
	}

	return nil
}
