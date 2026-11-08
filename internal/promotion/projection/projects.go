package projection

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
)

type ProjectListFilter struct {
	Integration string
	Cluster     string
	Project     string
	Limit       int
	Offset      int
}

type ProjectKey struct {
	Integration string
	Cluster     string
	Project     string
}

type ProjectSummary struct {
	Integration         string
	ClusterName         string
	Project             string
	StageCount          int
	UnhealthyStageCount int
	StaleStageCount     int
	PendingReviewCount  int
	LatestObservedAt    time.Time
}

type ProjectList struct {
	Items  []ProjectSummary
	Total  int
	Limit  int
	Offset int
}

type ProjectDetail struct {
	Integration string
	ClusterName string
	Project     string
	Stages      []ProjectStage
}

type ProjectStage struct {
	Stage                 promotion.Stage
	Policy                promotion.PolicyView
	PendingPromotionCount int
	ObservationStale      bool
	RequestedFreight      []ProjectFreightRequest
	Capabilities          StageCapabilities
}

type ProjectFreightRequest struct {
	Request    promotion.FreightRequest
	Sources    []ProjectStageSource
	Candidates []FreightCandidate
}

type ProjectStageSource struct {
	Name    string
	StageID *uuid.UUID
}

type FreightCandidate struct {
	Freight  promotion.FreightItem
	Evidence promotion.FreightReadinessEvidence
}

type StageCapabilities struct {
	PromotionReady   bool
	BranchDriftReady bool
	Issues           []string
}

type ProjectStore interface {
	StagePolicyStore
	ListPromotionStages(context.Context) ([]promotion.Stage, error)
	CountPendingPromotionsByStage(context.Context, []uuid.UUID) (map[uuid.UUID]int, error)
}

type ProjectService struct {
	integrations promotion.IntegrationSet
	store        ProjectStore
	now          func() time.Time
}

func NewProjectService(integrations promotion.IntegrationSet, store ProjectStore) *ProjectService {
	return &ProjectService{integrations: integrations, store: store, now: time.Now}
}

func (s *ProjectService) List(ctx context.Context, filter ProjectListFilter) (ProjectList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return ProjectList{}, err
	}

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return ProjectList{}, err
	}
	stageIDs := make([]uuid.UUID, 0, len(stages))
	for _, stage := range stages {
		if stage.ArchivedAt == nil && stageMatchesProjectFilter(stage, filter) {
			stageIDs = append(stageIDs, stage.ID)
		}
	}
	pending, err := s.store.CountPendingPromotionsByStage(ctx, stageIDs)
	if err != nil {
		return ProjectList{}, err
	}

	builders := make(map[ProjectKey]*ProjectSummary)
	evaluatedAt := s.now().UTC()
	for _, stage := range stages {
		if stage.ArchivedAt != nil || !stageMatchesProjectFilter(stage, filter) {
			continue
		}
		integration, ok := s.integrations.Lookup(stage.Integration)
		if !ok || integration.ClusterName != stage.ClusterName {
			continue
		}
		key := ProjectKey{Integration: stage.Integration, Cluster: stage.ClusterName, Project: stage.Project}
		summary := builders[key]
		if summary == nil {
			summary = &ProjectSummary{
				Integration: stage.Integration, ClusterName: stage.ClusterName, Project: stage.Project,
			}
			builders[key] = summary
		}
		summary.StageCount++
		summary.PendingReviewCount += pending[stage.ID]
		if stageObservationStale(stage, integration, evaluatedAt) {
			summary.StaleStageCount++
		}
		if stageNonReady(stage) {
			summary.UnhealthyStageCount++
		}
		if stage.LastObservedAt.After(summary.LatestObservedAt) {
			summary.LatestObservedAt = stage.LastObservedAt
		}
	}

	items := make([]ProjectSummary, 0, len(builders))
	for _, summary := range builders {
		items = append(items, *summary)
	}
	slices.SortFunc(items, compareProjects)
	total := len(items)

	return ProjectList{Items: page(items, filter.Offset, filter.Limit), Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *ProjectService) Get(ctx context.Context, key ProjectKey) (ProjectDetail, error) {
	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return ProjectDetail{}, err
	}
	integration, configured := s.integrations.Lookup(key.Integration)
	if !configured || integration.ClusterName != key.Cluster {
		return ProjectDetail{}, promotion.ErrProjectNotFound
	}
	active := make([]promotion.Stage, 0)
	byName := make(map[string]promotion.Stage)
	stageIDs := make([]uuid.UUID, 0)
	for _, stage := range stages {
		if stage.ArchivedAt != nil || stage.Integration != key.Integration || stage.ClusterName != key.Cluster ||
			stage.Project != key.Project {
			continue
		}
		active = append(active, stage)
		byName[stage.Name] = stage
		stageIDs = append(stageIDs, stage.ID)
	}
	if len(active) == 0 {
		return ProjectDetail{}, promotion.ErrProjectNotFound
	}
	evaluatedAt := s.now().UTC()
	policies, err := stagePolicyViews(ctx, s.store, stageIDs, evaluatedAt)
	if err != nil {
		return ProjectDetail{}, err
	}
	pending, err := s.store.CountPendingPromotionsByStage(ctx, stageIDs)
	if err != nil {
		return ProjectDetail{}, err
	}

	items := make([]ProjectStage, 0, len(active))
	for _, stage := range active {
		requests := make([]ProjectFreightRequest, 0, len(stage.RequestedFreight))
		for _, request := range stage.RequestedFreight {
			sources := make([]ProjectStageSource, 0, len(request.SourceStages))
			for _, name := range request.SourceStages {
				source := ProjectStageSource{Name: name}
				if resolved, ok := byName[name]; ok {
					source.StageID = new(resolved.ID)
				}
				sources = append(sources, source)
			}
			candidates := make([]FreightCandidate, 0)
			for _, candidate := range stage.CandidateFreight.Items {
				if candidate.Origin != request.Origin {
					continue
				}
				candidates = append(candidates, FreightCandidate{
					Freight: candidate, Evidence: promotion.EvaluateFreightReadiness(stage, candidate),
				})
			}
			requests = append(requests, ProjectFreightRequest{
				Request: request, Sources: sources, Candidates: candidates,
			})
		}
		items = append(items, ProjectStage{
			Stage:                 stage,
			Policy:                policies[stage.ID],
			PendingPromotionCount: pending[stage.ID],
			ObservationStale:      stageObservationStale(stage, integration, evaluatedAt),
			RequestedFreight:      requests,
			Capabilities:          stageCapabilities(stage),
		})
	}
	slices.SortFunc(items, func(left, right ProjectStage) int {
		return compareText(left.Stage.Name, right.Stage.Name)
	})

	return ProjectDetail{
		Integration: key.Integration, ClusterName: key.Cluster, Project: key.Project, Stages: items,
	}, nil
}

