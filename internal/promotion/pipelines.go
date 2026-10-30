package promotion

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

type FreightPipelineAttention string

const (
	FreightPipelineAttentionAny      FreightPipelineAttention = "any"
	FreightPipelineAttentionDrifted  FreightPipelineAttention = "drifted"
	FreightPipelineAttentionOutdated FreightPipelineAttention = "outdated"
)

type FreightPipelineListFilter struct {
	Integration string
	Cluster     string
	Project     string
	Repository  string
	Application string
	Attention   FreightPipelineAttention
	Limit       int
	Offset      int
}

type FreightPipelineStage struct {
	Stage                 Stage
	PromotionSources      []FreightPromotionSource
	AvailableFreight      []FreightItem
	PendingPromotionCount int
	ObservationStale      bool
}

type FreightPromotionSource struct {
	StageID     uuid.UUID
	StageName   string
	Environment string
	Freight     []FreightItem
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
	CountPendingPromotionsByStage(context.Context, []uuid.UUID) (map[uuid.UUID]int, error)
}

type FreightPipelineService struct {
	integrations IntegrationSet
	store        FreightPipelineStore
	now          func() time.Time
}

func NewFreightPipelineService(
	integrations IntegrationSet,
	store FreightPipelineStore,
) *FreightPipelineService {
	return &FreightPipelineService{integrations: integrations, store: store, now: time.Now}
}

