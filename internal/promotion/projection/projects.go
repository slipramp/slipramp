package projection

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

type ProjectListFilter struct {
	Integration      string
	Cluster          string
	Project          string
	FreightAlignment FreightAlignmentStatus
	Unhealthy        bool
	Stale            bool
	PendingReview    bool
	Limit            int
	Offset           int
}

type ProjectKey struct {
	Integration string
	Cluster     string
	Project     string
}

type ProjectSummary struct {
	Integration                   string
	ClusterName                   string
	Project                       string
	StageCount                    int
	UnhealthyStageCount           int
	StaleStageCount               int
	PendingReviewCount            int
	FreightDifferenceCount        int
	UnknownFreightComparisonCount int
	LatestObservedAt              time.Time
}

type ProjectListSummary struct {
	StageCount                    int
	UnhealthyStageCount           int
	StaleStageCount               int
	PendingReviewCount            int
	FreightDifferenceCount        int
	UnknownFreightComparisonCount int
}

type ProjectList struct {
	Items   []ProjectSummary
	Summary ProjectListSummary
	Total   int
	Limit   int
	Offset  int
}

type StageListFilter struct {
	Integration      string
	Cluster          string
	Project          string
	Application      string
	Environment      string
	FreightAlignment FreightAlignmentStatus
	NewerFreight     bool
	Unhealthy        bool
	Stale            bool
	PendingReview    bool
	Limit            int
	Offset           int
}

type StageList struct {
	Items  []StageDetail
	Total  int
	Limit  int
	Offset int
}

type PipelineListFilter struct {
	Integration      string
	Cluster          string
	Project          string
	FreightAlignment FreightAlignmentStatus
	NewerFreight     bool
	Unhealthy        bool
	Stale            bool
	PendingReview    bool
	Limit            int
	Offset           int
}

type Pipeline struct {
	Integration string
	ClusterName string
	Project     string
	Name        string
	Stages      []ProjectStage
}

