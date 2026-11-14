import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Grid,
  Group,
  Modal,
  NativeSelect,
  Paper,
  Stack,
  Table,
  Tabs,
  Text,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { useDebouncedCallback } from "@mantine/hooks";
import { IconListDetails, IconRoute, IconSearch } from "@tabler/icons-react";
import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import {
  type PromotionFreightAlignmentStatus,
  type PromotionFreightCandidate,
  type PromotionFreightItem,
  type PromotionPipeline,
  type PromotionProjectDetail,
  type PromotionProjectStage,
} from "~/api/generated";
import {
  PromotionCreateButton,
  PromotionCreateMenu,
} from "~/features/promotions/promotion-create-dialog";
import { eligibleStageFreight } from "~/features/promotions/promotion-creation";
import { FreightContents, FreightIdentifier } from "~/features/promotions/freight-display";
import {
  buildStageFreightDetailRows,
  buildPipelineFreightSourceRows,
  freightArtifactReference,
  freightAlignmentPresentation,
  freightDisplayLabel,
  freightNames,
  freightOriginLabel,
  freightSourceKindLabel,
  stageHasNewerFreight,
  type StageFreightDetailRow,
} from "~/features/promotions/promotion-freight";
import { promotionStagePolicyPresentation } from "~/features/promotions/promotion-policy";
import { StagePolicyBadge } from "~/features/promotions/promotion-policy-badge";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { PromotionProjectLink } from "~/features/promotions/promotion-project-link";
import { promotionStageFilterSearch } from "~/features/promotions/promotion-project-inventory-state";
import {
  promotionProjectAttention,
  type PromotionProjectAttention,
} from "~/features/promotions/promotion-project-filters";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import {
  topologyPipelines,
  type PromotionTopologyPipeline,
} from "~/features/promotions/promotion-topology";
import { KargoLink } from "~/features/promotions/kargo-link";
import { KargoObservationTime } from "~/features/promotions/kargo-observation-time";
import { RelativeTime } from "~/shared/display-values";
import { ListPagination } from "~/shared/list-controls";
import { EmptyState, PageHeader, PageHeaderFact, PageHeaderFacts } from "~/shared/page";

