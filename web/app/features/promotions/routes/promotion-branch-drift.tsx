import { Alert, Badge, Group, Paper, Stack, Table, Text, Title, Tooltip } from "@mantine/core";
import { redirect, useSearchParams, type ShouldRevalidateFunctionArgs } from "react-router";
import { createServerAPIClient } from "~/api/client.server";
import {
  listPromotionBranchDrift,
  listRepositories,
  type PromotionBranchDriftRepository,
  type PromotionBranchDriftStage,
  type PromotionBranchDriftStatus,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { RelativeTime } from "~/shared/display-values";
import { ListPagination, SearchParamFilters } from "~/shared/list-controls";
import { PageHeader } from "~/shared/page";
import { PromotionCreateButton } from "~/features/promotions/promotion-create-dialog";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { StagePolicyDialog } from "~/features/promotions/promotion-project-view";
import { promotionStagePolicyPresentation } from "~/features/promotions/promotion-policy";
import { promotionScopeFilters } from "~/features/promotions/promotion-scope-filters";
import { pageTitle } from "~/shared/page-title";
import { pageFromSearchParam } from "~/shared/search-params";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { BranchDriftCommit } from "~/features/promotions/branch-drift-commit";

import type { Route } from "./+types/promotion-branch-drift";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Promotion branch drift") }];
}

