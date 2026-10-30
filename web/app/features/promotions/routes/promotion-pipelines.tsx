import {
  Alert,
  Anchor,
  Badge,
  Box,
  Code,
  Group,
  Paper,
  Stack,
  Text,
  ThemeIcon,
  Title,
  Tooltip,
} from "@mantine/core";
import {
  IconArrowRight,
  IconArrowsDiff,
  IconCheck,
  IconExternalLink,
  IconMinus,
} from "@tabler/icons-react";
import { Fragment, useState } from "react";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getCurrentPrincipal,
  listPromotionPipelines,
  type PromotionPipelineAttention,
  type PromotionPipeline,
  type PromotionPipelineStage,
} from "~/api/generated";
import { KargoLink } from "~/features/promotions/kargo-link";
import { ListPagination, SearchParamFilters } from "~/shared/list-controls";
import { formatTimestamp } from "~/shared/display";
import { PageHeader } from "~/shared/page";
import {
  PromotionCreateButton,
  PromotionCreateDialog,
} from "~/features/promotions/promotion-create-dialog";
import { promotionScopeFilters } from "~/features/promotions/promotion-scope-filters";
import { pageTitle } from "~/shared/page-title";
import { pageFromSearchParam } from "~/shared/search-params";
import { useTimeZone } from "~/shared/time-zone-preference";
import {
  canWritePromotions,
  pipelinePromotionChoices,
  type PromotionChoice,
} from "~/features/promotions/promotion-creation";
import { submitPromotionCreation } from "~/features/promotions/promotion-creation.server";
import {
  buildFreightColumns,
  buildFreightRows,
  freightRowStatus,
  type FreightColumn,
  type FreightComparisonRow,
} from "~/features/promotions/promotion-freight";

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
  const attention = optionalAttention(url.searchParams.get("attention"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const [result, principalResult] = await Promise.all([
    listPromotionPipelines({
      client,
      query: {
        application,
        cluster,
        integration,
        limit: pageSize,
        offset: (requestedPage - 1) * pageSize,
        project,
        repository,
        attention,
      },
    }),
    getCurrentPrincipal({ client }),
  ]);

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
    pipelines: result.data?.items ?? [],
    project,
    principal: principalResult.data,
    repository,
    attention,
    total: result.data?.total ?? 0,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  return submitPromotionCreation(request, context);
}

export default function PromotionPipelines({ loaderData }: Route.ComponentProps) {
  const canWrite = canWritePromotions(loaderData.principal);

  return (
    <Stack gap="lg">
      <PageHeader
        description="Current Freight compared across configured Kargo environments."
        title="Promotion pipelines"
      />

      <PipelineFilters
        application={loaderData.application}
        attention={loaderData.attention}
        cluster={loaderData.cluster}
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
          <Text c="dimmed">
            {loaderData.attention
              ? "No pipelines match this attention filter."
              : "No active Kargo pipelines match these filters."}
          </Text>
        </Paper>
      ) : (
        loaderData.pipelines.map((pipeline) => (
          <PipelineComparison canWrite={canWrite} key={pipelineKey(pipeline)} pipeline={pipeline} />
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
  attention,
  cluster,
  integration,
  project,
  repository,
}: Readonly<{
  application?: string;
  attention?: PromotionPipelineAttention;
  cluster?: string;
  integration?: string;
  project?: string;
  repository?: string;
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
          active: Boolean(attention),
          data: [
            { label: "All pipelines", value: "" },
            { label: "Needs attention", value: "any" },
            { label: "Drifted", value: "drifted" },
            { label: "Outdated", value: "outdated" },
          ],
          kind: "select",
          label: "Attention",
          name: "attention",
          value: attention,
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
  canWrite,
  pipeline,
}: Readonly<{
  canWrite: boolean;
  pipeline: PromotionPipeline;
}>) {
  const columns = buildFreightColumns(pipeline);
  const rows = buildFreightRows(columns);
  const statuses = rows.map((row) => freightRowStatus(row, columns));
  const differentCount = statuses.filter((rowStatus) => rowStatus === "different").length;
  const equalCount = statuses.filter((rowStatus) => rowStatus === "equal").length;
  const outdatedCount = pipeline.stages.filter(
    (stage) => !stage.autoPromotionEnabled && stage.availableFreight.items.length > 0,
  ).length;

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
              <Badge color="yellow" title={`${differentCount} Freight differences`} variant="light">
                Drifted
              </Badge>
            ) : null}
            {outdatedCount > 0 ? (
              <Badge color="orange" variant="light">
                {outdatedCount === 1 ? "1 outdated Stage" : `${outdatedCount} outdated Stages`}
              </Badge>
            ) : null}
            {differentCount === 0 && outdatedCount === 0 && equalCount > 0 ? (
              <Badge color="green" variant="light">
                Freight aligned
              </Badge>
            ) : null}
          </Group>
        </Group>
        {rows.length === 0 ? (
          <Text c="dimmed">No Freight artifacts are currently reported.</Text>
        ) : null}
        <Box className="pipeline-flow">
          {columns.map((column, index) => (
            <Fragment key={column.key}>
              {index > 0 ? (
                <Box aria-hidden className="pipeline-stage-arrow">
                  <IconArrowRight size={38} stroke={1.8} />
                </Box>
              ) : null}
              <PipelineStageCard
                canWrite={canWrite}
                column={column}
                columns={columns}
                index={index}
                rows={rows}
              />
            </Fragment>
          ))}
        </Box>
      </Stack>
    </Paper>
  );
}

