import { Alert, Checkbox, Stack } from "@mantine/core";
import { redirect } from "react-router";

import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import {
  PromotionInventoryScopeFilters,
  usePromotionInventoryPresentation,
} from "~/features/promotions/promotion-project-inventory";
import {
  promotionStageInventoryPageSize,
  shouldRevalidatePromotionInventory,
} from "~/features/promotions/promotion-project-inventory-state";
import { loadPromotionStageInventory } from "~/features/promotions/promotion-stage-inventory.server";
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
  const data = await loadPromotionStageInventory(request, context);
  if (data.available) {
    const lastPage = Math.max(1, Math.ceil(data.total / promotionStageInventoryPageSize));
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
  const showFreight = presentation.searchParams.get("freight") === "visible";
  const stages = loaderData.stages.map(({ cluster, integration, project, stage }) => ({
    ...stage,
    projectContext: { cluster, integration, project },
  }));
  const visibleStages = stages.filter(
    (stage) =>
      stageMatchesQuery(stage, presentation.query) &&
      stageMatchesAttention(stage, presentation.attention) &&
      stageMatchesAlignment(stage, presentation.alignment),
  );
  const policyStage = stages.find(
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

  function changeShowFreight(checked: boolean) {
    const next = new URLSearchParams(presentation.searchParams);
    if (checked) next.set("freight", "visible");
    else next.delete("freight");
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
        count={visibleStages.length}
        noun="Stage"
        onAlignmentChange={presentation.changeAlignment}
        onAttentionChange={presentation.changeAttention}
        onChange={presentation.changeQuery}
        query={presentation.query}
      />

      <Checkbox
        checked={showFreight}
        label="Show current Freight"
        onChange={(event) => changeShowFreight(event.currentTarget.checked)}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Kargo Stages unavailable">
          {loaderData.error}
        </Alert>
      ) : loaderData.stages.length === 0 ? (
        <EmptyState
          description={
            filtered
              ? "No active observed Kargo Stages match these filters."
              : "No active Kargo Stage observations are available yet."
          }
        />
      ) : visibleStages.length === 0 ? (
        <EmptyState
          description={
            presentation.query.trim()
              ? `No Stages on this page match “${presentation.query.trim()}”.`
              : "No Stages on this page match the active filters."
          }
          title="No matching Stages"
        />
      ) : (
        <StageInventory
          canWrite={canWrite}
          onEditPolicy={(stage) => editPolicy(stage.id)}
          query={presentation.query}
          showFreight={showFreight}
          showProject
          stages={visibleStages}
        />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.stages.length}
          noun="Stages"
          page={loaderData.page}
          pageSize={promotionStageInventoryPageSize}
          total={loaderData.total}
        />
      ) : null}

      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
}