func stageCapabilities(stage promotion.Stage) StageCapabilities {
	issues := make([]string, 0)
	for label, value := range map[string]string{
		"application": stage.Application, "environment": stage.Environment, "repository mapping": stage.RepositoryName,
		"repository URL": stage.RepositoryURL, "target branch": stage.TargetBranch,
	} {
		if value == "" {
			issues = append(issues, fmt.Sprintf("Slipramp %s enrichment is missing.", label))
		}
	}
	if len(stage.SourcePaths) == 0 {
		issues = append(issues, "Slipramp source path enrichment is missing.")
	}
	branchDriftReady := stage.RepositoryName != "" && stage.TargetBranch != "" && len(stage.SourcePaths) > 0
	promotionReady := branchDriftReady && stage.Application != "" && stage.Environment != "" &&
		stage.RepositoryURL != "" && !stage.ControlFlow
	if stage.ControlFlow {
		issues = append(issues, "Control-flow Stages do not materialize Promotions.")
	}
	slices.Sort(issues)

	return StageCapabilities{
		PromotionReady: promotionReady, BranchDriftReady: branchDriftReady, Issues: issues,
	}
}

func stageNonReady(stage promotion.Stage) bool {
	for _, condition := range stage.Conditions {
		if strings.EqualFold(condition.Type, "Ready") {
			return !strings.EqualFold(condition.Status, "True")
		}
	}
	if stage.HealthStatus == nil {
		return false
	}
	return !strings.EqualFold(*stage.HealthStatus, "Healthy") &&
		!strings.EqualFold(*stage.HealthStatus, "NotApplicable")
}

func stageObservationStale(stage promotion.Stage, integration promotion.IntegrationConfig, now time.Time) bool {
	if integration.MaximumStageObservationAge <= 0 {
		return false
	}

	return stage.LastObservedAt.Before(now.UTC().Add(-integration.MaximumStageObservationAge))
}

func compareProjects(left, right ProjectSummary) int {
	for _, values := range [][2]string{
		{left.Integration, right.Integration},
		{left.Project, right.Project},
		{left.ClusterName, right.ClusterName},
	} {
		if value := compareText(values[0], values[1]); value != 0 {
			return value
		}
	}
	return 0
}

func stageMatchesProjectFilter(stage promotion.Stage, filter ProjectListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project)
}
