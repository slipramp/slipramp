package promotion

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFreightPipelineServiceGroupsAndOrdersActiveStages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	homelabB := pipelineStage("prod", "kargo-core", "ops-ci", "ops-ci-homelab-b", "homelab", now)
	homelabA := pipelineStage("prod", "kargo-core", "ops-ci", "ops-ci-homelab-a", "homelab", now)
	production := pipelineStage("prod", "kargo-core", "ops-ci", "ops-ci-prod", "prod", now)
	canary := pipelineStage("prod", "kargo-core", "ops-ci", "ops-ci-canary", "canary", now)
	otherApplication := pipelineStage("prod", "kargo-core", "external-dns", "external-dns-homelab", "homelab", now)
	archived := pipelineStage("prod", "kargo-core", "ops-ci", "ops-ci-archived", "old", now)
	archived.ArchivedAt = new(now)
	unconfigured := pipelineStage("removed", "kargo-core", "removed", "removed-prod", "prod", now)

	store := &reviewStore{
		stages: []Stage{
			production,
			homelabB,
			archived,
			canary,
			otherApplication,
			homelabA,
			unconfigured,
		},
		promotions: []Promotion{
			{ID: uuid.New(), StageID: homelabA.ID, Status: StatusPending},
			{ID: uuid.New(), StageID: homelabA.ID, Status: StatusApproved},
		},
	}
	service := NewFreightPipelineService(
		IntegrationSet{
			"prod": {
				EnvironmentOrder: []string{"homelab", "prod"},
			},
		},
		store,
	)

	result, err := service.List(t.Context(), FreightPipelineListFilter{Limit: DefaultListLimit})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 2 || len(result.Items) != 2 {
		t.Fatalf("List() = %#v", result)
	}

	pipeline := result.Items[1]
	if pipeline.Application != "ops-ci" ||
		!reflect.DeepEqual(pipeline.EnvironmentOrder, []string{"homelab", "prod"}) {
		t.Fatalf("ops-ci pipeline = %#v", pipeline)
	}
	wantStages := []string{"ops-ci-homelab-a", "ops-ci-homelab-b", "ops-ci-prod", "ops-ci-canary"}
	gotStages := make([]string, 0, len(pipeline.Stages))
	for _, stage := range pipeline.Stages {
		gotStages = append(gotStages, stage.Stage.Name)
	}
	if !reflect.DeepEqual(gotStages, wantStages) {
		t.Errorf("Stage order = %v, want %v", gotStages, wantStages)
	}
	if pipeline.Stages[0].PendingPromotionCount != 1 {
		t.Errorf("pending count = %d, want 1", pipeline.Stages[0].PendingPromotionCount)
	}

	missingEnvironment := result.Items[0]
	if missingEnvironment.Application != "external-dns" ||
		!reflect.DeepEqual(missingEnvironment.EnvironmentOrder, []string{"homelab", "prod"}) ||
		len(missingEnvironment.Stages) != 1 {
		t.Errorf("pipeline with missing environment = %#v", missingEnvironment)
	}
}

