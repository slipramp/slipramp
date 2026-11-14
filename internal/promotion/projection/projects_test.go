package projection

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestProjectServiceGroupsActiveStages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	first := projectTestStage("prod", "colo", "kargo-core", "external-dns-homelab", now)
	second := projectTestStage("prod", "colo", "kargo-core", "external-dns-prod", now.Add(time.Minute))
	third := projectTestStage("prod", "colo", "kargo-core", "metrics-prod", now.Add(2*time.Minute))
	other := projectTestStage("prod", "colo", "kargo-platform", "cert-manager-prod", now)
	archived := projectTestStage("prod", "colo", "kargo-archived", "old-prod", now)
	archived.ArchivedAt = new(now.Add(time.Minute))
	unconfigured := projectTestStage("removed", "colo", "kargo-removed", "removed-prod", now)

	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
		&projectionTestStore{stages: []promotion.Stage{archived, third, unconfigured, second, other, first}},
	)

	result, err := service.List(t.Context(), ProjectListFilter{Limit: promotion.DefaultListLimit})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 2 || len(result.Items) != 2 {
		t.Fatalf("List() = %#v", result)
	}

	wantProjects := []string{"kargo-core", "kargo-platform"}
	gotProjects := []string{result.Items[0].Project, result.Items[1].Project}
	if !reflect.DeepEqual(gotProjects, wantProjects) {
		t.Errorf("project order = %v, want %v", gotProjects, wantProjects)
	}
	core := result.Items[0]
	if core.StageCount != 3 || !core.LatestObservedAt.Equal(now.Add(2*time.Minute)) {
		t.Errorf("kargo-core summary = %#v", core)
	}
}

