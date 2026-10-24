import { Alert, Badge, Code, Group, Paper, Stack, Table, Text, Title } from "@mantine/core";
import { useState } from "react";
import { redirect } from "react-router";
import { createServerAPIClient } from "~/api/client.server";
import {
  getCurrentPrincipal,
  listPromotionBranchDrift,
  listRepositories,
  type GitCommit,
  type PromotionBranchDriftRepository,
  type PromotionBranchDriftStage,
  type PromotionBranchDriftStatus,
} from "~/api/generated";
import { RelativeTime, Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { KargoLink } from "~/components/kargo-link";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { PromotionCreateButton, PromotionCreateDialog } from "~/components/promotion-create-dialog";
import { promotionScopeFilters } from "~/components/promotion-scope-filters";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";
import {
  canWritePromotions,
  inPlacePromotionChoices,
  type PromotionChoice,
} from "~/promotion-creation";
import { submitPromotionCreation } from "~/promotion-creation.server";

import type { Route } from "./+types/promotion-branch-drift";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Promotion branch drift") }];
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
  const [result, principalResult, repositoriesResult] = await Promise.all([
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
    getCurrentPrincipal({ client }),
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
    error: result.error?.detail ?? result.error?.title,
    integration,
    page: requestedPage,
    project,
    principal: principalResult.data,
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
  return submitPromotionCreation(request, context);
}

export default function PromotionBranchDrift({ loaderData }: Route.ComponentProps) {
  const canWrite = canWritePromotions(loaderData.principal);

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
            {loaderData.status === "drifted"
              ? "All Kargo Stage branches are in sync."
              : "No active Kargo Stages match these filters."}
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
  const sharedSourcePaths = commonSourcePaths(repository.stages);

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
            {sharedSourcePaths.length > 0 ? (
              <Group gap="xs" mt={4} wrap="wrap">
                <Text c="dimmed" size="xs">
                  {sharedSourcePaths.length === 1
                    ? "Shared watch path on this page"
                    : "Shared watch paths on this page"}
                </Text>
                {sharedSourcePaths.map((path) => (
                  <Text ff="monospace" key={path} size="xs">
                    {path}
                  </Text>
                ))}
              </Group>
            ) : null}
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
                  sharedSourcePaths={sharedSourcePaths}
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
  sharedSourcePaths,
  sourceBranch,
  stage,
  repositoryId,
}: Readonly<{
  canWrite: boolean;
  repositoryId?: string;
  sharedSourcePaths: string[];
  sourceBranch?: string;
  stage: PromotionBranchDriftStage;
}>) {
  const stageSpecificPaths = stage.sourcePaths.filter((path) => !sharedSourcePaths.includes(path));
  const choices = inPlacePromotionChoices(stage);
  const [dialogChoices, setDialogChoices] = useState<PromotionChoice[] | undefined>();

  return (
    <>
      <Table.Tr>
        <Table.Td>
          <Stack gap={0}>
            {stage.kargoUrl ? (
              <KargoLink fw={600} size="sm" url={stage.kargoUrl}>
                {stage.name}
              </KargoLink>
            ) : (
              <Text fw={600}>{stage.name}</Text>
            )}
            <Text size="sm">
              {stage.application} · {stage.environment}
            </Text>
            <Text c="dimmed" size="xs">
              {stage.integration} · {stage.cluster} · {stage.project}
            </Text>
            {stageSpecificPaths.length > 0 ? (
              <Group gap={4} mt={4} wrap="wrap">
                {stageSpecificPaths.map((path) => (
                  <Code key={path}>{path}</Code>
                ))}
              </Group>
            ) : null}
          </Stack>
        </Table.Td>
        <Table.Td>
          <Stack align="flex-start" gap="xs">
            <Badge color={statusColor(stage.status)} variant="light">
              {statusLabel(stage.status)}
            </Badge>
            {stage.message ? (
              <Text c="dimmed" maw={300} size="xs">
                {stage.message}
              </Text>
            ) : null}
          </Stack>
        </Table.Td>
        <Table.Td>
          <CommitMoment
            branch={sourceBranch}
            commit={stage.sourceBranchCommit}
            repositoryId={repositoryId}
          />
        </Table.Td>
        <Table.Td>
          <CommitMoment
            branch={stage.targetBranch}
            commit={stage.acknowledgedRenderCommit}
            repositoryId={repositoryId}
          />
        </Table.Td>
        <Table.Td ta="right">
          {canWrite && stage.status === "drifted" ? (
            <PromotionCreateButton
              choices={choices}
              onClick={() => setDialogChoices(choices)}
              unavailableLabel="This Stage has no current Freight to promote in place."
            />
          ) : null}
        </Table.Td>
      </Table.Tr>
      {dialogChoices ? (
        <PromotionCreateDialog
          choices={dialogChoices}
          onClose={() => setDialogChoices(undefined)}
        />
      ) : null}
    </>
  );
}

function CommitMoment({
  branch,
  commit,
  repositoryId,
}: Readonly<{
  branch?: string;
  commit?: GitCommit;
  repositoryId?: string;
}>) {
  return (
    <Stack gap={2}>
      <Text fw={600} size="sm">
        {branch ?? "Unknown branch"}
      </Text>
      <Group gap="xs" wrap="wrap">
        <Text c="dimmed" size="xs">
          Commit
        </Text>
        {commit ? (
          <CommitReference commit={commit} repositoryId={repositoryId} />
        ) : (
          <Text size="xs">Unknown</Text>
        )}
      </Group>
      <Group gap="xs" wrap="wrap">
        <Text c="dimmed" size="xs">
          Date
        </Text>
        {commit?.authoredAt ? (
          <Timestamp size="xs" value={commit.authoredAt} />
        ) : (
          <Text size="xs">Unknown</Text>
        )}
      </Group>
    </Stack>
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

function commonSourcePaths(stages: PromotionBranchDriftStage[]) {
  const [firstStage, ...remainingStages] = stages;
  if (!firstStage) {
    return [];
  }

  return firstStage.sourcePaths.filter((path) =>
    remainingStages.every((stage) => stage.sourcePaths.includes(path)),
  );
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
