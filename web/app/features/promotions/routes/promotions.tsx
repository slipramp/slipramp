import { Alert, Paper, Stack, Text } from "@mantine/core";
import { redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listPromotions,
  type KargoPromotionOutcomePhase,
  type PromotionCreationDispatchPhase,
  type PromotionStatus,
  type CancellationState,
} from "~/api/generated";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { ListPagination, SearchParamFilters } from "~/shared/list-controls";
import { PageHeader } from "~/shared/page";
import { promotionScopeFilters } from "~/features/promotions/promotion-scope-filters";
import { promotionActivity } from "~/features/mascot/execution-activity";
import { PromotionTable } from "~/features/promotions/promotion-table";
import { pageTitle } from "~/shared/page-title";
import { pageFromSearchParam } from "~/shared/search-params";

import type { Route } from "./+types/promotions";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Promotions") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const application = optionalValue(url.searchParams.get("application"));
  const cluster = optionalValue(url.searchParams.get("cluster"));
  const status = optionalStatus(url.searchParams.get("status"));
  const cancellationState = optionalCancellationState(url.searchParams.get("cancellationState"));
  const creationDispatch = optionalCreationDispatch(url.searchParams.get("creationDispatch"));
  const kargoOutcome = optionalKargoOutcome(url.searchParams.get("kargoOutcome"));
  const stageId = optionalValue(url.searchParams.get("stageId"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const result = await listPromotions({
    client,
    query: {
      application,
      cluster,
      cancellationState,
      creationDispatch,
      integration,
      kargoOutcome,
      limit: pageSize,
      offset: (requestedPage - 1) * pageSize,
      project,
      stageId,
      status,
    },
  });

  if (result.data) {
    const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
    if (requestedPage > lastPage) {
      setPage(url, lastPage);
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    available: result.data !== undefined,
    error: result.error?.detail ?? result.error?.title,
    filters: {
      application,
      cluster,
      cancellationState,
      creationDispatch,
      integration,
      kargoOutcome,
      project,
      stageId,
      status,
    },
    page: requestedPage,
    promotions: result.data?.items ?? [],
    total: result.data?.total ?? 0,
  };
}

export default function Promotions({ loaderData }: Route.ComponentProps) {
  const hasActivePromotion = loaderData.promotions.some(
    (promotion) => promotionActivity(promotion) !== undefined,
  );
  useActivityRefresh(true, hasActivePromotion ? 3_000 : 30_000);

  return (
    <Stack gap="lg">
      <PageHeader
        description="Kargo promotion preparation and operator review."
        title="Promotions"
      />

      <PromotionFilters filters={loaderData.filters} />

      {!loaderData.available ? (
        <Alert color="red" title="Promotions unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.promotions.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No Promotions match these filters.</Text>
        </Paper>
      ) : (
        <PromotionTable promotions={loaderData.promotions} />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.promotions.length}
          noun="Promotions"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function PromotionFilters({
  filters,
}: Readonly<{
  filters: {
    application?: string;
    cluster?: string;
    cancellationState?: CancellationState;
    creationDispatch?: PromotionCreationDispatchPhase;
    integration?: string;
    kargoOutcome?: KargoPromotionOutcomePhase;
    project?: string;
    stageId?: string;
    status?: PromotionStatus;
  };
}>) {
  return (
    <SearchParamFilters
      clearTo="/promotions"
      filters={[
        ...promotionScopeFilters(filters),
        {
          active: Boolean(filters.cancellationState),
          kind: "select",
          name: "cancellationState",
          label: "Cancellation",
          value: filters.cancellationState,
          data: [
            { label: "All", value: "" },
            { label: "Not requested", value: "not-requested" },
            { label: "Canceling", value: "requested" },
            { label: "Canceled", value: "completed" },
            { label: "Overtaken", value: "overtaken" },
          ],
        },
        {
          active: Boolean(filters.cluster),
          kind: "text",
          label: "Cluster",
          name: "cluster",
          value: filters.cluster,
        },
        {
          active: Boolean(filters.status),
          data: [
            { label: "All statuses", value: "" },
            { label: "Pending review", value: "pending" },
            { label: "Creating", value: "creating" },
            { label: "Preparing", value: "preparing" },
            { label: "Approved", value: "approved" },
            { label: "Rejected", value: "rejected" },
            { label: "Expired", value: "expired" },
            { label: "Error", value: "error" },
          ],
          kind: "select",
          label: "Review status",
          name: "status",
          value: filters.status,
        },
        {
          active: Boolean(filters.creationDispatch),
          data: [
            { label: "All delivery states", value: "" },
            { label: "Awaiting delivery", value: "awaiting-delivery" },
            { label: "Delivered", value: "delivered" },
            { label: "Stopped", value: "stopped" },
            { label: "Delivery failed", value: "failed" },
            { label: "Kargo-originated", value: "not-applicable" },
          ],
          kind: "select",
          label: "Kargo delivery",
          name: "creationDispatch",
          value: filters.creationDispatch,
        },
        {
          active: Boolean(filters.kargoOutcome),
          data: [
            { label: "All Kargo outcomes", value: "" },
            { label: "Succeeded", value: "Succeeded" },
            { label: "Failed", value: "Failed" },
            { label: "Errored", value: "Errored" },
            { label: "Aborted", value: "Aborted" },
          ],
          kind: "select",
          label: "Kargo outcome",
          name: "kargoOutcome",
          value: filters.kargoOutcome,
        },
        {
          active: Boolean(filters.stageId),
          kind: "text",
          label: "Stage ID",
          name: "stageId",
          value: filters.stageId,
        },
      ]}
    />
  );
}

function optionalStatus(value: string | null): PromotionStatus | undefined {
  switch (value) {
    case "creating":
    case "preparing":
    case "pending":
    case "approved":
    case "rejected":
    case "expired":
    case "error":
      return value;
    default:
      return undefined;
  }
}

function optionalCreationDispatch(
  value: string | null,
): PromotionCreationDispatchPhase | undefined {
  switch (value) {
    case "not-applicable":
    case "awaiting-delivery":
    case "stopped":
    case "delivered":
    case "failed":
      return value;
    default:
      return undefined;
  }
}

function optionalKargoOutcome(value: string | null): KargoPromotionOutcomePhase | undefined {
  switch (value) {
    case "Succeeded":
    case "Failed":
    case "Errored":
    case "Aborted":
      return value;
    default:
      return undefined;
  }
}

function setPage(url: URL, page: number) {
  if (page === 1) url.searchParams.delete("page");
  else url.searchParams.set("page", String(page));
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}

function optionalCancellationState(value: string | null): CancellationState | undefined {
  switch (value) {
    case "not-requested":
    case "requested":
    case "completed":
    case "overtaken":
      return value;
    default:
      return undefined;
  }
}
