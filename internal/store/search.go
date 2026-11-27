package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) SearchTargets(
	ctx context.Context, terms []string, limit int32,
) ([]catalog.TargetSearchResult, error) {
	rows, err := d.queries.SearchTargets(ctx, storedb.SearchTargetsParams{
		Terms: terms, Query: strings.Join(terms, " "), ResultLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search active Targets: %w", err)
	}
	items := make([]catalog.TargetSearchResult, 0, len(rows))
	for _, row := range rows {
		items = append(items, catalog.TargetSearchResult{
			ID: uuid.UUID(row.ID.Bytes), Title: row.Title, Name: row.Name, Repository: row.Repository,
		})
	}
	return items, nil
}

func (d *Database) SearchPromotionInventory(
	ctx context.Context, integrations promotion.IntegrationSet, terms []string, limit int32,
) (promotion.InventorySearchResults, error) {
	var result promotion.InventorySearchResults
	if len(integrations) == 0 {
		return result, nil
	}

	names := integrations.Names()
	clusters := make([]string, len(names))
	for i, name := range names {
		clusters[i] = integrations[name].ClusterName
	}
	rows, err := d.queries.SearchPromotionInventory(ctx, storedb.SearchPromotionInventoryParams{
		Integrations: names, Clusters: clusters, Terms: terms, Query: strings.Join(terms, " "), ResultLimit: limit,
	})
	if err != nil {
		return result, fmt.Errorf("search promotion inventory: %w", err)
	}
	for _, row := range rows {
		item := promotion.InventorySearchResult{
			Integration: row.Integration, ClusterName: row.ClusterName, Project: row.Project,
			Name: row.Name, StageName: row.StageName, StageID: uuid.UUID(row.StageID.Bytes),
		}
		if row.IsPipeline {
			result.Pipelines = append(result.Pipelines, item)
		} else {
			result.Stages = append(result.Stages, item)
		}
	}
	return result, nil
}

func replacePipelineSearch(
	ctx context.Context, queries *storedb.Queries, snapshot promotion.StageSnapshot, stages []promotion.Stage,
) error {
	if err := queries.DeleteSnapshotPipelineSearch(ctx, storedb.DeleteSnapshotPipelineSearchParams{
		Integration: snapshot.Integration, ClusterName: snapshot.ClusterName,
	}); err != nil {
		return fmt.Errorf("clear snapshot Pipeline search rows: %w", err)
	}
	if len(stages) == 0 {
		return nil
	}

	params := storedb.InsertSnapshotPipelineSearchParams{
		Integration: snapshot.Integration, ClusterName: snapshot.ClusterName,
	}
	byProject := make(map[string][]promotion.Stage)
	for _, stage := range stages {
		byProject[stage.Project] = append(byProject[stage.Project], stage)
	}
	for project, members := range byProject {
		for _, pipeline := range promotion.ProjectPipelines(members) {
			params.Projects = append(params.Projects, project)
			params.StageNames = append(params.StageNames, pipeline.StageNames[0])
			params.PipelineNames = append(params.PipelineNames, pipeline.Name)
		}
	}
	if err := queries.InsertSnapshotPipelineSearch(ctx, params); err != nil {
		return fmt.Errorf("store snapshot Pipeline search rows: %w", err)
	}
	return nil
}
