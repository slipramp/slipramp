package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

var errPromotionEndpointsUnavailable = errors.New("promotion endpoints are not implemented in this milestone")

func (h *Handler) SubmitKargoPromotion(
	ctx context.Context,
	request api.SubmitKargoPromotionRequestObject,
) (api.SubmitKargoPromotionResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A Kargo Promotion request is required.")

		return api.SubmitKargoPromotion400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	principal := currentProductPrincipal(ctx)
	if principal == nil {
		return nil, errors.New("authenticated Kargo service account is missing")
	}

	record, err := h.promotionRequests.SubmitKargoPromotion(
		ctx,
		string(request.Integration),
		promotion.KargoRequest{
			ExternalPromotionID: request.Body.ExternalPromotionID,
			Project:             string(request.Body.Project),
			Stage:               request.Body.Stage,
			BaseRevision:        request.Body.BaseRevision,
			CandidateRevision:   request.Body.CandidateRevision,
			CandidateBranch:     request.Body.CandidateBranch,
		},
		*principal,
	)
	if err != nil {
		switch {
		case errors.Is(err, promotion.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.SubmitKargoPromotion400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrIntegrationNotFound):
			problem := NewProblem(http.StatusNotFound, "The Kargo integration does not exist.")

			return api.SubmitKargoPromotion404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrStageNotFound):
			problem := NewProblem(http.StatusNotFound, "The observed Kargo Stage does not exist.")

			return api.SubmitKargoPromotion404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrIdentityConflict),
			errors.Is(err, promotion.ErrStageArchived),
			errors.Is(err, promotion.ErrStageStale),
			errors.Is(err, promotion.ErrRepositoryNotAllowed),
			errors.Is(err, promotion.ErrRepositoryUnavailable):
			problem := NewProblem(http.StatusConflict, err.Error())

			return api.SubmitKargoPromotion409ApplicationProblemPlusJSONResponse{
				ConflictApplicationProblemPlusJSONResponse: api.ConflictApplicationProblemPlusJSONResponse(problem),
			}, nil
		default:
			return nil, err
		}
	}

	return api.SubmitKargoPromotion200JSONResponse{
		ID:                  record.ID,
		ExternalPromotionID: record.ExternalPromotionID,
		Status:              api.PromotionStatus(record.Status),
		ReviewRequired:      record.ReviewRequired,
		Message:             record.Message,
		UpdatedAt:           record.UpdatedAt,
	}, nil
}

func (h *Handler) ListPromotionStages(
	context.Context,
	api.ListPromotionStagesRequestObject,
) (api.ListPromotionStagesResponseObject, error) {
	return nil, errPromotionEndpointsUnavailable
}

func (h *Handler) UpdatePromotionStagePolicy(
	context.Context,
	api.UpdatePromotionStagePolicyRequestObject,
) (api.UpdatePromotionStagePolicyResponseObject, error) {
	return nil, errPromotionEndpointsUnavailable
}

func (h *Handler) ListPromotions(
	context.Context,
	api.ListPromotionsRequestObject,
) (api.ListPromotionsResponseObject, error) {
	return nil, errPromotionEndpointsUnavailable
}

func (h *Handler) GetPromotion(
	context.Context,
	api.GetPromotionRequestObject,
) (api.GetPromotionResponseObject, error) {
	return nil, errPromotionEndpointsUnavailable
}

func (h *Handler) DecidePromotion(
	context.Context,
	api.DecidePromotionRequestObject,
) (api.DecidePromotionResponseObject, error) {
	return nil, errPromotionEndpointsUnavailable
}

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
		freight, marshalErr := json.Marshal(observed.Freight)
		if marshalErr != nil {
			return nil, marshalErr
		}
		var acknowledgement json.RawMessage
		if observed.SuccessfulRenderAcknowledgement != nil {
			acknowledgement, marshalErr = json.Marshal(*observed.SuccessfulRenderAcknowledgement)
			if marshalErr != nil {
				return nil, marshalErr
			}
		}
		stages = append(stages, promotion.StageObservation{
			Project:                         string(observed.Project),
			Name:                            observed.Name,
			KubernetesUID:                   observed.KubernetesUID,
			ResourceVersion:                 observed.ResourceVersion,
			Application:                     observed.Application,
			Environment:                     observed.Environment,
			RepositoryURL:                   observed.RepositoryURL,
			TargetBranch:                    observed.TargetBranch,
			SourcePaths:                     observed.SourcePaths,
			Freight:                         freight,
			SuccessfulRenderAcknowledgement: acknowledgement,
		})
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
		case errors.Is(err, promotion.ErrRepositoryNotAllowed),
			errors.Is(err, promotion.ErrSnapshotConflict),
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
