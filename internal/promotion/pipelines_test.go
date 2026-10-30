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
		policies: map[uuid.UUID]StagePolicy{
			production.ID: {
				StageID: production.ID,
				Mode:    PolicyModeReviewRequired,
			},
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
	if pipeline.Stages[2].Policy.EffectiveMode != PolicyModeReviewRequired ||
		pipeline.Stages[0].Policy.EffectiveMode != PolicyModeAutomatic {
		t.Errorf("Stage policies = %#v", pipeline.Stages)
	}
	wantPendingStageIDs := []uuid.UUID{production.ID, homelabB.ID, canary.ID, otherApplication.ID, homelabA.ID}
	if !reflect.DeepEqual(store.pendingStageIDs, wantPendingStageIDs) {
		t.Errorf("pending count Stage IDs = %v, want %v", store.pendingStageIDs, wantPendingStageIDs)
	}

	missingEnvironment := result.Items[0]
	if missingEnvironment.Application != "external-dns" ||
		!reflect.DeepEqual(missingEnvironment.EnvironmentOrder, []string{"homelab", "prod"}) ||
		len(missingEnvironment.Stages) != 1 {
		t.Errorf("pipeline with missing environment = %#v", missingEnvironment)
	}
}

func TestFreightPipelineServiceReportsStageObservationFreshness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	fresh := pipelineStage("prod", "kargo-core", "fresh", "fresh-prod", "prod", now.Add(-5*time.Minute))
	stale := pipelineStage("prod", "kargo-core", "stale", "stale-prod", "prod", now.Add(-5*time.Minute-time.Microsecond))
	service := NewFreightPipelineService(
		IntegrationSet{
			"prod": {
				EnvironmentOrder:           []string{"prod"},
				MaximumStageObservationAge: 5 * time.Minute,
			},
		},
		&reviewStore{stages: []Stage{stale, fresh}},
	)
	service.now = func() time.Time { return now }

	result, err := service.List(t.Context(), FreightPipelineListFilter{Limit: DefaultListLimit})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].Stages[0].ObservationStale ||
		!result.Items[1].Stages[0].ObservationStale {
		t.Errorf("pipeline freshness = %#v", result.Items)
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
	wantPendingStageIDs := []uuid.UUID{store.stages[0].ID, store.stages[1].ID}
	if !reflect.DeepEqual(store.pendingStageIDs, wantPendingStageIDs) {
		t.Errorf("pending count Stage IDs = %v, want %v", store.pendingStageIDs, wantPendingStageIDs)
	}
}

