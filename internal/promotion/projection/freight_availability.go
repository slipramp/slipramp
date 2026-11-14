package projection

import "github.com/slipramp/slipramp/internal/promotion"

func stageHasNewerFreight(stage ProjectStage) bool {
	if stage.Capabilities.Promotion.Status != StageCapabilityReady {
		return false
	}

	currentByOrigin := make(map[string]promotion.FreightItem, len(stage.Stage.Freight.Items))
	for _, freight := range stage.Stage.Freight.Items {
		currentByOrigin[freight.Origin] = freight
	}

	for _, request := range stage.RequestedFreight {
		for _, candidate := range request.Candidates {
			if candidate.Evidence.Readiness != promotion.FreightReadinessReady {
				continue
			}
			current, ok := currentByOrigin[candidate.Freight.Origin]
			if !ok {
				return true
			}
			if current.DiscoveredAt == nil || candidate.Freight.DiscoveredAt == nil {
				continue
			}
			if candidate.Freight.DiscoveredAt.After(*current.DiscoveredAt) {
				return true
			}
		}
	}

	return false
}
