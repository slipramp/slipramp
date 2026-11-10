package handler

import (
	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

func promotionStageSnapshotResponse(value promotion.PromotionStageSnapshot) api.PromotionStageSnapshot {
	return api.PromotionStageSnapshot{
		ID:                              value.ID,
		Integration:                     api.CatalogName(value.Integration),
		Cluster:                         api.CatalogName(value.ClusterName),
		Project:                         api.CatalogName(value.Project),
		Name:                            value.Name,
		KubernetesUID:                   value.KubernetesUID,
		ResourceVersion:                 value.ResourceVersion,
		Enrichment:                      promotionStageEnrichmentResponse(value.Enrichment),
		RequestedFreight:                promotionFreightRequestResponse(value.RequestedFreight),
		Freight:                         promotionFreightResponse(value.Freight),
		SuccessfulRenderAcknowledgement: promotionAcknowledgementResponse(value.SuccessfulRenderAcknowledgement),
		LastObservedAt:                  value.LastObservedAt,
	}
}

func promotionStageEnrichmentResponse(value *promotion.PromotionStageEnrichment) *api.PromotionStageEnrichment {
	if value == nil {
		return nil
	}
	var repository *api.CatalogName
	if value.RepositoryName != "" {
		repository = new(api.CatalogName(value.RepositoryName))
	}
	var paths *[]string
	if len(value.SourcePaths) > 0 {
		paths = &value.SourcePaths
	}

	return &api.PromotionStageEnrichment{
		Application:   optionalString(value.Application),
		Environment:   optionalString(value.Environment),
		Repository:    repository,
		RepositoryURL: optionalString(value.RepositoryURL),
		TargetBranch:  optionalString(value.TargetBranch),
		SourcePaths:   paths,
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
