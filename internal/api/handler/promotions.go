package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
)

type PromotionRequestService interface {
	SubmitKargoPromotion(
		ctx context.Context,
		integrationName string,
		request promotion.KargoRequest,
		principal productidentity.Principal,
	) (promotion.Promotion, error)
}

type PromotionStageService interface {
	ReplaceStageSnapshot(
		ctx context.Context,
		integrationName string,
		clusterName string,
		replacement promotion.StageSnapshotReplacement,
	) error
}

type PromotionReviewService interface {
	ExpirePending(context.Context) error
	ListStages(context.Context, promotion.StageListFilter) (promotion.StageReviewList, error)
	UpdateStagePolicy(
		context.Context,
		uuid.UUID,
		promotion.PolicyUpdate,
		productidentity.Principal,
	) (promotion.PolicyView, error)
	ListPromotions(context.Context, promotion.PromotionListFilter) (promotion.PromotionReviewList, error)
	GetPromotion(context.Context, uuid.UUID) (promotion.PromotionReview, error)
	Decide(context.Context, promotion.DecisionRequest) (promotion.PromotionReview, error)
}

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
	if h.promotionReviews != nil {
		if err := h.promotionReviews.ExpirePending(ctx); err != nil {
			return nil, err
		}
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
	ctx context.Context,
	request api.ListPromotionStagesRequestObject,
) (api.ListPromotionStagesResponseObject, error) {
	filter := promotion.StageListFilter{
		Integration: optionalCatalogName(request.Params.Integration),
		Cluster:     optionalCatalogName(request.Params.Cluster),
		Project:     optionalCatalogName(request.Params.Project),
		Archived:    request.Params.Archived,
		Limit:       promotion.DefaultListLimit,
	}
	if request.Params.Limit != nil {
		filter.Limit = int(*request.Params.Limit)
	}
	if request.Params.Offset != nil {
		filter.Offset = int(*request.Params.Offset)
	}

	result, err := h.promotionReviews.ListStages(ctx, filter)
	if errors.Is(err, promotion.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListPromotionStages400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]api.PromotionStage, 0, len(result.Items))
	for _, review := range result.Items {
		item, convertErr := promotionStageResponse(review)
		if convertErr != nil {
			return nil, convertErr
		}
		items = append(items, item)
	}

	return api.ListPromotionStages200JSONResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}

func (h *Handler) UpdatePromotionStagePolicy(
	ctx context.Context,
	request api.UpdatePromotionStagePolicyRequestObject,
) (api.UpdatePromotionStagePolicyResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A Promotion Stage policy update is required.")

		return api.UpdatePromotionStagePolicy400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	principal := currentProductPrincipal(ctx)
	if principal == nil {
		return nil, errors.New("authenticated policy actor is missing")
	}

	policy, err := h.promotionReviews.UpdateStagePolicy(
		ctx,
		request.StageID,
		promotion.PolicyUpdate{
			Mode:      promotion.PolicyMode(request.Body.Mode),
			ExpiresAt: request.Body.ExpiresAt,
			Reason:    request.Body.Reason,
		},
		*principal,
	)
	if err != nil {
		switch {
		case errors.Is(err, promotion.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.UpdatePromotionStagePolicy400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrStageNotFound):
			problem := NewProblem(http.StatusNotFound, "The observed Kargo Stage does not exist.")

			return api.UpdatePromotionStagePolicy404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrStageArchived), errors.Is(err, promotion.ErrTransitionConflict):
			problem := NewProblem(http.StatusConflict, err.Error())

			return api.UpdatePromotionStagePolicy409ApplicationProblemPlusJSONResponse{
				ConflictApplicationProblemPlusJSONResponse: api.ConflictApplicationProblemPlusJSONResponse(problem),
			}, nil
		default:
			return nil, err
		}
	}

	return api.UpdatePromotionStagePolicy200JSONResponse(promotionPolicyResponse(policy)), nil
}

func (h *Handler) ListPromotions(
	ctx context.Context,
	request api.ListPromotionsRequestObject,
) (api.ListPromotionsResponseObject, error) {
	filter := promotion.PromotionListFilter{
		Integration: optionalCatalogName(request.Params.Integration),
		Cluster:     optionalCatalogName(request.Params.Cluster),
		Project:     optionalCatalogName(request.Params.Project),
		StageID:     request.Params.StageID,
		Limit:       promotion.DefaultListLimit,
	}
	if request.Params.Status != nil {
		filter.Status = promotion.PromotionStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		filter.Limit = int(*request.Params.Limit)
	}
	if request.Params.Offset != nil {
		filter.Offset = int(*request.Params.Offset)
	}

	result, err := h.promotionReviews.ListPromotions(ctx, filter)
	if errors.Is(err, promotion.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListPromotions400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]api.Promotion, 0, len(result.Items))
	for _, review := range result.Items {
		item, convertErr := promotionResponse(review)
		if convertErr != nil {
			return nil, convertErr
		}
		items = append(items, item)
	}

	return api.ListPromotions200JSONResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}

