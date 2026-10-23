package promotion

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

type FreightPipelineListFilter struct {
	Integration string
	Cluster     string
	Project     string
	Repository  string
	Application string
	Limit       int
	Offset      int
}

type FreightPipelineStage struct {
	Stage                 Stage
	PendingPromotionCount int
}

type FreightPipeline struct {
	Integration      string
	ClusterName      string
	Project          string
	Repository       string
	Application      string
	EnvironmentOrder []string
	Stages           []FreightPipelineStage
}

type FreightPipelineList struct {
	Items  []FreightPipeline
	Total  int
	Limit  int
	Offset int
}

type FreightPipelineStore interface {
	ListPromotionStages(context.Context) ([]Stage, error)
	ListPromotions(context.Context) ([]Promotion, error)
}

type FreightPipelineService struct {
	integrations IntegrationSet
	store        FreightPipelineStore
}

func NewFreightPipelineService(
	integrations IntegrationSet,
	store FreightPipelineStore,
) *FreightPipelineService {
	return &FreightPipelineService{integrations: integrations, store: store}
}

func (s *FreightPipelineService) List(
	ctx context.Context,
	filter FreightPipelineListFilter,
) (FreightPipelineList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return FreightPipelineList{}, err
	}

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return FreightPipelineList{}, err
	}
	promotions, err := s.store.ListPromotions(ctx)
	if err != nil {
		return FreightPipelineList{}, err
	}

	pendingCounts := make(map[uuid.UUID]int)
	for _, record := range promotions {
		if record.Status == StatusPending {
			pendingCounts[record.StageID]++
		}
	}

	type pipelineKey struct {
		integration string
		cluster     string
		project     string
		repository  string
		application string
	}
	type pipelineBuilder struct {
		pipeline    FreightPipeline
		unknownEnvs map[string]struct{}
	}

	builders := make(map[pipelineKey]*pipelineBuilder)
	for _, stage := range stages {
		if stage.ArchivedAt != nil || !stageMatchesPipelineFilter(stage, filter) {
			continue
		}
		integration, ok := s.integrations.Lookup(stage.Integration)
		if !ok {
			continue
		}

		key := pipelineKey{
			integration: stage.Integration,
			cluster:     stage.ClusterName,
			project:     stage.Project,
			repository:  stage.RepositoryName,
			application: stage.Application,
		}
		builder, ok := builders[key]
		if !ok {
			builder = &pipelineBuilder{
				pipeline: FreightPipeline{
					Integration:      stage.Integration,
					ClusterName:      stage.ClusterName,
					Project:          stage.Project,
					Repository:       stage.RepositoryName,
					Application:      stage.Application,
					EnvironmentOrder: slices.Clone(integration.EnvironmentOrder),
					Stages:           []FreightPipelineStage{},
				},
				unknownEnvs: map[string]struct{}{},
			}
			builders[key] = builder
		}

		if !slices.Contains(integration.EnvironmentOrder, stage.Environment) {
			builder.unknownEnvs[stage.Environment] = struct{}{}
		}
		builder.pipeline.Stages = append(builder.pipeline.Stages, FreightPipelineStage{
			Stage:                 stage,
			PendingPromotionCount: pendingCounts[stage.ID],
		})
	}

	items := make([]FreightPipeline, 0, len(builders))
	for _, builder := range builders {
		unknownEnvironments := make([]string, 0, len(builder.unknownEnvs))
		for environment := range builder.unknownEnvs {
			unknownEnvironments = append(unknownEnvironments, environment)
		}
		slices.Sort(unknownEnvironments)
		orderedEnvironments := append(slices.Clone(builder.pipeline.EnvironmentOrder), unknownEnvironments...)

		environmentRanks := make(map[string]int, len(orderedEnvironments))
		for index, environment := range orderedEnvironments {
			environmentRanks[environment] = index
		}
		slices.SortFunc(builder.pipeline.Stages, func(left, right FreightPipelineStage) int {
			if value := environmentRanks[left.Stage.Environment] - environmentRanks[right.Stage.Environment]; value != 0 {
				return value
			}
			if value := compareText(left.Stage.Name, right.Stage.Name); value != 0 {
				return value
			}

			return compareText(left.Stage.ID.String(), right.Stage.ID.String())
		})
		items = append(items, builder.pipeline)
	}

	slices.SortFunc(items, func(left, right FreightPipeline) int {
		for _, values := range [][2]string{
			{left.Integration, right.Integration},
			{left.Repository, right.Repository},
			{left.Project, right.Project},
			{left.Application, right.Application},
		} {
			if value := compareText(values[0], values[1]); value != 0 {
				return value
			}
		}

		return compareText(left.ClusterName, right.ClusterName)
	})

	total := len(items)
	items = page(items, filter.Offset, filter.Limit)

	return FreightPipelineList{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func stageMatchesPipelineFilter(stage Stage, filter FreightPipelineListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project) &&
		(filter.Repository == "" || stage.RepositoryName == filter.Repository) &&
		(filter.Application == "" || stage.Application == filter.Application)
}
