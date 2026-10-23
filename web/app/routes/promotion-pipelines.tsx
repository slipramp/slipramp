import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotionPipelines, type PromotionPipeline } from "~/api/generated";
import { pageTitle } from "~/page-title";
import {
  buildFreightColumns,
  buildFreightRows,
  freightNames,
  freightRowMatches,
  type FreightColumn,
  type FreightComparisonRow,
} from "~/promotion-freight";

import type { Route } from "./+types/promotion-pipelines";

const pipelineLimit = 50;

export function meta() {
  return [{ title: pageTitle("Promotion Pipelines") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const application = optionalValue(url.searchParams.get("application"));
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const repository = optionalValue(url.searchParams.get("repository"));
  const result = await listPromotionPipelines({
    client: createServerAPIClient(request),
    query: {
      application,
      integration,
      limit: pipelineLimit,
      project,
      repository,
    },
  });

  return {
    application,
    available: result.data !== undefined,
    error: result.error?.detail ?? result.error?.title,
    integration,
    pipelines: result.data?.items ?? [],
    project,
    repository,
    total: result.data?.total ?? 0,
  };
}

export default function PromotionPipelines({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Promotion Pipelines</Title>
          <Text c="dimmed">Current Freight compared across configured Kargo environments.</Text>
        </Stack>
        <Group>
          <Anchor component={Link} to="/promotion-branch-drift">
            Branch drift
          </Anchor>
          <Anchor component={Link} to="/promotion-stages">
            Stage inventory
          </Anchor>
          <Anchor component={Link} to="/promotions">
            Promotions
          </Anchor>
        </Group>
      </Group>

      <PipelineFilters
        application={loaderData.application}
        integration={loaderData.integration}
        project={loaderData.project}
        repository={loaderData.repository}
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
          <PipelineComparison key={pipelineKey(pipeline)} pipeline={pipeline} />
        ))
      )}

      {loaderData.available && loaderData.total > loaderData.pipelines.length ? (
        <Text c="dimmed" size="sm">
          Showing the first {loaderData.pipelines.length} of {loaderData.total} matching pipelines.
        </Text>
      ) : null}
    </Stack>
  );
}

function PipelineFilters({
  application,
  integration,
  project,
  repository,
}: Readonly<{
  application?: string;
  integration?: string;
  project?: string;
  repository?: string;
}>) {
  return (
    <Paper p="md" withBorder>
      <Form method="get">
        <Group align="flex-end">
          <TextInput defaultValue={integration} label="Integration" name="integration" />
          <TextInput defaultValue={repository} label="Repository" name="repository" />
          <TextInput defaultValue={project} label="Project" name="project" />
          <TextInput defaultValue={application} label="Application" name="application" />
          <Button type="submit" variant="light">
            Apply filters
          </Button>
        </Group>
      </Form>
    </Paper>
  );
}

function PipelineComparison({ pipeline }: Readonly<{ pipeline: PromotionPipeline }>) {
  const columns = buildFreightColumns(pipeline);
  const rows = buildFreightRows(columns);

  return (
    <Paper withBorder>
      <Stack gap={0}>
        <Group justify="space-between" p="md">
          <Stack gap={0}>
            <Title order={2} size="h3">
              {pipeline.application}
            </Title>
            <Text c="dimmed" size="sm">
              {pipeline.integration} · {pipeline.cluster} · {pipeline.project} ·{" "}
              {pipeline.repository}
            </Text>
          </Stack>
          <Text c="dimmed" size="sm">
            {columns.length} environment {columns.length === 1 ? "column" : "columns"}
          </Text>
        </Group>
        <Table.ScrollContainer minWidth={Math.max(900, 300 + columns.length * 260)}>
          <Table horizontalSpacing="md" verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th miw={280}>Freight artifact</Table.Th>
                {columns.map((column) => (
                  <Table.Th key={column.key} miw={240}>
                    <PipelineColumnHeader column={column} pipeline={pipeline} />
                  </Table.Th>
                ))}
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={columns.length + 1}>
                    <Text c="dimmed">No Freight artifacts are currently reported.</Text>
                  </Table.Td>
                </Table.Tr>
              ) : (
                rows.map((row) => <FreightRow columns={columns} key={row.key} row={row} />)
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Stack>
    </Paper>
  );
}

function PipelineColumnHeader({
  column,
  pipeline,
}: Readonly<{ column: FreightColumn; pipeline: PromotionPipeline }>) {
  if (!column.stage) {
    return (
      <Stack gap={2}>
        <Text fw={600}>{column.environment}</Text>
        <Badge color="gray" size="sm" variant="light">
          No Stage
        </Badge>
      </Stack>
    );
  }

  const names = freightNames(column.stage);

  return (
    <Stack gap={2}>
      <Group gap="xs">
        <Text fw={600}>{column.environment}</Text>
        {!column.configured ? (
          <Badge color="orange" size="sm" variant="light">
            Unconfigured
          </Badge>
        ) : null}
      </Group>
      <Text size="sm">{column.stage.name}</Text>
      {names.length > 0 ? (
        <Text c="dimmed" size="xs">
          {names.join(", ")}
        </Text>
      ) : null}
      <Text c="dimmed" component="time" dateTime={column.stage.lastObservedAt} size="xs">
        Observed {formatTimestamp(column.stage.lastObservedAt)}
      </Text>
      {column.stage.pendingPromotionCount > 0 ? (
        <Anchor component={Link} size="xs" to={`/promotions?stageId=${column.stage.id}`}>
          {column.stage.pendingPromotionCount} pending
        </Anchor>
      ) : (
        <Anchor
          component={Link}
          size="xs"
          to={`/promotion-stages?integration=${encodeURIComponent(pipeline.integration)}&project=${encodeURIComponent(pipeline.project)}`}
        >
          View Stage inventory
        </Anchor>
      )}
    </Stack>
  );
}

function FreightRow({
  columns,
  row,
}: Readonly<{ columns: FreightColumn[]; row: FreightComparisonRow }>) {
  const matches = freightRowMatches(row, columns);

  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={2}>
          <Group gap="xs">
            <Badge color={matches ? "green" : "yellow"} size="sm" variant="light">
              {matches ? "Equal" : "Different"}
            </Badge>
            <Badge color="gray" size="sm" variant="outline">
              {row.kind}
            </Badge>
          </Group>
          <Text fw={600} size="sm">
            {row.origin}
          </Text>
          {row.repository ? (
            <Text c="dimmed" size="xs">
              {row.repository}
            </Text>
          ) : null}
        </Stack>
      </Table.Td>
      {columns.map((column) => {
        const versions = row.values[column.key] ?? [];

        return (
          <Table.Td key={column.key}>
            {versions.length === 0 ? (
              <Text c="dimmed" size="sm">
                Missing
              </Text>
            ) : (
              <Stack gap={4}>
                {versions.map((version) => (
                  <Code key={version}>{version}</Code>
                ))}
              </Stack>
            )}
          </Table.Td>
        );
      })}
    </Table.Tr>
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

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}
