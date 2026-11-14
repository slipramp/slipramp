package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

func (h *Handler) ReplaceKargoStageSnapshot(
	ctx context.Context,
	request api.ReplaceKargoStageSnapshotRequestObject,
) (api.ReplaceKargoStageSnapshotResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A complete Kargo Stage snapshot is required.")

		return api.ReplaceKargoStageSnapshot400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}

	stages := make([]promotion.StageObservation, 0, len(request.Body.Stages))
	for _, observed := range request.Body.Stages {
		stage, err := stageObservationFromRequest(observed)
		if err != nil {
			problem := NewProblem(http.StatusBadRequest, fmt.Sprintf("Stage %s/%s: %v", observed.Project, observed.Name, err))
			return api.ReplaceKargoStageSnapshot400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
			}, nil
		}
		stages = append(stages, stage)
	}

	err = h.promotionStages.ReplaceStageSnapshot(
		ctx,
		string(request.Integration),
		principal.ClusterName,
		promotion.StageSnapshotReplacement{
			SnapshotID: request.Body.SnapshotID,
			ObservedAt: request.Body.ObservedAt,
			Stages:     stages,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, promotion.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.ReplaceKargoStageSnapshot400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrIntegrationNotFound):
			problem := NewProblem(http.StatusNotFound, "The Kargo integration does not exist.")

			return api.ReplaceKargoStageSnapshot404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrIntegrationCluster):
			problem := NewProblem(http.StatusForbidden, "The Kargo integration belongs to another cluster.")

			return api.ReplaceKargoStageSnapshot403ApplicationProblemPlusJSONResponse{
				ForbiddenApplicationProblemPlusJSONResponse: api.ForbiddenApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrSnapshotConflict),
			errors.Is(err, promotion.ErrStaleSnapshot):
			problem := NewProblem(http.StatusConflict, err.Error())

			return api.ReplaceKargoStageSnapshot409ApplicationProblemPlusJSONResponse{
				ConflictApplicationProblemPlusJSONResponse: api.ConflictApplicationProblemPlusJSONResponse(problem),
			}, nil
		default:
			return nil, err
		}
	}

	return api.ReplaceKargoStageSnapshot204Response{}, nil
}

func stageObservationFromRequest(observed api.PromotionStageObservation) (promotion.StageObservation, error) {
	requests, err := promotionFreightRequests(observed.RequestedFreight)
	if err != nil {
		return promotion.StageObservation{}, err
	}
	freight, err := promotionFreightFromRequest(observed.Freight)
	if err != nil {
		return promotion.StageObservation{}, err
	}
	candidates, err := promotionFreightFromRequest(observed.CandidateFreight)
	if err != nil {
		return promotion.StageObservation{}, err
	}
	sourcePaths := []string{}
	if observed.SourcePaths != nil {
		sourcePaths = append(sourcePaths, (*observed.SourcePaths)...)
	}
	return promotion.StageObservation{
		Project:                         string(observed.Project),
		Name:                            observed.Name,
		KubernetesUID:                   observed.KubernetesUID,
		ResourceVersion:                 observed.ResourceVersion,
		Application:                     valueOrEmptyString(observed.Application),
		Environment:                     valueOrEmptyString(observed.Environment),
		RepositoryURL:                   valueOrEmptyString(observed.RepositoryURL),
		TargetBranch:                    valueOrEmptyString(observed.TargetBranch),
		SourcePaths:                     sourcePaths,
		RequestedFreight:                requests,
		Freight:                         freight,
		CandidateFreight:                candidates,
		AutoPromotionEnabled:            observed.AutoPromotionEnabled,
		ControlFlow:                     observed.ControlFlow,
		Conditions:                      promotionStageConditionsFromRequest(observed.Conditions),
		CurrentPromotion:                promotionStagePromotionFromRequest(observed.CurrentPromotion),
		LastPromotion:                   promotionStagePromotionFromRequest(observed.LastPromotion),
		HealthStatus:                    observed.HealthStatus,
		ArgoApplicationNamespace:        observed.ArgoApplicationNamespace,
		SuccessfulRenderAcknowledgement: promotionAcknowledgementFromRequest(observed.SuccessfulRenderAcknowledgement),
	}, nil
}

func promotionFreightRequests(values []api.PromotionFreightRequest) ([]promotion.FreightRequest, error) {
	requests := make([]promotion.FreightRequest, 0, len(values))
	for _, value := range values {
		requiredSoakTime := time.Duration(0)
		if value.RequiredSoakTime != nil {
			var err error
			requiredSoakTime, err = parseFreightSoak("requiredSoakTime", *value.RequiredSoakTime)
			if err != nil {
				return nil, err
			}
		}
		requests = append(requests, promotion.FreightRequest{
			Origin:               value.Origin,
			Direct:               value.Direct,
			SourceStages:         value.SourceStages,
			AvailabilityStrategy: promotion.FreightAvailabilityStrategy(value.AvailabilityStrategy),
			RequiredSoakTime:     requiredSoakTime,
		})
	}

	return requests, nil
}

func promotionFreightFromRequest(value api.PromotionFreight) (promotion.Freight, error) {
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
				longestSoak, err := parseFreightSoak("longestCompletedSoak", evidence.LongestCompletedSoak)
				if err != nil {
					return promotion.Freight{}, err
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
			Origin: item.Origin, Name: item.Name, DiscoveredAt: item.DiscoveredAt, Artifacts: artifacts,
			ApprovedFor: approvedFor, VerifiedIn: verifiedIn, CurrentlyIn: currentlyIn,
			EvidenceObserved: item.ApprovedFor != nil && item.VerifiedIn != nil && item.CurrentlyIn != nil,
		})
	}

	return promotion.Freight{Items: items}, nil
}

func parseFreightSoak(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("%s must be a nonnegative Go duration", field)
	}

	return duration, nil
}

func promotionStageConditionsFromRequest(values []api.PromotionStageCondition) []promotion.StageCondition {
	conditions := make([]promotion.StageCondition, 0, len(values))
	for _, value := range values {
		conditions = append(conditions, promotion.StageCondition{
			Type:               value.Type,
			Status:             value.Status,
			Reason:             valueOrEmptyString(value.Reason),
			Message:            valueOrEmptyString(value.Message),
			LastTransitionTime: value.LastTransitionTime,
		})
	}

	return conditions
}

func promotionStagePromotionFromRequest(
	value *api.PromotionStagePromotionReference,
) *promotion.StagePromotionReference {
	if value == nil {
		return nil
	}

	return &promotion.StagePromotionReference{
		Name: value.Name, Phase: valueOrEmptyString(value.Phase), FinishedAt: value.FinishedAt,
	}
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
