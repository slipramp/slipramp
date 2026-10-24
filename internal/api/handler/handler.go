package handler

import (
	"context"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/readiness"
)

type ControllerIdentityService interface {
	EnrollController(ctx context.Context, clusterName string, credentialSHA256 string) error
	AuthenticateController(ctx context.Context, bearerToken string) (controlleridentity.Principal, error)
}

type Handler struct {
	clusters             ClusterService
	identities           ControllerIdentityService
	repositories         RepositoryService
	gitTriggerDeliveries GitTriggerDeliveryService
	commits              CommitService
	catalogs             CatalogService
	inbox                InboxService
	runs                 RunService
	artifacts            ArtifactService
	promotionRequests    PromotionRequestService
	promotionStages      PromotionStageService
	promotionReviews     PromotionReviewService
	promotionProjects    PromotionProjectService
	promotionPipelines   PromotionPipelineService
	promotionDrift       PromotionBranchDriftService
	kargoIntegrations    promotion.IntegrationSet
	readiness            ReadinessService
	productAuth          ProductAuthenticationService
	now                  func() time.Time
}

type ReadinessService interface {
	Check(ctx context.Context) readiness.Report
}

type ProductAuthenticationService interface {
	BrowserAuthenticationService
	AccessService
}

type Dependencies struct {
	Clusters              ClusterService
	ControllerIdentities  ControllerIdentityService
	Repositories          RepositoryService
	GitTriggerDeliveries  GitTriggerDeliveryService
	Commits               CommitService
	Catalogs              CatalogService
	Inbox                 InboxService
	Runs                  RunService
	Artifacts             ArtifactService
	PromotionRequests     PromotionRequestService
	PromotionStages       PromotionStageService
	PromotionReviews      PromotionReviewService
	PromotionProjects     PromotionProjectService
	PromotionPipelines    PromotionPipelineService
	PromotionBranchDrift  PromotionBranchDriftService
	KargoIntegrations     promotion.IntegrationSet
	Readiness             ReadinessService
	ProductAuthentication ProductAuthenticationService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func newHandler(dependencies Dependencies) *Handler {
	return &Handler{
		clusters:             dependencies.Clusters,
		identities:           dependencies.ControllerIdentities,
		repositories:         dependencies.Repositories,
		gitTriggerDeliveries: dependencies.GitTriggerDeliveries,
		commits:              dependencies.Commits,
		catalogs:             dependencies.Catalogs,
		inbox:                dependencies.Inbox,
		runs:                 dependencies.Runs,
		artifacts:            dependencies.Artifacts,
		promotionRequests:    dependencies.PromotionRequests,
		promotionStages:      dependencies.PromotionStages,
		promotionReviews:     dependencies.PromotionReviews,
		promotionProjects:    dependencies.PromotionProjects,
		promotionPipelines:   dependencies.PromotionPipelines,
		promotionDrift:       dependencies.PromotionBranchDrift,
		kargoIntegrations:    dependencies.KargoIntegrations,
		readiness:            dependencies.Readiness,
		productAuth:          dependencies.ProductAuthentication,
		now:                  time.Now,
	}
}
