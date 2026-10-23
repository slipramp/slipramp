package promotion

import (
	"reflect"
	"testing"
	"time"
)

func TestProjectServiceGroupsActiveStages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	first := pipelineStage("prod", "kargo-core", "external-dns", "external-dns-homelab", "homelab", now)
	second := pipelineStage("prod", "kargo-core", "external-dns", "external-dns-prod", "prod", now.Add(time.Minute))
	third := pipelineStage("prod", "kargo-core", "metrics", "metrics-prod", "prod", now.Add(2*time.Minute))
	other := pipelineStage("prod", "kargo-platform", "cert-manager", "cert-manager-prod", "prod", now)
	archived := pipelineStage("prod", "kargo-archived", "old", "old-prod", "prod", now)
	archived.ArchivedAt = new(now.Add(time.Minute))
	unconfigured := pipelineStage("removed", "kargo-removed", "removed", "removed-prod", "prod", now)

	service := NewProjectService(
		IntegrationSet{"prod": {}},
		&reviewStore{stages: []Stage{archived, third, unconfigured, second, other, first}},
	)

	result, err := service.List(t.Context(), ProjectListFilter{Limit: DefaultListLimit})
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
	if core.StageCount != 3 || core.ApplicationCount != 2 ||
		!core.LastObservedAt.Equal(now.Add(2*time.Minute)) {
		t.Errorf("kargo-core summary = %#v", core)
	}
}

func TestProjectServiceFiltersAndPaginatesProjects(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	first := pipelineStage("prod", "kargo-core", "alpha", "alpha-prod", "prod", now)
	second := pipelineStage("prod", "kargo-platform", "bravo", "bravo-prod", "prod", now)
	second.ClusterName = "edge"
	third := pipelineStage("dev", "kargo-dev", "charlie", "charlie-dev", "dev", now)

	service := NewProjectService(
		IntegrationSet{"dev": {}, "prod": {}},
		&reviewStore{stages: []Stage{third, second, first}},
	)

	result, err := service.List(t.Context(), ProjectListFilter{
		Integration: "prod",
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
	if result.Total != 3 || len(result.Items) != 1 || result.Items[0].Project != "kargo-core" {
		t.Errorf("paginated projects = %#v", result)
	}
}