export function shouldRevalidate({
  currentUrl,
  defaultShouldRevalidate,
  formMethod,
  nextUrl,
}: ShouldRevalidateFunctionArgs) {
  const loaderParamNames = [
    "application",
    "integration",
    "page",
    "project",
    "repository",
    "status",
  ];
  const loaderStateChanged = loaderParamNames.some(
    (name) => currentUrl.searchParams.get(name) !== nextUrl.searchParams.get(name),
  );
  if (formMethod === undefined && currentUrl.pathname === nextUrl.pathname && !loaderStateChanged) {
    return false;
  }
  return defaultShouldRevalidate;
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const project = optionalValue(url.searchParams.get("project"));
  const application = optionalValue(url.searchParams.get("application"));
  const repository = optionalValue(url.searchParams.get("repository"));
  const status = optionalStatus(url.searchParams.get("status"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const scopeQuery = { application, integration, limit: 1, project, repository };
  const [result, scopeResult, principal, repositoriesResult] = await Promise.all([
    listPromotionBranchDrift({
      client,
      query: {
        application,
        integration,
        limit: pageSize,
        offset: (requestedPage - 1) * pageSize,
        project,
        repository,
        status,
      },
    }),
    listPromotionBranchDrift({ client, query: scopeQuery }),
    getProductPrincipal(context),
    listRepositories({ client, query: { limit: 200 } }),
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
    available: result.data !== undefined,
    application,
    driftCapableTotal: scopeResult.data?.total,
    error: result.error?.detail ?? result.error?.title,
    integration,
    page: requestedPage,
    project,
    principal,
    repositories: result.data?.items ?? [],
    repositoryIds: Object.fromEntries(
      (repositoriesResult.data?.items ?? []).map((item) => [item.name, item.id]),
    ),
    repository,
    status,
    total: result.data?.total ?? 0,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  return submitPromotionProjectAction(request, context);
}

export default function PromotionBranchDrift({ actionData, loaderData }: Route.ComponentProps) {
  useActivityRefresh(true, 30_000);

  const [searchParams, setSearchParams] = useSearchParams();
  const canWrite = canWritePromotions(loaderData.principal);
  const policyStage = loaderData.repositories
    .flatMap((repository) => repository.stages)
    .find(
      (stage) =>
        stage.id === searchParams.get("policy") && promotionStagePolicyPresentation(stage).editable,
    );

  function closePolicy() {
    const next = new URLSearchParams(searchParams);
    next.delete("policy");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  return (
    <Stack gap="lg">
      <PageHeader
        description="Cached source-path and target-branch comparisons from successful Kargo renders."
        title="Promotion branch drift"
      />

      <BranchDriftFilters
        application={loaderData.application}
        integration={loaderData.integration}
        project={loaderData.project}
        repository={loaderData.repository}
        status={loaderData.status}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Promotion branch drift unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.repositories.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">
            {loaderData.driftCapableTotal === 0
              ? "No branch-drift-capable Kargo Stages match these filters."
              : loaderData.status === "drifted"
                ? "No Stages are currently reported behind."
                : "No branch-drift-capable Kargo Stages match the selected status."}
          </Text>
        </Paper>
      ) : (
        loaderData.repositories.map((repository) => (
          <BranchDriftRepository
            canWrite={canWrite}
            key={repository.repository}
            repository={repository}
            repositoryId={loaderData.repositoryIds[repository.repository]}
          />
        ))
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.repositories.reduce(
            (count, repository) => count + repository.stages.length,
            0,
          )}
          noun="Stages"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}

      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
}

function BranchDriftFilters({
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
  status?: PromotionBranchDriftStatus;
}>) {
  return (
    <SearchParamFilters
      clearTo="/promotion-branch-drift"
      filters={[
        ...promotionScopeFilters({ application, integration, project }),
        {
          active: Boolean(status),
          data: [
            { label: "All statuses", value: "" },
            { label: "In sync", value: "current" },
            { label: "Behind", value: "drifted" },
            { label: "Unknown", value: "unknown" },
          ],
          kind: "select",
          label: "Status",
          name: "status",
          value: status,
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

function BranchDriftRepository({
  canWrite,
  repository,
  repositoryId,
}: Readonly<{
  canWrite: boolean;
  repository: PromotionBranchDriftRepository;
  repositoryId?: string;
}>) {
  return (
    <Paper withBorder>
      <Stack gap={0}>
        <Group justify="space-between" p="md">
          <Stack gap={0}>
            <Title order={2} size="h3">
              {repository.repository}
            </Title>
            <Text c="dimmed" size="sm">
              Source branch {repository.sourceBranch ?? "unavailable"}
            </Text>
          </Stack>
          <Group gap={4}>
            <Text c="dimmed" size="sm">
              Scanned
            </Text>
            <RelativeTime c="dimmed" size="sm" value={repository.scannedAt} />
          </Group>
        </Group>
        {repository.error ? (
          <Alert color="red" m="md" mt={0} title="Repository scan failed">
            {repository.error}
          </Alert>
        ) : null}
        <Table.ScrollContainer minWidth={840} type="native">
          <Table horizontalSpacing="md" verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Stage</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th miw={210}>Source updated</Table.Th>
                <Table.Th miw={210}>Last rendered</Table.Th>
                <Table.Th aria-label="Actions" />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {repository.stages.map((stage) => (
                <BranchDriftRow
                  canWrite={canWrite}
                  key={stage.id}
                  repositoryId={repositoryId}
                  sourceBranch={repository.sourceBranch}
                  stage={stage}
                />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Stack>
    </Paper>
  );
}

function BranchDriftRow({
  canWrite,
  sourceBranch,
  stage,
  repositoryId,
}: Readonly<{
  canWrite: boolean;
  repositoryId?: string;
  sourceBranch?: string;
  stage: PromotionBranchDriftStage;
}>) {
  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <PromotionStageLink stage={stage} />
          {stage.application || stage.environment ? (
            <Text size="sm">
              {[stage.application, stage.environment].filter(Boolean).join(" · ")}
            </Text>
          ) : null}
          <Text c="dimmed" size="xs">
            {stage.integration} · {stage.cluster} · {stage.project}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        {stage.message ? (
          <Tooltip label={stage.message} maw={300} multiline withArrow>
            <Badge
              aria-label={`${statusLabel(stage.status)}: ${stage.message}`}
              color={statusColor(stage.status)}
              variant="light"
            >
              {statusLabel(stage.status)}
            </Badge>
          </Tooltip>
        ) : (
          <Badge color={statusColor(stage.status)} variant="light">
            {statusLabel(stage.status)}
          </Badge>
        )}
      </Table.Td>
      <Table.Td style={{ whiteSpace: "nowrap" }}>
        <BranchDriftCommit
          branch={sourceBranch}
          commit={stage.sourceBranchCommit}
          repositoryId={repositoryId}
        />
      </Table.Td>
      <Table.Td style={{ whiteSpace: "nowrap" }}>
        <BranchDriftCommit
          branch={stage.targetBranch}
          commit={stage.acknowledgedRenderCommit}
          repositoryId={repositoryId}
        />
      </Table.Td>
      <Table.Td ta="right">
        {canWrite && stage.status === "drifted" ? (
          <PromotionCreateButton
            stage={stage}
            freight={stage.freight.items}
            inPlace
            unavailableLabel="This Stage has no current Freight to promote in place."
          />
        ) : null}
      </Table.Td>
    </Table.Tr>
  );
}

function statusColor(status: PromotionBranchDriftStage["status"]) {
  switch (status) {
    case "current":
      return "green";
    case "drifted":
      return "yellow";
    default:
      return "gray";
  }
}

function statusLabel(status: PromotionBranchDriftStage["status"]) {
  switch (status) {
    case "current":
      return "In sync";
    case "drifted":
      return "Behind";
    default:
      return "Unknown";
  }
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}

function optionalStatus(value: string | null): PromotionBranchDriftStatus | undefined {
  switch (value) {
    case "current":
    case "drifted":
    case "unknown":
      return value;
    default:
      return undefined;
  }
}