type PipelineList struct {
	Items  []Pipeline
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

type StageDetail struct {
	Integration string
	ClusterName string
	Project     string
	Stage       ProjectStage
}

type ProjectStage struct {
	Stage                 promotion.Stage
	Policy                promotion.PromotionPolicySnapshot
	PendingPromotionCount int
	ObservationStale      bool
	RequestedFreight      []ProjectFreightRequest
	FreightAlignments     []FreightAlignment
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

type ProjectStore interface {
	promotion.RepositoryLister
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
	if !filter.FreightAlignment.Valid() {
		return ProjectList{}, fmt.Errorf("%w: Freight alignment filter is invalid", promotion.ErrInvalid)
	}

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return ProjectList{}, err
	}
	grouped := make(map[ProjectKey][]promotion.Stage)
	configs := make(map[ProjectKey]promotion.IntegrationConfig)
	stageIDs := make([]uuid.UUID, 0, len(stages))
	for _, stage := range stages {
		if stage.ArchivedAt != nil || !stageMatchesProjectFilter(stage, filter) {
			continue
		}
		integration, ok := s.integrations.Lookup(stage.Integration)
		if !ok || integration.ClusterName != stage.ClusterName {
			continue
		}
		key := stageProjectKey(stage)
		grouped[key] = append(grouped[key], stage)
		configs[key] = integration
		stageIDs = append(stageIDs, stage.ID)
	}

	pending, err := s.store.CountPendingPromotionsByStage(ctx, stageIDs)
	if err != nil {
		return ProjectList{}, err
	}

	evaluatedAt := s.now().UTC()
	items := make([]ProjectSummary, 0, len(grouped))
	listSummary := ProjectListSummary{}
	for key, projectStages := range grouped {
		summary := ProjectSummary{
			Integration: key.Integration, ClusterName: key.Cluster, Project: key.Project,
		}
		stale := make(map[uuid.UUID]bool, len(projectStages))
		for _, stage := range projectStages {
			summary.StageCount++
			summary.PendingReviewCount += pending[stage.ID]
			stale[stage.ID] = stageObservationStale(stage, configs[key], evaluatedAt)
			if stale[stage.ID] {
				summary.StaleStageCount++
			}
			if !stale[stage.ID] && stageNonReady(stage) {
				summary.UnhealthyStageCount++
			}
			if stage.LastObservedAt.After(summary.LatestObservedAt) {
				summary.LatestObservedAt = stage.LastObservedAt
			}
		}

		matchesAlignment := filter.FreightAlignment == ""
		for _, alignments := range freightAlignments(projectStages, stale) {
			for _, alignment := range alignments {
				switch alignment.Status {
				case FreightAlignmentDifferent:
					summary.FreightDifferenceCount++
				case FreightAlignmentUnknown:
					summary.UnknownFreightComparisonCount++
				}
				if alignment.Status == filter.FreightAlignment {
					matchesAlignment = true
				}
			}
		}
		if !matchesAlignment {
			continue
		}
		if filter.Unhealthy && summary.UnhealthyStageCount == 0 ||
			filter.Stale && summary.StaleStageCount == 0 ||
			filter.PendingReview && summary.PendingReviewCount == 0 {
			continue
		}

		listSummary.StageCount += summary.StageCount
		listSummary.UnhealthyStageCount += summary.UnhealthyStageCount
		listSummary.StaleStageCount += summary.StaleStageCount
		listSummary.PendingReviewCount += summary.PendingReviewCount
		listSummary.FreightDifferenceCount += summary.FreightDifferenceCount
		listSummary.UnknownFreightComparisonCount += summary.UnknownFreightComparisonCount
		items = append(items, summary)
	}
	slices.SortFunc(items, compareProjects)
	total := len(items)

	return ProjectList{
		Items: page(items, filter.Offset, filter.Limit), Summary: listSummary,
		Total: total, Limit: filter.Limit, Offset: filter.Offset,
	}, nil
}

func (s *ProjectService) ListStages(ctx context.Context, filter StageListFilter) (StageList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return StageList{}, err
	}
	if !filter.FreightAlignment.Valid() {
		return StageList{}, fmt.Errorf("%w: Freight alignment filter is invalid", promotion.ErrInvalid)
	}

	projects, err := s.projectDetails(ctx, func(stage promotion.Stage) bool {
		return stageMatchesStageScope(stage, filter)
	})
	if err != nil {
		return StageList{}, err
	}

	items := make([]StageDetail, 0)
	for _, project := range projects {
		for _, stage := range project.Stages {
			if !stageMatchesStageScope(stage.Stage, filter) || !projectStageMatchesListFilter(stage, filter) {
				continue
			}
			items = append(items, StageDetail{
				Integration: project.Integration,
				ClusterName: project.ClusterName,
				Project:     project.Project,
				Stage:       stage,
			})
		}
	}
	slices.SortFunc(items, compareStageDetails)
	total := len(items)

	return StageList{
		Items: page(items, filter.Offset, filter.Limit), Total: total,
		Limit: filter.Limit, Offset: filter.Offset,
	}, nil
}

func (s *ProjectService) ListPipelines(ctx context.Context, filter PipelineListFilter) (PipelineList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return PipelineList{}, err
	}
	if !filter.FreightAlignment.Valid() {
		return PipelineList{}, fmt.Errorf("%w: Freight alignment filter is invalid", promotion.ErrInvalid)
	}

	projects, err := s.projectDetails(ctx, func(stage promotion.Stage) bool {
		return stageMatchesPipelineScope(stage, filter)
	})
	if err != nil {
		return PipelineList{}, err
	}

	items := make([]Pipeline, 0)
	for _, project := range projects {
		for _, pipeline := range projectPipelines(project) {
			if pipelineMatchesListFilter(pipeline, filter) {
				items = append(items, pipeline)
			}
		}
	}
	slices.SortFunc(items, comparePipelines)
	total := len(items)

	return PipelineList{
		Items: page(items, filter.Offset, filter.Limit), Total: total,
		Limit: filter.Limit, Offset: filter.Offset,
	}, nil
}

func (s *ProjectService) Get(ctx context.Context, key ProjectKey) (ProjectDetail, error) {
	projects, err := s.projectDetails(ctx, func(stage promotion.Stage) bool {
		return stageProjectKey(stage) == key
	})
	if err != nil {
		return ProjectDetail{}, err
	}
	if len(projects) == 0 {
		return ProjectDetail{}, promotion.ErrProjectNotFound
	}
	return projects[0], nil
}

