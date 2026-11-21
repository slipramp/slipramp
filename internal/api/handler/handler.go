package handler

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/readiness"
)

type ControllerIdentityService interface {
	EnrollController(
		ctx context.Context,
		clusterName string,
		credentialSHA256 string,
		buildInfo cluster.ControllerBuildInfo,
	) error
	AuthenticateController(ctx context.Context, bearerToken string) (controlleridentity.Principal, error)
}

type Handler struct {
	serviceVersion           string
	clusters                 ClusterService
	placements               PlacementService
	placementPreflights      PlacementPreflightService
	identities               ControllerIdentityService
	controllerCommands       ControllerCommandService
	controllerCommandWakeups ControllerCommandWakeups
	repositories             RepositoryService
	gitTriggerDeliveries     GitTriggerDeliveryService
	commits                  CommitService
	catalogs                 CatalogService
	inbox                    InboxService
	notifications            NotificationService
	runs                     RunService
	gateSources              gate.SourceSet
	gateOverrides            GateOverrideStore
	gateEvaluatorProcessID   uuid.UUID
	artifacts                ArtifactService
	promotionRequests        PromotionRequestService
	promotionCreations       PromotionCreationService
	promotionDispatches      PromotionCreationDispatchService
	promotionReconciliations PromotionReconciliationService
	promotionCancellations   PromotionCancellationService
	promotionStages          PromotionStageService
	promotionReviews         PromotionReviewService
	promotionProjects        PromotionProjectService
	promotionDrift           PromotionBranchDriftService
	kargoIntegrations        promotion.IntegrationSet
	readiness                ReadinessService
	productAuth              ProductAuthenticationService
	now                      func() time.Time
}

type ReadinessService interface {
	Check(ctx context.Context) readiness.Report
}

type PlacementService interface {
	Inventory(ctx context.Context) (placement.Inventory, error)
}

type PlacementPreflightService interface {
	Create(context.Context, string, *productidentity.Principal) (placement.Preflight, error)
	List(context.Context, placement.PreflightListFilter) (placement.PreflightList, error)
	Report(context.Context, placement.PreflightReport) (placement.Preflight, error)
}

type ProductAuthenticationService interface {
	BrowserAuthenticationService
	AccessService
}

type Dependencies struct {
	ServiceVersion           string
	Clusters                 ClusterService
	Placements               PlacementService
	PlacementPreflights      PlacementPreflightService
	ControllerIdentities     ControllerIdentityService
	ControllerCommands       ControllerCommandService
	ControllerCommandWakeups ControllerCommandWakeups
	Repositories             RepositoryService
	GitTriggerDeliveries     GitTriggerDeliveryService
	Commits                  CommitService
	Catalogs                 CatalogService
	Inbox                    InboxService
	Notifications            NotificationService
	Runs                     RunService
	GateSources              gate.SourceSet
	GateOverrides            GateOverrideStore
	GateEvaluatorProcessID   uuid.UUID
	Artifacts                ArtifactService
	PromotionRequests        PromotionRequestService
	PromotionCreations       PromotionCreationService
	PromotionDispatches      PromotionCreationDispatchService
	PromotionReconciliations PromotionReconciliationService
	PromotionCancellations   PromotionCancellationService
	PromotionStages          PromotionStageService
	PromotionReviews         PromotionReviewService
	PromotionProjects        PromotionProjectService
	PromotionBranchDrift     PromotionBranchDriftService
	KargoIntegrations        promotion.IntegrationSet
	Readiness                ReadinessService
	ProductAuthentication    ProductAuthenticationService
}

var _ api.StrictServerInterface = (*Handler)(nil)

func newHandler(dependencies Dependencies) *Handler {
	serviceVersion := dependencies.ServiceVersion
	if serviceVersion == "" {
		serviceVersion = "dev"
	}

	return &Handler{
		serviceVersion:           serviceVersion,
		clusters:                 dependencies.Clusters,
		placements:               dependencies.Placements,
		placementPreflights:      dependencies.PlacementPreflights,
		identities:               dependencies.ControllerIdentities,
		controllerCommands:       dependencies.ControllerCommands,
		controllerCommandWakeups: dependencies.ControllerCommandWakeups,
		repositories:             dependencies.Repositories,
		gitTriggerDeliveries:     dependencies.GitTriggerDeliveries,
		commits:                  dependencies.Commits,
		catalogs:                 dependencies.Catalogs,
		inbox:                    dependencies.Inbox,
		notifications:            dependencies.Notifications,
		runs:                     dependencies.Runs,
		gateSources:              dependencies.GateSources,
		gateOverrides:            dependencies.GateOverrides,
		gateEvaluatorProcessID:   dependencies.GateEvaluatorProcessID,
		artifacts:                dependencies.Artifacts,
		promotionRequests:        dependencies.PromotionRequests,
		promotionCreations:       dependencies.PromotionCreations,
		promotionDispatches:      dependencies.PromotionDispatches,
		promotionReconciliations: dependencies.PromotionReconciliations,
		promotionCancellations:   dependencies.PromotionCancellations,
		promotionStages:          dependencies.PromotionStages,
		promotionReviews:         dependencies.PromotionReviews,
		promotionProjects:        dependencies.PromotionProjects,
		promotionDrift:           dependencies.PromotionBranchDrift,
		kargoIntegrations:        dependencies.KargoIntegrations,
		readiness:                dependencies.Readiness,
		productAuth:              dependencies.ProductAuthentication,
		now:                      time.Now,
	}
}
