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
import { IconSettings } from "@tabler/icons-react";
import { useState } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getPromotionProject,
  updatePromotionStagePolicy,
  type PromotionPolicyMode,
  type PromotionProjectDetail,
  type PromotionProjectStage,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import {
  PromotionCreateButton,
  PromotionCreateDialog,
} from "~/features/promotions/promotion-create-dialog";
import {
  canWritePromotions,
  projectPromotionChoices,
  type PromotionChoice,
} from "~/features/promotions/promotion-creation";
import { submitPromotionCreation } from "~/features/promotions/promotion-creation.server";
import {
  buildFreightColumns,
  buildFreightRows,
  freightRowStatus,
} from "~/features/promotions/promotion-freight";
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
  const policyStage = project.stages.find((stage) => stage.id === searchParams.get("policy"));
  const [promotionChoices, setPromotionChoices] = useState<PromotionChoice[]>();
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
                    onPromote={(choices) => setPromotionChoices(choices)}
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

      {promotionChoices ? (
        <PromotionCreateDialog
          choices={promotionChoices}
          onClose={() => setPromotionChoices(undefined)}
        />
      ) : null}
    </Stack>
  );
}

function StageCard({
  canWrite,
  onEditPolicy,
  onPromote,
  stage,
}: Readonly<{
  canWrite: boolean;
  onEditPolicy: () => void;
  onPromote: (choices: PromotionChoice[]) => void;
  stage: PromotionProjectStage;
}>) {
  const choices = projectPromotionChoices(stage);
  const nonReady = stage.conditions.find(
    (condition) => condition.type === "Ready" && condition.status !== "True",
  );

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
                  <Group gap={5} wrap="wrap">
                    {request.sources.map((source) => (
                      <Badge
                        color={source.stageId ? "blue" : "orange"}
                        key={source.name}
                        size="xs"
                        variant="light"
                      >
                        {source.name}
                        {source.stageId ? "" : " · unresolved"}
                      </Badge>
                    ))}
                  </Group>
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
              color={stage.policy.effectiveMode === "review-required" ? "yellow" : "green"}
              variant="light"
            >
              {stage.policy.effectiveMode === "review-required"
                ? "Review required"
                : "Automatic policy"}
            </Badge>
            {stage.pendingPromotionCount > 0 ? (
              <Anchor component={Link} size="sm" to={`/promotions?stageId=${stage.id}`}>
                {stage.pendingPromotionCount} pending
              </Anchor>
            ) : null}
          </Group>
          <Group gap="xs">
            {canWrite ? (
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
                choices={choices}
                onClick={() => onPromote(choices)}
                unavailableLabel={stage.capabilities.issues.join(" ") || undefined}
              />
            ) : null}
          </Group>
        </Group>

        {stage.capabilities.issues.length > 0 ? (
          <Alert color="orange" title="Limited Slipramp capabilities">
            {stage.capabilities.issues.join(" ")}
          </Alert>
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
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Stack gap={3}>
          <Title order={2} size="h3">
            Compare Freight
          </Title>
          <Text c="dimmed" size="sm">
            Select two or more Stages. Different versions are shown neutrally and do not imply drift
            or unhealthy state.
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
            { label: "Review required", value: "review-required" },
          ]}
        />
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
