import {
  Alert,
  Anchor,
  Badge,
  Code,
  Group,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  ThemeIcon,
  Title,
  Tooltip,
} from "@mantine/core";
import { IconArrowsDiff, IconCheck } from "@tabler/icons-react";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotionPipelines, type PromotionPipeline } from "~/api/generated";
import { KargoLink } from "~/components/kargo-link";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { PromotionScopeFilters } from "~/components/promotion-scope-filters";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";
import {
  buildFreightColumns,
  buildFreightRows,
  filterFreightRows,
  freightRowMatches,
  type FreightColumn,
  type FreightComparisonRow,
  type FreightComparisonStatus,
} from "~/promotion-freight";

import type { Route } from "./+types/promotion-pipelines";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Promotion pipelines") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const application = optionalValue(url.searchParams.get("application"));
  const repository = optionalValue(url.searchParams.get("repository"));
  const status = optionalFreightStatus(url.searchParams.get("status"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const result = await listPromotionPipelines({
    client: createServerAPIClient(request),
    query: {
      application,
      integration,
      limit: pageSize,
      offset: (requestedPage - 1) * pageSize,
      project,
      repository,
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
    error: result.error?.detail ?? result.error?.title,
    integration,
    page: requestedPage,
    pipelines: result.data?.items ?? [],
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
  integration,
  project,
  repository,
  status,
}: Readonly<{
  application?: string;
  integration?: string;
  project?: string;
  repository?: string;
  status?: FreightComparisonStatus;
}>) {
  const active = Boolean(application || integration || project || repository || status);

  return (
    <SearchParamFilters active={active} clearTo="/promotion-pipelines">
      <PromotionScopeFilters
        application={application}
        integration={integration}
        project={project}
      />
      <NativeSelect
        defaultValue={status ?? ""}
        label="Status"
        name="status"
        data={[
          { label: "All statuses", value: "" },
          { label: "Equal", value: "equal" },
          { label: "Different", value: "different" },
        ]}
        style={{ flex: "1 1 160px" }}
      />
      <TextInput
        defaultValue={repository}
        label="Repository"
        name="repository"
        style={{ flex: "1 1 160px" }}
        type="search"
      />
    </SearchParamFilters>
  );
}

function PipelineComparison({
  pipeline,
  status,
}: Readonly<{ pipeline: PromotionPipeline; status?: FreightComparisonStatus }>) {
  const columns = buildFreightColumns(pipeline);
  const rows = filterFreightRows(buildFreightRows(columns), columns, status);
  const differentCount = rows.filter((row) => !freightRowMatches(row, columns)).length;

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group align="flex-start" justify="space-between" wrap="wrap">
          <Stack gap={0}>
            <Title order={2} size="h3">
              {pipeline.application}
            </Title>
            <Text c="dimmed" size="sm">
              {pipeline.integration} · {pipeline.cluster} · {pipeline.project} ·{" "}
              {pipeline.repository}
            </Text>
          </Stack>
          <Group gap="xs">
            {differentCount > 0 ? (
              <Badge color="yellow" variant="light">
                {differentCount} different
              </Badge>
            ) : rows.length > 0 ? (
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
            <ThemeIcon radius="xl" size={30} variant="light">
              {index + 1}
            </ThemeIcon>
            <Stack gap={1}>
              <Group gap={6}>
                <Text fw={700}>{column.environment}</Text>
                {!column.configured ? (
                  <Badge color="orange" size="xs" variant="light">
                    Unconfigured
                  </Badge>
                ) : null}
              </Group>
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
            </Stack>
          </Group>
          {stage?.pendingPromotionCount ? (
            <Anchor component={Link} size="xs" to={`/promotions?stageId=${stage.id}`}>
              {stage.pendingPromotionCount} pending
            </Anchor>
          ) : null}
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
  const matches = freightRowMatches(row, columns);
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
        <Tooltip label={matches ? "Equal in every environment" : "Differs across environments"}>
          <ThemeIcon color={matches ? "green" : "yellow"} radius="xl" size={22} variant="light">
            {matches ? (
              <IconCheck aria-hidden size={13} />
            ) : (
              <IconArrowsDiff aria-hidden size={13} />
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
  return [pipeline.integration, pipeline.repository, pipeline.project, pipeline.application].join(
    "/",
  );
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}

function optionalFreightStatus(value: string | null): FreightComparisonStatus | undefined {
  return value === "equal" || value === "different" ? value : undefined;
}
