import { Alert, Stack } from "@mantine/core";
import { redirect, useNavigate, useSearchParams } from "react-router";

import type { PromotionFreightAlignment, PromotionPipeline } from "~/api/generated";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { freightComparisonStageIds } from "~/features/promotions/promotion-freight";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { loadPromotionPipelineInventory } from "~/features/promotions/promotion-pipeline-inventory.server";
import { PromotionInventoryFilters } from "~/features/promotions/promotion-project-inventory";
import {
  promotionPipelineInventoryPageSize,
  promotionProjectDetailPath,
  shouldRevalidatePromotionInventory,
} from "~/features/promotions/promotion-project-inventory-state";
import {
  PipelineList,
  StagePolicyDialog,
  projectPipelines,
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
  const url = new URL(request.url);
  let redirectNeeded = false;

  if (url.searchParams.has("q")) {
    url.searchParams.delete("q");
    redirectNeeded = true;
  }
  if (url.searchParams.has("attention") && !data.attention) {
    url.searchParams.delete("attention");
    redirectNeeded = true;
  }
  if (url.searchParams.has("alignment") && !data.alignment) {
    url.searchParams.delete("alignment");
    redirectNeeded = true;
  }

  if (data.available) {
    const lastPage = Math.max(1, Math.ceil(data.total / promotionPipelineInventoryPageSize));
    if (data.page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      redirectNeeded = true;
    }
  }

  if (redirectNeeded) return redirect(`${url.pathname}${url.search}`);
  return data;
}

export async function action({ context, request }: Route.ActionArgs) {
  return submitPromotionProjectAction(request, context);
}

export default function PromotionPipelines({ actionData, loaderData }: Route.ComponentProps) {
  useActivityRefresh(true, 30_000);

  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const pipelines = loaderData.pipelines.flatMap((item) => inventoryPipeline(item));
  const validPipelineKeys = new Set(pipelines.map((pipeline) => pipeline.key));
  const explicitPipelineSelection =
    searchParams.has("pipeline") || searchParams.get("pipelines") === "collapsed";
  const expandedPipelineKeys = explicitPipelineSelection
    ? [...new Set(searchParams.getAll("pipeline"))].filter((key) => validPipelineKeys.has(key))
    : pipelines.length <= 4
      ? pipelines.map((pipeline) => pipeline.key)
      : [];
  const stages = pipelines.flatMap((pipeline) => pipeline.stages);
  const policyStage = stages.find(
    (stage) =>
      stage.id === searchParams.get("policy") && promotionStagePolicyPresentation(stage).editable,
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
    loaderData.alignment ||
    loaderData.attention,
  );

  function editPolicy(stageId: string) {
    const next = new URLSearchParams(searchParams);
    next.set("policy", stageId);
    setSearchParams(next, { preventScrollReset: true });
  }

  function closePolicy() {
    const next = new URLSearchParams(searchParams);
    next.delete("policy");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeExpandedPipelines(keys: string[]) {
    const next = new URLSearchParams(searchParams);
    next.delete("pipeline");
    next.delete("pipelines");
    if (keys.length === 0) next.set("pipelines", "collapsed");
    else for (const key of keys) next.append("pipeline", key);
    setSearchParams(next, { preventScrollReset: true });
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

      <PromotionInventoryFilters
        cluster={loaderData.cluster}
        integration={loaderData.integration}
        inventoryState={{
          alignment: loaderData.alignment,
          status: loaderData.attention,
        }}
        path="/promotion-pipelines"
        project={loaderData.project}
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
      ) : (
        <PipelineList
          canWrite={canWrite}
          expandedKeys={expandedPipelineKeys}
          onCompareFreight={compareFreight}
          onEditPolicy={(stage) => editPolicy(stage.id)}
          onExpandedKeysChange={changeExpandedPipelines}
          pipelines={pipelines}
          query=""
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