func TestFreightPipelineServiceBuildsPromotionSourcesFromDeclaredTopology(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	source := pipelineStage("prod", "kargo-core", "shared-source", "app-homelab", "homelab", now)
	source.Freight = Freight{Items: []FreightItem{
		{Origin: "Warehouse/app", Name: "freight-42", Artifacts: []FreightArtifact{{Kind: "image", Version: "1.2.3"}}},
		{Origin: "Warehouse/other", Name: "freight-other", Artifacts: []FreightArtifact{}},
	}}
	target := pipelineStage("prod", "kargo-core", "app", "app-prod", "prod", now)
	target.RequestedFreight = []FreightRequest{{
		Origin:       "Warehouse/app",
		SourceStages: []string{source.Name},
	}}
	store := &reviewStore{stages: []Stage{target, source}}
	service := NewFreightPipelineService(
		IntegrationSet{"prod": {EnvironmentOrder: []string{"homelab", "prod"}}},
		store,
	)

	result, err := service.List(t.Context(), FreightPipelineListFilter{
		Application: target.Application,
		Limit:       DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !reflect.DeepEqual(store.pendingStageIDs, []uuid.UUID{target.ID}) {
		t.Errorf("pending count Stage IDs = %v, want only %s", store.pendingStageIDs, target.ID)
	}
	var targetPipeline FreightPipeline
	for _, pipeline := range result.Items {
		if pipeline.Application == target.Application {
			targetPipeline = pipeline
		}
	}
	if len(targetPipeline.Stages) != 1 || len(targetPipeline.Stages[0].PromotionSources) != 1 {
		t.Fatalf("target pipeline = %#v", targetPipeline)
	}
	promotionSource := targetPipeline.Stages[0].PromotionSources[0]
	if promotionSource.StageID != source.ID || promotionSource.StageName != source.Name ||
		promotionSource.Environment != source.Environment || len(promotionSource.Freight) != 1 ||
		promotionSource.Freight[0].Name != "freight-42" {
		t.Errorf("promotion source = %#v", promotionSource)
	}
	if len(targetPipeline.Stages[0].AvailableFreight) != 1 ||
		targetPipeline.Stages[0].AvailableFreight[0].Name != "freight-42" {
		t.Errorf("available Freight = %#v", targetPipeline.Stages[0].AvailableFreight)
	}
}

func TestFreightPipelineServiceReportsDirectFreightNotCurrentlyDeployed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	stage := pipelineStage("prod", "kargo-core", "app", "app-homelab", "homelab", now)
	stage.Freight = Freight{Items: []FreightItem{{Origin: "Warehouse/app", Name: "freight-old"}}}
	stage.DirectFreight = Freight{Items: []FreightItem{{Origin: "Warehouse/app", Name: "freight-new"}}}
	service := NewFreightPipelineService(
		IntegrationSet{"prod": {EnvironmentOrder: []string{"homelab"}}},
		&reviewStore{stages: []Stage{stage}},
	)

	result, err := service.List(t.Context(), FreightPipelineListFilter{Limit: DefaultListLimit})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	available := result.Items[0].Stages[0].AvailableFreight
	if len(available) != 1 || available[0].Name != "freight-new" {
		t.Errorf("available Freight = %#v", available)
	}
}

func TestFreightPipelineServiceFiltersAttentionBeforePagination(t *testing.T) {
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
	alphaHomelab.DirectFreight = pipelineFreight("2.0.0")
	alphaHomelab.DirectFreight.Items[0].Name = "freight-alpha-new"
	bravoHomelab.Freight = pipelineFreight("1.0.0")
	bravoProd.Freight = pipelineFreight("1.0.0")
	bravoProd.DirectFreight = pipelineFreight("2.0.0")
	bravoProd.DirectFreight.Items[0].Name = "freight-bravo-new"
	bravoProd.AutoPromotionEnabled = true
	charlieHomelab.Freight = pipelineFreight("1.0.0")
	charlieProd.Freight = pipelineFreight("2.0.0")
	deltaHomelab.Freight = pipelineFreight("2.0.0")
	deltaProd.Freight = pipelineFreight("3.0.0")
	deltaProd.DirectFreight = pipelineFreight("4.0.0")
	deltaProd.DirectFreight.Items[0].Name = "freight-delta-new"

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

	attention, err := service.List(t.Context(), FreightPipelineListFilter{
		Attention: FreightPipelineAttentionAny,
		Limit:     1,
		Offset:    1,
	})
	if err != nil {
		t.Fatalf("attention List() error = %v", err)
	}
	if attention.Total != 3 || len(attention.Items) != 1 || attention.Items[0].Application != "charlie" {
		t.Errorf("attention List() = %#v", attention)
	}

	drifted, err := service.List(t.Context(), FreightPipelineListFilter{
		Attention: FreightPipelineAttentionDrifted,
		Limit:     DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("drifted List() error = %v", err)
	}
	if drifted.Total != 2 || len(drifted.Items) != 2 || drifted.Items[0].Application != "charlie" ||
		drifted.Items[1].Application != "delta" {
		t.Errorf("drifted List() = %#v", drifted)
	}

	outdated, err := service.List(t.Context(), FreightPipelineListFilter{
		Attention: FreightPipelineAttentionOutdated,
		Limit:     DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("outdated List() error = %v", err)
	}
	if outdated.Total != 2 || len(outdated.Items) != 2 || outdated.Items[0].Application != "alpha" ||
		outdated.Items[1].Application != "delta" {
		t.Errorf("outdated List() = %#v", outdated)
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
	result, err := service.List(t.Context(), FreightPipelineListFilter{
		Attention: FreightPipelineAttentionDrifted,
		Limit:     DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("drifted List() error = %v", err)
	}
	if result.Total != 0 || len(result.Items) != 0 {
		t.Errorf("drifted List() = %#v", result)
	}
}

func TestFreightPipelineServiceRejectsInvalidAttention(t *testing.T) {
	t.Parallel()

	service := NewFreightPipelineService(IntegrationSet{}, &reviewStore{})
	_, err := service.List(t.Context(), FreightPipelineListFilter{
		Attention: "invalid",
		Limit:     DefaultListLimit,
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

func pipelineFreight(version string) Freight {
	return Freight{Items: []FreightItem{{
		Origin: "Warehouse/app", Name: "freight",
		Artifacts: []FreightArtifact{{Kind: "image", Repository: new("ghcr.io/example/app"), Version: version}},
	}}}
}
