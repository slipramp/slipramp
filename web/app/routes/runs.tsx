import {
  Alert,
  Anchor,
  Badge,
  Group,
  NativeSelect,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
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
import { RelativeTime, Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { Identity } from "~/components/identity";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { formatDuration, shortIdentifier } from "~/display";
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
    repositoryId,
    repositories: repositoriesResult.data?.items ?? [],
    runs: initialRunsResult.data.items,
    target,
    total: initialRunsResult.data.total,
  };
}

export default function RunHistory({ loaderData }: Route.ComponentProps) {
  const repositoryNames = new Map(
    loaderData.repositories.map((repository) => [repository.id, repository.name]),
  );
  return (
    <Stack gap="lg">
      <PageHeader description="Immutable action executions, newest first." title="Run history" />

      <HistoryFilters
        cluster={loaderData.cluster}
        clusters={loaderData.clusters}
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
  repositoryId,
  repositories,
  target,
}: Readonly<{
  cluster?: string;
  clusters: Cluster[];
  repositoryId?: string;
  repositories: Repository[];
  target?: string;
}>) {
  return (
    <SearchParamFilters active={Boolean(cluster || repositoryId || target)} clearTo="/runs">
      <NativeSelect
        data={[
          { label: "All repositories", value: "" },
          ...repositories.map((repository) => ({ label: repository.name, value: repository.id })),
        ]}
        defaultValue={repositoryId ?? ""}
        key={`repository:${repositoryId ?? ""}`}
        label="Repository"
        name="repositoryId"
        onChange={(event) => {
          if (event.currentTarget.value !== "") return;

          const targetInput = event.currentTarget.form?.elements.namedItem("target");
          if (targetInput instanceof HTMLInputElement) targetInput.value = "";
        }}
        style={{ flex: "1 1 260px" }}
      />
      <TextInput
        defaultValue={target}
        disabled={!repositoryId}
        key={`target:${repositoryId ?? ""}:${target ?? ""}`}
        label="Target"
        name="target"
        placeholder={repositoryId ? "Target name" : "Select a repository first"}
        style={{ flex: "1 1 260px" }}
        type="search"
      />
      <NativeSelect
        data={[
          { label: "All clusters", value: "" },
          ...clusters.map((item) => ({ label: item.name, value: item.name })),
        ]}
        defaultValue={cluster ?? ""}
        key={`cluster:${cluster ?? ""}`}
        label="Cluster"
        name="cluster"
        style={{ flex: "1 1 220px" }}
      />
    </SearchParamFilters>
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
                    <CommitReference commit={run.commit} />
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
