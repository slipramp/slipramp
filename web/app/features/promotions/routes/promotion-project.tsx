import {
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
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { IconArrowsDiff, IconSettings } from "@tabler/icons-react";
import { useState } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getPromotionProject,
  updatePromotionStagePolicy,
  type PromotionPolicyMode,
  type PromotionFreightAlignment,
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
  freightAlignmentForSource,
  freightAlignmentPresentation,
  freightComparisonStageIds,
  freightRowStatus,
} from "~/features/promotions/promotion-freight";
import { promotionStagePolicyPresentation } from "~/features/promotions/promotion-policy";
import { topologyLanes } from "~/features/promotions/promotion-topology";
import { KargoLink } from "~/features/promotions/kargo-link";
import { RelativeTime } from "~/shared/display-values";
import { formValue, optionalFormValue, problemDetail } from "~/shared/form-data";
import { PageHeader } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/promotion-project";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.project?.project ?? "Kargo project") }];
}

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
  const [searchParams, setSearchParams] = useSearchParams();
  const policyStage = project.stages.find(
    (stage) =>
      stage.id === searchParams.get("policy") && promotionStagePolicyPresentation(stage).editable,
  );
  const [selectedStageIds, setSelectedStageIds] = useState<string[]>([]);
  const selectedStages = project.stages.filter((stage) => selectedStageIds.includes(stage.id));
  const lanes = topologyLanes(project.stages);

  function closePolicy() {
    const next = new URLSearchParams(searchParams);
    next.delete("policy");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          project.kargoUrl ? <KargoLink url={project.kargoUrl}>Open in Kargo</KargoLink> : null
        }
        description={`${project.integration} · ${project.cluster}`}
        title={project.project}
      />

      <Stack gap="md">
        {lanes.map((lane, index) => (
          <Paper key={index} p="md" withBorder>
            <Stack gap="sm">
              <Text c="dimmed" fw={600} size="xs" tt="uppercase">
                Topology lane {index + 1}
              </Text>
              <SimpleGrid cols={{ base: 1, lg: Math.min(3, lane.length) }}>
                {lane.map((stage) => (
                  <StageCard
                    canWrite={canWrite}
                    key={stage.id}
                    onEditPolicy={() => {
                      const next = new URLSearchParams(searchParams);
                      next.set("policy", stage.id);
                      setSearchParams(next, { preventScrollReset: true });
                    }}
                    onCompareFreight={(alignment) => {
                      const ids = freightComparisonStageIds(alignment);
                      if (ids) setSelectedStageIds(ids);
                    }}
                    stage={stage}
                  />
                ))}
              </SimpleGrid>
            </Stack>
          </Paper>
        ))}
      </Stack>

      <FreightComparison
        selectedStageIds={selectedStageIds}
        selectedStages={selectedStages}
        setSelectedStageIds={setSelectedStageIds}
        stages={project.stages}
      />

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
  const nonReady = stage.conditions.find(
    (condition) => condition.type === "Ready" && condition.status !== "True",
  );
  const policyPresentation = promotionStagePolicyPresentation(stage);

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
            <Text c="dimmed" size="xs">
              UID {stage.kubernetesUid} · resource version {stage.resourceVersion}
            </Text>
          </Stack>
          <Stack align="flex-end" gap={5}>
            <Badge
              color={
                stage.observationStale
                  ? "gray"
                  : stage.healthStatus === "Healthy"
                    ? "green"
                    : stage.healthStatus
                      ? "red"
                      : "gray"
              }
              variant="light"
            >
              {stage.observationStale
                ? "Observation stale"
                : (stage.healthStatus ?? (nonReady ? "Not ready" : "Health unknown"))}
            </Badge>
            <RelativeTime size="xs" value={stage.lastObservedAt} />
          </Stack>
        </Group>

        {stage.enrichment ? (
          <Group gap="xs" wrap="wrap">
            {stage.enrichment.application ? (
              <Badge variant="outline">{stage.enrichment.application}</Badge>
            ) : null}
            {stage.enrichment.environment ? (
              <Badge variant="outline">{stage.enrichment.environment}</Badge>
            ) : null}
            {stage.enrichment.repository ? (
              <Badge variant="outline">{stage.enrichment.repository}</Badge>
            ) : null}
          </Group>
        ) : (
          <Text c="dimmed" size="sm">
            No Slipramp enrichment is attached.
          </Text>
        )}

        <Stack gap="xs">
          <Text fw={600} size="sm">
            Requested Freight
          </Text>
          {stage.requestedFreight.map((request) => (
            <Paper key={request.origin} p="xs" withBorder>
              <Stack gap={5}>
                <Group gap="xs" wrap="wrap">
                  <Text fw={600} size="sm">
                    {request.origin.replace(/^Warehouse\//, "")}
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
                {request.sources.length > 0 ? (
                  <Stack gap={5}>
                    {request.sources.map((source) => (
                      <FreightAlignmentSource
                        alignment={freightAlignmentForSource(stage, request.origin, source.name)}
                        key={source.name}
                        onCompareFreight={onCompareFreight}
                        sourceStageName={source.name}
                        targetStageName={stage.name}
                      />
                    ))}
                  </Stack>
                ) : null}
                {request.candidates.map((candidate) => (
                  <Group gap="xs" key={candidate.freight.name} wrap="wrap">
                    <Code>{candidate.freight.name}</Code>
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
                    {candidate.reason ? (
                      <Text c="dimmed" size="xs">
                        {candidate.reason}
                      </Text>
                    ) : null}
                  </Group>
                ))}
              </Stack>
            </Paper>
          ))}
        </Stack>

        <Group justify="space-between" wrap="wrap">
          <Group gap="xs">
            <Badge
              color={
                !policyPresentation.editable
                  ? "gray"
                  : stage.policy.effectiveMode === "review-required"
                    ? "yellow"
                    : "green"
              }
              variant="light"
            >
              {policyPresentation.label}
            </Badge>
            {stage.pendingPromotionCount > 0 ? (
              <Anchor component={Link} size="sm" to={`/promotions?stageId=${stage.id}`}>
                {stage.pendingPromotionCount} pending
              </Anchor>
            ) : null}
          </Group>
          <Group gap="xs">
            {canWrite && policyPresentation.editable ? (
              <Button
                leftSection={<IconSettings size={14} />}
                onClick={onEditPolicy}
                size="compact-xs"
                variant="default"
              >
                Policy
              </Button>
            ) : null}
            {canWrite ? (
              <PromotionCreateButton
                stage={stage}
                freight={eligibleStageFreight(stage)}
                environment={stage.enrichment?.environment}
              />
            ) : null}
          </Group>
        </Group>

        {stage.capabilities.review.status === "unavailable" ||
        stage.capabilities.branchDrift.status === "unavailable" ? (
          <Alert color="orange" title="Limited Slipramp capabilities">
            <Stack gap={4}>
              {stage.capabilities.review.reason ? (
                <Text size="sm">{stage.capabilities.review.reason}</Text>
              ) : null}
              {stage.capabilities.branchDrift.reason ? (
                <Text size="sm">{stage.capabilities.branchDrift.reason}</Text>
              ) : null}
            </Stack>
          </Alert>
        ) : null}
      </Stack>
    </Paper>
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
  const sourceFreight = alignment?.sourceCurrentFreight?.name ?? "none observed";
  const targetFreight = alignment?.targetCurrentFreight?.name ?? "none observed";

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
            {sourceStageName}: <Code>{sourceFreight}</Code> · {targetStageName}:{" "}
            <Code>{targetFreight}</Code>
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
