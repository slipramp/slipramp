import {
  Alert,
  Anchor,
  Badge,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  ThemeIcon,
  Title,
  Tooltip,
} from "@mantine/core";
import { IconArrowsDiff, IconCheck, IconMinus } from "@tabler/icons-react";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listPromotionPipelines,
  type PromotionFreightComparisonStatus,
  type PromotionPipeline,
} from "~/api/generated";
import { KargoLink } from "~/components/kargo-link";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { promotionScopeFilters } from "~/components/promotion-scope-filters";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";
import {
  buildFreightColumns,
  buildFreightRows,
  filterFreightRows,
  freightRowStatus,
  type FreightColumn,
  type FreightComparisonRow,
} from "~/promotion-freight";

import type { Route } from "./+types/promotion-pipelines";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Promotion pipelines") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const application = optionalValue(url.searchParams.get("application"));
  const cluster = optionalValue(url.searchParams.get("cluster"));
  const repository = optionalValue(url.searchParams.get("repository"));
  const status = optionalFreightStatus(url.searchParams.get("status"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const result = await listPromotionPipelines({
    client: createServerAPIClient(request, context),
    query: {
      application,
      cluster,
      integration,
      limit: pageSize,
      offset: (requestedPage - 1) * pageSize,
      project,
      repository,
      status,
    },
  });

  if (result.data) {
    const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
    if (requestedPage > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    application,
    available: result.data !== undefined,
    cluster,
    error: result.error?.detail ?? result.error?.title,
    integration,
    page: requestedPage,
    pipelines:
      result.data?.items.filter((pipeline) => {
        if (!status) return true;

        const columns = buildFreightColumns(pipeline);
        return filterFreightRows(buildFreightRows(columns), columns, status).length > 0;
      }) ?? [],
    project,
    repository,
    status,
    total: result.data?.total ?? 0,
  };
}

export default function PromotionPipelines({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="lg">
      <PageHeader
        description="Current Freight compared across configured Kargo environments."
        title="Promotion pipelines"
      />

      <PipelineFilters
        application={loaderData.application}
        cluster={loaderData.cluster}
        integration={loaderData.integration}
        project={loaderData.project}
        repository={loaderData.repository}
        status={loaderData.status}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Promotion pipelines unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.pipelines.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No active Kargo pipelines match these filters.</Text>
        </Paper>
      ) : (
        loaderData.pipelines.map((pipeline) => (
          <PipelineComparison
            key={pipelineKey(pipeline)}
            pipeline={pipeline}
            status={loaderData.status}
          />
        ))
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.pipelines.length}
          noun="Pipelines"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function PipelineFilters({
  application,
  cluster,
  integration,
  project,
  repository,
  status,
}: Readonly<{
  application?: string;
  cluster?: string;
  integration?: string;
  project?: string;
  repository?: string;
  status?: PromotionFreightComparisonStatus;
}>) {
  return (
    <SearchParamFilters
      clearTo="/promotion-pipelines"
      filters={[
        ...promotionScopeFilters({ application, integration, project }),
        {
          active: Boolean(cluster),
          kind: "text",
          label: "Cluster",
          name: "cluster",
          value: cluster,
        },
        {
          active: Boolean(status),
          data: [
            { label: "All statuses", value: "" },
            { label: "Equal", value: "equal" },
            { label: "Different", value: "different" },
          ],
          kind: "select",
          label: "Status",
          name: "status",
          value: status,
        },
        {
          active: Boolean(repository),
          kind: "text",
          label: "Repository",
          name: "repository",
          value: repository,
        },
      ]}
    />
  );
}

function PipelineComparison({
  pipeline,
  status,
}: Readonly<{ pipeline: PromotionPipeline; status?: PromotionFreightComparisonStatus }>) {
  const columns = buildFreightColumns(pipeline);
  const rows = filterFreightRows(buildFreightRows(columns), columns, status);
  const statuses = rows.map((row) => freightRowStatus(row, columns));
  const differentCount = statuses.filter((rowStatus) => rowStatus === "different").length;
  const equalCount = statuses.filter((rowStatus) => rowStatus === "equal").length;

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group align="flex-start" justify="space-between" wrap="wrap">
          <Stack gap={0}>
            <Title order={2} size="h3">
              {pipeline.application}
            </Title>
            <Text c="dimmed" size="sm">
              {pipeline.project} · {pipeline.repository}
            </Text>
          </Stack>
          <Group gap="xs">
            {differentCount > 0 ? (
              <Badge color="yellow" variant="light">
                {differentCount} different
              </Badge>
            ) : equalCount > 0 ? (
              <Badge color="green" variant="light">
                Freight aligned
              </Badge>
            ) : null}
            <Badge color="gray" variant="outline">
              {columns.length} environment{columns.length === 1 ? "" : "s"}
            </Badge>
          </Group>
        </Group>
        {rows.length === 0 ? (
          <Text c="dimmed">
            {status
              ? `No ${status} Freight artifacts match this filter.`
              : "No Freight artifacts are currently reported."}
          </Text>
        ) : (
          <SimpleGrid
            className="pipeline-flow"
            cols={{ base: 1, sm: 2, xl: Math.max(1, Math.min(columns.length, 4)) }}
            spacing="md"
          >
            {columns.map((column, index) => (
              <PipelineStageCard
                column={column}
                columns={columns}
                index={index}
                key={column.key}
                rows={rows}
              />
            ))}
          </SimpleGrid>
        )}
      </Stack>
    </Paper>
  );
}

function PipelineStageCard({
  column,
  columns,
  index,
  rows,
}: Readonly<{
  column: FreightColumn;
  columns: FreightColumn[];
  index: number;
  rows: FreightComparisonRow[];
}>) {
  const stage = column.stage;

  return (
    <Paper className="pipeline-stage-card" p="md" withBorder>
      <Stack gap="md">
        <Group align="flex-start" justify="space-between" wrap="nowrap">
          <Group align="flex-start" gap="xs" wrap="nowrap">
            <ThemeIcon radius="xl" size={26} variant="light">
              {index + 1}
            </ThemeIcon>
            <Group gap={6}>
              <Text fw={700}>{column.environment}</Text>
              {!column.configured ? (
                <Badge color="orange" size="xs" variant="light">
                  Unconfigured
                </Badge>
              ) : null}
            </Group>
          </Group>
          <Stack align="flex-end" gap={1}>
            {stage ? (
              stage.kargoUrl ? (
                <KargoLink size="sm" url={stage.kargoUrl}>
                  {stage.name}
                </KargoLink>
              ) : (
                <Text size="sm">{stage.name}</Text>
              )
            ) : (
              <Badge color="gray" size="sm" variant="light">
                No Stage
              </Badge>
            )}
            {stage?.pendingPromotionCount ? (
              <Anchor component={Link} size="xs" to={`/promotions?stageId=${stage.id}`}>
                {stage.pendingPromotionCount} pending
              </Anchor>
            ) : null}
          </Stack>
        </Group>

        {stage ? (
          <Stack gap="xs">
            {rows.map((row) => (
              <FreightArtifactCard column={column} columns={columns} key={row.key} row={row} />
            ))}
          </Stack>
        ) : (
          <Text c="dimmed" size="sm">
            Freight has not reached this configured environment.
          </Text>
        )}
      </Stack>
    </Paper>
  );
}

function FreightArtifactCard({
  column,
  columns,
  row,
}: Readonly<{
  column: FreightColumn;
  columns: FreightColumn[];
  row: FreightComparisonRow;
}>) {
  const status = freightRowStatus(row, columns);
  const versions = row.values[column.key] ?? [];

  return (
    <Stack className="pipeline-freight-card" gap={5} p="xs">
      <Group justify="space-between" wrap="nowrap">
        <Group gap={5} wrap="nowrap">
          <Badge color="gray" size="xs" variant="outline">
            {row.kind}
          </Badge>
          <Text fw={600} lineClamp={1} size="xs" title={row.origin}>
            {row.origin.replace(/^Warehouse\//, "")}
          </Text>
        </Group>
        <Tooltip
          label={
            status === "equal"
              ? "Equal in every environment"
              : status === "different"
                ? "Differs across environments"
                : "Needs Stages in at least two environments"
          }
        >
          <ThemeIcon
            color={status === "equal" ? "green" : status === "different" ? "yellow" : "gray"}
            radius="xl"
            size={22}
            variant="light"
          >
            {status === "equal" ? (
              <IconCheck aria-hidden size={13} />
            ) : status === "different" ? (
              <IconArrowsDiff aria-hidden size={13} />
            ) : (
              <IconMinus aria-hidden size={13} />
            )}
          </ThemeIcon>
        </Tooltip>
      </Group>
      {row.repository ? (
        <Text c="dimmed" lineClamp={1} size="xs" title={row.repository}>
          {row.repository}
        </Text>
      ) : null}
      {versions.length === 0 ? (
        <Text c="dimmed" size="sm">
          Missing
        </Text>
      ) : (
        <Group gap={4}>
          {versions.map((version) => (
            <Code key={version}>{version}</Code>
          ))}
        </Group>
      )}
    </Stack>
  );
}

function pipelineKey(pipeline: PromotionPipeline) {
  return [
    pipeline.integration,
    pipeline.cluster,
    pipeline.repository,
    pipeline.project,
    pipeline.application,
  ].join("/");
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}

function optionalFreightStatus(value: string | null): PromotionFreightComparisonStatus | undefined {
  return value === "equal" || value === "different" ? value : undefined;
}
