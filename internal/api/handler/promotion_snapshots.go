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
		Application:                     value.Application,
		Environment:                     value.Environment,
		Repository:                      api.CatalogName(value.RepositoryName),
		RepositoryURL:                   value.RepositoryURL,
		TargetBranch:                    value.TargetBranch,
		SourcePaths:                     value.SourcePaths,
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
		items = append(items, promotion.FreightItem{Origin: item.Origin, Name: item.Name, Artifacts: artifacts})
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
