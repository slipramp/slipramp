import { Alert, Anchor, Badge, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotions, type Promotion, type PromotionStatus } from "~/api/generated";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime } from "~/shared/display-values";
import { Identity } from "~/shared/identity";
import { KargoLink } from "~/features/promotions/kargo-link";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { ListPagination, SearchParamFilters } from "~/shared/list-controls";
import { PageHeader } from "~/shared/page";
import { promotionScopeFilters } from "~/features/promotions/promotion-scope-filters";
import { abbreviateMiddle } from "~/shared/display";
import { promotionActivity } from "~/features/mascot/execution-activity";
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
  const stageId = optionalValue(url.searchParams.get("stageId"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const result = await listPromotions({
    client,
    query: {
      application,
      cluster,
      integration,
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
    filters: { application, cluster, integration, project, stageId, status },
    page: requestedPage,
    promotions: result.data?.items ?? [],
    total: result.data?.total ?? 0,
  };
}

export default function Promotions({ loaderData }: Route.ComponentProps) {
  useActivityRefresh(
    loaderData.promotions.some((promotion) => promotionActivity(promotion) !== undefined),
  );

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
    integration?: string;
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
          label: "Status",
          name: "status",
          value: filters.status,
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

function PromotionTable({ promotions }: Readonly<{ promotions: Promotion[] }>) {
  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={760} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Promotion / Stage</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Application</Table.Th>
              <Table.Th>Source / author</Table.Th>
              <Table.Th>Created</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {promotions.map((promotion) => (
              <PromotionRow key={promotion.id} promotion={promotion} />
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function PromotionRow({ promotion }: Readonly<{ promotion: Promotion }>) {
  const stageName = promotion.stageSnapshot.name;
  const project = promotion.stageSnapshot.project;
  const application = promotion.stageSnapshot.application;

  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <Anchor
            component={Link}
            fw={600}
            title={promotion.externalPromotionId}
            to={`/promotions/${promotion.id}`}
          >
            {abbreviateMiddle(promotion.externalPromotionId, 36)}
          </Anchor>
          <Group gap="xs" wrap="wrap">
            <Text c="dimmed" size="xs">
              {stageName}
              {project ? ` · ${project}` : ""}
            </Text>
            {promotion.kargoUrl ? (
              <KargoLink size="xs" url={promotion.kargoUrl}>
                Kargo
              </KargoLink>
            ) : null}
          </Group>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Group gap="xs" justify="space-between" wrap="nowrap">
          <Stack gap={4}>
            <Badge color={promotionStatusColor(promotion.status)} variant="light">
              {promotionStatusLabel(promotion.status)}
            </Badge>
            {promotion.status === "approved" ? (
              <Badge
                color={
                  promotion.kargoOutcome?.phase === "Succeeded"
                    ? "green"
                    : promotion.kargoOutcome
                      ? "red"
                      : "cyan"
                }
                variant="light"
              >
                {promotion.kargoOutcome
                  ? `Kargo ${promotion.kargoOutcome.phase.toLocaleLowerCase()}`
                  : "Kargo running"}
              </Badge>
            ) : null}
          </Stack>
          <KawaiiActivity
            activity={promotionActivity(promotion)}
            animationKey={promotion.id}
            compact
          />
        </Group>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Anchor
            component={Link}
            to={`/promotions?application=${encodeURIComponent(application)}`}
          >
            {application}
          </Anchor>
          <Text c="dimmed" size="xs">
            {promotion.integration}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Text size="sm">{promotion.source === "kargo" ? "Kargo" : "Slipramp"}</Text>
          <Identity
            avatarSize={22}
            name={promotion.requestedBy.displayName}
            nameProps={{ c: "dimmed", fw: 500, size: "xs" }}
          />
        </Stack>
      </Table.Td>
      <Table.Td style={{ whiteSpace: "nowrap" }}>
        <RelativeTime size="sm" value={promotion.createdAt} />
      </Table.Td>
    </Table.Tr>
  );
}

export function promotionStatusColor(status: PromotionStatus) {
  switch (status) {
    case "creating":
      return "cyan";
    case "pending":
      return "yellow";
    case "approved":
      return "green";
    case "rejected":
    case "error":
      return "red";
    case "expired":
      return "gray";
    case "preparing":
      return "blue";
  }
}

export function promotionStatusLabel(status: PromotionStatus) {
  switch (status) {
    case "creating":
      return "Creating in Kargo";
    case "pending":
      return "Pending review";
    case "preparing":
      return "Preparing diffs";
    case "approved":
      return "Approved";
    case "rejected":
      return "Rejected";
    case "expired":
      return "Expired";
    case "error":
      return "Failed";
  }
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

function setPage(url: URL, page: number) {
  if (page === 1) url.searchParams.delete("page");
  else url.searchParams.set("page", String(page));
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}