function PipelineStageCard({
  canWrite,
  column,
  columns,
  index,
  rows,
}: Readonly<{
  canWrite: boolean;
  column: FreightColumn;
  columns: FreightColumn[];
  index: number;
  rows: FreightComparisonRow[];
}>) {
  const stage = column.stage;
  const choices = stage ? pipelinePromotionChoices(stage) : [];
  const availableFreight = stage?.availableFreight.items ?? [];
  const [dialogChoices, setDialogChoices] = useState<PromotionChoice[] | undefined>();

  return (
    <>
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
            <Stack align="flex-end" gap={4}>
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
              {stage ? <StageHealthBadge stage={stage} /> : null}
              {stage?.pendingPromotionCount ? (
                <Anchor component={Link} size="xs" to={`/promotions?stageId=${stage.id}`}>
                  {stage.pendingPromotionCount} pending
                </Anchor>
              ) : null}
              {stage && !stage.autoPromotionEnabled && availableFreight.length > 0 ? (
                <Badge
                  color="yellow"
                  size="sm"
                  title={availableFreight.map((item) => item.name).join(", ")}
                  variant="light"
                >
                  {availableFreight.length === 1
                    ? "New Freight available"
                    : `${availableFreight.length} Freight available`}
                </Badge>
              ) : null}
              {canWrite && stage ? (
                <PromotionCreateButton
                  choices={choices}
                  onClick={() => setDialogChoices(choices)}
                />
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
      {dialogChoices ? (
        <PromotionCreateDialog
          choices={dialogChoices}
          onClose={() => setDialogChoices(undefined)}
        />
      ) : null}
    </>
  );
}

function StageHealthBadge({ stage }: Readonly<{ stage: PromotionPipelineStage }>) {
  const { timeZone } = useTimeZone();
  const presentation = stageHealthPresentation(stage);
  const tooltip = stage.observationStale
    ? `Last observed ${formatTimestamp(stage.lastObservedAt, timeZone)}. The reported health may be outdated.`
    : stage.healthStatus
      ? `Kargo reported ${stage.healthStatus}. Last observed ${formatTimestamp(stage.lastObservedAt, timeZone)}.`
      : `Kargo has not reported Stage health. Last observed ${formatTimestamp(stage.lastObservedAt, timeZone)}.`;
  const badge = (
    <Badge color={presentation.color} size="sm" variant="light">
      <Group component="span" gap={4} wrap="nowrap">
        {presentation.label}
        {stage.argoCdUrl ? <IconExternalLink aria-hidden size={11} /> : null}
      </Group>
    </Badge>
  );

  return (
    <Tooltip label={tooltip}>
      {stage.argoCdUrl ? (
        <Anchor href={stage.argoCdUrl} rel="noreferrer" target="_blank" underline="never">
          {badge}
        </Anchor>
      ) : (
        badge
      )}
    </Tooltip>
  );
}

function stageHealthPresentation(stage: PromotionPipelineStage) {
  if (stage.observationStale) {
    return { color: "gray", label: "Observation stale" };
  }

  switch (stage.healthStatus?.toLowerCase()) {
    case "healthy":
      return { color: "green", label: stage.healthStatus };
    case "progressing":
      return { color: "yellow", label: stage.healthStatus };
    case "degraded":
    case "unhealthy":
      return { color: "red", label: stage.healthStatus };
    case "suspended":
      return { color: "orange", label: stage.healthStatus };
    default:
      return { color: "gray", label: stage.healthStatus ?? "Health unavailable" };
  }
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

function optionalAttention(value: string | null): PromotionPipelineAttention | undefined {
  return value === "any" || value === "drifted" || value === "outdated" ? value : undefined;
}