func TestFreightPipelineServiceFiltersAndPaginatesPipelines(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	store := &reviewStore{
		stages: []Stage{
			pipelineStage("prod", "kargo-core", "alpha", "alpha-prod", "prod", now),
			pipelineStage("prod", "kargo-core", "bravo", "bravo-prod", "prod", now),
			pipelineStage("prod", "kargo-platform", "charlie", "charlie-prod", "prod", now),
		},
	}
	service := NewFreightPipelineService(
		IntegrationSet{"prod": {EnvironmentOrder: []string{"prod"}}},
		store,
	)

	result, err := service.List(t.Context(), FreightPipelineListFilter{
		Project: "kargo-core",
		Limit:   1,
		Offset:  1,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 2 || len(result.Items) != 1 || result.Items[0].Application != "bravo" {
		t.Errorf("filtered page = %#v", result)
	}
}

func TestFreightPipelineServiceFiltersComparisonStatusBeforePagination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	alphaHomelab := pipelineStage("prod", "kargo-core", "alpha", "alpha-homelab", "homelab", now)
	bravoHomelab := pipelineStage("prod", "kargo-core", "bravo", "bravo-homelab", "homelab", now)
	bravoProd := pipelineStage("prod", "kargo-core", "bravo", "bravo-prod", "prod", now)
	charlieHomelab := pipelineStage("prod", "kargo-core", "charlie", "charlie-homelab", "homelab", now)
	charlieProd := pipelineStage("prod", "kargo-core", "charlie", "charlie-prod", "prod", now)
	deltaHomelab := pipelineStage("prod", "kargo-core", "delta", "delta-homelab", "homelab", now)
	deltaProd := pipelineStage("prod", "kargo-core", "delta", "delta-prod", "prod", now)

	alphaHomelab.Freight = pipelineFreight("1.0.0")
	bravoHomelab.Freight = pipelineFreight("1.0.0")
	bravoProd.Freight = pipelineFreight("1.0.0")
	charlieHomelab.Freight = pipelineFreight("1.0.0")
	charlieProd.Freight = pipelineFreight("2.0.0")
	deltaHomelab.Freight = pipelineFreight("2.0.0")
	deltaProd.Freight = pipelineFreight("3.0.0")

	store := &reviewStore{stages: []Stage{
		alphaHomelab,
		bravoHomelab,
		bravoProd,
		charlieHomelab,
		charlieProd,
		deltaHomelab,
		deltaProd,
	}}
	service := NewFreightPipelineService(
		IntegrationSet{"prod": {EnvironmentOrder: []string{"homelab", "prod"}}},
		store,
	)

	equal, err := service.List(t.Context(), FreightPipelineListFilter{
		Status: FreightComparisonStatusEqual,
		Limit:  DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("equal List() error = %v", err)
	}
	if equal.Total != 1 || len(equal.Items) != 1 || equal.Items[0].Application != "bravo" {
		t.Errorf("equal List() = %#v", equal)
	}

	different, err := service.List(t.Context(), FreightPipelineListFilter{
		Status: FreightComparisonStatusDifferent,
		Limit:  1,
		Offset: 1,
	})
	if err != nil {
		t.Fatalf("different List() error = %v", err)
	}
	if different.Total != 2 || len(different.Items) != 1 || different.Items[0].Application != "delta" {
		t.Errorf("different List() = %#v", different)
	}
}

func TestFreightPipelineServiceRequiresStagesInTwoEnvironmentsForComparison(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	homelabA := pipelineStage("prod", "kargo-core", "app", "app-homelab-a", "homelab", now)
	homelabB := pipelineStage("prod", "kargo-core", "app", "app-homelab-b", "homelab", now)
	homelabA.Freight = pipelineFreight("1.0.0")
	homelabB.Freight = pipelineFreight("2.0.0")

	service := NewFreightPipelineService(
		IntegrationSet{"prod": {EnvironmentOrder: []string{"homelab", "prod"}}},
		&reviewStore{stages: []Stage{homelabA, homelabB}},
	)
	for _, status := range []FreightComparisonStatus{
		FreightComparisonStatusEqual,
		FreightComparisonStatusDifferent,
	} {
		result, err := service.List(t.Context(), FreightPipelineListFilter{
			Status: status,
			Limit:  DefaultListLimit,
		})
		if err != nil {
			t.Fatalf("%s List() error = %v", status, err)
		}
		if result.Total != 0 || len(result.Items) != 0 {
			t.Errorf("%s List() = %#v", status, result)
		}
	}
}

func TestFreightPipelineServiceRejectsInvalidComparisonStatus(t *testing.T) {
	t.Parallel()

	service := NewFreightPipelineService(IntegrationSet{}, &reviewStore{})
	_, err := service.List(t.Context(), FreightPipelineListFilter{
		Status: "drifted",
		Limit:  DefaultListLimit,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("List() error = %v, want ErrInvalid", err)
	}
}

func pipelineStage(
	integration string,
	project string,
	application string,
	name string,
	environment string,
	now time.Time,
) Stage {
	stage := reviewStage(integration, "colo", project, name, now)
	stage.Application = application
	stage.Environment = environment

	return stage
}

func pipelineFreight(version string) []byte {
	return []byte(`{"items":[{"origin":"Warehouse/app","name":"freight","artifacts":[` +
		`{"kind":"image","repository":"ghcr.io/example/app","version":"` + version + `"}]}]}`)
}
