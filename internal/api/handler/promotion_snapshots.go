package handler

import (
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

func promotionStageSnapshotResponse(value promotion.PromotionStageSnapshot) api.PromotionStageSnapshot {
	var enrichment *api.PromotionStageEnrichment
	if value.Enrichment != nil {
		var repository *api.CatalogName
		if value.Enrichment.RepositoryName != "" {
			name := api.CatalogName(value.Enrichment.RepositoryName)
			repository = &name
		}
		paths := append([]string(nil), value.Enrichment.SourcePaths...)
		enrichment = &api.PromotionStageEnrichment{
			Application: optionalString(value.Enrichment.Application),
			Environment: optionalString(value.Enrichment.Environment),
			Repository:  repository, RepositoryURL: optionalString(value.Enrichment.RepositoryURL),
			TargetBranch: optionalString(value.Enrichment.TargetBranch), SourcePaths: &paths,
		}
	}
	return api.PromotionStageSnapshot{
		ID:                              value.ID,
		Integration:                     api.CatalogName(value.Integration),
		Cluster:                         api.CatalogName(value.ClusterName),
		Project:                         api.CatalogName(value.Project),
		Name:                            value.Name,
		KubernetesUID:                   value.KubernetesUID,
		ResourceVersion:                 value.ResourceVersion,
		Enrichment:                      enrichment,
		RequestedFreight:                promotionFreightRequestResponse(value.RequestedFreight),
		Freight:                         promotionFreightResponse(value.Freight),
		SuccessfulRenderAcknowledgement: promotionAcknowledgementResponse(value.SuccessfulRenderAcknowledgement),
		LastObservedAt:                  value.LastObservedAt,
	}
}

func promotionFreightResponse(value promotion.Freight) api.PromotionFreight {
	items := make([]api.PromotionFreightItem, 0, len(value.Items))
	for _, item := range value.Items {
		items = append(items, promotionFreightItemResponse(item))
	}

	return api.PromotionFreight{Items: items}
}

func promotionFreightItemResponse(item promotion.FreightItem) api.PromotionFreightItem {
	artifacts := make([]api.PromotionFreightArtifact, 0, len(item.Artifacts))
	for _, artifact := range item.Artifacts {
		artifacts = append(artifacts, api.PromotionFreightArtifact{
			Kind:       api.PromotionFreightArtifactKind(artifact.Kind),
			Repository: artifact.Repository,
			Version:    artifact.Version,
		})
	}
	result := api.PromotionFreightItem{Origin: item.Origin, Name: item.Name, Artifacts: artifacts}
	if !item.EvidenceObserved {
		return result
	}
	approvedFor := append([]string{}, item.ApprovedFor...)
	verifiedIn := make([]api.PromotionFreightVerification, 0, len(item.VerifiedIn))
	for _, evidence := range item.VerifiedIn {
		verifiedIn = append(verifiedIn, api.PromotionFreightVerification{
			Stage: evidence.Stage, LongestCompletedSoak: evidence.LongestCompletedSoak.String(),
		})
	}
	currentlyIn := make([]api.PromotionFreightOccupancy, 0, len(item.CurrentlyIn))
	for _, evidence := range item.CurrentlyIn {
		currentlyIn = append(currentlyIn, api.PromotionFreightOccupancy{
			Stage: evidence.Stage, Since: evidence.Since,
		})
	}

	result.ApprovedFor = &approvedFor
	result.VerifiedIn = &verifiedIn
	result.CurrentlyIn = &currentlyIn

	return result
}

func promotionRepositorySnapshotResponse(value promotion.PromotionRepositorySnapshot) api.PromotionRepositorySnapshot {
	return api.PromotionRepositorySnapshot{
		Name:                   api.CatalogName(value.Name),
		CloneURL:               value.CloneURL,
		Branch:                 value.Branch,
		CheckoutCredentialName: value.CheckoutCredentialName,
	}
}

func promotionFreightFromRequest(value api.PromotionFreight) promotion.Freight {
	items := make([]promotion.FreightItem, 0, len(value.Items))
	for _, item := range value.Items {
		artifacts := make([]promotion.FreightArtifact, 0, len(item.Artifacts))
		for _, artifact := range item.Artifacts {
			artifacts = append(artifacts, promotion.FreightArtifact{
				Kind: string(artifact.Kind), Repository: artifact.Repository, Version: artifact.Version,
			})
		}
		approvedFor := []string{}
		if item.ApprovedFor != nil {
			approvedFor = append(approvedFor, (*item.ApprovedFor)...)
		}
		verifiedIn := []promotion.FreightVerification{}
		if item.VerifiedIn != nil {
			for _, evidence := range *item.VerifiedIn {
				longestSoak, err := time.ParseDuration(evidence.LongestCompletedSoak)
				if err != nil {
					longestSoak = 0
				}
				verifiedIn = append(verifiedIn, promotion.FreightVerification{
					Stage: evidence.Stage, LongestCompletedSoak: longestSoak,
				})
			}
		}
		currentlyIn := []promotion.FreightOccupancy{}
		if item.CurrentlyIn != nil {
			for _, evidence := range *item.CurrentlyIn {
				currentlyIn = append(currentlyIn, promotion.FreightOccupancy{
					Stage: evidence.Stage, Since: evidence.Since,
				})
			}
		}
		items = append(items, promotion.FreightItem{
			Origin: item.Origin, Name: item.Name, Artifacts: artifacts,
			ApprovedFor: approvedFor, VerifiedIn: verifiedIn, CurrentlyIn: currentlyIn,
			EvidenceObserved: item.ApprovedFor != nil && item.VerifiedIn != nil && item.CurrentlyIn != nil,
		})
	}

	return promotion.Freight{Items: items}
}

func promotionAcknowledgementFromRequest(
	value *api.SuccessfulRenderAcknowledgement,
) *promotion.SuccessfulRenderAcknowledgement {
	if value == nil {
		return nil
	}
	result := &promotion.SuccessfulRenderAcknowledgement{}
	if value.SourceRevision != nil {
		result.SourceRevision = *value.SourceRevision
	}
	if value.TargetRevision != nil {
		result.TargetRevision = *value.TargetRevision
	}
	if value.ResultRevision != nil {
		result.ResultRevision = *value.ResultRevision
	}

	return result
}

func promotionAcknowledgementResponse(
	value *promotion.SuccessfulRenderAcknowledgement,
) *api.SuccessfulRenderAcknowledgement {
	if value == nil {
		return nil
	}

	return &api.SuccessfulRenderAcknowledgement{
		SourceRevision: optionalString(value.SourceRevision),
		TargetRevision: optionalString(value.TargetRevision),
		ResultRevision: optionalString(value.ResultRevision),
	}
}
