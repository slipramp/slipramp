package promotion

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type FreightAvailabilityStrategy string

const (
	FreightAvailabilityOneOf FreightAvailabilityStrategy = "OneOf"
	FreightAvailabilityAll   FreightAvailabilityStrategy = "All"
)

type Freight struct {
	Items []FreightItem `json:"items"`
}

type FreightRequest struct {
	Origin               string                      `json:"origin"`
	Direct               bool                        `json:"direct"`
	SourceStages         []string                    `json:"sourceStages"`
	AvailabilityStrategy FreightAvailabilityStrategy `json:"availabilityStrategy"`
	RequiredSoakTime     time.Duration               `json:"requiredSoakTime"`
}

type FreightItem struct {
	Origin           string                `json:"origin"`
	Name             string                `json:"name"`
	DiscoveredAt     *time.Time            `json:"discoveredAt,omitempty"`
	Artifacts        []FreightArtifact     `json:"artifacts"`
	ApprovedFor      []string              `json:"approvedFor,omitempty"`
	VerifiedIn       []FreightVerification `json:"verifiedIn,omitempty"`
	CurrentlyIn      []FreightOccupancy    `json:"currentlyIn,omitempty"`
	EvidenceObserved bool                  `json:"evidenceObserved,omitempty"`
}

type FreightVerification struct {
	Stage                string        `json:"stage"`
	LongestCompletedSoak time.Duration `json:"longestCompletedSoak"`
}

type FreightOccupancy struct {
	Stage string    `json:"stage"`
	Since time.Time `json:"since"`
}

type FreightArtifact struct {
	Kind       string  `json:"kind"`
	Repository *string `json:"repository,omitempty"`
	Version    string  `json:"version"`
}

func cloneFreight(freight Freight) Freight {
	items := make([]FreightItem, len(freight.Items))
	for index, item := range freight.Items {
		items[index] = cloneFreightItem(item)
	}

	return Freight{Items: items}
}

func cloneFreightItem(item FreightItem) FreightItem {
	if item.DiscoveredAt != nil {
		item.DiscoveredAt = new(*item.DiscoveredAt)
	}
	artifacts := make([]FreightArtifact, len(item.Artifacts))
	for index, value := range item.Artifacts {
		artifacts[index] = value
		if value.Repository != nil {
			repository := *value.Repository
			artifacts[index].Repository = &repository
		}
	}
	item.Artifacts = artifacts
	item.ApprovedFor = slices.Clone(item.ApprovedFor)
	item.VerifiedIn = slices.Clone(item.VerifiedIn)
	item.CurrentlyIn = slices.Clone(item.CurrentlyIn)

	return item
}

func (s FreightAvailabilityStrategy) Valid() bool {
	return s == "" || s == FreightAvailabilityOneOf || s == FreightAvailabilityAll
}

func (s FreightAvailabilityStrategy) Effective() FreightAvailabilityStrategy {
	if s == "" {
		return FreightAvailabilityOneOf
	}

	return s
}

type FreightReadiness string

const (
	FreightReadinessReady       FreightReadiness = "ready"
	FreightReadinessUnavailable FreightReadiness = "unavailable"
	FreightReadinessUnknown     FreightReadiness = "unknown"
)

type FreightReadinessEvidence struct {
	Readiness FreightReadiness
	Reason    string
	Facts     []string
}

func EvaluateFreightReadiness(target Stage, candidate FreightItem) FreightReadinessEvidence {
	var request *FreightRequest
	for index := range target.RequestedFreight {
		if target.RequestedFreight[index].Origin == candidate.Origin {
			request = &target.RequestedFreight[index]
			break
		}
	}
	if request == nil {
		return FreightReadinessEvidence{
			Readiness: FreightReadinessUnavailable,
			Reason:    "The Stage does not request Freight from this origin.",
		}
	}
	for _, current := range target.Freight.Items {
		if current.Origin == candidate.Origin && current.Name == candidate.Name {
			return FreightReadinessEvidence{
				Readiness: FreightReadinessUnavailable,
				Reason:    "This Freight is already current in the Stage.",
			}
		}
	}
	if request.Direct {
		return FreightReadinessEvidence{
			Readiness: FreightReadinessReady,
			Facts:     []string{"The Stage accepts Freight directly from its origin."},
		}
	}
	if slices.Contains(candidate.ApprovedFor, target.Name) {
		return FreightReadinessEvidence{
			Readiness: FreightReadinessReady,
			Facts:     []string{"Kargo reports this Freight explicitly approved for the Stage."},
		}
	}

	qualified := make([]string, 0, len(request.SourceStages))
	missing := make([]string, 0, len(request.SourceStages))
	for _, source := range request.SourceStages {
		if freightVerifiedAndSoaked(candidate, source, request.RequiredSoakTime, target.LastObservedAt) {
			qualified = append(qualified, source)
		} else {
			missing = append(missing, source)
		}
	}
	strategy := request.AvailabilityStrategy.Effective()
	ready := strategy == FreightAvailabilityAll && len(qualified) == len(request.SourceStages)
	ready = ready || strategy == FreightAvailabilityOneOf && len(qualified) > 0
	if ready {
		return FreightReadinessEvidence{
			Readiness: FreightReadinessReady,
			Facts: []string{fmt.Sprintf(
				"Kargo reports verification and required soak evidence in: %s.",
				strings.Join(qualified, ", "),
			)},
		}
	}
	if !candidate.EvidenceObserved {
		return FreightReadinessEvidence{
			Readiness: FreightReadinessUnknown,
			Reason:    "The observation does not include complete Kargo approval and verification evidence.",
		}
	}

	return FreightReadinessEvidence{
		Readiness: FreightReadinessUnavailable,
		Reason: fmt.Sprintf(
			"The %s requirement is not satisfied by observed verification and soak evidence for: %s.",
			strategy,
			strings.Join(missing, ", "),
		),
	}
}

func freightVerifiedAndSoaked(candidate FreightItem, stage string, required time.Duration, observedAt time.Time) bool {
	verified := false
	longestSoak := time.Duration(0)
	for _, evidence := range candidate.VerifiedIn {
		if evidence.Stage == stage {
			verified = true
			longestSoak = evidence.LongestCompletedSoak
			break
		}
	}
	if !verified {
		return false
	}
	for _, occupancy := range candidate.CurrentlyIn {
		if occupancy.Stage == stage && !occupancy.Since.IsZero() && observedAt.After(occupancy.Since) {
			longestSoak = max(longestSoak, observedAt.Sub(occupancy.Since))
		}
	}

	return longestSoak >= required
}

func cloneFreightRequests(requests []FreightRequest) []FreightRequest {
	cloned := slices.Clone(requests)
	for index := range cloned {
		cloned[index].SourceStages = slices.Clone(cloned[index].SourceStages)
	}

	return cloned
}
