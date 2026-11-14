import { Alert, Checkbox, Group, Stack } from "@mantine/core";
import { redirect, useSearchParams } from "react-router";

import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { PromotionInventoryFilters } from "~/features/promotions/promotion-project-inventory";
import {
  promotionStageInventoryPageSize,
  shouldRevalidatePromotionInventory,
} from "~/features/promotions/promotion-project-inventory-state";
import { loadPromotionStageInventory } from "~/features/promotions/promotion-stage-inventory.server";
import { StageInventory, StagePolicyDialog } from "~/features/promotions/promotion-project-view";
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
    const lastPage = Math.max(1, Math.ceil(data.total / promotionStageInventoryPageSize));
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

export default function PromotionStages({ actionData, loaderData }: Route.ComponentProps) {
  useActivityRefresh(true, 30_000);

  const [searchParams, setSearchParams] = useSearchParams();
  const showFreight = searchParams.get("freight") === "visible";
  const stages = loaderData.stages.map(({ cluster, integration, project, stage }) => ({
    ...stage,
    projectContext: { cluster, integration, project },
  }));
  const policyStage = stages.find(
    (stage) =>
      stage.id === searchParams.get("policy") && promotionStagePolicyPresentation(stage).editable,
  );
  const canWrite = canWritePromotions(loaderData.principal);
  const filtered = Boolean(
    loaderData.application ||
    loaderData.cluster ||
    loaderData.environment ||
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

  function changeShowFreight(checked: boolean) {
    const next = new URLSearchParams(searchParams);
    if (checked) next.set("freight", "visible");
    else next.delete("freight");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  return (
    <Stack gap="lg">
      <PageHeader
        description="Active Kargo Stages across every observed Project and configured integration."
        title="Stages"
      />

      <PromotionInventoryFilters
        cluster={loaderData.cluster}
        integration={loaderData.integration}
        path="/promotion-stages"
        project={loaderData.project}
        inventoryState={{
          alignment: loaderData.alignment,
          status: loaderData.attention,
        }}
        metadata={{
          application: loaderData.application,
          environment: loaderData.environment,
        }}
      />

      <Group justify="flex-end">
        <Checkbox
          checked={showFreight}
          label="Show current Freight"
          onChange={(event) => changeShowFreight(event.currentTarget.checked)}
        />
      </Group>

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
      ) : (
        <StageInventory
          canWrite={canWrite}
          linkFilters
          onEditPolicy={(stage) => editPolicy(stage.id)}
          query=""
          showFreight={showFreight}
          showProject
          stages={stages}
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
