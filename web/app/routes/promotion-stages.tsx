import {
  Alert,
  Anchor,
  Badge,
  Button,
  Group,
  Modal,
  NativeSelect,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconSettings } from "@tabler/icons-react";
import { useCallback, useEffect, useRef } from "react";
import { Form, Link, redirect, useNavigation, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getCurrentPrincipal,
  listPromotionStages,
  updatePromotionStagePolicy,
  type ProductPrincipal,
  type PromotionStage,
  type PromotionPolicyMode,
} from "~/api/generated";
import { RelativeTime } from "~/components/display";
import { Identity } from "~/components/identity";
import { KargoLink } from "~/components/kargo-link";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { promotionScopeFilters } from "~/components/promotion-scope-filters";
import { abbreviateMiddle } from "~/display";
import { formValue, optionalFormValue } from "~/form-data";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/promotion-stages";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Promotion stages") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const archivedFilter = url.searchParams.get("archived") ?? "active";
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const application = optionalValue(url.searchParams.get("application"));
  const cluster = optionalValue(url.searchParams.get("cluster"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const [stagesResult, principalResult] = await Promise.all([
    listPromotionStages({
      client,
      query: {
        archived: archivedFilter === "all" ? undefined : archivedFilter === "archived",
        application,
        cluster,
        integration,
        limit: pageSize,
        offset: (requestedPage - 1) * pageSize,
        project,
      },
    }),
    getCurrentPrincipal({ client }),
  ]);

  if (stagesResult.data) {
    const lastPage = Math.max(1, Math.ceil(stagesResult.data.total / pageSize));
    if (requestedPage > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    archivedFilter,
    application,
    available: stagesResult.data !== undefined,
    cluster,
    error: stagesResult.error?.detail ?? stagesResult.error?.title,
    integration,
    page: requestedPage,
    principal: principalResult.data,
    project,
    stages: stagesResult.data?.items ?? [],
    total: stagesResult.data?.total ?? 0,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const stageId = formValue(formData, "stageId");
  const mode = formValue(formData, "mode") as PromotionPolicyMode;
  const reason = optionalFormValue(formData, "reason");
  const expiresAtValue = optionalFormValue(formData, "expiresAt");
  const result = await updatePromotionStagePolicy({
    body: {
      mode,
      reason,
      expiresAt: mode === "review-required" ? expiresAtValue : undefined,
    },
    client: createServerAPIClient(request, context),
    path: { stageId },
  });

  return result.data
    ? { message: "The Stage policy was updated.", stageId }
    : {
        error:
          result.error?.detail ?? result.error?.title ?? "The Stage policy could not be updated.",
        stageId,
      };
}

export default function PromotionStages({ actionData, loaderData }: Route.ComponentProps) {
  const canWrite = canWritePromotions(loaderData.principal);

  return (
    <Stack gap="lg">
      <PageHeader
        description="Observed Kargo inventory and review policy."
        title="Promotion stages"
      />

      <StageFilters
        archivedFilter={loaderData.archivedFilter}
        application={loaderData.application}
        cluster={loaderData.cluster}
        integration={loaderData.integration}
        project={loaderData.project}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Promotion stages unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.stages.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No observed stages match these filters.</Text>
        </Paper>
      ) : (
        <StageTable actionData={actionData} canWrite={canWrite} stages={loaderData.stages} />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.stages.length}
          noun="Stages"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function StageFilters({
  archivedFilter,
  application,
  cluster,
  integration,
  project,
}: Readonly<{
  archivedFilter: string;
  application?: string;
  cluster?: string;
  integration?: string;
  project?: string;
}>) {
  return (
    <SearchParamFilters
      clearTo="/promotion-stages"
      filters={[
        ...promotionScopeFilters({ application, integration, project }),
        {
          active: archivedFilter !== "active",
          data: [
            { label: "Active Stages", value: "" },
            { label: "Archived Stages", value: "archived" },
            { label: "All Stages", value: "all" },
          ],
          kind: "select",
          label: "Status",
          name: "archived",
          value: archivedFilter === "active" ? "" : archivedFilter,
        },
        {
          active: Boolean(cluster),
          kind: "text",
          label: "Cluster",
          name: "cluster",
          value: cluster,
        },
      ]}
    />
  );
}

function StageTable({
  actionData,
  canWrite,
  stages,
}: Readonly<{
  actionData: Route.ComponentProps["actionData"];
  canWrite: boolean;
  stages: PromotionStage[];
}>) {
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedStageId = searchParams.get("policy") ?? undefined;
  const selectedStage = stages.find((stage) => stage.id === selectedStageId);
  const handledActionData = useRef<unknown>(actionData);

  const setSelectedStageId = useCallback(
    (stageId: string | undefined, replace = false) => {
      const nextSearchParams = new URLSearchParams(searchParams);
      if (stageId) {
        nextSearchParams.set("policy", stageId);
      } else {
        nextSearchParams.delete("policy");
      }
      setSearchParams(nextSearchParams, { preventScrollReset: true, replace });
    },
    [searchParams, setSearchParams],
  );

  useEffect(() => {
    if (!actionData || handledActionData.current === actionData) {
      return;
    }

    handledActionData.current = actionData;
    if (actionData.stageId === selectedStageId && actionData.message) {
      notifications.show({
        color: "green",
        message: actionData.message,
        title: "Policy updated",
      });
      setSelectedStageId(undefined, true);
    }
  }, [actionData, selectedStageId, setSelectedStageId]);

  return (
    <>
      <Paper withBorder>
        <Table.ScrollContainer minWidth={1060} type="native">
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Stage</Table.Th>
                <Table.Th>Application</Table.Th>
                <Table.Th>Freight</Table.Th>
                <Table.Th>Activity</Table.Th>
                <Table.Th>Policy</Table.Th>
                <Table.Th aria-label="Actions" />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {stages.map((stage) => (
                <StageRow
                  canWrite={canWrite}
                  key={stage.id}
                  onEditPolicy={() => setSelectedStageId(stage.id)}
                  stage={stage}
                />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>

      <Modal
        centered
        onClose={() => setSelectedStageId(undefined, true)}
        opened={selectedStage !== undefined}
        size="lg"
        title={selectedStage ? `Policy · ${selectedStage.name}` : "Stage policy"}
      >
        {selectedStage ? (
          <Stack gap="md">
            <Text c="dimmed" size="sm">
              {selectedStage.application} · {selectedStage.environment} · {selectedStage.cluster}
            </Text>
            {actionData?.stageId === selectedStage.id && actionData.error ? (
              <Alert color="red" title="Policy update failed">
                {actionData.error}
              </Alert>
            ) : null}
            <PolicyForm
              onCancel={() => setSelectedStageId(undefined, true)}
              stage={selectedStage}
            />
          </Stack>
        ) : null}
      </Modal>
    </>
  );
}

function StageRow({
  canWrite,
  onEditPolicy,
  stage,
}: Readonly<{ canWrite: boolean; onEditPolicy: () => void; stage: PromotionStage }>) {
  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <Group gap="xs">
            {stage.kargoUrl ? (
              <KargoLink fw={600} size="sm" url={stage.kargoUrl}>
                {stage.name}
              </KargoLink>
            ) : (
              <Text fw={600}>{stage.name}</Text>
            )}
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
        <FreightSummary stage={stage} />
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <RelativeTime size="sm" value={stage.lastObservedAt} />
          {stage.pendingPromotionCount > 0 ? (
            <Text c="dimmed" size="xs">
              {stage.pendingPromotionCount} pending review
            </Text>
          ) : null}
        </Stack>
      </Table.Td>
      <Table.Td>
        <PolicySummary stage={stage} />
      </Table.Td>
      <Table.Td ta="right">
        {canWrite && !stage.archivedAt ? (
          <Button
            leftSection={<IconSettings size={14} />}
            onClick={onEditPolicy}
            size="compact-xs"
            variant="light"
          >
            Edit policy
          </Button>
        ) : null}
      </Table.Td>
    </Table.Tr>
  );
}

function FreightSummary({ stage }: Readonly<{ stage: PromotionStage }>) {
  if (stage.freight.items.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        No Freight reported
      </Text>
    );
  }

  return (
    <Stack gap={4} maw={360}>
      {stage.freight.items.slice(0, 3).map((item) => (
        <Stack gap={3} key={`${item.origin}:${item.name}`}>
          <Group gap={4}>
            {item.artifacts.length === 0 ? (
              <Badge color="gray" size="sm" variant="light">
                No artifacts
              </Badge>
            ) : (
              item.artifacts.map((artifact) => (
                <Badge
                  key={`${artifact.kind}:${artifact.repository ?? ""}:${artifact.version}`}
                  size="sm"
                  title={`Freight ${item.name} · ${artifact.kind}${artifact.repository ? ` · ${artifact.repository}` : ""}`}
                  variant="light"
                >
                  {abbreviateMiddle(artifact.version, 20)}
                </Badge>
              ))
            )}
          </Group>
          <Text c="dimmed" lineClamp={1} size="xs" title={item.origin}>
            {item.origin.replace(/^Warehouse\//, "")}
          </Text>
        </Stack>
      ))}
      {stage.freight.items.length > 3 ? (
        <Text c="dimmed" size="xs">
          +{stage.freight.items.length - 3} more
        </Text>
      ) : null}
      <Anchor component={Link} size="xs" to={`/promotions?stageId=${stage.id}`}>
        View promotion history
      </Anchor>
    </Stack>
  );
}

function PolicyForm({
  onCancel,
  stage,
}: Readonly<{ onCancel: () => void; stage: PromotionStage }>) {
  const navigation = useNavigation();
  const submittingStage = navigation.formData?.get("stageId") === stage.id;

  return (
    <Form method="post">
      <Stack gap="md">
        <input name="stageId" type="hidden" value={stage.id} />
        <NativeSelect
          defaultValue={stage.policy.configuredMode}
          label="Mode"
          name="mode"
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
        />
        <TextInput
          defaultValue={stage.policy.reason}
          label="Reason"
          maxLength={4096}
          name="reason"
        />
        <Group justify="space-between">
          <Badge variant="light">Effective: {policyLabel(stage.policy.effectiveMode)}</Badge>
          <Group gap="sm">
            <Button onClick={onCancel} type="button" variant="default">
              Cancel
            </Button>
            <Button loading={submittingStage} type="submit">
              Save policy
            </Button>
          </Group>
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
        <Group gap={4}>
          <Text c="dimmed" size="xs">
            Updated by
          </Text>
          <Identity
            avatarSize={20}
            name={stage.policy.updatedBy.displayName}
            nameProps={{ c: "dimmed", fw: 500, size: "xs" }}
          />
        </Group>
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
