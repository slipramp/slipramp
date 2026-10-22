import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  NativeSelect,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, Link, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getCurrentPrincipal,
  listPromotionStages,
  updatePromotionStagePolicy,
  type ProductPrincipal,
  type PromotionStage,
  type PromotionPolicyMode,
} from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/promotion-stages";

const stageLimit = 200;

export function meta() {
  return [{ title: pageTitle("Promotion Stages") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const archivedFilter = url.searchParams.get("archived") ?? "active";
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const client = createServerAPIClient(request);
  const [stagesResult, principalResult] = await Promise.all([
    listPromotionStages({
      client,
      query: {
        archived: archivedFilter === "all" ? undefined : archivedFilter === "archived",
        integration,
        limit: stageLimit,
        project,
      },
    }),
    getCurrentPrincipal({ client }),
  ]);

  return {
    archivedFilter,
    available: stagesResult.data !== undefined,
    error: stagesResult.error?.detail ?? stagesResult.error?.title,
    integration,
    principal: principalResult.data,
    project,
    stages: stagesResult.data?.items ?? [],
    total: stagesResult.data?.total ?? 0,
  };
}

export async function action({ request }: Route.ActionArgs) {
  const formData = await request.formData();
  const stageId = formValue(formData, "stageId");
  const mode = formValue(formData, "mode") as PromotionPolicyMode;
  const reason = optionalValue(formValue(formData, "reason"));
  const expiresAtValue = optionalValue(formValue(formData, "expiresAt"));
  const result = await updatePromotionStagePolicy({
    body: {
      mode,
      reason,
      expiresAt: mode === "review-required" ? expiresAtValue : undefined,
    },
    client: createServerAPIClient(request),
    path: { stageId },
  });

  return result.data
    ? { message: "The Stage policy was updated." }
    : {
        error:
          result.error?.detail ?? result.error?.title ?? "The Stage policy could not be updated.",
      };
}

export default function PromotionStages({ actionData, loaderData }: Route.ComponentProps) {
  const canWrite = canWritePromotions(loaderData.principal);

  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Promotion Stages</Title>
          <Text c="dimmed">Observed Kargo inventory and review policy.</Text>
        </Stack>
        <Anchor component={Link} to="/promotions">
          View Promotions
        </Anchor>
      </Group>

      {actionData?.error ? (
        <Alert color="red" title="Policy update failed">
          {actionData.error}
        </Alert>
      ) : null}
      {actionData?.message ? (
        <Alert color="green" title="Policy updated">
          {actionData.message}
        </Alert>
      ) : null}

      <StageFilters
        archivedFilter={loaderData.archivedFilter}
        integration={loaderData.integration}
        project={loaderData.project}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Promotion Stages unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.stages.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No observed Stages match these filters.</Text>
        </Paper>
      ) : (
        <StageTable canWrite={canWrite} stages={loaderData.stages} />
      )}

      {loaderData.available && loaderData.total > loaderData.stages.length ? (
        <Text c="dimmed" size="sm">
          Showing the first {loaderData.stages.length} of {loaderData.total} matching Stages.
        </Text>
      ) : null}
    </Stack>
  );
}

function StageFilters({
  archivedFilter,
  integration,
  project,
}: Readonly<{ archivedFilter: string; integration?: string; project?: string }>) {
  return (
    <Paper p="md" withBorder>
      <Form method="get">
        <Group align="flex-end">
          <NativeSelect
            defaultValue={archivedFilter}
            label="Inventory"
            name="archived"
            data={[
              { label: "Active Stages", value: "active" },
              { label: "Archived Stages", value: "archived" },
              { label: "All Stages", value: "all" },
            ]}
          />
          <TextInput defaultValue={integration} label="Integration" name="integration" />
          <TextInput defaultValue={project} label="Project" name="project" />
          <Button type="submit" variant="light">
            Apply filters
          </Button>
        </Group>
      </Form>
    </Paper>
  );
}

