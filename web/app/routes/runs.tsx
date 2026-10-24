import { Alert, Anchor, Badge, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listClusters,
  listRepositories,
  listRuns,
  type Cluster,
  type Repository,
  type Run,
} from "~/api/generated";
import { useKawaiiActivityRefresh } from "~/components/activity-refresh";
import { RelativeTime, Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { Identity } from "~/components/identity";
import { KawaiiActivity } from "~/components/kawaii-activity";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { formatDuration, shortIdentifier } from "~/display";
import { runActivity } from "~/execution-activity";
import { pageTitle } from "~/page-title";
import { runStatusColor } from "~/run-status";
import { runSourceLabel } from "~/run-source";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/runs";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Run history") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const repositoryId = optionalSearchValue(url, "repositoryId");
  const target = repositoryId ? optionalSearchValue(url, "target") : undefined;
  const cluster = optionalSearchValue(url, "cluster");
  const phase = optionalRunPhase(url.searchParams.get("phase"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const offset = (requestedPage - 1) * pageSize;
  const client = createServerAPIClient(request, context);
  const [initialRunsResult, repositoriesResult, clustersResult] = await Promise.all([
    listRuns({
      client,
      query: {
        cluster,
        limit: pageSize,
        offset,
        phase,
        repositoryId,
        target,
      },
    }),
    listRepositories({ client, query: { limit: 200 } }),
    listClusters({ client, query: { limit: 200 } }),
  ]);

  if (!initialRunsResult.data) {
    return {
      available: false,
      cluster,
      clusters: clustersResult.data?.items ?? [],
      error:
        initialRunsResult.error?.detail ??
        initialRunsResult.error?.title ??
        "Run history could not be loaded.",
      page: 1,
      phase,
      repositoryId,
      repositories: repositoriesResult.data?.items ?? [],
      runs: [],
      target,
      total: 0,
    };
  }

  const pageCount = Math.ceil(initialRunsResult.data.total / pageSize);
  const lastPage = Math.max(1, pageCount);
  if (requestedPage > lastPage) {
    if (lastPage === 1) {
      url.searchParams.delete("page");
    } else {
      url.searchParams.set("page", String(lastPage));
    }
    return redirect(`${url.pathname}${url.search}`);
  }

  return {
    available: true,
    cluster,
    clusters: clustersResult.data?.items ?? [],
    error: undefined,
    page: requestedPage,
    phase,
    repositoryId,
    repositories: repositoriesResult.data?.items ?? [],
    runs: initialRunsResult.data.items,
    target,
    total: initialRunsResult.data.total,
  };
}

export default function RunHistory({ loaderData }: Route.ComponentProps) {
  useKawaiiActivityRefresh(loaderData.runs.some((run) => run.phase !== "finished"));
  const repositoryNames = new Map(
    loaderData.repositories.map((repository) => [repository.id, repository.name]),
  );
  return (
    <Stack gap="lg">
      <PageHeader description="Immutable action executions, newest first." title="Run history" />

      <HistoryFilters
        cluster={loaderData.cluster}
        clusters={loaderData.clusters}
        phase={loaderData.phase}
        repositoryId={loaderData.repositoryId}
        repositories={loaderData.repositories}
        target={loaderData.target}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Run history unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : (
        <>
          <HistoryTable repositoryNames={repositoryNames} runs={loaderData.runs} />
          {loaderData.total > 0 ? (
            <ListPagination
              itemCount={loaderData.runs.length}
              noun="Runs"
              page={loaderData.page}
              pageSize={pageSize}
              total={loaderData.total}
            />
          ) : null}
        </>
      )}
    </Stack>
  );
}

