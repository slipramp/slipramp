import { Alert, Stack } from "@mantine/core";
import { redirect, useNavigate } from "react-router";

import type { PromotionFreightAlignment, PromotionPipeline } from "~/api/generated";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { freightComparisonStageIds } from "~/features/promotions/promotion-freight";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { loadPromotionPipelineInventory } from "~/features/promotions/promotion-pipeline-inventory.server";
import {
  PromotionInventoryScopeFilters,
  usePromotionInventoryPresentation,
} from "~/features/promotions/promotion-project-inventory";
import {
  promotionPipelineInventoryPageSize,
  promotionProjectDetailPath,
  shouldRevalidatePromotionInventory,
} from "~/features/promotions/promotion-project-inventory-state";
import {
  PipelineList,
  ProjectFilter,
  StagePolicyDialog,
  projectPipelines,
  stageMatchesAlignment,
  stageMatchesAttention,
  stageMatchesQuery,
  type ProjectPipeline,
} from "~/features/promotions/promotion-project-view";
import { promotionStagePolicyPresentation } from "~/features/promotions/promotion-policy";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { ListPagination } from "~/shared/list-controls";
import { EmptyState, PageHeader } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/promotion-pipelines";

export function meta() {
  return [{ title: pageTitle("Pipelines") }];
}

export const shouldRevalidate = shouldRevalidatePromotionInventory;

export async function loader({ context, request }: Route.LoaderArgs) {
  const data = await loadPromotionPipelineInventory(request, context);
  if (data.available) {
    const lastPage = Math.max(1, Math.ceil(data.total / promotionPipelineInventoryPageSize));
    if (data.page > lastPage) {
      const url = new URL(request.url);
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }
  return data;
}

export async function action({ context, request }: Route.ActionArgs) {
  return submitPromotionProjectAction(request, context);
}

export default function PromotionPipelines({ actionData, loaderData }: Route.ComponentProps) {
  useActivityRefresh(true, 30_000);

  const navigate = useNavigate();
  const presentation = usePromotionInventoryPresentation();
  const pipelines = loaderData.pipelines.flatMap((item) => inventoryPipeline(item));
  const visiblePipelines = pipelines.filter(
    (pipeline) =>
      pipelineMatchesQuery(pipeline, presentation.query) &&
      (!presentation.attention ||
        pipeline.stages.some((stage) => stageMatchesAttention(stage, presentation.attention))) &&
      (!presentation.alignment ||
        pipeline.stages.some((stage) => stageMatchesAlignment(stage, presentation.alignment))),
  );
  const validPipelineKeys = new Set(pipelines.map((pipeline) => pipeline.key));
  const explicitPipelineSelection =
    presentation.searchParams.has("pipeline") ||
    presentation.searchParams.get("pipelines") === "collapsed";
  const expandedPipelineKeys = explicitPipelineSelection
    ? [...new Set(presentation.searchParams.getAll("pipeline"))].filter((key) =>
        validPipelineKeys.has(key),
      )
    : visiblePipelines.length <= 4
      ? visiblePipelines.map((pipeline) => pipeline.key)
      : [];
  const stages = pipelines.flatMap((pipeline) => pipeline.stages);
  const policyStage = stages.find(
    (stage) =>
      stage.id === presentation.searchParams.get("policy") &&
      promotionStagePolicyPresentation(stage).editable,
  );
  const projectsByStage = new Map(
    pipelines.flatMap((pipeline) =>
      pipeline.projectContext
        ? pipeline.stages.map((stage) => [stage.id, pipeline.projectContext] as const)
        : [],
    ),
  );
  const canWrite = canWritePromotions(loaderData.principal);
  const filtered = Boolean(
    loaderData.cluster ||
    loaderData.integration ||
    loaderData.project ||
    presentation.alignment ||
    presentation.attention ||
    presentation.query,
  );

  function editPolicy(stageId: string) {
    const next = new URLSearchParams(presentation.searchParams);
    next.set("policy", stageId);
    presentation.setSearchParams(next, { preventScrollReset: true });
  }

  function closePolicy() {
    const next = new URLSearchParams(presentation.searchParams);
    next.delete("policy");
    presentation.setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeExpandedPipelines(keys: string[]) {
    const next = new URLSearchParams(presentation.searchParams);
    next.delete("pipeline");
    next.delete("pipelines");
    if (keys.length === 0) next.set("pipelines", "collapsed");
    else for (const key of keys) next.append("pipeline", key);
    presentation.setSearchParams(next, { preventScrollReset: true });
  }

  function compareFreight(alignment: PromotionFreightAlignment) {
    const project = projectsByStage.get(alignment.targetStageId);
    const stageIds = freightComparisonStageIds(alignment);
    if (!project || !stageIds) return;

    const search = new URLSearchParams({ view: "compare" });
    for (const id of stageIds) search.append("stage", id);
    navigate(`${promotionProjectDetailPath(project)}?${search}`);
  }

  return (
    <Stack gap="lg">
      <PageHeader
        description="Connected Kargo Stage graphs across every observed Project and configured integration."
        title="Pipelines"
      />

      <PromotionInventoryScopeFilters
        cluster={loaderData.cluster}
        integration={loaderData.integration}
        path="/promotion-pipelines"
        project={loaderData.project}
      />

      <ProjectFilter
        alignment={presentation.alignment}
        attention={presentation.attention}
        count={visiblePipelines.length}
        noun="pipeline"
        onAlignmentChange={presentation.changeAlignment}
        onAttentionChange={presentation.changeAttention}
        onChange={presentation.changeQuery}
        query={presentation.query}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Kargo pipelines unavailable">
          {loaderData.error}
        </Alert>
      ) : loaderData.pipelines.length === 0 ? (
        <EmptyState
          description={
            filtered
              ? "No active observed Kargo pipelines match these filters."
              : "No Kargo pipelines with active Stage observations are available yet."
          }
        />
      ) : visiblePipelines.length === 0 ? (
        <EmptyState
          description={`No pipelines on this page match “${presentation.query.trim()}”.`}
          title="No matching pipelines"
        />
      ) : (
        <PipelineList
          canWrite={canWrite}
          expandedKeys={expandedPipelineKeys}
          onCompareFreight={compareFreight}
          onEditPolicy={(stage) => editPolicy(stage.id)}
          onExpandedKeysChange={changeExpandedPipelines}
          pipelines={visiblePipelines}
          query={presentation.query}
          showProject
        />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.pipelines.length}
          noun="Pipelines"
          page={loaderData.page}
          pageSize={promotionPipelineInventoryPageSize}
          total={loaderData.total}
        />
      ) : null}

      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
}

function inventoryPipeline(item: PromotionPipeline): ProjectPipeline[] {
  const pipeline = projectPipelines(item.stages)[0];
  if (!pipeline) return [];

  return [
    {
      ...pipeline,
      key: `${item.integration}:${item.cluster}:${item.project}:${pipeline.key}`,
      label: item.name,
      projectContext: {
        cluster: item.cluster,
        integration: item.integration,
        project: item.project,
      },
    },
  ];
}

function pipelineMatchesQuery(pipeline: ProjectPipeline, query: string) {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return true;

  return (
    pipeline.label.toLocaleLowerCase().includes(normalized) ||
    pipeline.projectContext?.project.toLocaleLowerCase().includes(normalized) ||
    pipeline.projectContext?.integration.toLocaleLowerCase().includes(normalized) ||
    pipeline.projectContext?.cluster.toLocaleLowerCase().includes(normalized) ||
    pipeline.stages.some((stage) => stageMatchesQuery(stage, query))
  );
}
