import { Alert, Stack } from "@mantine/core";
import { redirect, useNavigate } from "react-router";

import type { PromotionFreightAlignment, PromotionProjectDetail } from "~/api/generated";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { freightComparisonStageIds } from "~/features/promotions/promotion-freight";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { loadPromotionProjectInventory } from "~/features/promotions/promotion-project-inventory.server";
import {
  PromotionInventoryScopeFilters,
  PromotionProjectSectionHeader,
  usePromotionInventoryPresentation,
} from "~/features/promotions/promotion-project-inventory";
import {
  promotionProjectDetailPath,
  promotionProjectInventoryPageSize,
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
  const data = await loadPromotionProjectInventory(request, context);
  if (data.available) {
    const lastPage = Math.max(1, Math.ceil(data.total / promotionProjectInventoryPageSize));
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
  const projects = loaderData.projects.flatMap((entry) => (entry.detail ? [entry.detail] : []));
  const pipelineGroups = projects
    .map((project) => ({
      pipelines: projectPipelines(project.stages).filter(
        (pipeline) =>
          pipeline.stages.some((stage) => stageMatchesQuery(stage, presentation.query)) &&
          (!presentation.attention ||
            pipeline.stages.some((stage) =>
              stageMatchesAttention(stage, presentation.attention),
            )) &&
          (!presentation.alignment ||
            pipeline.stages.some((stage) => stageMatchesAlignment(stage, presentation.alignment))),
      ),
      project,
    }))
    .filter(({ pipelines }) => pipelines.length > 0);
  const visiblePipelineCount = pipelineGroups.reduce(
    (count, project) => count + project.pipelines.length,
    0,
  );
  const visiblePipelineKeys = pipelineGroups.flatMap((project) =>
    project.pipelines.map((pipeline) => pipeline.key),
  );
  const validPipelineKeys = new Set(
    projects.flatMap((project) => projectPipelines(project.stages).map((pipeline) => pipeline.key)),
  );
  const explicitPipelineSelection =
    presentation.searchParams.has("pipeline") ||
    presentation.searchParams.get("pipelines") === "collapsed";
  const expandedPipelineKeys = explicitPipelineSelection
    ? [...new Set(presentation.searchParams.getAll("pipeline"))].filter((key) =>
        validPipelineKeys.has(key),
      )
    : visiblePipelineCount <= 4
      ? visiblePipelineKeys
      : [];
  const policyStage = projects
    .flatMap((project) => project.stages)
    .find(
      (stage) =>
        stage.id === presentation.searchParams.get("policy") &&
        promotionStagePolicyPresentation(stage).editable,
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

  function changeExpandedPipelines(projectKeys: string[], keys: string[]) {
    const projectKeySet = new Set(projectKeys);
    const nextKeys = [...expandedPipelineKeys.filter((key) => !projectKeySet.has(key)), ...keys];
    const next = new URLSearchParams(presentation.searchParams);
    next.delete("pipeline");
    next.delete("pipelines");
    if (nextKeys.length === 0) next.set("pipelines", "collapsed");
    else for (const key of nextKeys) next.append("pipeline", key);
    presentation.setSearchParams(next, { preventScrollReset: true });
  }

  function compareFreight(project: PromotionProjectDetail, alignment: PromotionFreightAlignment) {
    const stageIds = freightComparisonStageIds(alignment);
    if (!stageIds) return;

    const search = new URLSearchParams({ view: "compare" });
    for (const id of stageIds) search.append("stage", id);
    navigate(`${promotionProjectDetailPath(project)}?${search}`);
  }

  return (
    <Stack gap="lg">
      <PageHeader
        description="Disconnected Kargo Stage graphs across every observed Project and configured integration."
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
        count={visiblePipelineCount}
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
      ) : loaderData.projects.length === 0 ? (
        <EmptyState
          description={
            filtered
              ? "No active observed Kargo pipelines match these filters."
              : "No Kargo Projects with active Stage observations are available yet."
          }
        />
      ) : pipelineGroups.length === 0 ? (
        <EmptyState
          description={
            presentation.query.trim()
              ? `No pipelines on this page match “${presentation.query.trim()}”.`
              : "No pipelines on this page match the active filters."
          }
          title="No matching pipelines"
        />
      ) : (
        pipelineGroups.map(({ pipelines, project }) => {
          const projectKeys = projectPipelines(project.stages).map((pipeline) => pipeline.key);
          return (
            <Stack gap="sm" key={`${project.integration}:${project.cluster}:${project.project}`}>
              <PromotionProjectSectionHeader project={project} view="pipelines" />
              <PipelineList
                canWrite={canWrite}
                expandedKeys={expandedPipelineKeys.filter((key) => projectKeys.includes(key))}
                onCompareFreight={(alignment) => compareFreight(project, alignment)}
                onEditPolicy={(stage) => editPolicy(stage.id)}
                onExpandedKeysChange={(keys) => changeExpandedPipelines(projectKeys, keys)}
                pipelines={pipelines}
                query={presentation.query}
              />
            </Stack>
          );
        })
      )}

      {loaderData.projects.map((entry) =>
        entry.error ? (
          <Alert
            color="yellow"
            key={`${entry.summary.integration}:${entry.summary.cluster}:${entry.summary.project}`}
            title={`${entry.summary.project} unavailable`}
          >
            {entry.error}
          </Alert>
        ) : null,
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.projects.length}
          noun="Projects"
          page={loaderData.page}
          pageSize={promotionProjectInventoryPageSize}
          total={loaderData.total}
        />
      ) : null}

      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
}