func TestProjectServiceBuildsNativeTopologyAndRetainsUnenrichedStages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 8, 12, 0, 0, 0, time.UTC)
	source := projectTestStage("prod", "colo", "kargo-core", "test", now)
	source.Application = ""
	source.Environment = ""
	source.RepositoryName = ""
	source.RepositoryURL = ""
	source.TargetBranch = ""
	source.SourcePaths = nil
	target := projectTestStage("prod", "colo", "kargo-core", "prod", now)
	target.RequestedFreight = []promotion.FreightRequest{
		{
			Origin: "Warehouse/app", SourceStages: []string{"test", "missing"},
			AvailabilityStrategy: promotion.FreightAvailabilityOneOf,
		},
		{Origin: "Warehouse/config", Direct: true},
	}
	target.CandidateFreight = promotion.Freight{Items: []promotion.FreightItem{
		{
			Origin: "Warehouse/app", Name: "freight-1", EvidenceObserved: true,
			VerifiedIn: []promotion.FreightVerification{{Stage: "test"}},
		},
		{Origin: "Warehouse/config", Name: "freight-2"},
	}}
	otherCluster := projectTestStage("prod", "edge", "kargo-core", "test", now)
	otherProject := projectTestStage("prod", "colo", "kargo-platform", "test", now)
	otherIntegration := projectTestStage("dev", "colo", "kargo-core", "test", now)
	store := &projectionTestStore{
		repositories: []repository.Repository{{Name: target.RepositoryName, CloneURL: target.RepositoryURL}},
		stages:       []promotion.Stage{otherCluster, target, otherProject, otherIntegration, source},
	}
	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo", Repositories: []string{"ops"},
			Preparation: promotion.PreparationConfig{Repository: "ops", Target: "app", Action: "prepare"},
		}},
		store,
	)
	service.now = func() time.Time { return now }

	result, err := service.Get(t.Context(), ProjectKey{
		Integration: "prod", Cluster: "colo", Project: "kargo-core",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(result.Stages) != 2 || result.Stages[0].Stage.Name != "prod" || result.Stages[1].Stage.Name != "test" {
		t.Fatalf("project Stages = %#v", result.Stages)
	}
	if len(result.Stages[0].RequestedFreight) != 2 {
		t.Fatalf("requested Freight = %#v", result.Stages[0].RequestedFreight)
	}
	request := result.Stages[0].RequestedFreight[0]
	if request.Sources[0].StageID == nil || *request.Sources[0].StageID != source.ID ||
		request.Sources[1].StageID != nil {
		t.Errorf("resolved sources = %#v", request.Sources)
	}
	if len(request.Candidates) != 1 ||
		request.Candidates[0].Evidence.Readiness != promotion.FreightReadinessReady {
		t.Errorf("Freight candidates = %#v", request.Candidates)
	}
	if candidates := result.Stages[0].RequestedFreight[1].Candidates; len(candidates) != 1 ||
		candidates[0].Evidence.Readiness != promotion.FreightReadinessReady {
		t.Errorf("direct Freight candidates = %#v", candidates)
	}
	if caps := result.Stages[0].Capabilities; caps.Review.Status != StageCapabilityReady ||
		caps.BranchDrift.Status != StageCapabilityReady {
		t.Errorf("enriched Stage capabilities = %#v", caps)
	}
	capabilities := result.Stages[1].Capabilities
	if capabilities.Promotion.Status != StageCapabilityReady ||
		capabilities.Review.Status != StageCapabilityNotConfigured ||
		capabilities.BranchDrift.Status != StageCapabilityNotConfigured {
		t.Errorf("unannotated Stage capabilities = %#v", result.Stages[1].Capabilities)
	}
	if capabilities.Review.Reason == "" || capabilities.BranchDrift.Reason == "" {
		t.Errorf("unannotated Stage limitations = %#v", capabilities)
	}
	controlFlow := target
	controlFlow.ControlFlow = true
	controlFlowCapabilities := stageCapabilities(controlFlow, promotion.IntegrationConfig{
		ClusterName: "colo", Repositories: []string{"ops"},
		Preparation: promotion.PreparationConfig{Repository: "ops", Target: "app", Action: "prepare"},
	}, target.RepositoryURL, false)
	if controlFlowCapabilities.Promotion.Status != StageCapabilityNotApplicable ||
		controlFlowCapabilities.Promotion.Reason != controlFlowPromotionReason {
		t.Errorf("control-flow Stage capabilities = %#v", controlFlowCapabilities)
	}
	if controlFlowCapabilities.Review.Status != StageCapabilityNotApplicable ||
		controlFlowCapabilities.Review.Reason != controlFlowPromotionReason {
		t.Errorf("control-flow Stage review capability = %#v", controlFlowCapabilities.Review)
	}
	if controlFlowCapabilities.BranchDrift.Status != StageCapabilityReady {
		t.Errorf("control-flow Stage branch-drift capability = %#v", controlFlowCapabilities.BranchDrift)
	}
	staleCapabilities := stageCapabilities(target, promotion.IntegrationConfig{
		ClusterName: "colo", Repositories: []string{"ops"},
		Preparation: promotion.PreparationConfig{Repository: "ops", Target: "app", Action: "prepare"},
	}, target.RepositoryURL, true)
	if staleCapabilities.Promotion.Status != StageCapabilityUnavailable ||
		staleCapabilities.Review.Status != StageCapabilityReady ||
		staleCapabilities.BranchDrift.Status != StageCapabilityReady ||
		staleCapabilities.Promotion.Reason != staleObservationPromotionReason {
		t.Errorf("stale Stage capabilities = %#v", staleCapabilities)
	}
}

func TestProjectServiceGetsActiveStageByDurableID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	stage := projectTestStage("prod", "colo", "kargo-core", "prod", now)
	archived := projectTestStage("prod", "colo", "kargo-core", "old", now)
	archived.ArchivedAt = new(now)
	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
		&projectionTestStore{stages: []promotion.Stage{archived, stage}},
	)
	service.now = func() time.Time { return now }

	result, err := service.GetStage(t.Context(), stage.ID)
	if err != nil {
		t.Fatalf("GetStage() error = %v", err)
	}
	if result.Integration != "prod" || result.ClusterName != "colo" || result.Project != "kargo-core" ||
		result.Stage.Stage.ID != stage.ID {
		t.Errorf("Stage detail = %#v", result)
	}

	for name, stageID := range map[string]uuid.UUID{
		"archived": archived.ID,
		"unknown":  uuid.New(),
	} {
		t.Run(name, func(t *testing.T) {
			_, getErr := service.GetStage(t.Context(), stageID)
			if !errors.Is(getErr, promotion.ErrStageNotFound) {
				t.Errorf("GetStage() error = %v, want ErrStageNotFound", getErr)
			}
		})
	}
}

