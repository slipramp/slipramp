import { Alert, Stack } from "@mantine/core";
import { redirect } from "react-router";

import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { loadPromotionProjectInventory } from "~/features/promotions/promotion-project-inventory.server";
import {
  PromotionInventoryScopeFilters,
  PromotionProjectSectionHeader,
  usePromotionInventoryPresentation,
} from "~/features/promotions/promotion-project-inventory";
import {
  promotionProjectInventoryPageSize,
  shouldRevalidatePromotionInventory,
} from "~/features/promotions/promotion-project-inventory-state";
import {
  ProjectFilter,
  StageInventory,
  StagePolicyDialog,
  stageMatchesAlignment,
  stageMatchesAttention,
  stageMatchesQuery,
} from "~/features/promotions/promotion-project-view";
import { promotionStagePolicyPresentation } from "~/features/promotions/promotion-policy";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { ListPagination } from "~/shared/list-controls";
import { EmptyState, PageHeader } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/promotion-stages";

export function meta() {
  return [{ title: pageTitle("Stages") }];
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

export default function PromotionStages({ actionData, loaderData }: Route.ComponentProps) {
  useActivityRefresh(true, 30_000);

  const presentation = usePromotionInventoryPresentation();
  const projects = loaderData.projects.flatMap((entry) => (entry.detail ? [entry.detail] : []));
  const visibleProjects = projects
    .map((project) => ({
      project,
      stages: project.stages.filter(
        (stage) =>
          stageMatchesQuery(stage, presentation.query) &&
          stageMatchesAttention(stage, presentation.attention) &&
          stageMatchesAlignment(stage, presentation.alignment),
      ),
    }))
    .filter(({ stages }) => stages.length > 0);
  const visibleStageCount = visibleProjects.reduce(
    (count, project) => count + project.stages.length,
    0,
  );
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

  return (
    <Stack gap="lg">
      <PageHeader
        description="Active Kargo Stages across every observed Project and configured integration."
        title="Stages"
      />

      <PromotionInventoryScopeFilters
        cluster={loaderData.cluster}
        integration={loaderData.integration}
        path="/promotion-stages"
        project={loaderData.project}
      />

      <ProjectFilter
        alignment={presentation.alignment}
        attention={presentation.attention}
        count={visibleStageCount}
        noun="Stage"
        onAlignmentChange={presentation.changeAlignment}
        onAttentionChange={presentation.changeAttention}
        onChange={presentation.changeQuery}
        query={presentation.query}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Kargo Stages unavailable">
          {loaderData.error}
        </Alert>
      ) : loaderData.projects.length === 0 ? (
        <EmptyState
          description={
            filtered
              ? "No active observed Kargo Stages match these filters."
              : "No active Kargo Stage observations are available yet."
          }
        />
      ) : visibleProjects.length === 0 ? (
        <EmptyState
          description={
            presentation.query.trim()
              ? `No Stages on this page match “${presentation.query.trim()}”.`
              : "No Stages on this page match the active filters."
          }
          title="No matching Stages"
        />
      ) : (
        visibleProjects.map(({ project, stages }) => (
          <Stack gap="sm" key={`${project.integration}:${project.cluster}:${project.project}`}>
            <PromotionProjectSectionHeader project={project} view="stages" />
            <StageInventory
              canWrite={canWrite}
              onEditPolicy={(stage) => editPolicy(stage.id)}
              query={presentation.query}
              stages={stages}
            />
          </Stack>
        ))
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
