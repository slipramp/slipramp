package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

type PromotionRequestService interface {
	SubmitKargoPromotion(
		ctx context.Context,
		integrationName string,
		request promotion.KargoRequest,
		principal productidentity.Principal,
	) (promotion.Promotion, error)
}

type PromotionCreationService interface {
	Create(context.Context, promotion.OperatorPromotionRequest) (promotion.Promotion, error)
}

type PromotionCreationDispatchService interface {
	Report(context.Context, string, promotion.CreationDispatchReport) error
}

type PromotionCreationReconciliationService interface {
	Report(context.Context, string, promotion.CreationReconciliationReport) error
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

type PromotionPipelineService interface {
	List(
		context.Context,
		promotion.FreightPipelineListFilter,
	) (promotion.FreightPipelineList, error)
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

func (h *Handler) CreatePromotion(
	ctx context.Context,
	request api.CreatePromotionRequestObject,
) (api.CreatePromotionResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A Promotion creation request is required.")

		return api.CreatePromotion400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	principal := currentProductPrincipal(ctx)
	if principal == nil {
		return nil, errors.New("authenticated Promotion creator is missing")
	}

	record, err := h.promotionCreations.Create(ctx, promotion.OperatorPromotionRequest{
		TargetStageID: request.StageID,
		SourceStageID: request.Body.SourceStageID,
		FreightName:   request.Body.FreightName,
		Principal:     *principal,
	})
	if err != nil {
		switch {
		case errors.Is(err, promotion.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.CreatePromotion400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrStageNotFound), errors.Is(err, promotion.ErrIntegrationNotFound):
			problem := NewProblem(http.StatusNotFound, "The observed Kargo Stage does not exist.")

			return api.CreatePromotion404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
			}, nil
		case errors.Is(err, promotion.ErrStageArchived), errors.Is(err, promotion.ErrStageStale),
			errors.Is(err, promotion.ErrStageScope), errors.Is(err, promotion.ErrFreightNotFound),
			errors.Is(err, promotion.ErrFreightSource), errors.Is(err, promotion.ErrRepositoryNotAllowed),
			errors.Is(err, promotion.ErrRepositoryUnavailable):
			problem := NewProblem(http.StatusConflict, err.Error())

			return api.CreatePromotion409ApplicationProblemPlusJSONResponse{
				ConflictApplicationProblemPlusJSONResponse: api.ConflictApplicationProblemPlusJSONResponse(problem),
			}, nil
		default:
			return nil, err
		}
	}

	body, err := promotionResponse(
		promotion.PromotionReview{Promotion: record},
		nil,
		h.kargoIntegrations,
	)
	if err != nil {
		return nil, err
	}
	location := fmt.Sprintf("/api/v1/promotions/%s", record.ID)

	return api.CreatePromotion201JSONResponse{
		Body: body,
		Headers: api.CreatePromotion201ResponseHeaders{
			Location: &location,
		},
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
	if request.Params.Application != nil {
		filter.Application = *request.Params.Application
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
		item := h.promotionStageResponse(review)
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
		RepositoryID: request.Params.RepositoryID,
		Integration:  optionalCatalogName(request.Params.Integration),
		Cluster:      optionalCatalogName(request.Params.Cluster),
		Project:      optionalCatalogName(request.Params.Project),
		StageID:      request.Params.StageID,
		Limit:        promotion.DefaultListLimit,
	}
	if request.Params.CommitSha != nil {
		filter.CommitSHA = *request.Params.CommitSha
	}
	if request.Params.Application != nil {
		filter.Application = *request.Params.Application
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
	keys := make([]repository.CommitKey, 0, len(result.Items)*2)
	for _, review := range result.Items {
		if review.Promotion.BaseRevision != "" {
			keys = append(keys, commitKey(review.Promotion.RepositoryID, review.Promotion.BaseRevision))
		}
		if review.Promotion.CandidateRevision != "" {
			keys = append(keys, commitKey(review.Promotion.RepositoryID, review.Promotion.CandidateRevision))
		}
	}
	commits, commitErr := h.commitResponses(ctx, keys)
	if commitErr != nil {
		commits = commitFallbacks(keys)
	}

	items := make([]api.Promotion, 0, len(result.Items))
	for _, review := range result.Items {
		item, convertErr := promotionResponse(review, commits, h.kargoIntegrations)
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
			RequestedFreight:                promotionFreightRequests(observed.RequestedFreight),
			Freight:                         promotionFreightFromRequest(observed.Freight),
			SuccessfulRenderAcknowledgement: promotionAcknowledgementFromRequest(observed.SuccessfulRenderAcknowledgement),
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

func (h *Handler) promotionStageResponse(review promotion.StageReview) api.PromotionStage {
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
		RequestedFreight:                promotionFreightRequestResponse(review.Stage.RequestedFreight),
		Freight:                         promotionFreightResponse(review.Stage.Freight),
		SuccessfulRenderAcknowledgement: promotionAcknowledgementResponse(review.Stage.SuccessfulRenderAcknowledgement),
		Policy:                          promotionPolicyResponse(review.Policy),
		PendingPromotionCount:           review.PendingPromotionCount,
		FirstObservedAt:                 review.Stage.FirstObservedAt,
		LastObservedAt:                  review.Stage.LastObservedAt,
		ArchivedAt:                      review.Stage.ArchivedAt,
		KargoURL: h.kargoIntegrations.KargoStageURL(
			review.Stage.Integration,
			review.Stage.Project,
			review.Stage.Name,
		),
	}
}

func promotionFreightRequests(values []api.PromotionFreightRequest) []promotion.FreightRequest {
	requests := make([]promotion.FreightRequest, 0, len(values))
	for _, value := range values {
		requests = append(requests, promotion.FreightRequest{
			Origin:       value.Origin,
			Direct:       value.Direct,
			SourceStages: value.SourceStages,
		})
	}

	return requests
}

func promotionFreightRequestResponse(values []promotion.FreightRequest) []api.PromotionFreightRequest {
	if values == nil {
		return []api.PromotionFreightRequest{}
	}
	requests := make([]api.PromotionFreightRequest, 0, len(values))
	for _, value := range values {
		requests = append(requests, api.PromotionFreightRequest{
			Origin:       value.Origin,
			Direct:       value.Direct,
			SourceStages: value.SourceStages,
		})
	}

	return requests
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

func promotionResponse(
	review promotion.PromotionReview,
	commits map[repository.CommitKey]api.GitCommit,
	integrations promotion.IntegrationSet,
) (api.Promotion, error) {
	record := review.Promotion
	requestedBy := apiRunActor(&record.RequestedBySnapshot)
	if requestedBy == nil {
		return api.Promotion{}, errors.New("promotion requesting actor is missing")
	}
	project := record.StageSnapshot.Project
	var selectedFreight *api.PromotionFreightItem
	if record.SelectedFreightSnapshot != nil {
		value := promotionFreightItemResponse(*record.SelectedFreightSnapshot)
		selectedFreight = &value
	}
	var baseCommit *api.GitCommit
	if record.BaseRevision != "" {
		value := commits[commitKey(record.RepositoryID, record.BaseRevision)]
		baseCommit = &value
	}
	var candidateCommit *api.GitCommit
	if record.CandidateRevision != "" {
		value := commits[commitKey(record.RepositoryID, record.CandidateRevision)]
		candidateCommit = &value
	}
	var candidateBranch *string
	if record.CandidateBranch != "" {
		candidateBranch = &record.CandidateBranch
	}

	body := api.Promotion{
		ID:                     record.ID,
		Source:                 api.PromotionSource(record.Source),
		Integration:            api.CatalogName(record.Integration),
		ExternalPromotionID:    record.ExternalPromotionID,
		StageID:                record.StageID,
		SourceStageID:          record.SourceStageID,
		StageSnapshot:          promotionStageSnapshotResponse(record.StageSnapshot),
		RepositoryID:           record.RepositoryID,
		RepositorySnapshot:     promotionRepositorySnapshotResponse(record.RepositorySnapshot),
		BaseCommit:             baseCommit,
		CandidateCommit:        candidateCommit,
		CandidateBranch:        candidateBranch,
		TargetBranch:           record.TargetBranch,
		PolicySnapshot:         promotionPolicyResponse(promotion.PolicyView(record.PolicySnapshot)),
		SelectedFreight:        selectedFreight,
		ReviewRequired:         record.ReviewRequired,
		Status:                 api.PromotionStatus(record.Status),
		Message:                record.Message,
		DispatchAttemptCount:   record.DispatchAttemptCount,
		NextDispatchAt:         record.NextDispatchAt,
		LastDispatchAt:         record.LastDispatchAt,
		DispatchedAt:           record.DispatchedAt,
		KargoPromotionUID:      record.KargoPromotionUID,
		NextReconciliationAt:   record.NextReconciliationAt,
		LastReconciliationAt:   record.LastReconciliationAt,
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
	if record.Source == promotion.SourceKargo || record.KargoPromotionUID != nil {
		body.KargoURL = integrations.KargoPromotionURL(
			record.Integration,
			project,
			record.ExternalPromotionID,
		)
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
	keys := make([]repository.CommitKey, 0, 2)
	if review.Promotion.BaseRevision != "" {
		keys = append(keys, commitKey(review.Promotion.RepositoryID, review.Promotion.BaseRevision))
	}
	if review.Promotion.CandidateRevision != "" {
		keys = append(keys, commitKey(review.Promotion.RepositoryID, review.Promotion.CandidateRevision))
	}
	commits, commitErr := h.commitResponses(ctx, keys)
	if commitErr != nil {
		commits = commitFallbacks(keys)
	}
	body, err := promotionResponse(review, commits, h.kargoIntegrations)
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
	artifactKeys := make([]repository.CommitKey, 0, len(artifacts))
	for _, record := range artifacts {
		artifactKeys = append(
			artifactKeys,
			commitKey(record.Provenance.RepositoryID, record.Provenance.CommitSHA),
		)
	}
	artifactCommits, err := h.commitResponses(ctx, artifactKeys)
	if err != nil {
		return api.Promotion{}, err
	}
	for _, record := range artifacts {
		commit := artifactCommits[commitKey(record.Provenance.RepositoryID, record.Provenance.CommitSHA)]
		switch record.ID {
		case valueOrNilUUID(review.Promotion.GitDiffArtifactID):
			value, err := artifactResponse(record, commit)
			if err != nil {
				return api.Promotion{}, err
			}
			body.GitDiffArtifact = &value
		case valueOrNilUUID(review.Promotion.ArgoDiffArtifactID):
			value, err := artifactResponse(record, commit)
			if err != nil {
				return api.Promotion{}, err
			}
			body.ArgoDiffArtifact = &value
		}
	}

	return body, nil
}

func valueOrNilUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}

	return *value
}