function HistoryFilters({
  cluster,
  clusters,
  phase,
  repositoryId,
  repositories,
  target,
}: Readonly<{
  cluster?: string;
  clusters: Cluster[];
  phase?: Run["phase"];
  repositoryId?: string;
  repositories: Repository[];
  target?: string;
}>) {
  return (
    <SearchParamFilters
      clearTo="/runs"
      filters={[
        {
          active: Boolean(repositoryId),
          clearWhenEmpty: ["target"],
          data: [
            { label: "All repositories", value: "" },
            ...repositories.map((repository) => ({
              label: repository.name,
              value: repository.id,
            })),
          ],
          kind: "select",
          label: "Repository",
          name: "repositoryId",
          value: repositoryId,
        },
        {
          active: Boolean(target),
          disabled: !repositoryId,
          kind: "text",
          label: "Target",
          name: "target",
          placeholder: "Target name",
          value: target,
        },
        {
          active: Boolean(cluster),
          data: [
            { label: "All clusters", value: "" },
            ...clusters.map((item) => ({ label: item.name, value: item.name })),
          ],
          kind: "select",
          label: "Cluster",
          name: "cluster",
          value: cluster,
        },
        {
          active: Boolean(phase),
          data: [
            { label: "Any phase", value: "" },
            { label: "Queued", value: "queued" },
            { label: "Assigned", value: "assigned" },
            { label: "Preparing", value: "preparing" },
            { label: "Running", value: "running" },
            { label: "Finished", value: "finished" },
          ],
          kind: "select",
          label: "Phase",
          name: "phase",
          value: phase,
        },
      ]}
    />
  );
}

function HistoryTable({
  repositoryNames,
  runs,
}: Readonly<{ repositoryNames: Map<string, string>; runs: Run[] }>) {
  if (runs.length === 0) {
    return (
      <Paper p="md" withBorder>
        <Text c="dimmed">No Runs match these filters.</Text>
      </Paper>
    );
  }

  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={1080} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Run / workload</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Source / actor</Table.Th>
              <Table.Th>Execution</Table.Th>
              <Table.Th>Timing</Table.Th>
              <Table.Th>Revision</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {runs.map((run) => (
              <Table.Tr key={run.id}>
                <Table.Td>
                  <Anchor component={Link} fw={600} to={`/runs/${run.id}`}>
                    {run.target} · {run.action}
                  </Anchor>
                  <Group gap="xs">
                    <Anchor
                      c="dimmed"
                      component={Link}
                      size="xs"
                      to={`/repositories/${run.repositoryId}/targets/${encodeURIComponent(run.target)}`}
                    >
                      {repositoryNames.get(run.repositoryId) ?? shortIdentifier(run.repositoryId)}
                    </Anchor>
                    <Text c="dimmed" size="xs">
                      Run {shortIdentifier(run.id)}
                    </Text>
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs" wrap="nowrap">
                    <KawaiiActivity activity={runActivity(run)} compact />
                    <Badge color={runStatusColor(run)} variant="light">
                      {run.outcome ?? run.phase}
                    </Badge>
                    {run.observationState === "unknown" ? (
                      <Badge color="yellow" variant="outline">
                        Unknown
                      </Badge>
                    ) : null}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <Text size="sm">{runSourceLabel(run.source)}</Text>
                    <Identity
                      avatarSize={22}
                      name={run.createdBy?.displayName ?? "System"}
                      nameProps={{ c: "dimmed", fw: 500, size: "xs" }}
                    />
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <Text size="sm">{run.cluster}</Text>
                    <Text c="dimmed" size="xs">
                      {run.placement}
                    </Text>
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <RelativeTime size="sm" value={run.createdAt} />
                    {run.startedAt && run.finishedAt ? (
                      <Text c="dimmed" size="xs">
                        {formatDuration(run.startedAt, run.finishedAt)} runtime
                      </Text>
                    ) : run.startedAt ? (
                      <Timestamp c="dimmed" size="xs" value={run.startedAt} />
                    ) : (
                      <Text c="dimmed" size="xs">
                        Not started
                      </Text>
                    )}
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <CommitReference commit={run.commit} repositoryId={run.repositoryId} />
                    <Text c="dimmed" size="xs">
                      Catalog {shortIdentifier(run.catalogRevisionId)}
                    </Text>
                  </Stack>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function optionalSearchValue(url: URL, name: string) {
  return url.searchParams.get(name)?.trim() || undefined;
}

function optionalRunPhase(value: string | null): Run["phase"] | undefined {
  switch (value) {
    case "queued":
    case "assigned":
    case "preparing":
    case "running":
    case "finished":
      return value;
    default:
      return undefined;
  }
}
