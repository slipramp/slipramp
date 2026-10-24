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
	clusters                 ClusterService
	identities               ControllerIdentityService
	controllerCommands       ControllerCommandService
	repositories             RepositoryService
	gitTriggerDeliveries     GitTriggerDeliveryService
	commits                  CommitService
	catalogs                 CatalogService
	inbox                    InboxService
	notifications            NotificationService
	runs                     RunService
	artifacts                ArtifactService
	promotionRequests        PromotionRequestService
	promotionCreations       PromotionCreationService
	promotionDispatches      PromotionCreationDispatchService
	promotionReconciliations PromotionCreationReconciliationService
	promotionStages          PromotionStageService
	promotionReviews         PromotionReviewService
	promotionProjects        PromotionProjectService
	promotionPipelines       PromotionPipelineService
	promotionDrift           PromotionBranchDriftService
	kargoIntegrations        promotion.IntegrationSet
	readiness                ReadinessService
	productAuth              ProductAuthenticationService
	now                      func() time.Time
}

type ReadinessService interface {
	Check(ctx context.Context) readiness.Report
}

type ProductAuthenticationService interface {
	BrowserAuthenticationService
	AccessService
}

type Dependencies struct {
	Clusters                 ClusterService
	ControllerIdentities     ControllerIdentityService
	ControllerCommands       ControllerCommandService
	Repositories             RepositoryService
	GitTriggerDeliveries     GitTriggerDeliveryService
	Commits                  CommitService
	Catalogs                 CatalogService
	Inbox                    InboxService
	Notifications            NotificationService
	Runs                     RunService
	Artifacts                ArtifactService
	PromotionRequests        PromotionRequestService
	PromotionCreations       PromotionCreationService
	PromotionDispatches      PromotionCreationDispatchService
	PromotionReconciliations PromotionCreationReconciliationService
	PromotionStages          PromotionStageService
	PromotionReviews         PromotionReviewService
	PromotionProjects        PromotionProjectService
	PromotionPipelines       PromotionPipelineService
	PromotionBranchDrift     PromotionBranchDriftService
	KargoIntegrations        promotion.IntegrationSet
	Readiness                ReadinessService
	ProductAuthentication    ProductAuthenticationService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func newHandler(dependencies Dependencies) *Handler {
	return &Handler{
		clusters:                 dependencies.Clusters,
		identities:               dependencies.ControllerIdentities,
		controllerCommands:       dependencies.ControllerCommands,
		repositories:             dependencies.Repositories,
		gitTriggerDeliveries:     dependencies.GitTriggerDeliveries,
		commits:                  dependencies.Commits,
		catalogs:                 dependencies.Catalogs,
		inbox:                    dependencies.Inbox,
		notifications:            dependencies.Notifications,
		runs:                     dependencies.Runs,
		artifacts:                dependencies.Artifacts,
		promotionRequests:        dependencies.PromotionRequests,
		promotionCreations:       dependencies.PromotionCreations,
		promotionDispatches:      dependencies.PromotionDispatches,
		promotionReconciliations: dependencies.PromotionReconciliations,
		promotionStages:          dependencies.PromotionStages,
		promotionReviews:         dependencies.PromotionReviews,
		promotionProjects:        dependencies.PromotionProjects,
		promotionPipelines:       dependencies.PromotionPipelines,
		promotionDrift:           dependencies.PromotionBranchDrift,
		kargoIntegrations:        dependencies.KargoIntegrations,
		readiness:                dependencies.Readiness,
		productAuth:              dependencies.ProductAuthentication,
		now:                      time.Now,
	}
}