function StageTable({
  canWrite,
  stages,
}: Readonly<{ canWrite: boolean; stages: PromotionStage[] }>) {
  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={1400}>
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Stage</Table.Th>
              <Table.Th>Application</Table.Th>
              <Table.Th>Freight</Table.Th>
              <Table.Th>Observation</Table.Th>
              <Table.Th>Reviews</Table.Th>
              <Table.Th>Policy</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {stages.map((stage) => (
              <StageRow canWrite={canWrite} key={stage.id} stage={stage} />
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function StageRow({ canWrite, stage }: Readonly<{ canWrite: boolean; stage: PromotionStage }>) {
  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <Group gap="xs">
            <Text fw={600}>{stage.name}</Text>
            {stage.archivedAt ? (
              <Badge color="gray" size="sm" variant="light">
                Archived
              </Badge>
            ) : null}
          </Group>
          <Text c="dimmed" size="xs">
            {stage.integration} · {stage.cluster} · {stage.project}
          </Text>
          <Text c="dimmed" size="xs">
            {stage.repository}:{stage.targetBranch}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Text>{stage.application}</Text>
          <Text c="dimmed" size="xs">
            {stage.environment}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Code block maw={320} style={{ maxHeight: 120, overflow: "auto" }}>
          {JSON.stringify(stage.freight, null, 2)}
        </Code>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Text component="time" dateTime={stage.lastObservedAt} size="sm">
            {formatTimestamp(stage.lastObservedAt)}
          </Text>
          <Text c="dimmed" size="xs">
            Resource version {stage.resourceVersion}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Anchor component={Link} to={`/promotions?stageId=${stage.id}`}>
          {stage.pendingPromotionCount} pending
        </Anchor>
      </Table.Td>
      <Table.Td>
        {canWrite && !stage.archivedAt ? (
          <PolicyForm stage={stage} />
        ) : (
          <PolicySummary stage={stage} />
        )}
      </Table.Td>
    </Table.Tr>
  );
}

function PolicyForm({ stage }: Readonly<{ stage: PromotionStage }>) {
  const navigation = useNavigation();
  const submittingStage = navigation.formData?.get("stageId") === stage.id;

  return (
    <Form method="post">
      <Stack gap="xs" miw={300}>
        <input name="stageId" type="hidden" value={stage.id} />
        <NativeSelect
          defaultValue={stage.policy.configuredMode}
          label="Mode"
          name="mode"
          size="xs"
          data={[
            { label: "Automatic", value: "automatic" },
            { label: "Review required", value: "review-required" },
          ]}
        />
        <TextInput
          defaultValue={stage.policy.expiresAt}
          description="Optional RFC 3339 time for review-required mode"
          label="Expires at"
          name="expiresAt"
          size="xs"
        />
        <TextInput
          defaultValue={stage.policy.reason}
          label="Reason"
          maxLength={4096}
          name="reason"
          size="xs"
        />
        <Group justify="space-between">
          <Badge variant="light">Effective: {policyLabel(stage.policy.effectiveMode)}</Badge>
          <Button loading={submittingStage} size="compact-xs" type="submit" variant="light">
            Save
          </Button>
        </Group>
      </Stack>
    </Form>
  );
}

function PolicySummary({ stage }: Readonly<{ stage: PromotionStage }>) {
  return (
    <Stack gap={2}>
      <Badge
        color={stage.policy.effectiveMode === "review-required" ? "yellow" : "green"}
        variant="light"
      >
        {policyLabel(stage.policy.effectiveMode)}
      </Badge>
      {stage.policy.configuredMode !== stage.policy.effectiveMode ? (
        <Text c="dimmed" size="xs">
          Configured {policyLabel(stage.policy.configuredMode)}; policy expired
        </Text>
      ) : null}
      {stage.policy.reason ? <Text size="xs">{stage.policy.reason}</Text> : null}
      {stage.policy.updatedBy ? (
        <Text c="dimmed" size="xs">
          Updated by {stage.policy.updatedBy.displayName}
        </Text>
      ) : null}
    </Stack>
  );
}

function policyLabel(mode: PromotionPolicyMode) {
  return mode === "review-required" ? "Review required" : "Automatic";
}

function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}

function formValue(formData: FormData, name: string) {
  const value = formData.get(name);
  return typeof value === "string" ? value.trim() : "";
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}
