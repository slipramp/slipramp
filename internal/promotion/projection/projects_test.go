package projection

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
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

func TestProjectServiceBuildsNativeTopologyWithinExactProjectBoundary(t *testing.T) {
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
		stages: []promotion.Stage{otherCluster, target, otherProject, otherIntegration, source},
	}
	service := NewProjectService(
		promotion.IntegrationSet{"prod": {ClusterName: "colo"}},
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
	if result.Stages[1].Capabilities.PromotionReady || result.Stages[1].Capabilities.BranchDriftReady {
		t.Errorf("unannotated Stage capabilities = %#v", result.Stages[1].Capabilities)
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

func TestProjectServiceSummarizesExactOperationalEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.November, 8, 12, 0, 0, 0, time.UTC)
	stale := projectTestStage("prod", "colo", "kargo-core", "stale", observedAt)
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
}

func projectTestStage(integration, cluster, project, name string, observedAt time.Time) promotion.Stage {
	stage := projectionTestStage(integration, cluster, project, name, observedAt)
	stage.KubernetesUID = "uid-" + name
	stage.ResourceVersion = "1"

	return stage
}
