import {
  Alert,
  Badge,
  Group,
  NativeSelect,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { redirect } from "react-router";
import { createServerAPIClient } from "~/api/client.server";
import {
  listPromotionBranchDrift,
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
import { PromotionScopeFilters } from "~/components/promotion-scope-filters";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

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
  const result = await listPromotionBranchDrift({
    client: createServerAPIClient(request, context),
    query: {
      application,
      integration,
      limit: pageSize,
      offset: (requestedPage - 1) * pageSize,
      project,
      repository,
      status,
    },
  });

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
    repositories: result.data?.items ?? [],
    repository,
    status,
    total: result.data?.total ?? 0,
  };
}

export default function PromotionBranchDrift({ loaderData }: Route.ComponentProps) {
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
          <Text c="dimmed">No active Kargo Stages match these filters.</Text>
        </Paper>
      ) : (
        loaderData.repositories.map((repository) => (
          <BranchDriftRepository key={repository.repository} repository={repository} />
        ))
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.repositories.length}
          noun="Repositories"
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
  const filtered =
    application !== undefined ||
    integration !== undefined ||
    project !== undefined ||
    repository !== undefined ||
    status !== undefined;

  return (
    <SearchParamFilters active={filtered} clearTo="/promotion-branch-drift">
      <PromotionScopeFilters
        application={application}
        integration={integration}
        project={project}
      />
      <NativeSelect
        defaultValue={status ?? ""}
        label="Status"
        name="status"
        data={[
          { label: "All statuses", value: "" },
          { label: "In sync", value: "current" },
          { label: "Behind", value: "drifted" },
          { label: "Unknown", value: "unknown" },
        ]}
        style={{ flex: "1 1 150px" }}
      />
      <TextInput
        defaultValue={repository}
        label="Repository"
        name="repository"
        style={{ flex: "1 1 160px" }}
        type="search"
      />
    </SearchParamFilters>
  );
}

function BranchDriftRepository({
  repository,
}: Readonly<{ repository: PromotionBranchDriftRepository }>) {
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
                  {sharedSourcePaths.length === 1 ? "Shared watch path" : "Shared watch paths"}
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
                <Table.Th>Source updated</Table.Th>
                <Table.Th>Last rendered</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {repository.stages.map((stage) => (
                <BranchDriftRow
                  key={stage.id}
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
  sharedSourcePaths,
  sourceBranch,
  stage,
}: Readonly<{
  sharedSourcePaths: string[];
  sourceBranch?: string;
  stage: PromotionBranchDriftStage;
}>) {
  const stageSpecificPaths = stage.sourcePaths.filter((path) => !sharedSourcePaths.includes(path));

  return (
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
            <Text c="dimmed" size="xs">
              Also watches {stageSpecificPaths.join(", ")}
            </Text>
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
        <CommitMoment branch={sourceBranch} commit={stage.sourceBranchCommit} />
      </Table.Td>
      <Table.Td>
        <CommitMoment branch={stage.targetBranch} commit={stage.acknowledgedRenderCommit} />
      </Table.Td>
    </Table.Tr>
  );
}

function CommitMoment({
  branch,
  commit,
}: Readonly<{
  branch?: string;
  commit?: GitCommit;
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
        {commit ? <CommitReference commit={commit} /> : <Text size="xs">Unknown</Text>}
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