func (s *ProjectService) projectDetails(
	ctx context.Context,
	matches func(promotion.Stage) bool,
) ([]ProjectDetail, error) {
	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return nil, err
	}

	grouped := make(map[ProjectKey][]promotion.Stage)
	var needRepositories bool
	for _, stage := range stages {
		if stage.ArchivedAt != nil || !matches(stage) {
			continue
		}
		integration, configured := s.integrations.Lookup(stage.Integration)
		if configured && integration.ClusterName == stage.ClusterName {
			grouped[stageProjectKey(stage)] = nil
			needRepositories = needRepositories || len(integration.Repositories) > 0
		}
	}
	if len(grouped) == 0 {
		return nil, nil
	}

	// Include upstream Stages even when only their downstream Stage matches the filter.
	var stageIDs []uuid.UUID
	for _, stage := range stages {
		key := stageProjectKey(stage)
		if siblings, selected := grouped[key]; selected && stage.ArchivedAt == nil {
			grouped[key] = append(siblings, stage)
			stageIDs = append(stageIDs, stage.ID)
		}
	}

	evaluatedAt := s.now().UTC()
	policies, err := stagePolicyViews(ctx, s.store, stageIDs, evaluatedAt)
	if err != nil {
		return nil, err
	}
	pending, err := s.store.CountPendingPromotionsByStage(ctx, stageIDs)
	if err != nil {
		return nil, err
	}
	var repositories map[string]repository.Repository
	if needRepositories {
		repositories, err = activeRepositoriesByName(ctx, s.store)
		if err != nil {
			return nil, err
		}
	}

	facts := stageFacts{evaluatedAt: evaluatedAt, policies: policies, pending: pending, repositories: repositories}
	projects := make([]ProjectDetail, 0, len(grouped))
	for key, siblings := range grouped {
		projects = append(projects, facts.projectDetail(key, s.integrations[key.Integration], siblings))
	}
	return projects, nil
}

type stageFacts struct {
	evaluatedAt  time.Time
	policies     map[uuid.UUID]promotion.PromotionPolicySnapshot
	pending      map[uuid.UUID]int
	repositories map[string]repository.Repository
}

func (facts stageFacts) projectDetail(
	key ProjectKey,
	integration promotion.IntegrationConfig,
	active []promotion.Stage,
) ProjectDetail {
	byName := make(map[string]promotion.Stage, len(active))
	stale := make(map[uuid.UUID]bool, len(active))
	for _, stage := range active {
		byName[stage.Name] = stage
		stale[stage.ID] = stageObservationStale(stage, integration, facts.evaluatedAt)
	}
	alignments := freightAlignments(active, stale)
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
			Policy:                facts.policies[stage.ID],
			PendingPromotionCount: facts.pending[stage.ID],
			ObservationStale:      stale[stage.ID],
			RequestedFreight:      requests,
			FreightAlignments:     alignments[stage.ID],
			Capabilities: stageCapabilities(
				stage,
				integration,
				facts.repositories[stage.RepositoryName].CloneURL,
				stale[stage.ID],
			),
		})
	}
	slices.SortFunc(items, func(left, right ProjectStage) int {
		return cmp.Compare(left.Stage.Name, right.Stage.Name)
	})

	return ProjectDetail{
		Integration: key.Integration, ClusterName: key.Cluster, Project: key.Project, Stages: items,
	}
}

func (s *ProjectService) GetStage(ctx context.Context, stageID uuid.UUID) (StageDetail, error) {
	projects, err := s.projectDetails(ctx, func(stage promotion.Stage) bool { return stage.ID == stageID })
	if err != nil {
		return StageDetail{}, err
	}
	if len(projects) == 0 {
		return StageDetail{}, promotion.ErrStageNotFound
	}
	project := projects[0]
	for _, stage := range project.Stages {
		if stage.Stage.ID == stageID {
			return StageDetail{
				Integration: project.Integration,
				ClusterName: project.ClusterName,
				Project:     project.Project,
				Stage:       stage,
			}, nil
		}
	}

	return StageDetail{}, promotion.ErrStageNotFound
}

func stageProjectKey(stage promotion.Stage) ProjectKey {
	return ProjectKey{Integration: stage.Integration, Cluster: stage.ClusterName, Project: stage.Project}
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
		if value := cmp.Compare(values[0], values[1]); value != 0 {
			return value
		}
	}
	return 0
}

