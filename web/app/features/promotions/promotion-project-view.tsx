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
import { Fragment, useState } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import {
  type PromotionFreightAlignmentStatus,
  type PromotionFreightCandidate,
  type PromotionFreightItem,
  type PromotionPolicyMode,
  type PromotionPipeline,
  type PromotionProjectDetail,
  type PromotionProjectStage,
} from "~/api/generated";
import {
  PromotionCreateButton,
  PromotionCreateMenu,
} from "~/features/promotions/promotion-create-dialog";
import { eligibleStageFreight } from "~/features/promotions/promotion-creation";
import {
  buildPipelineFreightSourceRows,
  freightArtifactReference,
  freightAlignmentForSource,
  freightAlignmentPresentation,
  freightDisplayLabel,
  freightNames,
  freightOriginLabel,
  otherFreightCandidates,
} from "~/features/promotions/promotion-freight";
import {
  promotionStagePolicyColor,
  promotionStagePolicyPresentation,
} from "~/features/promotions/promotion-policy";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { PromotionProjectLink } from "~/features/promotions/promotion-project-link";
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
import { RelativeTime } from "~/shared/display-values";
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
          {differences > 0 ? <Badge color="yellow">{differences} Freight differences</Badge> : null}
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
  const stages = pipeline.lanes.flatMap((lane) => lane);
  const freightRows = buildPipelineFreightSourceRows(stages);
  const warehouseGroups = Map.groupBy(freightRows, (row) => row.origin);
  const freightSourceColumnStyle = { maxWidth: 300, minWidth: 300, width: 300 } as const;

  return (
    <Table.ScrollContainer
      minWidth={Math.max(720, 300 + stages.length * 240)}
      style={{ borderTop: "1px solid var(--slipramp-border)" }}
      type="native"
    >
      <Table styles={{ th: { textTransform: "none" } }} verticalSpacing="sm" withColumnBorders>
        <Table.Thead>
          <Table.Tr>
            <Table.Th style={{ ...freightSourceColumnStyle, verticalAlign: "top" }}>
              Freight source
            </Table.Th>
            {pipeline.lanes.flatMap((lane, laneIndex) =>
              lane.map((stage) => (
                <Table.Th key={stage.id} miw={240} style={{ verticalAlign: "top" }}>
                  <PipelineStageColumnTitle
                    canWrite={canWrite}
                    laneIndex={laneIndex}
                    onEditPolicy={() => onEditPolicy(stage)}
                    stage={stage}
                  />
                </Table.Th>
              )),
            )}
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
                  <Group gap="xs">
                    <Text fw={700} size="sm">
                      {freightOriginLabel(origin)}
                    </Text>
                    <Text c="dimmed" size="xs">
                      Warehouse
                    </Text>
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
                No Freight sources are requested by these Stages.
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
  const policy = promotionStagePolicyPresentation(stage);
  const warnings = stageCapabilityWarnings(stage);

  return (
    <Stack gap={6}>
      <Group align="center" justify="space-between" wrap="nowrap">
        <PromotionStageLink stage={stage} />
        <StageHealthBadge stage={stage} />
      </Group>
      <Group gap={5} wrap="wrap">
        <Text c="dimmed" fw={600} size="xs">
          {stage.enrichment?.environment ?? (laneIndex === 0 ? "Entry" : `Downstream ${laneIndex}`)}
        </Text>
        <Text c="dimmed" size="xs">
          ·
        </Text>
        <RelativeTime c="dimmed" size="xs" value={stage.lastObservedAt} />
        {stage.controlFlow ? (
          <Badge color="gray" size="xs" tt="none" variant="light">
            Control flow
          </Badge>
        ) : null}
        {warnings.length > 0 ? (
          <Badge color="orange" size="xs" tt="none" variant="light">
            {warnings.length} warning{warnings.length === 1 ? "" : "s"}
          </Badge>
        ) : null}
      </Group>
      <Group gap={6} wrap="wrap">
        <StagePolicyBadge
          canEdit={canWrite && policy.editable}
          editable={policy.editable}
          label={policy.label}
          mode={stage.policy.effectiveMode}
          onEdit={onEditPolicy}
          stageName={stage.name}
        />
        {stage.pendingPromotionCount > 0 ? (
          <Anchor component={Link} size="xs" to={`/promotions?stageId=${stage.id}`}>
            {stage.pendingPromotionCount} pending
          </Anchor>
        ) : null}
        <StageFreightDetailsDialog compact includeWarnings canWrite={canWrite} stage={stage} />
        {canWrite ? <StagePromotionActions stage={stage} /> : null}
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

function freightSourceKindLabel(kind: PromotionFreightItem["artifacts"][number]["kind"]) {
  switch (kind) {
    case "git":
      return "Git";
    case "image":
      return "Image";
    case "chart":
      return "Chart";
  }
}

export function StageInventory({
  canWrite,
  onEditPolicy,
  query,
  showFreight = true,
  showProject = false,
  stages,
}: Readonly<{
  canWrite: boolean;
  onEditPolicy: (stage: PromotionProjectStage) => void;
  query: string;
  showFreight?: boolean;
  showProject?: boolean;
  stages: StageInventoryStage[];
}>) {
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
                const policy = promotionStagePolicyPresentation(stage);
                const currentFreight = stage.freight.items;

                return (
                  <Table.Tr key={stage.id}>
                    <Table.Td style={{ whiteSpace: "nowrap" }}>
                      <Stack gap={3}>
                        <PromotionStageLink fw={650} stage={stage} />
                        {stage.enrichment?.application || stage.enrichment?.environment ? (
                          <Text c="dimmed" size="xs" style={{ whiteSpace: "nowrap" }}>
                            {[stage.enrichment.application, stage.enrichment.environment]
                              .filter(Boolean)
                              .join(" · ")}
                          </Text>
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
                            <Text c="dimmed" size="xs">
                              {stage.projectContext.integration} · {stage.projectContext.cluster}
                            </Text>
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
                      <RelativeTime size="sm" value={stage.lastObservedAt} />
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={4}>
                        <StagePolicyBadge
                          canEdit={canWrite && policy.editable}
                          editable={policy.editable}
                          label={policy.label}
                          mode={stage.policy.effectiveMode}
                          onEdit={() => onEditPolicy(stage)}
                          stageName={stage.name}
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
  }
}

function stageNeedsAttention(stage: PromotionProjectStage) {
  return (
    stage.observationStale ||
    stageIsUnhealthy(stage) ||
    stage.pendingPromotionCount > 0 ||
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
        tt="none"
        variant="light"
      >
        {presentation.label}
      </Badge>
    );
  }

  return (
    <Badge color={presentation.color} tt="none" variant="light">
      {presentation.label}
    </Badge>
  );
}

function StagePolicyBadge({
  canEdit,
  editable,
  label,
  mode,
  onEdit,
  stageName,
}: Readonly<{
  canEdit: boolean;
  editable: boolean;
  label: string;
  mode: PromotionPolicyMode;
  onEdit: () => void;
  stageName: string;
}>) {
  const color = editable ? promotionStagePolicyColor(mode) : "gray";
  if (!canEdit) {
    return (
      <Badge color={color} tt="none" variant="light">
        {label}
      </Badge>
    );
  }

  return (
    <Badge
      aria-label={`Edit ${stageName} policy`}
      color={color}
      component="button"
      onClick={onEdit}
      style={{ cursor: "pointer" }}
      tt="none"
      type="button"
      variant="light"
    >
      {label}
    </Badge>
  );
}

export function StageCard({
  canWrite,
  linkName = true,
  onEditPolicy,
  showKargoLink = true,
  stage,
}: Readonly<{
  canWrite: boolean;
  linkName?: boolean;
  onEditPolicy: () => void;
  showKargoLink?: boolean;
  stage: PromotionProjectStage;
}>) {
  const policyPresentation = promotionStagePolicyPresentation(stage);
  const capabilityWarnings = stageCapabilityWarnings(stage);

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
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
              {stage.controlFlow ? <Badge variant="light">Control flow</Badge> : null}
            </Group>
          </Stack>
          <Stack align="flex-end" gap={5}>
            <StageHealthBadge stage={stage} />
            <RelativeTime size="xs" value={stage.lastObservedAt} />
          </Stack>
        </Group>

        <StageFreight canWrite={canWrite} stage={stage} />

        <Group justify="space-between" wrap="wrap">
          <Group gap="xs">
            <StagePolicyBadge
              canEdit={canWrite && policyPresentation.editable}
              editable={policyPresentation.editable}
              label={policyPresentation.label}
              mode={stage.policy.effectiveMode}
              onEdit={onEditPolicy}
              stageName={stage.name}
            />
            {stage.pendingPromotionCount > 0 ? (
              <Anchor component={Link} size="sm" to={`/promotions?stageId=${stage.id}`}>
                {stage.pendingPromotionCount} pending
              </Anchor>
            ) : null}
          </Group>
          <Group gap="xs">{canWrite ? <StagePromotionActions stage={stage} /> : null}</Group>
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
    </Paper>
  );
}

function StagePromotionActions({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  return (
    <PromotionCreateMenu
      currentFreight={stage.freight.items}
      eligibleFreight={eligibleStageFreight(stage)}
      environment={stage.enrichment?.environment}
      stage={stage}
    />
  );
}

function StageFreight({
  canWrite,
  stage,
}: Readonly<{
  canWrite: boolean;
  stage: PromotionProjectStage;
}>) {
  const activeFreight = stage.freight.items;

  return (
    <Stack gap="xs">
      {activeFreight.length > 0 ? (
        <Stack gap="xs">
          {activeFreight.map((freight) => (
            <FreightSummary freight={freight} key={`${freight.origin}:${freight.name}`} />
          ))}
        </Stack>
      ) : (
        <Text c="dimmed" size="sm">
          No active Freight is currently observed.
        </Text>
      )}
      <StageFreightDetailsDialog canWrite={canWrite} stage={stage} />
    </Stack>
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
  const activeFreight = stage.freight.items;
  const requests = stage.requestedFreight.map((request) => ({
    request,
    otherCandidates: otherFreightCandidates(activeFreight, request.candidates),
  }));
  const otherCount = requests.reduce((count, request) => count + request.otherCandidates.length, 0);
  const eligibleFreight = eligibleStageFreight(stage);
  const upstreamPromotions = stage.requestedFreight.flatMap((request) =>
    request.sources.flatMap((source) => {
      const alignment = freightAlignmentForSource(stage, request.origin, source.name);
      const freight = alignment?.sourceCurrentFreight;
      if (!alignment?.sourceStageId || !freight) return [];
      const eligible = eligibleFreight.some(
        (item) => item.origin === freight.origin && item.name === freight.name,
      );
      if (!eligible) return [];

      return [
        {
          freight,
          origin: request.origin,
          sourceStageId: alignment.sourceStageId,
          sourceStageName: source.name,
        },
      ];
    }),
  );
  const warnings = includeWarnings ? stageCapabilityWarnings(stage) : [];
  const warehouses = new Map<
    string,
    { activeFreight: PromotionFreightItem[]; requests: typeof requests }
  >();

  for (const freight of activeFreight) {
    const warehouse = warehouses.get(freight.origin) ?? { activeFreight: [], requests: [] };
    warehouse.activeFreight.push(freight);
    warehouses.set(freight.origin, warehouse);
  }
  for (const request of requests) {
    const warehouse = warehouses.get(request.request.origin) ?? {
      activeFreight: [],
      requests: [],
    };
    warehouse.requests.push(request);
    warehouses.set(request.request.origin, warehouse);
  }

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
        size="min(72rem, calc(100vw - 2rem))"
        title={`Freight details · ${stage.name}`}
      >
        <Stack gap="md">
          <Group justify="space-between" wrap="wrap">
            <Text c="dimmed" size="sm">
              Current Freight, requested sources, and observed alternatives by Warehouse.
            </Text>
            {otherCount > 0 ? (
              <Badge color="gray" tt="none" variant="light">
                {otherCount} other option{otherCount === 1 ? "" : "s"}
              </Badge>
            ) : null}
          </Group>

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
            <Table.ScrollContainer minWidth={720} type="native">
              <Table
                styles={{ th: { textTransform: "none" } }}
                verticalSpacing="sm"
                withColumnBorders
              >
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th miw={150}>Source</Table.Th>
                    <Table.Th miw={270}>Freight</Table.Th>
                    <Table.Th miw={120}>State</Table.Th>
                    <Table.Th miw={130}>Actions</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {[...warehouses]
                    .toSorted(([left], [right]) => left.localeCompare(right))
                    .map(([origin, warehouse]) => {
                      const detailRowCount =
                        warehouse.activeFreight.length +
                        warehouse.requests.reduce(
                          (count, item) =>
                            count + item.request.sources.length + item.otherCandidates.length,
                          0,
                        );

                      return (
                        <Fragment key={origin}>
                          <Table.Tr>
                            <Table.Th
                              bg="var(--mantine-color-default-hover)"
                              colSpan={4}
                              scope="rowgroup"
                            >
                              <Group gap="xs" wrap="wrap">
                                <Text fw={700} size="sm">
                                  {freightOriginLabel(origin)}
                                </Text>
                                <Text c="dimmed" size="xs">
                                  Warehouse
                                </Text>
                                {warehouse.requests.map(({ request }, requestIndex) => (
                                  <Fragment key={`${request.origin}:${requestIndex}`}>
                                    {request.direct ? (
                                      <Badge size="xs" tt="none">
                                        Direct
                                      </Badge>
                                    ) : null}
                                    <Badge size="xs" tt="none" variant="outline">
                                      {request.availabilityStrategy}
                                    </Badge>
                                    {request.requiredSoakTime ? (
                                      <Badge size="xs" tt="none" variant="outline">
                                        Soak {request.requiredSoakTime}
                                      </Badge>
                                    ) : null}
                                  </Fragment>
                                ))}
                              </Group>
                            </Table.Th>
                          </Table.Tr>

                          {warehouse.activeFreight.map((freight) => (
                            <Table.Tr key={`active:${freight.name}`}>
                              <Table.Th scope="row">
                                <Text fw={600} size="sm">
                                  Current Stage
                                </Text>
                              </Table.Th>
                              <Table.Td>
                                <FreightTableValue freight={freight} />
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
                          ))}

                          {warehouse.requests.flatMap(({ request }) =>
                            request.sources.map((source) => {
                              const alignment = freightAlignmentForSource(
                                stage,
                                request.origin,
                                source.name,
                              );
                              const presentation = freightAlignmentPresentation(
                                alignment?.status ?? "unknown",
                              );
                              const upstreamPromotion = upstreamPromotions.find(
                                (promotion) =>
                                  promotion.origin === request.origin &&
                                  promotion.sourceStageId === alignment?.sourceStageId,
                              );

                              return (
                                <Table.Tr
                                  key={`source:${source.stageId ?? "unresolved"}:${source.name}`}
                                >
                                  <Table.Th scope="row">
                                    <Text fw={600} size="sm">
                                      {source.name}
                                    </Text>
                                    <Text c="dimmed" size="xs">
                                      Upstream Stage
                                    </Text>
                                  </Table.Th>
                                  <Table.Td>
                                    {alignment?.sourceCurrentFreight ? (
                                      <FreightTableValue freight={alignment.sourceCurrentFreight} />
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
                                        ...(alignment?.targetCurrentFreight
                                          ? [
                                              `${stage.name}: ${freightDisplayLabel(
                                                alignment.targetCurrentFreight,
                                              )}`,
                                            ]
                                          : []),
                                        ...(alignment?.reason ? [alignment.reason] : []),
                                      ]}
                                      label={presentation.label}
                                      variant="light"
                                    />
                                  </Table.Td>
                                  <Table.Td>
                                    <Group gap="xs" wrap="wrap">
                                      {canWrite && upstreamPromotion ? (
                                        <PromotionCreateButton
                                          environment={stage.enrichment?.environment}
                                          freight={[upstreamPromotion.freight]}
                                          initialFreightName={upstreamPromotion.freight.name}
                                          sourceStageId={upstreamPromotion.sourceStageId}
                                          sourceStageName={upstreamPromotion.sourceStageName}
                                          stage={stage}
                                        />
                                      ) : null}
                                    </Group>
                                  </Table.Td>
                                </Table.Tr>
                              );
                            }),
                          )}

                          {warehouse.requests.flatMap(({ otherCandidates, request }) =>
                            otherCandidates.map((candidate) => (
                              <Table.Tr key={`candidate:${candidate.freight.name}`}>
                                <Table.Th scope="row">
                                  <Text fw={600} size="sm">
                                    {request.direct ? "Direct candidate" : "Candidate"}
                                  </Text>
                                </Table.Th>
                                <Table.Td>
                                  <FreightTableValue freight={candidate.freight} />
                                </Table.Td>
                                <Table.Td>
                                  <FreightReadinessBadge candidate={candidate} />
                                </Table.Td>
                                <Table.Td>
                                  {canWrite && request.direct && candidate.readiness === "ready" ? (
                                    <PromotionCreateButton
                                      stage={stage}
                                      freight={eligibleFreight}
                                      environment={stage.enrichment?.environment}
                                      initialFreightName={candidate.freight.name}
                                    />
                                  ) : null}
                                </Table.Td>
                              </Table.Tr>
                            )),
                          )}

                          {detailRowCount === 0 ? (
                            <Table.Tr>
                              <Table.Td c="dimmed" colSpan={4}>
                                No Freight is currently observed for this Warehouse.
                              </Table.Td>
                            </Table.Tr>
                          ) : null}
                        </Fragment>
                      );
                    })}
                  {warehouses.size === 0 ? (
                    <Table.Tr>
                      <Table.Td c="dimmed" colSpan={4}>
                        This Stage does not request or report Freight.
                      </Table.Td>
                    </Table.Tr>
                  ) : null}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </Paper>
        </Stack>
      </Modal>
    </>
  );
}

function FreightTableValue({ freight }: Readonly<{ freight: PromotionFreightItem }>) {
  return (
    <Stack gap={4}>
      {freight.artifacts.length > 0 ? (
        freight.artifacts.map((artifact) => (
          <Group
            align="center"
            gap="xs"
            key={`${artifact.kind}:${artifact.repository ?? ""}:${artifact.version}`}
            wrap="wrap"
          >
            <Badge size="xs" tt="none" variant="light">
              {freightSourceKindLabel(artifact.kind)}
            </Badge>
            <Text size="xs" style={{ overflowWrap: "anywhere" }}>
              {artifact.repository ?? freightOriginLabel(freight.origin)}
            </Text>
            <Code>{artifact.version}</Code>
          </Group>
        ))
      ) : (
        <Text c="dimmed" size="xs">
          No versioned artifacts
        </Text>
      )}
      <Text c="dimmed" size="xs">
        Kargo Freight <Code>{freight.name}</Code>
      </Text>
    </Stack>
  );
}

function FreightSummary({ freight }: Readonly<{ freight: PromotionFreightItem }>) {
  return (
    <Paper p="sm" withBorder>
      <Stack gap="xs">
        <Group gap="xs" wrap="wrap">
          <Badge color="green" size="xs" tt="none">
            Active
          </Badge>
          <Text c="dimmed" size="xs">
            {freightOriginLabel(freight.origin)}
          </Text>
        </Group>
        <FreightTableValue freight={freight} />
      </Stack>
    </Paper>
  );
}

function FreightReadinessBadge({ candidate }: Readonly<{ candidate: PromotionFreightCandidate }>) {
  return (
    <FreightStatusBadge
      color={
        candidate.readiness === "ready"
          ? "green"
          : candidate.readiness === "unknown"
            ? "gray"
            : "orange"
      }
      details={[...(candidate.reason ? [candidate.reason] : []), ...(candidate.evidence ?? [])]}
      label={candidate.readiness}
      size="xs"
    />
  );
}

function FreightStatusBadge({
  color,
  details,
  label,
  size,
  variant,
}: Readonly<{
  color: string;
  details: string[];
  label: string;
  size?: "xs";
  variant?: "light";
}>) {
  const badge = (
    <Badge color={color} size={size} tt="none" variant={variant}>
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
  stage?: PromotionProjectStage;
}>) {
  return (
    <Modal
      centered
      onClose={onClose}
      opened={stage !== undefined}
      size="lg"
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
  stage: PromotionProjectStage;
}>) {
  const navigation = useNavigation();
  const submitting = navigation.formData?.get("stageId") === stage.id;
  const policyActionData = policyActionFeedback(actionData);
  const policyPresentation = promotionStagePolicyPresentation(stage);

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
