import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Checkbox,
  Code,
  Group,
  Modal,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Tabs,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { IconArrowsDiff, IconListDetails, IconRoute, IconSearch } from "@tabler/icons-react";
import type { ReactNode } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getPromotionProject,
  updatePromotionStagePolicy,
  type PromotionFreightAlignment,
  type PromotionFreightAlignmentStatus,
  type PromotionFreightCandidate,
  type PromotionFreightItem,
  type PromotionPolicyMode,
  type PromotionProjectDetail,
  type PromotionProjectStage,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { PromotionCreateButton } from "~/features/promotions/promotion-create-dialog";
import { canWritePromotions, eligibleStageFreight } from "~/features/promotions/promotion-creation";
import { submitPromotionCreation } from "~/features/promotions/promotion-creation.server";
import {
  buildFreightColumns,
  buildFreightRows,
  freightArtifactVersionLabel,
  freightAlignmentForSource,
  freightAlignmentPresentation,
  freightComparisonStageIds,
  freightDisplayLabel,
  freightNames,
  freightOriginLabel,
  freightRowStatus,
  otherFreightCandidates,
} from "~/features/promotions/promotion-freight";
import { promotionStagePolicyPresentation } from "~/features/promotions/promotion-policy";
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
import { formValue, optionalFormValue, problemDetail } from "~/shared/form-data";
import { EmptyState, PageHeader, PageHeaderFact, PageHeaderFacts } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import type { Route } from "./+types/promotion-project";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.project?.project ?? "Kargo project") }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [result, principal] = await Promise.all([
    getPromotionProject({
      client,
      path: { cluster: params.cluster, integration: params.integration, project: params.project },
    }),
    getProductPrincipal(context),
  ]);

  return {
    error: result.data
      ? undefined
      : problemDetail(result.error, "The Kargo Project could not be loaded."),
    principal,
    project: result.data,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  const formData = await request.clone().formData();
  if (formData.has("mode")) return submitPromotionCreation(request, context);

  const stageId = formValue(formData, "stageId");
  const mode = formValue(formData, "policyMode") as PromotionPolicyMode;
  const reason = optionalFormValue(formData, "reason");
  const expiresAt = optionalFormValue(formData, "expiresAt");
  const result = await updatePromotionStagePolicy({
    body: { expiresAt: mode === "review-required" ? expiresAt : undefined, mode, reason },
    client: createServerAPIClient(request, context),
    path: { stageId },
  });

  return result.data
    ? { message: "The Stage policy was updated.", stageId }
    : { error: problemDetail(result.error, "The Stage policy could not be updated."), stageId };
}

export default function PromotionProject({ actionData, loaderData }: Route.ComponentProps) {
  if (!loaderData.project) {
    return (
      <Stack gap="lg">
        <PageHeader title="Kargo project" />
        <Alert color="red" title="Kargo Project unavailable">
          {loaderData.error}
        </Alert>
      </Stack>
    );
  }

  return (
    <ProjectDetail
      actionData={actionData}
      canWrite={canWritePromotions(loaderData.principal)}
      project={loaderData.project}
    />
  );
}