func (h *Handler) GetPromotion(
	ctx context.Context,
	request api.GetPromotionRequestObject,
) (api.GetPromotionResponseObject, error) {
	review, err := h.promotionReviews.GetPromotion(ctx, request.PromotionID)
	if errors.Is(err, promotion.ErrPromotionNotFound) {
		problem := NewProblem(http.StatusNotFound, "The Promotion does not exist.")

		return api.GetPromotion404ApplicationProblemPlusJSONResponse{
			NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	body, err := h.promotionDetailResponse(ctx, review)
	if err != nil {
		return nil, err
	}

	return api.GetPromotion200JSONResponse(body), nil
}

func (h *Handler) DecidePromotion(
	ctx context.Context,
	request api.DecidePromotionRequestObject,
) (api.DecidePromotionResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A Promotion decision is required.")

		return api.DecidePromotion400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	principal := currentProductPrincipal(ctx)
	if principal == nil {
		return nil, errors.New("authenticated decision actor is missing")
	}

	review, err := h.promotionReviews.Decide(
		ctx,
		promotion.DecisionRequest{
			PromotionID: request.PromotionID,
			Decision:    promotion.DecisionValue(request.Body.Decision),
			Comment:     request.Body.Comment,
			Principal:   *principal,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, promotion.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.DecidePromotion400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrPromotionNotFound):
			problem := NewProblem(http.StatusNotFound, "The Promotion does not exist.")

			return api.DecidePromotion404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrDecisionConflict):
			problem := NewProblem(http.StatusConflict, "The Promotion already has a different immutable decision.")

			return api.DecidePromotion409ApplicationProblemPlusJSONResponse{
				ConflictApplicationProblemPlusJSONResponse: api.ConflictApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrTransitionConflict):
			problem := NewProblem(http.StatusConflict, "Only a pending, unexpired Promotion can be decided.")

			return api.DecidePromotion409ApplicationProblemPlusJSONResponse{
				ConflictApplicationProblemPlusJSONResponse: api.ConflictApplicationProblemPlusJSONResponse(problem),
			}, nil
		default:
			return nil, err
		}
	}

	body, err := h.promotionDetailResponse(ctx, review)
	if err != nil {
		return nil, err
	}

	return api.DecidePromotion200JSONResponse(body), nil
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

func optionalCatalogName(value *api.CatalogName) string {
	if value == nil {
		return ""
	}

	return string(*value)
}

func promotionStageResponse(review promotion.StageReview) (api.PromotionStage, error) {
	freight, err := rawObject(review.Stage.Freight)
	if err != nil {
		return api.PromotionStage{}, fmt.Errorf("decode Promotion Stage Freight: %w", err)
	}
	var acknowledgement *map[string]any
	if len(review.Stage.SuccessfulRenderAcknowledgement) > 0 {
		value, decodeErr := rawObject(review.Stage.SuccessfulRenderAcknowledgement)
		if decodeErr != nil {
			return api.PromotionStage{}, fmt.Errorf("decode Promotion Stage acknowledgement: %w", decodeErr)
		}
		acknowledgement = &value
	}

	return api.PromotionStage{
		ID:                              review.Stage.ID,
		Integration:                     api.CatalogName(review.Stage.Integration),
		Cluster:                         api.CatalogName(review.Stage.ClusterName),
		Project:                         api.CatalogName(review.Stage.Project),
		Name:                            review.Stage.Name,
		KubernetesUID:                   review.Stage.KubernetesUID,
		ResourceVersion:                 review.Stage.ResourceVersion,
		Application:                     review.Stage.Application,
		Environment:                     review.Stage.Environment,
		Repository:                      api.CatalogName(review.Stage.RepositoryName),
		RepositoryURL:                   review.Stage.RepositoryURL,
		TargetBranch:                    review.Stage.TargetBranch,
		SourcePaths:                     review.Stage.SourcePaths,
		Freight:                         freight,
		SuccessfulRenderAcknowledgement: acknowledgement,
		Policy:                          promotionPolicyResponse(review.Policy),
		PendingPromotionCount:           review.PendingPromotionCount,
		FirstObservedAt:                 review.Stage.FirstObservedAt,
		LastObservedAt:                  review.Stage.LastObservedAt,
		ArchivedAt:                      review.Stage.ArchivedAt,
	}, nil
}

func promotionPolicyResponse(value promotion.PolicyView) api.PromotionStagePolicy {
	return api.PromotionStagePolicy{
		ConfiguredMode: api.PromotionPolicyMode(value.ConfiguredMode),
		EffectiveMode:  api.PromotionPolicyMode(value.EffectiveMode),
		ExpiresAt:      value.ExpiresAt,
		Reason:         value.Reason,
		UpdatedBy:      apiRunActor(value.UpdatedBy),
		UpdatedAt:      value.UpdatedAt,
	}
}

func promotionResponse(review promotion.PromotionReview) (api.Promotion, error) {
	record := review.Promotion
	stageSnapshot, err := rawObject(record.StageSnapshot)
	if err != nil {
		return api.Promotion{}, fmt.Errorf("decode Promotion Stage snapshot: %w", err)
	}
	repositorySnapshot, err := rawObject(record.RepositorySnapshot)
	if err != nil {
		return api.Promotion{}, fmt.Errorf("decode Promotion repository snapshot: %w", err)
	}
	policySnapshot, err := rawObject(record.PolicySnapshot)
	if err != nil {
		return api.Promotion{}, fmt.Errorf("decode Promotion policy snapshot: %w", err)
	}
	requestedBy := apiRunActor(&record.RequestedBySnapshot)
	if requestedBy == nil {
		return api.Promotion{}, errors.New("promotion requesting actor is missing")
	}

	body := api.Promotion{
		ID:                     record.ID,
		Integration:            api.CatalogName(record.Integration),
		ExternalPromotionID:    record.ExternalPromotionID,
		StageID:                record.StageID,
		StageSnapshot:          stageSnapshot,
		RepositoryID:           record.RepositoryID,
		RepositorySnapshot:     repositorySnapshot,
		BaseRevision:           record.BaseRevision,
		CandidateRevision:      record.CandidateRevision,
		CandidateBranch:        record.CandidateBranch,
		TargetBranch:           record.TargetBranch,
		PolicySnapshot:         policySnapshot,
		ReviewRequired:         record.ReviewRequired,
		Status:                 api.PromotionStatus(record.Status),
		Message:                record.Message,
		PreparationRunID:       record.PreparationRunID,
		GitDiffArtifactID:      record.GitDiffArtifactID,
		ArgoDiffArtifactID:     record.ArgoDiffArtifactID,
		RequestedBy:            *requestedBy,
		CreatedAt:              record.CreatedAt,
		PreparationCompletedAt: record.PreparationCompletedAt,
		DecidedAt:              record.DecidedAt,
		ExpiresAt:              record.ExpiresAt,
		UpdatedAt:              record.UpdatedAt,
	}
	if review.Decision != nil {
		decidedBy := apiRunActor(&review.Decision.DecidedBySnapshot)
		if decidedBy == nil {
			return api.Promotion{}, errors.New("promotion decision actor is missing")
		}
		body.Decision = &api.PromotionDecision{
			Decision:  api.PromotionDecisionDecision(review.Decision.Decision),
			Comment:   review.Decision.Comment,
			DecidedBy: *decidedBy,
			DecidedAt: review.Decision.DecidedAt,
		}
	}

	return body, nil
}

func (h *Handler) promotionDetailResponse(
	ctx context.Context,
	review promotion.PromotionReview,
) (api.Promotion, error) {
	body, err := promotionResponse(review)
	if err != nil {
		return api.Promotion{}, err
	}
	if review.Promotion.PreparationRunID == nil {
		return body, nil
	}

	preparationRun, err := h.runs.GetRun(ctx, *review.Promotion.PreparationRunID)
	if err != nil {
		return api.Promotion{}, fmt.Errorf("load Promotion preparation Run: %w", err)
	}
	runBody, err := h.runResponse(ctx, preparationRun)
	if err != nil {
		return api.Promotion{}, err
	}
	body.PreparationRun = &runBody

	artifacts, err := h.artifacts.List(ctx, *review.Promotion.PreparationRunID)
	if err != nil {
		return api.Promotion{}, fmt.Errorf("load Promotion preparation artifacts: %w", err)
	}
	for _, record := range artifacts {
		switch record.ID {
		case valueOrNilUUID(review.Promotion.GitDiffArtifactID):
			value := artifactResponse(record)
			body.GitDiffArtifact = &value
		case valueOrNilUUID(review.Promotion.ArgoDiffArtifactID):
			value := artifactResponse(record)
			body.ArgoDiffArtifact = &value
		}
	}

	return body, nil
}

func rawObject(contents []byte) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal(contents, &value); err != nil {
		return nil, err
	}

	return value, nil
}

func valueOrNilUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}

	return *value
}