export function ProjectDetail({
  actionData,
  canWrite,
  project,
}: Readonly<{
  actionData: unknown;
  canWrite: boolean;
  project: PromotionProjectDetail;
}>) {
  useActivityRefresh(true, 30_000);

  const [searchParams, setSearchParams] = useSearchParams();
  const activeView = projectView(searchParams.get("view"));
  const attention = promotionProjectAttention(searchParams.get("attention"));
  const alignment = projectAlignment(searchParams.get("alignment"));
  const query = searchParams.get("q") ?? "";
  const policyStage = project.stages.find(
    (stage) =>
      stage.id === searchParams.get("policy") && promotionStagePolicyPresentation(stage).editable,
  );
  const pipelines = projectPipelines(project.stages);
  const visiblePipelines = pipelines.filter(
    (pipeline) =>
      pipeline.stages.some((stage) => stageMatchesQuery(stage, query)) &&
      (!attention || pipeline.stages.some((stage) => stageMatchesAttention(stage, attention))) &&
      (!alignment || pipeline.stages.some((stage) => stageMatchesAlignment(stage, alignment))),
  );
  const visibleStages = project.stages.filter(
    (stage) =>
      stageMatchesQuery(stage, query) &&
      stageMatchesAttention(stage, attention) &&
      stageMatchesAlignment(stage, alignment),
  );
  const validPipelineKeys = new Set(pipelines.map((pipeline) => pipeline.key));
  const explicitPipelineSelection =
    searchParams.has("pipeline") || searchParams.get("pipelines") === "collapsed";
  const expandedPipelineKeys = explicitPipelineSelection
    ? [...new Set(searchParams.getAll("pipeline"))].filter((key) => validPipelineKeys.has(key))
    : visiblePipelines.length <= 4
      ? visiblePipelines.map((pipeline) => pipeline.key)
      : [];

  function closePolicy() {
    const next = new URLSearchParams(searchParams);
    next.delete("policy");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function editPolicy(stage: PromotionProjectStage) {
    const next = new URLSearchParams(searchParams);
    next.set("policy", stage.id);
    setSearchParams(next, { preventScrollReset: true });
  }

  function changeView(view: ProjectView) {
    const next = new URLSearchParams(searchParams);
    if (view === "pipelines") next.delete("view");
    else next.set("view", view);
    setSearchParams(next, { preventScrollReset: true });
  }

  function changeAttention(nextAttention?: PromotionProjectAttention) {
    const next = new URLSearchParams(searchParams);
    if (nextAttention) next.set("attention", nextAttention);
    else next.delete("attention");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeAlignment(nextAlignment?: PromotionFreightAlignmentStatus) {
    const next = new URLSearchParams(searchParams);
    if (nextAlignment) next.set("alignment", nextAlignment);
    else next.delete("alignment");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeQuery(nextQuery: string) {
    const next = new URLSearchParams(searchParams);
    if (nextQuery) next.set("q", nextQuery);
    else next.delete("q");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeExpandedPipelines(keys: string[]) {
    const next = new URLSearchParams(searchParams);
    next.delete("pipeline");
    next.delete("pipelines");
    if (keys.length === 0) next.set("pipelines", "collapsed");
    else for (const key of keys) next.append("pipeline", key);
    setSearchParams(next, { preventScrollReset: true });
  }

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          project.kargoUrl ? <KargoLink url={project.kargoUrl}>Open in Kargo</KargoLink> : null
        }
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Integration">
              <Anchor
                component={Link}
                to={`/promotion-projects?integration=${encodeURIComponent(project.integration)}`}
              >
                {project.integration}
              </Anchor>
            </PageHeaderFact>
            <PageHeaderFact label="Cluster">
              <Anchor component={Link} to={`/clusters/${encodeURIComponent(project.cluster)}`}>
                {project.cluster}
              </Anchor>
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={project.project}
      />

      <ProjectSummary pipelineCount={pipelines.length} stages={project.stages} />

      <Tabs
        keepMounted={false}
        onChange={(value) => {
          if (value) changeView(value as ProjectView);
        }}
        value={activeView}
        variant="outline"
      >
        <Tabs.List
          aria-label="Kargo Project views"
          style={{ flexWrap: "nowrap", overflowX: "auto" }}
        >
          <Tabs.Tab
            leftSection={<IconRoute aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="pipelines"
          >
            Pipelines
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconListDetails aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="stages"
          >
            Stages
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel pt="lg" value="pipelines">
          <Stack gap="md">
            <ProjectFilter
              alignment={alignment}
              attention={attention}
              count={visiblePipelines.length}
              noun="pipeline"
              onAlignmentChange={changeAlignment}
              onAttentionChange={changeAttention}
              onChange={changeQuery}
              query={query}
            />
            <PipelineList
              canWrite={canWrite}
              expandedKeys={expandedPipelineKeys}
              onExpandedKeysChange={changeExpandedPipelines}
              onEditPolicy={editPolicy}
              pipelines={visiblePipelines}
              query={query}
            />
          </Stack>
        </Tabs.Panel>

        <Tabs.Panel pt="lg" value="stages">
          <Stack gap="md">
            <ProjectFilter
              alignment={alignment}
              attention={attention}
              count={visibleStages.length}
              noun="Stage"
              onAlignmentChange={changeAlignment}
              onAttentionChange={changeAttention}
              onChange={changeQuery}
              query={query}
            />
            <StageInventory
              canWrite={canWrite}
              onEditPolicy={editPolicy}
              query={query}
              stages={visibleStages}
            />
          </Stack>
        </Tabs.Panel>
      </Tabs>

      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
}

type ProjectView = "pipelines" | "stages";

function projectView(value: string | null): ProjectView {
  return value === "stages" ? value : "pipelines";
}

export function projectAlignment(
  value: string | null,
): PromotionFreightAlignmentStatus | undefined {
  return value === "aligned" || value === "different" || value === "unknown" ? value : undefined;
}

export type ProjectPipeline = PromotionTopologyPipeline & {
  key: string;
  label: string;
  projectContext?: Pick<PromotionPipeline, "cluster" | "integration" | "project">;
};

export function projectPipelines(stages: PromotionProjectStage[]) {
  return topologyPipelines(stages)
    .map((pipeline, index) => ({
      ...pipeline,
      key: pipeline.stages.map((stage) => stage.id).join(":"),
      label: pipelineLabel(pipeline, index),
    }))
    .toSorted((left, right) => left.label.localeCompare(right.label, undefined, { numeric: true }));
}

function ProjectSummary({
  pipelineCount,
  stages,
}: Readonly<{ pipelineCount: number; stages: PromotionProjectStage[] }>) {
  const stale = stages.filter((stage) => stage.observationStale).length;
  const unhealthy = stages.filter(stageIsUnhealthy).length;
  const pending = stages.reduce((count, stage) => count + stage.pendingPromotionCount, 0);
  const differences = stages.reduce(
    (count, stage) =>
      count +
      stage.freightAlignments.filter((alignment) => alignment.status === "different").length,
    0,
  );

  return (
    <Paper p="md" withBorder>
      <Group align="flex-start" justify="space-between" wrap="wrap">
        <Stack gap={3}>
          <Text fw={650}>
            {pipelineCount} pipeline{pipelineCount === 1 ? "" : "s"} across {stages.length} Stage
            {stages.length === 1 ? "" : "s"}
          </Text>
          <Text c="dimmed" size="sm">
            Disconnected Stage graphs are separated into pipelines. The Stage inventory remains
            available for scanning the entire Project.
          </Text>
        </Stack>
        <Group gap="xs" wrap="wrap">
          {unhealthy > 0 ? <Badge color="red">{unhealthy} non-ready</Badge> : null}
          {stale > 0 ? <Badge color="gray">{stale} stale</Badge> : null}
          {pending > 0 ? <Badge color="yellow">{pending} pending</Badge> : null}
          {differences > 0 ? <Badge color="yellow">{differences} Freight Diff</Badge> : null}
          {unhealthy === 0 && stale === 0 && pending === 0 && differences === 0 ? (
            <Badge color="green" variant="light">
              No reported issues
            </Badge>
          ) : null}
        </Group>
      </Group>
    </Paper>
  );
}

export function ProjectFilter({
  alignment,
  attention,
  count,
  noun,
  onAlignmentChange,
  onAttentionChange,
  onChange,
  query,
}: Readonly<{
  alignment?: PromotionFreightAlignmentStatus;
  attention?: PromotionProjectAttention;
  count: number;
  noun: string;
  onAlignmentChange: (value?: PromotionFreightAlignmentStatus) => void;
  onAttentionChange: (value?: PromotionProjectAttention) => void;
  onChange: (value: string) => void;
  query: string;
}>) {
  const [queryDraft, setQueryDraft] = useState({ source: query, value: query });
  const updateQuery = useDebouncedCallback(onChange, 250);
  const draftQuery = queryDraft.source === query ? queryDraft.value : query;

  return (
    <Group align="flex-end" justify="space-between" wrap="wrap">
      <TextInput
        aria-label={`Filter ${noun}s`}
        leftSection={<IconSearch aria-hidden size={15} />}
        onBlur={updateQuery.flush}
        onChange={(event) => {
          const value = event.currentTarget.value;
          setQueryDraft({ source: query, value });
          updateQuery(value);
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter") updateQuery.flush();
        }}
        placeholder="Filter by application, Stage, environment, or Warehouse"
        style={{ flex: "1 1 24rem" }}
        value={draftQuery}
      />
      <NativeSelect
        aria-label="Filter by attention state"
        data={[
          { label: "All Stages", value: "" },
          { label: "Any attention", value: "all" },
          { label: "Non-ready", value: "unhealthy" },
          { label: "Stale", value: "stale" },
          { label: "Pending review", value: "pending-review" },
          { label: "Newer Freight available", value: "newer-freight" },
        ]}
        onChange={(event) =>
          onAttentionChange(promotionProjectAttention(event.currentTarget.value))
        }
        value={attention ?? ""}
      />
      <NativeSelect
        aria-label="Filter by Freight alignment"
        data={[
          { label: "Any Freight alignment", value: "" },
          { label: "Different Freight", value: "different" },
          { label: "Unknown comparison", value: "unknown" },
          { label: "Aligned Freight", value: "aligned" },
        ]}
        onChange={(event) => onAlignmentChange(projectAlignment(event.currentTarget.value))}
        value={alignment ?? ""}
      />
      <Text c="dimmed" size="sm">
        {count} {noun}
        {count === 1 ? "" : "s"}
      </Text>
    </Group>
  );
}

export function PipelineList({
  canWrite,
  expandedKeys,
  onEditPolicy,
  onExpandedKeysChange,
  pipelines,
  query,
  showProject = false,
}: Readonly<{
  canWrite: boolean;
  expandedKeys: string[];
  onEditPolicy: (stage: PromotionProjectStage) => void;
  onExpandedKeysChange: (keys: string[]) => void;
  pipelines: ProjectPipeline[];
  query: string;
  showProject?: boolean;
}>) {
  if (pipelines.length === 0) {
    return (
      <EmptyState
        description={
          query.trim()
            ? `No pipelines match “${query.trim()}”.`
            : "No pipelines match the active filters."
        }
        title="No matching pipelines"
      />
    );
  }

  return (
    <Accordion
      multiple
      onChange={onExpandedKeysChange}
      styles={{ content: { padding: 0 } }}
      value={expandedKeys}
      variant="separated"
    >
      {pipelines.map((pipeline) => (
        <Accordion.Item key={pipeline.key} value={pipeline.key}>
          <Accordion.Control>
            {showProject && pipeline.projectContext ? (
              <Grid align="start" gap="xl" mr="sm">
                <Grid.Col span={{ base: 12, sm: 6, md: 5 }}>
                  <Stack gap={2}>
                    <Text fw={650}>{pipeline.label}</Text>
                    <Text c="dimmed" size="sm">
                      {pipelinePath(pipeline)}
                    </Text>
                  </Stack>
                </Grid.Col>
                <Grid.Col span={{ base: 12, sm: 6, md: 4 }} style={{ alignSelf: "center" }}>
                  <Stack gap={2}>
                    <Text fw={600} size="sm">
                      {pipeline.projectContext.project}
                    </Text>
                    <Text c="dimmed" size="xs">
                      {pipeline.projectContext.integration} · {pipeline.projectContext.cluster}
                    </Text>
                  </Stack>
                </Grid.Col>
                <Grid.Col span={{ base: 12, md: 3 }} style={{ alignSelf: "center" }}>
                  <PipelineBadges pipeline={pipeline} />
                </Grid.Col>
              </Grid>
            ) : (
              <Group justify="space-between" mr="sm" wrap="wrap">
                <Stack gap={2}>
                  <Text fw={650}>{pipeline.label}</Text>
                  <Text c="dimmed" size="sm">
                    {pipelinePath(pipeline)}
                  </Text>
                </Stack>
                <PipelineBadges pipeline={pipeline} />
              </Group>
            )}
          </Accordion.Control>
          <Accordion.Panel>
            <PipelineTopology canWrite={canWrite} onEditPolicy={onEditPolicy} pipeline={pipeline} />
          </Accordion.Panel>
        </Accordion.Item>
      ))}
    </Accordion>
  );
}

function PipelineBadges({ pipeline }: Readonly<{ pipeline: ProjectPipeline }>) {
  const stale = pipeline.stages.filter((stage) => stage.observationStale).length;
  const unhealthy = pipeline.stages.filter(stageIsUnhealthy).length;
  const pending = pipeline.stages.reduce((count, stage) => count + stage.pendingPromotionCount, 0);
  const differences = pipeline.stages.reduce(
    (count, stage) =>
      count +
      stage.freightAlignments.filter((alignment) => alignment.status === "different").length,
    0,
  );

  return (
    <Group gap={6} justify="flex-end" style={{ marginLeft: "auto" }} wrap="wrap">
      {unhealthy > 0 ? <Badge color="red">{unhealthy} non-ready</Badge> : null}
      {stale > 0 ? <Badge color="gray">{stale} stale</Badge> : null}
      {pending > 0 ? <Badge color="yellow">{pending} pending</Badge> : null}
      {differences > 0 ? <Badge color="yellow">{differences} different</Badge> : null}
    </Group>
  );
}

function PipelineTopology({
  canWrite,
  onEditPolicy,
  pipeline,
}: Readonly<{
  canWrite: boolean;
  onEditPolicy: (stage: PromotionProjectStage) => void;
  pipeline: ProjectPipeline;
}>) {
  const columns = pipeline.lanes.flatMap((lane, laneIndex) =>
    lane.map((stage) => ({
      header: (
        <PipelineStageColumnTitle
          canWrite={canWrite}
          laneIndex={laneIndex}
          onEditPolicy={() => onEditPolicy(stage)}
          stage={stage}
        />
      ),
      stage,
    })),
  );

  return (
    <FreightSourceTable
      borderTop
      columns={columns}
      emptyMessage="No Freight sources are requested by these Stages."
      minWidth={Math.max(720, 300 + columns.length * 240)}
    />
  );
}

function FreightSourceTable({
  borderTop = false,
  columns,
  emptyMessage,
  minWidth,
  warehouseDiscoveredAt,
}: Readonly<{
  borderTop?: boolean;
  columns: Array<{ header: ReactNode; stage: PromotionProjectStage }>;
  emptyMessage: string;
  minWidth: number;
  warehouseDiscoveredAt?: Record<string, string>;
}>) {
  const stages = columns.map((column) => column.stage);
  const freightRows = buildPipelineFreightSourceRows(stages);
  const warehouseGroups = Map.groupBy(freightRows, (row) => row.origin);
  const freightSourceColumnStyle =
    stages.length === 1
      ? ({ minWidth: 300 } as const)
      : ({ maxWidth: 300, minWidth: 300, width: 300 } as const);

  return (
    <Table.ScrollContainer
      minWidth={minWidth}
      style={borderTop ? { borderTop: "1px solid var(--slipramp-border)" } : undefined}
      type="native"
    >
      <Table styles={{ th: { textTransform: "none" } }} verticalSpacing="sm" withColumnBorders>
        <Table.Thead>
          <Table.Tr>
            <Table.Th style={{ ...freightSourceColumnStyle, verticalAlign: "top" }}>
              Freight source
            </Table.Th>
            {columns.map((column) => (
              <Table.Th key={column.stage.id} miw={240} style={{ verticalAlign: "top" }}>
                {column.header}
              </Table.Th>
            ))}
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {[...warehouseGroups].map(([origin, rows]) => (
            <Fragment key={origin}>
              <Table.Tr>
                <Table.Th
                  bg="var(--mantine-color-default-hover)"
                  colSpan={stages.length + 1}
                  scope="rowgroup"
                >
                  <Group justify="space-between" wrap="wrap">
                    <Group gap="xs">
                      <Text fw={700} size="sm">
                        {freightOriginLabel(origin)}
                      </Text>
                      <Text c="dimmed" size="xs">
                        Warehouse
                      </Text>
                    </Group>
                    {warehouseDiscoveredAt?.[origin] ? (
                      <Group gap={4} wrap="wrap">
                        <Text c="dimmed" size="xs">
                          Discovered
                        </Text>
                        <RelativeTime c="dimmed" size="xs" value={warehouseDiscoveredAt[origin]} />
                      </Group>
                    ) : null}
                  </Group>
                </Table.Th>
              </Table.Tr>
              {rows.map((row) => (
                <Table.Tr key={row.key}>
                  <Table.Th scope="row" style={freightSourceColumnStyle}>
                    <Stack gap={3}>
                      <Text fw={600} size="xs" style={{ overflowWrap: "anywhere" }}>
                        {row.source}
                      </Text>
                      {row.kind ? (
                        <Text c="dimmed" size="xs">
                          {freightSourceKindLabel(row.kind)}
                        </Text>
                      ) : null}
                    </Stack>
                  </Table.Th>
                  {stages.map((stage) => (
                    <Table.Td key={stage.id}>
                      <PipelineFreightValues values={row.values[stage.id] ?? []} />
                    </Table.Td>
                  ))}
                </Table.Tr>
              ))}
            </Fragment>
          ))}
          {freightRows.length === 0 ? (
            <Table.Tr>
              <Table.Td c="dimmed" colSpan={stages.length + 1}>
                {emptyMessage}
              </Table.Td>
            </Table.Tr>
          ) : null}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

function PipelineStageColumnTitle({
  canWrite,
  laneIndex,
  onEditPolicy,
  stage,
}: Readonly<{
  canWrite: boolean;
  laneIndex: number;
  onEditPolicy: () => void;
  stage: PromotionProjectStage;
}>) {
  const warnings = stageCapabilityWarnings(stage);

  return (
    <Stack gap={6}>
      <Group align="center" justify="space-between" wrap="nowrap">
        <Group gap={5} wrap="nowrap">
          <PromotionStageLink stage={stage} />
          <KargoObservationTime value={stage.lastObservedAt} />
        </Group>
        <StageHealthBadge stage={stage} />
      </Group>
      <Group gap={5} wrap="wrap">
        <Text c="dimmed" fw={600} size="xs">
          {stage.enrichment?.environment ?? (laneIndex === 0 ? "Entry" : `Downstream ${laneIndex}`)}
        </Text>
        {stage.controlFlow ? (
          <Badge color="gray" size="xs" variant="light">
            Control flow
          </Badge>
        ) : null}
        {warnings.length > 0 ? (
          <Badge color="orange" size="xs" variant="light">
            {warnings.length} warning{warnings.length === 1 ? "" : "s"}
          </Badge>
        ) : null}
      </Group>
      <Group gap={6} justify="space-between" wrap="wrap">
        <Group gap={6} wrap="wrap">
          <StagePolicyBadge canEdit={canWrite} onEdit={onEditPolicy} stage={stage} />
          {stage.pendingPromotionCount > 0 ? (
            <Anchor component={Link} size="xs" to={`/promotions?stageId=${stage.id}`}>
              {stage.pendingPromotionCount} pending
            </Anchor>
          ) : null}
        </Group>
        <Group gap={6} ml="auto" wrap="wrap">
          <StageFreightDetailsDialog compact includeWarnings canWrite={canWrite} stage={stage} />
          {canWrite ? <StagePromotionActions stage={stage} /> : null}
        </Group>
      </Group>
    </Stack>
  );
}

function PipelineFreightValues({ values }: Readonly<{ values: string[] }>) {
  if (values.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        —
      </Text>
    );
  }

  return (
    <Stack gap={4}>
      {values.map((value) => (
        <Code key={value} style={{ overflowWrap: "anywhere", width: "fit-content" }}>
          {value}
        </Code>
      ))}
    </Stack>
  );
}

export function StageInventory({
  canWrite,
  linkFilters = false,
  onEditPolicy,
  query,
  showFreight = true,
  showProject = false,
  stages,
}: Readonly<{
  canWrite: boolean;
  linkFilters?: boolean;
  onEditPolicy: (stage: PromotionProjectStage) => void;
  query: string;
  showFreight?: boolean;
  showProject?: boolean;
  stages: StageInventoryStage[];
}>) {
  const [searchParams] = useSearchParams();

  if (stages.length === 0) {
    return (
      <EmptyState
        description={
          query.trim()
            ? `No Stages match “${query.trim()}”.`
            : "No Stages match the active filters."
        }
        title="No matching Stages"
      />
    );
  }

  return (
    <Paper withBorder>
      <Table.ScrollContainer
        minWidth={(showProject ? 1220 : 1040) - (showFreight ? 0 : 240)}
        type="native"
      >
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th miw={200} style={{ whiteSpace: "nowrap" }}>
                Stage
              </Table.Th>
              {showProject ? <Table.Th miw={180}>Project</Table.Th> : null}
              <Table.Th miw={180}>Health</Table.Th>
              {showFreight ? <Table.Th>Current Freight</Table.Th> : null}
              <Table.Th>Last observed</Table.Th>
              <Table.Th miw={150}>Policy</Table.Th>
              <Table.Th>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {stages
              .toSorted((left, right) => left.name.localeCompare(right.name))
              .map((stage) => {
                const currentFreight = stage.freight.items;

                return (
                  <Table.Tr key={stage.id}>
                    <Table.Td style={{ whiteSpace: "nowrap" }}>
                      <Stack gap={3}>
                        <PromotionStageLink fw={650} stage={stage} />
                        {stage.enrichment?.application || stage.enrichment?.environment ? (
                          linkFilters ? (
                            <Group gap={4} wrap="nowrap">
                              {stage.enrichment.application ? (
                                <StageFilterLink
                                  name="application"
                                  searchParams={searchParams}
                                  value={stage.enrichment.application}
                                />
                              ) : null}
                              {stage.enrichment.application && stage.enrichment.environment ? (
                                <Text c="dimmed" size="xs">
                                  ·
                                </Text>
                              ) : null}
                              {stage.enrichment.environment ? (
                                <StageFilterLink
                                  name="environment"
                                  searchParams={searchParams}
                                  value={stage.enrichment.environment}
                                />
                              ) : null}
                            </Group>
                          ) : (
                            <Text c="dimmed" size="xs" style={{ whiteSpace: "nowrap" }}>
                              {[stage.enrichment.application, stage.enrichment.environment]
                                .filter(Boolean)
                                .join(" · ")}
                            </Text>
                          )
                        ) : (
                          <Text c="dimmed" size="xs">
                            Kargo native
                          </Text>
                        )}
                        {stage.controlFlow ? (
                          <Badge size="xs" variant="light">
                            Control flow
                          </Badge>
                        ) : null}
                      </Stack>
                    </Table.Td>
                    {showProject ? (
                      <Table.Td>
                        {stage.projectContext ? (
                          <Stack gap={2}>
                            <PromotionProjectLink project={stage.projectContext} />
                            {linkFilters ? (
                              <Group gap={4} wrap="nowrap">
                                <StageFilterLink
                                  name="integration"
                                  searchParams={searchParams}
                                  value={stage.projectContext.integration}
                                />
                                <Text c="dimmed" size="xs">
                                  ·
                                </Text>
                                <StageFilterLink
                                  name="cluster"
                                  searchParams={searchParams}
                                  value={stage.projectContext.cluster}
                                />
                              </Group>
                            ) : (
                              <Text c="dimmed" size="xs">
                                {stage.projectContext.integration} · {stage.projectContext.cluster}
                              </Text>
                            )}
                          </Stack>
                        ) : null}
                      </Table.Td>
                    ) : null}
                    <Table.Td>
                      <StageHealthBadge stage={stage} />
                    </Table.Td>
                    {showFreight ? (
                      <Table.Td>
                        {currentFreight.length > 0 ? (
                          <Stack gap={3}>
                            {currentFreight.map((freight) => (
                              <Stack gap={0} key={`${freight.origin}:${freight.name}`}>
                                {freight.artifacts.length > 0 ? (
                                  <>
                                    {freight.artifacts.map((artifact) => (
                                      <Text
                                        fw={600}
                                        key={`${artifact.kind}:${artifact.repository ?? ""}:${artifact.version}`}
                                        size="xs"
                                        style={{ overflowWrap: "anywhere" }}
                                      >
                                        {freightArtifactReference(artifact)}
                                      </Text>
                                    ))}
                                    <Text c="dimmed" size="xs">
                                      {freightOriginLabel(freight.origin)}
                                    </Text>
                                  </>
                                ) : (
                                  <Text fw={600} size="xs" style={{ overflowWrap: "anywhere" }}>
                                    {freightOriginLabel(freight.origin)}
                                  </Text>
                                )}
                              </Stack>
                            ))}
                          </Stack>
                        ) : (
                          <Text c="dimmed" size="xs">
                            None observed
                          </Text>
                        )}
                      </Table.Td>
                    ) : null}
                    <Table.Td>
                      <KargoObservationTime value={stage.lastObservedAt} />
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={4}>
                        <StagePolicyBadge
                          canEdit={canWrite}
                          onEdit={() => onEditPolicy(stage)}
                          stage={stage}
                        />
                        {stage.pendingPromotionCount > 0 ? (
                          <Anchor component={Link} size="xs" to={`/promotions?stageId=${stage.id}`}>
                            {stage.pendingPromotionCount} pending
                          </Anchor>
                        ) : null}
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" wrap="nowrap">
                        {canWrite ? <StagePromotionActions stage={stage} /> : null}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                );
              })}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

type StageInventoryStage = PromotionProjectStage & {
  projectContext?: Pick<PromotionProjectDetail, "cluster" | "integration" | "project">;
};

function StageFilterLink({
  name,
  searchParams,
  value,
}: Readonly<{
  name: "application" | "cluster" | "environment" | "integration";
  searchParams: URLSearchParams;
  value: string;
}>) {
  return (
    <Anchor
      c="dimmed"
      component={Link}
      size="xs"
      to={`?${promotionStageFilterSearch(searchParams, name, value)}`}
    >
      {value}
    </Anchor>
  );
}

function pipelineLabel(pipeline: PromotionTopologyPipeline, index: number) {
  const applications = pipelineApplications(pipeline);
  if (applications.length === 1) return applications[0] ?? `Pipeline ${index + 1}`;
  if (applications.length > 1) return `${applications[0]} + ${applications.length - 1} more`;
  if (pipeline.stages.length === 1) return pipeline.stages[0]?.name ?? `Pipeline ${index + 1}`;
  return `Pipeline ${index + 1}`;
}

function pipelinePath(pipeline: PromotionTopologyPipeline) {
  const useEnvironment = pipelineApplications(pipeline).length === 1;
  return pipeline.lanes
    .map((lane) =>
      lane
        .map((stage) =>
          useEnvironment ? (stage.enrichment?.environment ?? stage.name) : stage.name,
        )
        .join(" + "),
    )
    .join(" → ");
}

function pipelineApplications(pipeline: PromotionTopologyPipeline) {
  return pipeline.stages
    .flatMap((stage) => (stage.enrichment?.application ? [stage.enrichment.application] : []))
    .filter((application, index, applications) => applications.indexOf(application) === index)
    .toSorted((left, right) => left.localeCompare(right));
}

export function stageMatchesQuery(stage: StageInventoryStage, query: string) {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return true;

  return [
    stage.name,
    stage.enrichment?.application,
    stage.enrichment?.environment,
    stage.enrichment?.repository,
    stage.projectContext?.project,
    stage.projectContext?.integration,
    stage.projectContext?.cluster,
    ...stage.requestedFreight.map((request) => request.origin.replace(/^Warehouse\//, "")),
    ...freightNames(stage),
  ].some((value) => value?.toLocaleLowerCase().includes(normalized));
}

export function stageMatchesAlignment(
  stage: PromotionProjectStage,
  alignment?: PromotionFreightAlignmentStatus,
) {
  return (
    alignment === undefined ||
    stage.freightAlignments.some((candidate) => candidate.status === alignment)
  );
}

function stageIsUnhealthy(stage: PromotionProjectStage) {
  if (stage.observationStale) return false;
  if (stage.healthStatus) {
    return stage.healthStatus !== "Healthy" && stage.healthStatus !== "NotApplicable";
  }
  return stage.conditions.some(
    (condition) => condition.type === "Ready" && condition.status !== "True",
  );
}

export function stageMatchesAttention(
  stage: PromotionProjectStage,
  attention?: PromotionProjectAttention,
) {
  switch (attention) {
    case undefined:
      return true;
    case "all":
      return stageNeedsAttention(stage);
    case "unhealthy":
      return stageIsUnhealthy(stage);
    case "stale":
      return stage.observationStale;
    case "pending-review":
      return stage.pendingPromotionCount > 0;
    case "newer-freight":
      return stageHasNewerFreight(stage);
  }
}

function stageNeedsAttention(stage: PromotionProjectStage) {
  return (
    stage.observationStale ||
    stageIsUnhealthy(stage) ||
    stage.pendingPromotionCount > 0 ||
    stageHasNewerFreight(stage) ||
    stage.freightAlignments.some((alignment) => alignment.status === "different") ||
    stageCapabilityWarnings(stage).length > 0
  );
}

function stageCapabilityWarnings(stage: PromotionProjectStage) {
  const warnings: string[] = [];
  const reviewBlocked =
    stage.policy.effectiveMode === "review-required" &&
    stage.capabilities.review.status !== "ready";
  if (stage.capabilities.review.status === "unavailable" || reviewBlocked) {
    warnings.push(
      stage.capabilities.review.reason ??
        "The configured review policy is blocked until Slipramp review is ready.",
    );
  }
  if (stage.capabilities.branchDrift.status === "unavailable") {
    warnings.push(
      stage.capabilities.branchDrift.reason ??
        "Configured branch-drift enrichment is currently unavailable.",
    );
  }
  return [...new Set(warnings)];
}

export function StageHealthBadge({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  const nonReady = stage.conditions.find(
    (condition) => condition.type === "Ready" && condition.status !== "True",
  );
  const presentation = stage.observationStale
    ? { color: "gray", label: "Observation stale" }
    : stage.healthStatus === "Healthy"
      ? { color: "green", label: "Healthy" }
      : stage.healthStatus === "NotApplicable"
        ? { color: "gray", label: "Health not applicable" }
        : stage.healthStatus
          ? { color: "red", label: stage.healthStatus }
          : nonReady
            ? { color: "red", label: "Not ready" }
            : { color: "gray", label: "Health unknown" };

  if (stage.argoCdUrl) {
    return (
      <Badge
        aria-label={`Open ${stage.name} health in Argo CD`}
        color={presentation.color}
        component="a"
        href={stage.argoCdUrl}
        rel="noreferrer"
        style={{ cursor: "pointer" }}
        target="_blank"
        variant="light"
      >
        {presentation.label}
      </Badge>
    );
  }

  return (
    <Badge color={presentation.color} variant="light">
      {presentation.label}
    </Badge>
  );
}

export function StageCard({
  canWrite,
  linkName = true,
  onEditPolicy,
  showFreightDetails = true,
  showKargoLink = true,
  showStageHeader = true,
  stage,
}: Readonly<{
  canWrite: boolean;
  linkName?: boolean;
  onEditPolicy: () => void;
  showFreightDetails?: boolean;
  showKargoLink?: boolean;
  showStageHeader?: boolean;
  stage: PromotionProjectStage;
}>) {
  const capabilityWarnings = stageCapabilityWarnings(stage);
  const policyControls = (
    <>
      <StagePolicyBadge canEdit={canWrite} onEdit={onEditPolicy} stage={stage} />
      {stage.pendingPromotionCount > 0 ? (
        <Anchor component={Link} size="sm" to={`/promotions?stageId=${stage.id}`}>
          {stage.pendingPromotionCount} pending
        </Anchor>
      ) : null}
    </>
  );

  return (
    <Paper style={{ overflow: "hidden" }} withBorder>
      <Stack gap={0}>
        <Stack gap="md" p="md">
          {showStageHeader ? (
            <Group align="flex-start" justify="space-between" wrap="nowrap">
              <Stack gap={3}>
                <Group gap="xs">
                  {linkName ? (
                    <PromotionStageLink heading showKargoLink={showKargoLink} stage={stage} />
                  ) : (
                    <Title order={3} size="h4">
                      {stage.name}
                    </Title>
                  )}
                  <KargoObservationTime value={stage.lastObservedAt} />
                  {stage.controlFlow ? <Badge variant="light">Control flow</Badge> : null}
                </Group>
              </Stack>
              <StageHealthBadge stage={stage} />
            </Group>
          ) : null}

          <Group justify={showStageHeader ? "space-between" : "flex-end"} wrap="wrap">
            {showStageHeader ? <Group gap="xs">{policyControls}</Group> : null}
            {!showStageHeader || showFreightDetails || canWrite ? (
              <Group gap="xs">
                {!showStageHeader ? policyControls : null}
                {showFreightDetails ? (
                  <StageFreightDetailsDialog compact canWrite={canWrite} stage={stage} />
                ) : null}
                {canWrite ? <StagePromotionActions stage={stage} /> : null}
              </Group>
            ) : null}
          </Group>

          {capabilityWarnings.length > 0 ? (
            <Alert color="orange" title="Slipramp enrichment needs attention">
              <Stack gap={4}>
                {capabilityWarnings.map((warning) => (
                  <Text key={warning} size="sm">
                    {warning}
                  </Text>
                ))}
              </Stack>
            </Alert>
          ) : null}
        </Stack>

        <StageFreight stage={stage} />
      </Stack>
    </Paper>
  );
}

function StagePromotionActions({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  return (
    <PromotionCreateMenu
      currentFreight={stage.freight.items}
      eligibleFreight={eligibleStageFreight(stage)}
      stage={stage}
    />
  );
}

function StageFreight({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  const warehouseDiscoveredAt = Object.fromEntries(
    stage.freight.items.flatMap((freight) =>
      freight.discoveredAt ? [[freight.origin, freight.discoveredAt]] : [],
    ),
  );

  return (
    <FreightSourceTable
      borderTop
      columns={[
        {
          header: (
            <Text fw={650} size="sm">
              Current Freight
            </Text>
          ),
          stage,
        },
      ]}
      emptyMessage="This Stage does not request or report Freight."
      minWidth={540}
      warehouseDiscoveredAt={warehouseDiscoveredAt}
    />
  );
}

function StageFreightDetailsDialog({
  canWrite,
  compact = false,
  includeWarnings = false,
  stage,
}: Readonly<{
  canWrite: boolean;
  compact?: boolean;
  includeWarnings?: boolean;
  stage: PromotionProjectStage;
}>) {
  const [opened, setOpened] = useState(false);

  return (
    <>
      <Button
        leftSection={<IconListDetails aria-hidden size={14} />}
        onClick={() => setOpened(true)}
        size="compact-xs"
        variant={compact ? "subtle" : "default"}
      >
        {compact ? "Freight details" : "Other Freight and source details"}
      </Button>
      <Modal
        centered
        onClose={() => setOpened(false)}
        opened={opened}
        size="min(90rem, calc(100vw - 2rem))"
        title={`Freight details · ${stage.name}`}
      >
        <StageFreightDetails canWrite={canWrite} includeWarnings={includeWarnings} stage={stage} />
      </Modal>
    </>
  );
}

export function StageFreightDetails({
  canWrite,
  includeWarnings = false,
  page = 1,
  pageParam = "freightPage",
  pageSize,
  stage,
}: Readonly<{
  canWrite: boolean;
  includeWarnings?: boolean;
  page?: number;
  pageParam?: string;
  pageSize?: number;
  stage: PromotionProjectStage;
}>) {
  const rows = buildStageFreightDetailRows(stage);
  const visibleRows = pageSize ? rows.slice((page - 1) * pageSize, page * pageSize) : rows;
  const warnings = includeWarnings ? stageCapabilityWarnings(stage) : [];
  const eligibleFreight = eligibleStageFreight(stage);

  return (
    <Stack gap="md">
      {warnings.length > 0 ? (
        <Alert color="orange" title="Slipramp enrichment needs attention">
          <Stack gap={4}>
            {warnings.map((warning) => (
              <Text key={warning} size="sm">
                {warning}
              </Text>
            ))}
          </Stack>
        </Alert>
      ) : null}

      <Paper withBorder>
        <Table.ScrollContainer minWidth={840} type="native">
          <Table styles={{ th: { textTransform: "none" } }} verticalSpacing="sm" withColumnBorders>
            <Table.Thead>
              <Table.Tr>
                <Table.Th miw={120}>Freight ID</Table.Th>
                <Table.Th miw={150}>Source</Table.Th>
                <Table.Th miw={270}>Contents</Table.Th>
                <Table.Th miw={120}>State</Table.Th>
                <Table.Th miw={130}>Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {visibleRows.map((row, index) => (
                <Fragment key={row.key}>
                  {index === 0 || visibleRows[index - 1]?.origin !== row.origin ? (
                    <StageFreightWarehouseHeader origin={row.origin} stage={stage} />
                  ) : null}
                  <StageFreightDetailTableRow
                    canWrite={canWrite}
                    eligibleFreight={eligibleFreight}
                    row={row}
                    stage={stage}
                  />
                </Fragment>
              ))}
              {rows.length === 0 ? (
                <Table.Tr>
                  <Table.Td c="dimmed" colSpan={5}>
                    This Stage does not request or report Freight.
                  </Table.Td>
                </Table.Tr>
              ) : null}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>

      {pageSize && rows.length > 0 ? (
        <ListPagination
          itemCount={visibleRows.length}
          noun="Freight entries"
          page={page}
          pageParam={pageParam}
          pageSize={pageSize}
          total={rows.length}
        />
      ) : null}
    </Stack>
  );
}

function StageFreightWarehouseHeader({
  origin,
  stage,
}: Readonly<{ origin: string; stage: PromotionProjectStage }>) {
  const requests = stage.requestedFreight.filter((request) => request.origin === origin);

  return (
    <Table.Tr>
      <Table.Th bg="var(--mantine-color-default-hover)" colSpan={5} scope="rowgroup">
        <Group gap="xs" wrap="wrap">
          <Text fw={700} size="sm">
            {freightOriginLabel(origin)}
          </Text>
          <Text c="dimmed" size="xs">
            Warehouse
          </Text>
          {requests.map((request, requestIndex) => (
            <Fragment key={`${request.origin}:${requestIndex}`}>
              {request.direct ? <Badge size="xs">Direct</Badge> : null}
              <Badge size="xs" variant="outline">
                {request.availabilityStrategy}
              </Badge>
              {request.requiredSoakTime ? (
                <Badge size="xs" variant="outline">
                  Soak {request.requiredSoakTime}
                </Badge>
              ) : null}
            </Fragment>
          ))}
        </Group>
      </Table.Th>
    </Table.Tr>
  );
}

function StageFreightDetailTableRow({
  canWrite,
  eligibleFreight,
  row,
  stage,
}: Readonly<{
  canWrite: boolean;
  eligibleFreight: PromotionFreightItem[];
  row: StageFreightDetailRow;
  stage: PromotionProjectStage;
}>) {
  if (row.kind === "current") {
    return (
      <Table.Tr>
        <Table.Th scope="row">
          <FreightIdentifier freight={row.freight} />
        </Table.Th>
        <Table.Td>
          <Text fw={600} size="sm">
            {stage.name}
          </Text>
          <Text c="dimmed" size="xs">
            Current Stage
          </Text>
        </Table.Td>
        <Table.Td>
          <FreightContents freight={row.freight} />
        </Table.Td>
        <Table.Td>
          <FreightStatusBadge
            color="green"
            details={[`Observed in ${stage.name}.`]}
            label="Active"
          />
        </Table.Td>
        <Table.Td />
      </Table.Tr>
    );
  }

  if (row.kind === "source") {
    const presentation = freightAlignmentPresentation(row.alignment?.status ?? "unknown");
    const sourceFreight = row.alignment?.sourceCurrentFreight;
    const sourceStageId = row.alignment?.sourceStageId;
    const canPromote =
      canWrite &&
      sourceStageId &&
      sourceFreight &&
      eligibleFreight.some(
        (item) => item.origin === sourceFreight.origin && item.name === sourceFreight.name,
      );

    return (
      <Table.Tr>
        <Table.Th scope="row">
          {sourceFreight ? (
            <FreightIdentifier freight={sourceFreight} />
          ) : (
            <Text c="dimmed">—</Text>
          )}
        </Table.Th>
        <Table.Td>
          <Text fw={600} size="sm">
            {row.source.name}
          </Text>
          <Text c="dimmed" size="xs">
            Upstream Stage
          </Text>
        </Table.Td>
        <Table.Td>
          {sourceFreight ? (
            <FreightContents freight={sourceFreight} />
          ) : (
            <Text c="dimmed" size="xs">
              None observed
            </Text>
          )}
        </Table.Td>
        <Table.Td>
          <FreightStatusBadge
            color={presentation.color}
            details={[
              ...(row.alignment?.targetCurrentFreight
                ? [`${stage.name}: ${freightDisplayLabel(row.alignment.targetCurrentFreight)}`]
                : []),
              ...(row.alignment?.reason ? [row.alignment.reason] : []),
            ]}
            label={presentation.label}
            variant="light"
          />
        </Table.Td>
        <Table.Td>
          {canPromote ? (
            <PromotionCreateButton
              freight={[sourceFreight]}
              initialFreightName={sourceFreight.name}
              sourceStageId={sourceStageId}
              sourceStageName={row.source.name}
              stage={stage}
            />
          ) : null}
        </Table.Td>
      </Table.Tr>
    );
  }

  if (row.kind === "candidate") {
    return (
      <Table.Tr>
        <Table.Th scope="row">
          <FreightIdentifier freight={row.candidate.freight} />
        </Table.Th>
        <Table.Td>
          <Text fw={600} size="sm">
            {row.request.direct ? "Direct candidate" : "Candidate"}
          </Text>
        </Table.Td>
        <Table.Td>
          <FreightContents freight={row.candidate.freight} />
        </Table.Td>
        <Table.Td>
          <FreightReadinessBadge candidate={row.candidate} />
        </Table.Td>
        <Table.Td>
          {canWrite && row.request.direct && row.candidate.readiness === "ready" ? (
            <PromotionCreateButton
              freight={eligibleFreight}
              initialFreightName={row.candidate.freight.name}
              stage={stage}
            />
          ) : null}
        </Table.Td>
      </Table.Tr>
    );
  }

  return (
    <Table.Tr>
      <Table.Td c="dimmed" colSpan={5}>
        No Freight is currently observed for this Warehouse.
      </Table.Td>
    </Table.Tr>
  );
}

function FreightReadinessBadge({ candidate }: Readonly<{ candidate: PromotionFreightCandidate }>) {
  return (
    <FreightStatusBadge
      color={
        candidate.readiness === "ready"
          ? "blue"
          : candidate.readiness === "unknown"
            ? "gray"
            : "orange"
      }
      details={[...(candidate.reason ? [candidate.reason] : []), ...(candidate.evidence ?? [])]}
      label={candidate.readiness}
    />
  );
}

function FreightStatusBadge({
  color,
  details,
  label,
  variant,
}: Readonly<{
  color: string;
  details: string[];
  label: string;
  variant?: "light";
}>) {
  const badge = (
    <Badge color={color} variant={variant}>
      {label}
    </Badge>
  );

  if (details.length === 0) return badge;

  return (
    <Tooltip
      label={
        <Stack gap={3}>
          {details.map((detail, index) => (
            <Text inherit key={`${detail}:${index}`}>
              {detail}
            </Text>
          ))}
        </Stack>
      }
      maw={360}
      multiline
      withArrow
    >
      {badge}
    </Tooltip>
  );
}

export function StagePolicyDialog({
  actionData,
  onClose,
  stage,
}: Readonly<{
  actionData: unknown;
  onClose: () => void;
  stage?: Pick<PromotionProjectStage, "id" | "name" | "policy" | "capabilities">;
}>) {
  return (
    <Modal
      centered
      onClose={onClose}
      opened={stage !== undefined}
      size="lg"
      stackId="stage-policy"
      title={stage ? `Policy · ${stage.name}` : "Stage policy"}
    >
      {stage ? <PolicyForm actionData={actionData} onCancel={onClose} stage={stage} /> : null}
    </Modal>
  );
}

function PolicyForm({
  actionData,
  onCancel,
  stage,
}: Readonly<{
  actionData: unknown;
  onCancel: () => void;
  stage: Pick<PromotionProjectStage, "id" | "name" | "policy" | "capabilities">;
}>) {
  const navigation = useNavigation();
  const submitting = navigation.formData?.get("stageId") === stage.id;
  const submitted = useRef(false);
  const policyActionData = policyActionFeedback(actionData);
  const policyPresentation = promotionStagePolicyPresentation(stage);

  useEffect(() => {
    if (submitting) {
      submitted.current = true;
      return;
    }
    if (!submitted.current || policyActionData?.stageId !== stage.id) return;

    submitted.current = false;
    if (policyActionData.message) onCancel();
  }, [onCancel, policyActionData, stage.id, submitting]);

  return (
    <Form method="post">
      <Stack gap="md">
        {policyActionData?.stageId === stage.id && policyActionData.error ? (
          <Alert color="red">{policyActionData.error}</Alert>
        ) : null}
        {policyActionData?.stageId === stage.id && policyActionData.message ? (
          <Alert color="green">{policyActionData.message}</Alert>
        ) : null}
        <input name="stageId" type="hidden" value={stage.id} />
        <NativeSelect
          defaultValue={stage.policy.configuredMode}
          label="Mode"
          name="policyMode"
          data={[
            { label: "Automatic", value: "automatic" },
            {
              disabled: policyPresentation.reviewRequiredDisabled,
              label: "Review required",
              value: "review-required",
            },
          ]}
        />
        {policyPresentation.reviewRequiredDisabled ? (
          <Text c="dimmed" size="sm">
            {stage.capabilities.review.reason} The existing policy is preserved until you save an
            explicit change.
          </Text>
        ) : null}
        <TextInput defaultValue={stage.policy.expiresAt} label="Expires at" name="expiresAt" />
        <TextInput
          defaultValue={stage.policy.reason}
          label="Reason"
          maxLength={4096}
          name="reason"
        />
        <Group justify="flex-end">
          <Button onClick={onCancel} type="button" variant="default">
            Cancel
          </Button>
          <Button loading={submitting} type="submit">
            Save policy
          </Button>
        </Group>
      </Stack>
    </Form>
  );
}

function policyActionFeedback(value: unknown) {
  if (!value || typeof value !== "object" || !("stageId" in value)) return undefined;

  const candidate = value as { error?: unknown; message?: unknown; stageId?: unknown };
  return {
    error: typeof candidate.error === "string" ? candidate.error : undefined,
    message: typeof candidate.message === "string" ? candidate.message : undefined,
    stageId: typeof candidate.stageId === "string" ? candidate.stageId : undefined,
  };
}
