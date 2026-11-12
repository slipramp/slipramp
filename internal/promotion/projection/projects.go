package projection

import (
	"cmp"
	"context"
	"errors"
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
	FreightAlignment FreightAlignmentStatus
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
		key := ProjectKey{Integration: stage.Integration, Cluster: stage.ClusterName, Project: stage.Project}
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

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return StageList{}, err
	}
	keys := make(map[ProjectKey]struct{})
	for _, stage := range stages {
		if stage.ArchivedAt != nil || !stageMatchesStageScope(stage, filter) {
			continue
		}
		integration, configured := s.integrations.Lookup(stage.Integration)
		if !configured || integration.ClusterName != stage.ClusterName {
			continue
		}
		keys[ProjectKey{
			Integration: stage.Integration,
			Cluster:     stage.ClusterName,
			Project:     stage.Project,
		}] = struct{}{}
	}

	items := make([]StageDetail, 0)
	for key := range keys {
		project, detailErr := s.projectDetail(ctx, key, stages)
		if detailErr != nil {
			return StageList{}, detailErr
		}
		for _, stage := range project.Stages {
			if !projectStageMatchesListFilter(stage, filter) {
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

func (s *ProjectService) Get(ctx context.Context, key ProjectKey) (ProjectDetail, error) {
	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return ProjectDetail{}, err
	}

	return s.projectDetail(ctx, key, stages)
}

func (s *ProjectService) projectDetail(
	ctx context.Context,
	key ProjectKey,
	stages []promotion.Stage,
) (ProjectDetail, error) {
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
	stale := make(map[uuid.UUID]bool, len(active))
	for _, stage := range active {
		stale[stage.ID] = stageObservationStale(stage, integration, evaluatedAt)
	}
	alignments := freightAlignments(active, stale)

	var repositories map[string]repository.Repository
	if len(integration.Repositories) > 0 {
		repositories, err = activeRepositoriesByName(ctx, s.store)
		if err != nil {
			return ProjectDetail{}, err
		}
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
			ObservationStale:      stale[stage.ID],
			RequestedFreight:      requests,
			FreightAlignments:     alignments[stage.ID],
			Capabilities: stageCapabilities(
				stage,
				integration,
				repositories[stage.RepositoryName].CloneURL,
				stale[stage.ID],
			),
		})
	}
	slices.SortFunc(items, func(left, right ProjectStage) int {
		return cmp.Compare(left.Stage.Name, right.Stage.Name)
	})

	return ProjectDetail{
		Integration: key.Integration, ClusterName: key.Cluster, Project: key.Project, Stages: items,
	}, nil
}

func (s *ProjectService) GetStage(ctx context.Context, stageID uuid.UUID) (StageDetail, error) {
	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return StageDetail{}, err
	}

	var key ProjectKey
	for _, stage := range stages {
		if stage.ID == stageID && stage.ArchivedAt == nil {
			key = ProjectKey{
				Integration: stage.Integration,
				Cluster:     stage.ClusterName,
				Project:     stage.Project,
			}
			break
		}
	}
	if key == (ProjectKey{}) {
		return StageDetail{}, promotion.ErrStageNotFound
	}

	project, err := s.projectDetail(ctx, key, stages)
	if errors.Is(err, promotion.ErrProjectNotFound) {
		return StageDetail{}, promotion.ErrStageNotFound
	}
	if err != nil {
		return StageDetail{}, err
	}
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

func stageMatchesProjectFilter(stage promotion.Stage, filter ProjectListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project)
}

func stageMatchesStageScope(stage promotion.Stage, filter StageListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project) &&
		(filter.Application == "" || stage.Application == filter.Application)
}

func projectStageMatchesListFilter(stage ProjectStage, filter StageListFilter) bool {
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
