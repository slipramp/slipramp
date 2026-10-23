package promotion

import (
	"context"
	"slices"
	"time"
)

type ProjectListFilter struct {
	Integration string
	Cluster     string
	Project     string
	Limit       int
	Offset      int
}

type ProjectSummary struct {
	Integration      string
	ClusterName      string
	Project          string
	StageCount       int
	ApplicationCount int
	LastObservedAt   time.Time
}

type ProjectList struct {
	Items  []ProjectSummary
	Total  int
	Limit  int
	Offset int
}

type ProjectStore interface {
	ListPromotionStages(context.Context) ([]Stage, error)
}

type ProjectService struct {
	integrations IntegrationSet
	store        ProjectStore
}

func NewProjectService(integrations IntegrationSet, store ProjectStore) *ProjectService {
	return &ProjectService{integrations: integrations, store: store}
}

func (s *ProjectService) List(ctx context.Context, filter ProjectListFilter) (ProjectList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return ProjectList{}, err
	}

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return ProjectList{}, err
	}

	type projectKey struct {
		integration string
		cluster     string
		project     string
	}
	type projectBuilder struct {
		summary      ProjectSummary
		applications map[string]struct{}
	}

	builders := make(map[projectKey]*projectBuilder)
	for _, stage := range stages {
		if stage.ArchivedAt != nil || !stageMatchesProjectFilter(stage, filter) {
			continue
		}
		if _, ok := s.integrations.Lookup(stage.Integration); !ok {
			continue
		}

		key := projectKey{
			integration: stage.Integration,
			cluster:     stage.ClusterName,
			project:     stage.Project,
		}
		builder, ok := builders[key]
		if !ok {
			builder = &projectBuilder{
				summary: ProjectSummary{
					Integration:    stage.Integration,
					ClusterName:    stage.ClusterName,
					Project:        stage.Project,
					LastObservedAt: stage.LastObservedAt,
				},
				applications: map[string]struct{}{},
			}
			builders[key] = builder
		}

		builder.summary.StageCount++
		builder.applications[stage.Application] = struct{}{}
		if stage.LastObservedAt.After(builder.summary.LastObservedAt) {
			builder.summary.LastObservedAt = stage.LastObservedAt
		}
	}

	items := make([]ProjectSummary, 0, len(builders))
	for _, builder := range builders {
		builder.summary.ApplicationCount = len(builder.applications)
		items = append(items, builder.summary)
	}
	slices.SortFunc(items, func(left, right ProjectSummary) int {
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
	})

	total := len(items)
	items = page(items, filter.Offset, filter.Limit)

	return ProjectList{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func stageMatchesProjectFilter(stage Stage, filter ProjectListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project)
}