func compareStageDetails(left, right StageDetail) int {
	for _, values := range [][2]string{
		{left.Stage.Stage.Name, right.Stage.Stage.Name},
		{left.Project, right.Project},
		{left.Integration, right.Integration},
		{left.ClusterName, right.ClusterName},
		{left.Stage.Stage.ID.String(), right.Stage.Stage.ID.String()},
	} {
		if value := cmp.Compare(values[0], values[1]); value != 0 {
			return value
		}
	}
	return 0
}

func comparePipelines(left, right Pipeline) int {
	for _, values := range [][2]string{
		{left.Name, right.Name},
		{left.Project, right.Project},
		{left.Integration, right.Integration},
		{left.ClusterName, right.ClusterName},
		{pipelineIdentity(left), pipelineIdentity(right)},
	} {
		if value := cmp.Compare(values[0], values[1]); value != 0 {
			return value
		}
	}
	return 0
}

func pipelineIdentity(pipeline Pipeline) string {
	ids := make([]string, 0, len(pipeline.Stages))
	for _, stage := range pipeline.Stages {
		ids = append(ids, stage.Stage.ID.String())
	}
	slices.Sort(ids)

	return strings.Join(ids, ":")
}

func stageMatchesProjectFilter(stage promotion.Stage, filter ProjectListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project)
}

func stageMatchesStageScope(stage promotion.Stage, filter StageListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project) &&
		(filter.Application == "" || stage.Application == filter.Application) &&
		(filter.Environment == "" || stage.Environment == filter.Environment)
}

func stageMatchesPipelineScope(stage promotion.Stage, filter PipelineListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project)
}

func projectStageMatchesListFilter(stage ProjectStage, filter StageListFilter) bool {
	if filter.NewerFreight && !stageHasNewerFreight(stage) {
		return false
	}
	if filter.Unhealthy && (stage.ObservationStale || !stageNonReady(stage.Stage)) {
		return false
	}
	if filter.Stale && !stage.ObservationStale {
		return false
	}
	if filter.PendingReview && stage.PendingPromotionCount == 0 {
		return false
	}
	if filter.FreightAlignment != "" && !slices.ContainsFunc(
		stage.FreightAlignments,
		func(alignment FreightAlignment) bool { return alignment.Status == filter.FreightAlignment },
	) {
		return false
	}

	return true
}

func pipelineMatchesListFilter(pipeline Pipeline, filter PipelineListFilter) bool {
	if filter.NewerFreight && !slices.ContainsFunc(pipeline.Stages, stageHasNewerFreight) {
		return false
	}
	if filter.Unhealthy && !slices.ContainsFunc(pipeline.Stages, func(stage ProjectStage) bool {
		return !stage.ObservationStale && stageNonReady(stage.Stage)
	}) {
		return false
	}
	if filter.Stale && !slices.ContainsFunc(pipeline.Stages, func(stage ProjectStage) bool {
		return stage.ObservationStale
	}) {
		return false
	}
	if filter.PendingReview && !slices.ContainsFunc(pipeline.Stages, func(stage ProjectStage) bool {
		return stage.PendingPromotionCount > 0
	}) {
		return false
	}
	if filter.FreightAlignment != "" && !slices.ContainsFunc(pipeline.Stages, func(stage ProjectStage) bool {
		return slices.ContainsFunc(stage.FreightAlignments, func(alignment FreightAlignment) bool {
			return alignment.Status == filter.FreightAlignment
		})
	}) {
		return false
	}

	return true
}

func projectPipelines(project ProjectDetail) []Pipeline {
	byName := make(map[string]ProjectStage, len(project.Stages))
	stages := make([]promotion.Stage, 0, len(project.Stages))
	for _, stage := range project.Stages {
		byName[stage.Stage.Name] = stage
		stages = append(stages, stage.Stage)
	}
	groups := promotion.ProjectPipelines(stages)
	pipelines := make([]Pipeline, 0, len(groups))
	for _, group := range groups {
		members := make([]ProjectStage, 0, len(group.StageNames))
		for _, name := range group.StageNames {
			members = append(members, byName[name])
		}
		pipelines = append(pipelines, Pipeline{
			Integration: project.Integration,
			ClusterName: project.ClusterName,
			Project:     project.Project,
			Name:        group.Name,
			Stages:      members,
		})
	}

	return pipelines
}