func (s *FreightPipelineService) List(
	ctx context.Context,
	filter FreightPipelineListFilter,
) (FreightPipelineList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return FreightPipelineList{}, err
	}
	if filter.Attention != "" && !filter.Attention.Valid() {
		return FreightPipelineList{}, fmt.Errorf("%w: pipeline attention filter is invalid", ErrInvalid)
	}

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return FreightPipelineList{}, err
	}
	evaluatedAt := s.now()
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
	activeStages := make(map[stageIdentity]Stage)
	stageIDs := make([]uuid.UUID, 0, len(stages))
	for _, stage := range stages {
		if stage.ArchivedAt != nil {
			continue
		}
		integration, ok := s.integrations.Lookup(stage.Integration)
		if !ok {
			continue
		}
		activeStages[newStageIdentity(stage)] = stage
		if !stageMatchesPipelineFilter(stage, filter) {
			continue
		}
		stageIDs = append(stageIDs, stage.ID)

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
			Stage:            stage,
			ObservationStale: stageObservationStale(stage, integration, evaluatedAt),
		})
	}

	pendingCounts, err := s.store.CountPendingPromotionsByStage(ctx, stageIDs)
	if err != nil {
		return FreightPipelineList{}, err
	}

	items := make([]FreightPipeline, 0, len(builders))
	for _, builder := range builders {
		for index := range builder.pipeline.Stages {
			stage := &builder.pipeline.Stages[index]
			stage.PromotionSources = promotionSources(stage.Stage, activeStages)
			stage.AvailableFreight = availableFreight(stage.Stage, stage.PromotionSources)
			stage.PendingPromotionCount = pendingCounts[stage.Stage.ID]
		}
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
		if freightPipelineMatchesAttention(builder.pipeline, filter.Attention) {
			items = append(items, builder.pipeline)
		}
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

func stageObservationStale(stage Stage, integration IntegrationConfig, now time.Time) bool {
	if integration.MaximumStageObservationAge <= 0 {
		return false
	}

	return stage.LastObservedAt.Before(now.UTC().Add(-integration.MaximumStageObservationAge))
}

type stageIdentity struct {
	integration string
	cluster     string
	project     string
	name        string
}

func newStageIdentity(stage Stage) stageIdentity {
	return stageIdentity{
		integration: stage.Integration,
		cluster:     stage.ClusterName,
		project:     stage.Project,
		name:        stage.Name,
	}
}

func promotionSources(target Stage, stages map[stageIdentity]Stage) []FreightPromotionSource {
	sources := make([]FreightPromotionSource, 0)
	sourceIndexes := make(map[uuid.UUID]int)
	for _, request := range target.RequestedFreight {
		for _, sourceName := range request.SourceStages {
			identity := newStageIdentity(target)
			identity.name = sourceName
			source, ok := stages[identity]
			if !ok {
				continue
			}

			for _, item := range source.Freight.Items {
				if item.Origin != request.Origin {
					continue
				}
				index, exists := sourceIndexes[source.ID]
				if !exists {
					index = len(sources)
					sourceIndexes[source.ID] = index
					sources = append(sources, FreightPromotionSource{
						StageID:     source.ID,
						StageName:   source.Name,
						Environment: source.Environment,
						Freight:     []FreightItem{},
					})
				}
				sources[index].Freight = append(sources[index].Freight, item)
			}
		}
	}

	return sources
}

func availableFreight(target Stage, sources []FreightPromotionSource) []FreightItem {
	current := make(map[string]string, len(target.Freight.Items))
	for _, item := range target.Freight.Items {
		current[item.Origin] = item.Name
	}

	available := make([]FreightItem, 0)
	seen := make(map[string]struct{})
	add := func(item FreightItem) {
		if current[item.Origin] == item.Name {
			return
		}
		key := item.Origin + "\x00" + item.Name
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		available = append(available, cloneFreightItem(item))
	}

	for _, item := range target.DirectFreight.Items {
		add(item)
	}
	for _, source := range sources {
		for _, item := range source.Freight {
			add(item)
		}
	}

	slices.SortFunc(available, func(left, right FreightItem) int {
		if value := compareText(left.Origin, right.Origin); value != 0 {
			return value
		}
		return compareText(left.Name, right.Name)
	})

	return available
}

func (a FreightPipelineAttention) Valid() bool {
	switch a {
	case FreightPipelineAttentionAny, FreightPipelineAttentionDrifted, FreightPipelineAttentionOutdated:
		return true
	default:
		return false
	}
}

func freightPipelineMatchesAttention(pipeline FreightPipeline, attention FreightPipelineAttention) bool {
	if attention == "" {
		return true
	}
	drifted := freightPipelineDrifted(pipeline)
	outdated := freightPipelineOutdated(pipeline)

	switch attention {
	case FreightPipelineAttentionAny:
		return drifted || outdated
	case FreightPipelineAttentionDrifted:
		return drifted
	case FreightPipelineAttentionOutdated:
		return outdated
	default:
		return false
	}
}

func freightPipelineDrifted(pipeline FreightPipeline) bool {
	environments := make(map[string]struct{}, len(pipeline.Stages))
	rows := make(map[freightComparisonRowKey]map[uuid.UUID][]string)
	for _, pipelineStage := range pipeline.Stages {
		environments[pipelineStage.Stage.Environment] = struct{}{}

		for _, item := range pipelineStage.Stage.Freight.Items {
			for _, artifact := range item.Artifacts {
				repository := ""
				if artifact.Repository != nil {
					repository = *artifact.Repository
				}
				key := freightComparisonRowKey{
					origin:     item.Origin,
					kind:       artifact.Kind,
					repository: repository,
				}
				values := rows[key]
				if values == nil {
					values = make(map[uuid.UUID][]string)
					rows[key] = values
				}
				if !slices.Contains(values[pipelineStage.Stage.ID], artifact.Version) {
					values[pipelineStage.Stage.ID] = append(values[pipelineStage.Stage.ID], artifact.Version)
				}
			}
		}
	}

	if len(environments) < 2 {
		return false
	}

	for _, values := range rows {
		if !freightValuesEqual(values, pipeline.Stages) {
			return true
		}
	}

	return false
}

func freightPipelineOutdated(pipeline FreightPipeline) bool {
	for _, stage := range pipeline.Stages {
		if !stage.Stage.AutoPromotionEnabled && len(stage.AvailableFreight) > 0 {
			return true
		}
	}

	return false
}

type freightComparisonRowKey struct {
	origin     string
	kind       string
	repository string
}

func freightValuesEqual(values map[uuid.UUID][]string, stages []FreightPipelineStage) bool {
	var expected []string
	for _, pipelineStage := range stages {
		versions := slices.Sorted(slices.Values(values[pipelineStage.Stage.ID]))
		if len(versions) == 0 {
			return false
		}
		if expected == nil {
			expected = versions
			continue
		}
		if !slices.Equal(versions, expected) {
			return false
		}
	}

	return len(expected) > 0
}

func stageMatchesPipelineFilter(stage Stage, filter FreightPipelineListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Cluster == "" || stage.ClusterName == filter.Cluster) &&
		(filter.Project == "" || stage.Project == filter.Project) &&
		(filter.Repository == "" || stage.RepositoryName == filter.Repository) &&
		(filter.Application == "" || stage.Application == filter.Application)
}