func TestProjectServiceFiltersAndPaginatesProjects(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	first := projectTestStage("prod", "prod", "kargo-core", "alpha-prod", now)
	second := projectTestStage("edge", "edge", "kargo-platform", "bravo-prod", now)
	third := projectTestStage("dev", "dev", "kargo-dev", "charlie-dev", now)

	service := NewProjectService(
		promotion.IntegrationSet{
			"dev": {ClusterName: "dev"}, "edge": {ClusterName: "edge"}, "prod": {ClusterName: "prod"},
		},
		&projectionTestStore{stages: []promotion.Stage{third, second, first}},
	)

	result, err := service.List(t.Context(), ProjectListFilter{
		Integration: "edge",
		Cluster:     "edge",
		Project:     "kargo-platform",
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].Project != "kargo-platform" {
		t.Errorf("filtered page = %#v", result)
	}

	result, err = service.List(t.Context(), ProjectListFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List() pagination error = %v", err)
	}
	if result.Total != 3 || len(result.Items) != 1 || result.Items[0].Project != "kargo-platform" {
		t.Errorf("paginated projects = %#v", result)
	}
}

func TestProjectServiceListsLooseStagesByName(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	alphaPlatform := projectTestStage("prod", "colo", "platform", "alpha", now)
	alphaCore := projectTestStage("prod", "colo", "core", "alpha", now)
	bravo := projectTestStage("prod", "colo", "core", "bravo", now)
	bravo.Application = "payments"
	bravo.Environment = "production"
	bravo.Conditions = []promotion.StageCondition{{Type: "Ready", Status: "False"}}
	store := &projectionTestStore{
		stages:        []promotion.Stage{bravo, alphaPlatform, alphaCore},
		pendingCounts: map[uuid.UUID]int{bravo.ID: 1},
	}
	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
		store,
	)
	service.now = func() time.Time { return now }

	result, err := service.ListStages(t.Context(), StageListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListStages() error = %v", err)
	}
	if result.Total != 3 || len(result.Items) != 2 || result.Limit != 2 || result.Offset != 0 {
		t.Fatalf("Stage page = %#v", result)
	}
	want := []string{"alpha/core", "alpha/platform"}
	got := []string{
		result.Items[0].Stage.Stage.Name + "/" + result.Items[0].Project,
		result.Items[1].Stage.Stage.Name + "/" + result.Items[1].Project,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Stage order = %v, want %v", got, want)
	}

	filtered, err := service.ListStages(t.Context(), StageListFilter{
		Integration: "prod", Cluster: "colo", Project: "core",
		Application: bravo.Application, Environment: bravo.Environment,
		Unhealthy: true, PendingReview: true, Limit: promotion.DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("ListStages(filtered) error = %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].Stage.Stage.ID != bravo.ID {
		t.Errorf("filtered Stages = %#v", filtered)
	}

	environmentFiltered, err := service.ListStages(t.Context(), StageListFilter{
		Environment: "production", Limit: promotion.DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("ListStages(environment) error = %v", err)
	}
	if environmentFiltered.Total != 1 || len(environmentFiltered.Items) != 1 ||
		environmentFiltered.Items[0].Stage.Stage.ID != bravo.ID {
		t.Errorf("environment-filtered Stages = %#v", environmentFiltered)
	}
}

func TestProjectServiceListsCompletePipelinesByName(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	alpha := projectTestStage("prod", "colo", "core", "alpha-prod", now)
	alpha.Application = "alpha"
	beta := projectTestStage("prod", "colo", "platform", "beta-prod", now)
	beta.Application = "beta"
	zetaSource := projectTestStage("prod", "colo", "core", "zeta-test", now)
	zetaSource.Application = "zeta"
	zetaTarget := projectTestStage("prod", "colo", "core", "zeta-prod", now)
	zetaTarget.Application = "zeta"
	zetaTarget.RequestedFreight = []promotion.FreightRequest{{
		Origin: "Warehouse/zeta", SourceStages: []string{zetaSource.Name},
	}}
	zetaTarget.Conditions = []promotion.StageCondition{{Type: "Ready", Status: "False"}}
	store := &projectionTestStore{
		stages: []promotion.Stage{zetaTarget, beta, alpha, zetaSource},
		pendingCounts: map[uuid.UUID]int{
			zetaTarget.ID: 1,
		},
	}
	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
		store,
	)
	service.now = func() time.Time { return now }

	result, err := service.ListPipelines(t.Context(), PipelineListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if result.Total != 3 || len(result.Items) != 2 {
		t.Fatalf("Pipeline page = %#v", result)
	}
	if names := []string{result.Items[0].Name, result.Items[1].Name}; !reflect.DeepEqual(
		names,
		[]string{"alpha", "beta"},
	) {
		t.Errorf("pipeline order = %v", names)
	}

	page, err := service.ListPipelines(t.Context(), PipelineListFilter{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("ListPipelines(page) error = %v", err)
	}
	if len(page.Items) != 2 || page.Items[1].Name != "zeta" || len(page.Items[1].Stages) != 2 {
		t.Errorf("paginated pipelines = %#v", page)
	}

	filtered, err := service.ListPipelines(t.Context(), PipelineListFilter{
		Integration: "prod", Cluster: "colo", Project: "core",
		Unhealthy: true, PendingReview: true, Limit: promotion.DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("ListPipelines(filtered) error = %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].Name != "zeta" {
		t.Errorf("filtered pipelines = %#v", filtered)
	}
}

func TestProjectServiceFiltersStagesAndCompletePipelinesByNewerFreight(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	currentAt := observedAt.Add(-2 * time.Hour)
	olderAt := currentAt.Add(-time.Hour)
	newerAt := currentAt.Add(time.Hour)

	available := projectTestStage("prod", "colo", "core", "app-dev", observedAt)
	available.Application = "app"
	available.RequestedFreight = []promotion.FreightRequest{{Origin: "Warehouse/app", Direct: true}}
	available.Freight.Items = []promotion.FreightItem{{
		Origin: "Warehouse/app", Name: "current", DiscoveredAt: &currentAt,
	}}
	available.CandidateFreight.Items = []promotion.FreightItem{
		{Origin: "Warehouse/app", Name: "rollback", DiscoveredAt: &olderAt},
		{Origin: "Warehouse/app", Name: "release", DiscoveredAt: &newerAt},
	}

	downstream := projectTestStage("prod", "colo", "core", "app-prod", observedAt)
	downstream.Application = "app"
	downstream.RequestedFreight = []promotion.FreightRequest{{
		Origin: "Warehouse/app", SourceStages: []string{available.Name},
	}}

	rollbackOnly := projectTestStage("prod", "colo", "core", "legacy", observedAt)
	rollbackOnly.Application = "legacy"
	rollbackOnly.RequestedFreight = []promotion.FreightRequest{{Origin: "Warehouse/legacy", Direct: true}}
	rollbackOnly.Freight.Items = []promotion.FreightItem{{
		Origin: "Warehouse/legacy", Name: "current", DiscoveredAt: &currentAt,
	}}
	rollbackOnly.CandidateFreight.Items = []promotion.FreightItem{{
		Origin: "Warehouse/legacy", Name: "rollback", DiscoveredAt: &olderAt,
	}}

	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
		&projectionTestStore{stages: []promotion.Stage{rollbackOnly, downstream, available}},
	)
	service.now = func() time.Time { return observedAt }

	stages, err := service.ListStages(t.Context(), StageListFilter{
		NewerFreight: true, Limit: promotion.DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("ListStages() error = %v", err)
	}
	if stages.Total != 1 || len(stages.Items) != 1 || stages.Items[0].Stage.Stage.ID != available.ID {
		t.Errorf("Stages with newer Freight = %#v", stages)
	}

	pipelines, err := service.ListPipelines(t.Context(), PipelineListFilter{NewerFreight: true, Limit: 1})
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if pipelines.Total != 1 || len(pipelines.Items) != 1 || pipelines.Items[0].Name != "app" ||
		len(pipelines.Items[0].Stages) != 2 {
		t.Errorf("complete pipelines with newer Freight = %#v", pipelines)
	}
}

func TestProjectServiceSummarizesAndFiltersFreightAlignmentBeforePagination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	alignedSource := projectTestStage("prod", "colo", "a-aligned", "source", now)
	alignedSource.Freight.Items = []promotion.FreightItem{*freightAlignmentTestItem("freight-1")}
	alignedTarget := projectTestStage("prod", "colo", "a-aligned", "target", now)
	alignedTarget.Freight.Items = []promotion.FreightItem{*freightAlignmentTestItem("freight-1")}
	alignedTarget.RequestedFreight = []promotion.FreightRequest{{
		Origin: "Warehouse/app", SourceStages: []string{"source"},
	}}

	differentSource := projectTestStage("prod", "colo", "b-different", "source", now)
	differentSource.Freight.Items = []promotion.FreightItem{*freightAlignmentTestItem("freight-2")}
	differentTarget := projectTestStage("prod", "colo", "b-different", "target", now)
	differentTarget.Freight.Items = []promotion.FreightItem{*freightAlignmentTestItem("freight-1")}
	differentTarget.RequestedFreight = []promotion.FreightRequest{{
		Origin: "Warehouse/app", SourceStages: []string{"source"},
	}}

	unknownTarget := projectTestStage("prod", "colo", "c-unknown", "target", now)
	unknownTarget.RequestedFreight = []promotion.FreightRequest{{
		Origin: "Warehouse/app", SourceStages: []string{"missing"},
	}}

	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
		&projectionTestStore{stages: []promotion.Stage{
			unknownTarget, differentTarget, differentSource, alignedTarget, alignedSource,
		}},
	)
	service.now = func() time.Time { return now }

	all, err := service.List(t.Context(), ProjectListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if all.Total != 3 || len(all.Items) != 1 || all.Items[0].Project != "a-aligned" {
		t.Fatalf("paginated Projects = %#v", all)
	}
	if all.Summary.FreightDifferenceCount != 1 || all.Summary.UnknownFreightComparisonCount != 1 {
		t.Errorf("unpaginated Freight alignment summary = %#v", all.Summary)
	}

	filters := []struct {
		status  FreightAlignmentStatus
		project string
	}{
		{status: FreightAlignmentAligned, project: "a-aligned"},
		{status: FreightAlignmentDifferent, project: "b-different"},
		{status: FreightAlignmentUnknown, project: "c-unknown"},
	}
	for _, filter := range filters {
		result, filterErr := service.List(t.Context(), ProjectListFilter{
			FreightAlignment: filter.status, Limit: 1,
		})
		if filterErr != nil {
			t.Fatalf("List(%s) error = %v", filter.status, filterErr)
		}
		if result.Total != 1 || len(result.Items) != 1 || result.Items[0].Project != filter.project {
			t.Errorf("Projects filtered by %s = %#v", filter.status, result)
		}
	}
}

func TestProjectServiceDoesNotResolveFreightSourcesAcrossProjectBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		source promotion.Stage
	}{
		{
			name:   "different Project",
			source: projectTestStage("prod", "colo", "other-project", "source", now),
		},
		{
			name:   "different Cluster",
			source: projectTestStage("prod", "edge", "kargo-core", "source", now),
		},
		{
			name:   "different integration",
			source: projectTestStage("dev", "colo", "kargo-core", "source", now),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			target := projectTestStage("prod", "colo", "kargo-core", "target", now)
			target.RequestedFreight = []promotion.FreightRequest{{
				Origin: "Warehouse/app", SourceStages: []string{"source"},
			}}
			service := NewProjectService(
				promotion.IntegrationSet{
					"prod": {ClusterName: "colo"},
					"dev":  {ClusterName: "colo"},
				},
				&projectionTestStore{stages: []promotion.Stage{target, test.source}},
			)
			service.now = func() time.Time { return now }

			result, err := service.Get(t.Context(), ProjectKey{
				Integration: "prod", Cluster: "colo", Project: "kargo-core",
			})
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			got := result.Stages[0].FreightAlignments
			if len(got) != 1 || got[0].Status != FreightAlignmentUnknown || got[0].SourceStageID != nil {
				t.Errorf("cross-boundary alignment = %#v", got)
			}
		})
	}
}

func TestProjectServiceTreatsArchivedFreightSourceAsUnresolved(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	source := projectTestStage("prod", "colo", "kargo-core", "source", now)
	source.ArchivedAt = new(now)
	target := projectTestStage("prod", "colo", "kargo-core", "target", now)
	target.RequestedFreight = []promotion.FreightRequest{{
		Origin: "Warehouse/app", SourceStages: []string{"source"},
	}}
	archivedTarget := projectTestStage("prod", "colo", "kargo-core", "archived-target", now)
	archivedTarget.ArchivedAt = new(now)
	archivedTarget.RequestedFreight = []promotion.FreightRequest{{
		Origin: "Warehouse/app", SourceStages: []string{"target"},
	}}
	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
		&projectionTestStore{stages: []promotion.Stage{source, target, archivedTarget}},
	)
	service.now = func() time.Time { return now }

	result, err := service.List(t.Context(), ProjectListFilter{Limit: promotion.DefaultListLimit})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].StageCount != 1 ||
		result.Items[0].UnknownFreightComparisonCount != 1 {
		t.Errorf("active Project summary = %#v", result)
	}
}

func TestProjectServiceSummarizesExactOperationalEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.November, 8, 12, 0, 0, 0, time.UTC)
	stale := projectTestStage("prod", "colo", "kargo-core", "stale", observedAt)
	stale.Conditions = []promotion.StageCondition{{Type: "Ready", Status: "False"}}
	unhealthy := projectTestStage("prod", "colo", "kargo-core", "unhealthy", observedAt.Add(9*time.Minute))
	unhealthy.Conditions = []promotion.StageCondition{{Type: "Ready", Status: "False"}}
	store := &projectionTestStore{
		stages:        []promotion.Stage{stale, unhealthy},
		pendingCounts: map[uuid.UUID]int{unhealthy.ID: 2},
	}
	service := NewProjectService(
		promotion.IntegrationSet{"prod": {
			ClusterName: "colo", MaximumStageObservationAge: 5 * time.Minute,
		}},
		store,
	)
	service.now = func() time.Time { return observedAt.Add(10 * time.Minute) }

	result, err := service.List(t.Context(), ProjectListFilter{Limit: promotion.DefaultListLimit})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("projects = %#v", result.Items)
	}
	summary := result.Items[0]
	if summary.StageCount != 2 || summary.StaleStageCount != 1 || summary.UnhealthyStageCount != 1 ||
		summary.PendingReviewCount != 2 || !summary.LatestObservedAt.Equal(unhealthy.LastObservedAt) {
		t.Errorf("operational summary = %#v", summary)
	}
	if result.Summary.StageCount != 2 || result.Summary.StaleStageCount != 1 ||
		result.Summary.UnhealthyStageCount != 1 || result.Summary.PendingReviewCount != 2 {
		t.Errorf("unpaginated operational summary = %#v", result.Summary)
	}

	for name, filter := range map[string]ProjectListFilter{
		"unhealthy":      {Unhealthy: true, Limit: promotion.DefaultListLimit},
		"stale":          {Stale: true, Limit: promotion.DefaultListLimit},
		"pending review": {PendingReview: true, Limit: promotion.DefaultListLimit},
	} {
		t.Run(name, func(t *testing.T) {
			filtered, filterErr := service.List(t.Context(), filter)
			if filterErr != nil {
				t.Fatalf("List() error = %v", filterErr)
			}
			if filtered.Total != 1 || len(filtered.Items) != 1 {
				t.Errorf("filtered Projects = %#v", filtered)
			}
		})
	}
}

func projectTestStage(integration, cluster, project, name string, observedAt time.Time) promotion.Stage {
	stage := projectionTestStage(integration, cluster, project, name, observedAt)
	stage.KubernetesUID = "uid-" + name
	stage.ResourceVersion = "1"

	return stage
}