function ProjectDetail({
  actionData,
  canWrite,
  project,
}: Readonly<{
  actionData: Route.ComponentProps["actionData"];
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
  const validStageIds = new Set(project.stages.map((stage) => stage.id));
  const selectedStageIds = [...new Set(searchParams.getAll("stage"))].filter((id) =>
    validStageIds.has(id),
  );
  const selectedStages = project.stages.filter((stage) => selectedStageIds.includes(stage.id));
  const pipelines = topologyPipelines(project.stages)
    .map((pipeline, index) => ({
      ...pipeline,
      key: pipeline.stages.map((stage) => stage.id).join(":"),
      label: pipelineLabel(pipeline, index),
    }))
    .toSorted((left, right) => left.label.localeCompare(right.label, undefined, { numeric: true }));
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

  function compareFreight(freightAlignment: PromotionFreightAlignment) {
    const ids = freightComparisonStageIds(freightAlignment);
    if (!ids) return;
    const next = new URLSearchParams(searchParams);
    next.set("view", "compare");
    next.delete("stage");
    for (const id of ids) next.append("stage", id);
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

  function changeSelectedStageIds(ids: string[]) {
    const next = new URLSearchParams(searchParams);
    next.delete("stage");
    for (const id of ids) next.append("stage", id);
    setSearchParams(next, { preventScrollReset: true, replace: true });
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
          <Tabs.Tab
            leftSection={<IconArrowsDiff aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="compare"
          >
            Compare Freight
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
              onCompareFreight={compareFreight}
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

        <Tabs.Panel pt="lg" value="compare">
          <FreightComparison
            selectedStageIds={selectedStageIds}
            selectedStages={selectedStages}
            setSelectedStageIds={changeSelectedStageIds}
            stages={project.stages}
          />
        </Tabs.Panel>
      </Tabs>

      <Modal
        centered
        onClose={closePolicy}
        opened={policyStage !== undefined}
        size="lg"
        title={policyStage ? `Policy · ${policyStage.name}` : "Stage policy"}
      >
        {policyStage ? (
          <PolicyForm actionData={actionData} onCancel={closePolicy} stage={policyStage} />
        ) : null}
      </Modal>
    </Stack>
  );
}

type ProjectView = "pipelines" | "stages" | "compare";

function projectView(value: string | null): ProjectView {
  return value === "stages" || value === "compare" ? value : "pipelines";
}

function projectAlignment(value: string | null): PromotionFreightAlignmentStatus | undefined {
  return value === "aligned" || value === "different" || value === "unknown" ? value : undefined;
}

type ProjectPipeline = PromotionTopologyPipeline & {
  key: string;
  label: string;
};

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

function ProjectFilter({
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
  return (
    <Group align="flex-end" justify="space-between" wrap="wrap">
      <TextInput
        aria-label={`Filter ${noun}s`}
        leftSection={<IconSearch aria-hidden size={15} />}
        onChange={(event) => onChange(event.currentTarget.value)}
        placeholder="Filter by application, Stage, environment, or Warehouse"
        style={{ flex: "1 1 24rem" }}
        value={query}
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

function PipelineList({
  canWrite,
  expandedKeys,
  onCompareFreight,
  onEditPolicy,
  onExpandedKeysChange,
  pipelines,
  query,
}: Readonly<{
  canWrite: boolean;
  expandedKeys: string[];
  onCompareFreight: (alignment: PromotionFreightAlignment) => void;
  onEditPolicy: (stage: PromotionProjectStage) => void;
  onExpandedKeysChange: (keys: string[]) => void;
  pipelines: ProjectPipeline[];
  query: string;
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
    <Accordion multiple onChange={onExpandedKeysChange} value={expandedKeys} variant="separated">
      {pipelines.map((pipeline) => (
        <Accordion.Item key={pipeline.key} value={pipeline.key}>
          <Accordion.Control>
            <Group justify="space-between" mr="sm" wrap="wrap">
              <Stack gap={2}>
                <Text fw={650}>{pipeline.label}</Text>
                <Text c="dimmed" size="sm">
                  {pipelinePath(pipeline)}
                </Text>
              </Stack>
              <PipelineBadges pipeline={pipeline} />
            </Group>
          </Accordion.Control>
          <Accordion.Panel>
            <PipelineTopology
              canWrite={canWrite}
              onCompareFreight={onCompareFreight}
              onEditPolicy={onEditPolicy}
              pipeline={pipeline}
            />
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
    <Group gap={6} wrap="wrap">
      <Badge variant="light">
        {pipeline.stages.length} Stage{pipeline.stages.length === 1 ? "" : "s"}
      </Badge>
      {unhealthy > 0 ? <Badge color="red">{unhealthy} non-ready</Badge> : null}
      {stale > 0 ? <Badge color="gray">{stale} stale</Badge> : null}
      {pending > 0 ? <Badge color="yellow">{pending} pending</Badge> : null}
      {differences > 0 ? <Badge color="yellow">{differences} different</Badge> : null}
    </Group>
  );
}

function PipelineTopology({
  canWrite,
  onCompareFreight,
  onEditPolicy,
  pipeline,
}: Readonly<{
  canWrite: boolean;
  onCompareFreight: (alignment: PromotionFreightAlignment) => void;
  onEditPolicy: (stage: PromotionProjectStage) => void;
  pipeline: ProjectPipeline;
}>) {
  return (
    <SimpleGrid
      cols={{
        base: 1,
        md: Math.min(2, pipeline.lanes.length),
        xl: Math.min(3, pipeline.lanes.length),
      }}
    >
      {pipeline.lanes.map((lane, index) => (
        <Stack gap="xs" key={lane.map((stage) => stage.id).join(":")}>
          <Group gap="xs">
            <Text c="dimmed" fw={600} size="xs" tt="uppercase">
              {index === 0 ? "Entry" : `Downstream ${index}`}
            </Text>
            {lane.length > 1 ? (
              <Badge color="gray" size="xs" variant="light">
                {lane.length} Stages
              </Badge>
            ) : null}
          </Group>
          {lane.map((stage) => (
            <StageCard
              canWrite={canWrite}
              key={stage.id}
              onCompareFreight={onCompareFreight}
              onEditPolicy={() => onEditPolicy(stage)}
              stage={stage}
            />
          ))}
        </Stack>
      ))}
    </SimpleGrid>
  );
}

function StageInventory({
  canWrite,
  onEditPolicy,
  query,
  stages,
}: Readonly<{
  canWrite: boolean;
  onEditPolicy: (stage: PromotionProjectStage) => void;
  query: string;
  stages: PromotionProjectStage[];
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
      <Table.ScrollContainer minWidth={1040} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Stage</Table.Th>
              <Table.Th>Application / Environment</Table.Th>
              <Table.Th>Health</Table.Th>
              <Table.Th>Current Freight</Table.Th>
              <Table.Th>Policy</Table.Th>
              <Table.Th>Last observed</Table.Th>
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
                    <Table.Td>
                      <Stack gap={3}>
                        {stage.kargoUrl ? (
                          <KargoLink fw={650} url={stage.kargoUrl}>
                            {stage.name}
                          </KargoLink>
                        ) : (
                          <Text fw={650}>{stage.name}</Text>
                        )}
                        {stage.controlFlow ? (
                          <Badge size="xs" variant="light">
                            Control flow
                          </Badge>
                        ) : null}
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={2}>
                        <Text size="sm">{stage.enrichment?.application ?? "Kargo native"}</Text>
                        <Text c="dimmed" size="xs">
                          {stage.enrichment?.environment ?? "No environment metadata"}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <StageHealthBadge stage={stage} />
                    </Table.Td>
                    <Table.Td>
                      {currentFreight.length > 0 ? (
                        <Stack gap={3}>
                          {currentFreight.slice(0, 2).map((freight) => (
                            <Stack gap={0} key={`${freight.origin}:${freight.name}`}>
                              <Text fw={600} size="sm">
                                {freightDisplayLabel(freight)}
                              </Text>
                              <Text c="dimmed" size="xs">
                                {freightOriginLabel(freight.origin)}
                              </Text>
                            </Stack>
                          ))}
                          {currentFreight.length > 2 ? (
                            <Text c="dimmed" size="xs">
                              +{currentFreight.length - 2} more
                            </Text>
                          ) : null}
                        </Stack>
                      ) : (
                        <Text c="dimmed" size="sm">
                          None observed
                        </Text>
                      )}
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
                      <RelativeTime size="sm" value={stage.lastObservedAt} />
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" wrap="nowrap">
                        {canWrite ? (
                          <PromotionCreateButton
                            stage={stage}
                            freight={eligibleStageFreight(stage)}
                            environment={stage.enrichment?.environment}
                          />
                        ) : null}
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

function stageMatchesQuery(stage: PromotionProjectStage, query: string) {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return true;

  return [
    stage.name,
    stage.enrichment?.application,
    stage.enrichment?.environment,
    stage.enrichment?.repository,
    ...stage.requestedFreight.map((request) => request.origin.replace(/^Warehouse\//, "")),
    ...freightNames(stage),
  ].some((value) => value?.toLocaleLowerCase().includes(normalized));
}

function stageMatchesAlignment(
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

function stageMatchesAttention(
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

function StageHealthBadge({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
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
  const color = !editable ? "gray" : mode === "review-required" ? "yellow" : "green";
  if (!canEdit) {
    return (
      <Badge color={color} variant="light">
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
      type="button"
      variant="light"
    >
      {label}
    </Badge>
  );
}

function StageCard({
  canWrite,
  onCompareFreight,
  onEditPolicy,
  stage,
}: Readonly<{
  canWrite: boolean;
  onCompareFreight: (alignment: PromotionFreightAlignment) => void;
  onEditPolicy: () => void;
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
              {stage.kargoUrl ? (
                <KargoLink fw={700} url={stage.kargoUrl}>
                  {stage.name}
                </KargoLink>
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

        <StageFreight canWrite={canWrite} onCompareFreight={onCompareFreight} stage={stage} />

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
          <Group gap="xs">
            {canWrite ? (
              <PromotionCreateButton
                stage={stage}
                freight={eligibleStageFreight(stage)}
                environment={stage.enrichment?.environment}
              />
            ) : null}
          </Group>
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

function StageFreight({
  canWrite,
  onCompareFreight,
  stage,
}: Readonly<{
  canWrite: boolean;
  onCompareFreight: (alignment: PromotionFreightAlignment) => void;
  stage: PromotionProjectStage;
}>) {
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
          key: `${alignment.sourceStageId}:${freight.origin}:${freight.name}`,
          sourceStageId: alignment.sourceStageId,
          sourceStageName: source.name,
        },
      ];
    }),
  );

  return (
    <Stack gap="xs">
      {activeFreight.length > 0 ? (
        <Stack gap="xs">
          {activeFreight.map((freight) => (
            <FreightSummary active freight={freight} key={`${freight.origin}:${freight.name}`} />
          ))}
        </Stack>
      ) : (
        <Text c="dimmed" size="sm">
          No active Freight is currently observed.
        </Text>
      )}

      {canWrite && upstreamPromotions.length > 0 ? (
        <Stack gap="xs">
          <Text fw={600} size="sm">
            Promote from upstream
          </Text>
          {upstreamPromotions.map((promotion) => (
            <Paper key={promotion.key} p="sm" withBorder>
              <Group align="center" justify="space-between" wrap="wrap">
                <Stack gap={2}>
                  <Text fw={600} size="sm">
                    {promotion.sourceStageName}
                  </Text>
                  <Text c="dimmed" size="xs">
                    {freightDisplayLabel(promotion.freight)}
                  </Text>
                </Stack>
                <PromotionCreateButton
                  environment={stage.enrichment?.environment}
                  freight={[promotion.freight]}
                  initialFreightName={promotion.freight.name}
                  sourceStageId={promotion.sourceStageId}
                  sourceStageName={promotion.sourceStageName}
                  stage={stage}
                />
              </Group>
            </Paper>
          ))}
        </Stack>
      ) : null}

      {requests.length > 0 ? (
        <Accordion variant="contained">
          <Accordion.Item value="other-freight">
            <Accordion.Control>
              <Group gap="xs" wrap="wrap">
                <Text fw={600} size="sm">
                  Other Freight and source details
                </Text>
                <Badge color="gray" size="xs" variant="light">
                  {otherCount} option{otherCount === 1 ? "" : "s"}
                </Badge>
              </Group>
            </Accordion.Control>
            <Accordion.Panel>
              <Stack gap="sm">
                {requests.map(({ otherCandidates, request }) => (
                  <Paper key={request.origin} p="sm" withBorder>
                    <Stack gap="xs">
                      <Group gap="xs" wrap="wrap">
                        <Text fw={600} size="sm">
                          {freightOriginLabel(request.origin)}
                        </Text>
                        {request.direct ? <Badge size="xs">Direct</Badge> : null}
                        <Badge size="xs" variant="outline">
                          {request.availabilityStrategy}
                        </Badge>
                        {request.requiredSoakTime ? (
                          <Badge size="xs" variant="outline">
                            Soak {request.requiredSoakTime}
                          </Badge>
                        ) : null}
                      </Group>
                      {request.sources.map((source) => (
                        <FreightAlignmentSource
                          alignment={freightAlignmentForSource(stage, request.origin, source.name)}
                          key={source.name}
                          onCompareFreight={onCompareFreight}
                          sourceStageName={source.name}
                          targetStageName={stage.name}
                        />
                      ))}
                      {otherCandidates.length > 0 ? (
                        <Stack gap="xs">
                          {otherCandidates.map((candidate) => (
                            <FreightSummary
                              actionControl={
                                canWrite && request.direct && candidate.readiness === "ready" ? (
                                  <PromotionCreateButton
                                    stage={stage}
                                    freight={eligibleFreight}
                                    environment={stage.enrichment?.environment}
                                    initialFreightName={candidate.freight.name}
                                  />
                                ) : null
                              }
                              candidate={candidate}
                              freight={candidate.freight}
                              key={`${candidate.freight.origin}:${candidate.freight.name}`}
                            />
                          ))}
                        </Stack>
                      ) : (
                        <Text c="dimmed" size="xs">
                          No other Freight is currently observed for this source.
                        </Text>
                      )}
                    </Stack>
                  </Paper>
                ))}
              </Stack>
            </Accordion.Panel>
          </Accordion.Item>
        </Accordion>
      ) : null}
    </Stack>
  );
}

function FreightSummary({
  actionControl,
  active,
  candidate,
  freight,
}: Readonly<{
  actionControl?: ReactNode;
  active?: boolean;
  candidate?: PromotionFreightCandidate;
  freight: PromotionFreightItem;
}>) {
  return (
    <Paper p="sm" withBorder>
      <Stack gap="xs">
        <Group align="flex-start" justify="space-between" wrap="wrap">
          <Group gap="xs" wrap="wrap">
            {active ? (
              <Badge color="green" size="xs">
                Active
              </Badge>
            ) : null}
            {candidate ? <FreightReadinessBadge candidate={candidate} /> : null}
            <Text c="dimmed" size="xs">
              {freightOriginLabel(freight.origin)}
            </Text>
          </Group>
          {actionControl}
        </Group>

        {freight.artifacts.length > 0 ? (
          <Stack gap="xs">
            {freight.artifacts.map((artifact) => (
              <Group
                align="flex-end"
                justify="space-between"
                key={`${artifact.kind}:${artifact.repository ?? ""}:${artifact.version}`}
                wrap="wrap"
              >
                <Stack gap={2}>
                  <Group gap={5}>
                    <Text c="dimmed" size="xs">
                      Source
                    </Text>
                    <Badge size="xs" variant="light">
                      {artifact.kind}
                    </Badge>
                  </Group>
                  <Text fw={600} size="sm">
                    {artifact.repository ?? freightOriginLabel(freight.origin)}
                  </Text>
                </Stack>
                <Stack align="flex-end" gap={2}>
                  <Text c="dimmed" size="xs">
                    {freightArtifactVersionLabel(artifact.kind)}
                  </Text>
                  <Code>{artifact.version}</Code>
                </Stack>
              </Group>
            ))}
          </Stack>
        ) : (
          <Text c="dimmed" size="sm">
            This Freight does not report versioned artifacts.
          </Text>
        )}

        <Text c="dimmed" size="xs">
          Kargo Freight <Code>{freight.name}</Code>
        </Text>
        {candidate?.reason ? (
          <Text c="dimmed" size="xs">
            {candidate.reason}
          </Text>
        ) : null}
      </Stack>
    </Paper>
  );
}

function FreightReadinessBadge({ candidate }: Readonly<{ candidate: PromotionFreightCandidate }>) {
  return (
    <Badge
      color={
        candidate.readiness === "ready"
          ? "green"
          : candidate.readiness === "unknown"
            ? "gray"
            : "orange"
      }
      size="xs"
    >
      {candidate.readiness}
    </Badge>
  );
}

function FreightAlignmentSource({
  alignment,
  onCompareFreight,
  sourceStageName,
  targetStageName,
}: Readonly<{
  alignment?: PromotionFreightAlignment;
  onCompareFreight: (alignment: PromotionFreightAlignment) => void;
  sourceStageName: string;
  targetStageName: string;
}>) {
  const presentation = freightAlignmentPresentation(alignment?.status ?? "unknown");
  const sourceFreight = alignment?.sourceCurrentFreight;
  const targetFreight = alignment?.targetCurrentFreight;

  return (
    <Paper p="xs" withBorder>
      <Stack gap={4}>
        <Group justify="space-between" wrap="wrap">
          <Group gap="xs" wrap="wrap">
            <Badge color={alignment?.sourceStageId ? "blue" : "gray"} size="xs" variant="light">
              {sourceStageName}
            </Badge>
            <Badge color={presentation.color} size="xs" variant="light">
              {presentation.label}
            </Badge>
          </Group>
          {alignment?.sourceStageId ? (
            <Button
              leftSection={<IconArrowsDiff aria-hidden size={13} />}
              onClick={() => onCompareFreight(alignment)}
              size="compact-xs"
              variant="subtle"
            >
              Compare Freight
            </Button>
          ) : null}
        </Group>
        {alignment ? (
          <Text c="dimmed" size="xs">
            {sourceStageName}:{" "}
            {sourceFreight ? freightDisplayLabel(sourceFreight) : "none observed"} ·{" "}
            {targetStageName}:{" "}
            {targetFreight ? freightDisplayLabel(targetFreight) : "none observed"}
          </Text>
        ) : null}
        {alignment?.reason ? (
          <Text c="dimmed" size="xs">
            {alignment.reason}
          </Text>
        ) : null}
      </Stack>
    </Paper>
  );
}

function FreightComparison({
  selectedStageIds,
  selectedStages,
  setSelectedStageIds,
  stages,
}: Readonly<{
  selectedStageIds: string[];
  selectedStages: PromotionProjectStage[];
  setSelectedStageIds: (ids: string[]) => void;
  stages: PromotionProjectStage[];
}>) {
  const columns = buildFreightColumns(selectedStages);
  const rows = buildFreightRows(columns);

  return (
    <Paper id="freight-comparison" p="md" withBorder>
      <Stack gap="md">
        <Stack gap={3}>
          <Title order={2} size="h3">
            Compare Freight
          </Title>
          <Text c="dimmed" size="sm">
            Select any two or more Stages for an exploratory artifact comparison. Requested-source
            alignment above compares observed Kargo Freight identity; neither view describes Stage
            health or Git branch drift.
          </Text>
        </Stack>
        <Checkbox.Group onChange={setSelectedStageIds} value={selectedStageIds}>
          <Group gap="md" wrap="wrap">
            {stages.map((stage) => (
              <Checkbox key={stage.id} label={stage.name} value={stage.id} />
            ))}
          </Group>
        </Checkbox.Group>
        {selectedStages.length < 2 ? (
          <Text c="dimmed" size="sm">
            Choose at least two Stages to compare.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={720} type="native">
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Artifact</Table.Th>
                  {columns.map((column) => (
                    <Table.Th key={column.key}>{column.label}</Table.Th>
                  ))}
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((row) => (
                  <Table.Tr key={row.key}>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text fw={600} size="sm">
                          {row.origin}
                        </Text>
                        <Text c="dimmed" size="xs">
                          {row.kind}
                          {row.repository ? ` · ${row.repository}` : ""} ·{" "}
                          {freightRowStatus(row, columns) === "equal" ? "same" : "different"}
                        </Text>
                      </Stack>
                    </Table.Td>
                    {columns.map((column) => (
                      <Table.Td key={column.key}>
                        {(row.values[column.key] ?? []).map((version) => (
                          <Code key={version} mr={4}>
                            {version}
                          </Code>
                        ))}
                      </Table.Td>
                    ))}
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Stack>
    </Paper>
  );
}

function PolicyForm({
  actionData,
  onCancel,
  stage,
}: Readonly<{
  actionData: Route.ComponentProps["actionData"];
  onCancel: () => void;
  stage: PromotionProjectStage;
}>) {
  const navigation = useNavigation();
  const submitting = navigation.formData?.get("stageId") === stage.id;
  const policyActionData = actionData && "stageId" in actionData ? actionData : undefined;
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
