import { Alert, Anchor, Badge, Code, Group, Paper, Stack, Table, Text, Title } from "@mantine/core";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listRepositories, listRuns, type Run } from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/runs";

const historyLimit = 50;

export function meta() {
  return [{ title: pageTitle("Run history") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const repositoryId = optionalSearchValue(url, "repositoryId");
  const target = optionalSearchValue(url, "target");
  const client = createServerAPIClient();
  const [runsResult, repositoriesResult] = await Promise.all([
    listRuns({
      client,
      query: {
        limit: historyLimit,
        repositoryId,
        target,
      },
    }),
    listRepositories({ client }),
  ]);

  return {
    available: runsResult.data !== undefined,
    error: runsResult.error?.detail ?? runsResult.error?.title,
    repositoryId,
    repositories: repositoriesResult.data?.items ?? [],
    runs: runsResult.data?.items ?? [],
    target,
  };
}

export default function RunHistory({ loaderData }: Route.ComponentProps) {
  const repositoryNames = new Map(
    loaderData.repositories.map((repository) => [repository.id, repository.name]),
  );
  const repositoryName = loaderData.repositoryId
    ? repositoryNames.get(loaderData.repositoryId)
    : undefined;

  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Run history</Title>
          <Text c="dimmed">Up to {historyLimit} recent Runs, newest first.</Text>
        </Stack>
        {loaderData.repositoryId || loaderData.target ? (
          <Anchor component={Link} to="/runs">
            Clear filters
          </Anchor>
        ) : null}
      </Group>

      <HistoryFilters
        repositoryId={loaderData.repositoryId}
        repositoryName={repositoryName}
        target={loaderData.target}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Run history unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : (
        <HistoryTable repositoryNames={repositoryNames} runs={loaderData.runs} />
      )}
    </Stack>
  );
}

function HistoryFilters({
  repositoryId,
  repositoryName,
  target,
}: Readonly<{ repositoryId?: string; repositoryName?: string; target?: string }>) {
  if (!repositoryId && !target) return null;

  return (
    <Group gap="xs">
      <Text c="dimmed" size="sm">
        Filtered by
      </Text>
      {repositoryId ? (
        <Badge variant="light">Repository: {repositoryName ?? repositoryId.slice(0, 8)}</Badge>
      ) : null}
      {target ? <Badge variant="light">Target: {target}</Badge> : null}
    </Group>
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
      <Table.ScrollContainer minWidth={960}>
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Run</Table.Th>
              <Table.Th>Repository</Table.Th>
              <Table.Th>Target / action</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Cluster</Table.Th>
              <Table.Th>Commit</Table.Th>
              <Table.Th>Created</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {runs.map((run) => (
              <Table.Tr key={run.id}>
                <Table.Td>
                  <Anchor component={Link} fw={600} to={`/runs/${run.id}`}>
                    {run.id.slice(0, 8)}
                  </Anchor>
                </Table.Td>
                <Table.Td>
                  <Anchor component={Link} to={`/repositories/${run.repositoryId}`}>
                    {repositoryNames.get(run.repositoryId) ?? run.repositoryId.slice(0, 8)}
                  </Anchor>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <Anchor
                      component={Link}
                      to={`/repositories/${run.repositoryId}/targets/${encodeURIComponent(run.target)}`}
                    >
                      {run.target}
                    </Anchor>
                    <Text c="dimmed" size="xs">
                      {run.action}
                    </Text>
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs" wrap="nowrap">
                    <Badge color={statusColor(run)} variant="light">
                      {run.outcome ?? run.phase}
                    </Badge>
                    {run.observationState === "unknown" ? (
                      <Badge color="yellow" variant="outline">
                        Unknown
                      </Badge>
                    ) : null}
                  </Group>
                </Table.Td>
                <Table.Td>{run.cluster}</Table.Td>
                <Table.Td>
                  <Code>{run.commitSha.slice(0, 12)}</Code>
                </Table.Td>
                <Table.Td>
                  <Text component="time" dateTime={run.createdAt} size="sm">
                    {formatTimestamp(run.createdAt)}
                  </Text>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function statusColor(run: Run) {
  if (run.observationState === "unknown") return "yellow";

  switch (run.outcome ?? run.phase) {
    case "succeeded":
      return "green";
    case "failed":
      return "red";
    case "timed-out":
      return "orange";
    case "running":
      return "blue";
    case "preparing":
      return "cyan";
    default:
      return "gray";
  }
}

function optionalSearchValue(url: URL, name: string) {
  return url.searchParams.get(name)?.trim() || undefined;
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
