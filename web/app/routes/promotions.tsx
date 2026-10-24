import { Alert, Anchor, Badge, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotions, type Promotion, type PromotionStatus } from "~/api/generated";
import { useKawaiiActivityRefresh } from "~/components/activity-refresh";
import { RelativeTime } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { Identity } from "~/components/identity";
import { KargoLink } from "~/components/kargo-link";
import { KawaiiActivity } from "~/components/kawaii-activity";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { promotionScopeFilters } from "~/components/promotion-scope-filters";
import { abbreviateMiddle } from "~/display";
import { promotionActivity } from "~/execution-activity";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

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
  useKawaiiActivityRefresh(
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
      <Table.ScrollContainer minWidth={960} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Promotion / Stage</Table.Th>
              <Table.Th>Application</Table.Th>
              <Table.Th>Change</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Requested</Table.Th>
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
  const stageName =
    snapshotString(promotion.stageSnapshot, "name") ?? promotion.stageId.slice(0, 8);
  const project = snapshotString(promotion.stageSnapshot, "project");
  const application = snapshotString(promotion.stageSnapshot, "application") ?? "—";

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
            {abbreviateMiddle(promotion.externalPromotionId, 26)}
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
        <Stack gap={0}>
          <Text>{application}</Text>
          <Text c="dimmed" size="xs">
            {promotion.integration}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        {promotion.candidateBranch && promotion.baseCommit && promotion.candidateCommit ? (
          <Stack gap={0}>
            <Text size="sm">
              <Text component="span" inherit title={promotion.candidateBranch}>
                {abbreviateMiddle(promotion.candidateBranch, 30)}
              </Text>{" "}
              → {promotion.targetBranch}
            </Text>
            <Text c="dimmed" size="xs">
              <CommitReference
                commit={promotion.baseCommit}
                length={10}
                repositoryId={promotion.repositoryId}
              />{" "}
              →{" "}
              <CommitReference
                commit={promotion.candidateCommit}
                length={10}
                repositoryId={promotion.repositoryId}
              />
            </Text>
          </Stack>
        ) : (
          <Stack gap={0}>
            <Text size="sm">→ {promotion.targetBranch}</Text>
            <Text c="dimmed" size="xs">
              Waiting for Kargo callback
            </Text>
          </Stack>
        )}
      </Table.Td>
      <Table.Td>
        <Group gap="xs" justify="space-between" wrap="nowrap">
          <Badge color={promotionStatusColor(promotion.status)} variant="light">
            {promotionStatusLabel(promotion.status)}
          </Badge>
          <KawaiiActivity
            activity={promotionActivity(promotion)}
            animationKey={promotion.id}
            compact
          />
        </Group>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Identity
            avatarSize={24}
            name={promotion.requestedBy.displayName}
            nameProps={{ fw: 500, size: "sm" }}
          />
          <RelativeTime c="dimmed" size="xs" value={promotion.createdAt} />
        </Stack>
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

export function snapshotString(snapshot: Record<string, unknown>, key: string) {
  const value = snapshot[key];
  return typeof value === "string" ? value : undefined;
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
