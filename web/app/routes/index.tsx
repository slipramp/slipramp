import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  NativeSelect,
  Pagination,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, Link, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listRepositories, listTargets } from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/index";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Targets") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const query = url.searchParams.get("q")?.trim() ?? "";
  const repositoryId = url.searchParams.get("repositoryId")?.trim() ?? "";
  const requestedPage = positiveInteger(url.searchParams.get("page"));
  const client = createServerAPIClient(request);
  const offset = (requestedPage - 1) * pageSize;
  const [repositoriesResult, initialTargetsResult] = await Promise.all([
    listRepositories({ client }),
    listTargets({
      client,
      query: {
        limit: pageSize,
        offset,
        q: query || undefined,
        repositoryId: repositoryId || undefined,
      },
    }),
  ]);
  const repositories = repositoriesResult.data?.items ?? [];

  if (!initialTargetsResult.data) {
    return {
      available: false,
      error:
        initialTargetsResult.error?.detail ??
        initialTargetsResult.error?.title ??
        "The target index could not be loaded.",
      filteredTargetCount: 0,
      page: 1,
      pageCount: 0,
      query,
      repositories,
      repositoryId,
      targets: [],
      totalTargetCount: 0,
    };
  }
  const pageCount = Math.ceil(initialTargetsResult.data.total / pageSize);
  const page = pageCount === 0 ? 1 : Math.min(requestedPage, pageCount);
  let targetData = initialTargetsResult.data;

  if (page !== requestedPage) {
    const targetsResult = await listTargets({
      client,
      query: {
        limit: pageSize,
        offset: (page - 1) * pageSize,
        q: query || undefined,
        repositoryId: repositoryId || undefined,
      },
    });
    if (targetsResult.data) targetData = targetsResult.data;
  }

  return {
    available: true,
    error: undefined,
    filteredTargetCount: targetData.total,
    page,
    pageCount,
    query,
    repositories,
    repositoryId,
    targets: targetData.items,
    totalTargetCount: targetData.total,
  };
}

export default function TargetIndex({ loaderData }: Route.ComponentProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const firstTarget = (loaderData.page - 1) * pageSize + 1;
  const lastTarget = firstTarget + loaderData.targets.length - 1;
  const filtered = Boolean(loaderData.query || loaderData.repositoryId);

  function changePage(page: number) {
    const nextSearchParams = new URLSearchParams(searchParams);
    if (page === 1) {
      nextSearchParams.delete("page");
    } else {
      nextSearchParams.set("page", String(page));
    }
    setSearchParams(nextSearchParams);
  }

  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Targets</Title>
          <Text c="dimmed">Browse actions across every active repository catalog.</Text>
        </Stack>
        {loaderData.available ? (
          <Group gap="xs">
            <Badge size="lg" variant="light">
              {loaderData.totalTargetCount} {filtered ? "matching" : "targets"}
            </Badge>
            <Badge size="lg" variant="outline">
              {loaderData.repositories.length} repositories
            </Badge>
          </Group>
        ) : null}
      </Group>

      {!loaderData.available ? (
        <Alert color="red" title="Targets unavailable">
          {loaderData.error}
        </Alert>
      ) : (
        <>
          <TargetFilters
            query={loaderData.query}
            repositories={loaderData.repositories}
            repositoryId={loaderData.repositoryId}
          />

          {loaderData.targets.length === 0 ? (
            <Paper p="lg" withBorder>
              <Stack align="flex-start" gap="xs">
                <Text fw={600}>{filtered ? "No matching targets" : "No targets available"}</Text>
                <Text c="dimmed" size="sm">
                  {filtered
                    ? "Try a different target, action, or repository filter."
                    : "Connect a repository with an accepted catalog to populate this view."}
                </Text>
                {filtered ? (
                  <Anchor component={Link} to="/">
                    Clear filters
                  </Anchor>
                ) : (
                  <Anchor component={Link} to="/repositories">
                    Manage repositories
                  </Anchor>
                )}
              </Stack>
            </Paper>
          ) : (
            <>
              <Paper withBorder>
                <Table.ScrollContainer minWidth={980}>
                  <Table highlightOnHover verticalSpacing="sm">
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>Target</Table.Th>
                        <Table.Th>Repository</Table.Th>
                        <Table.Th>Actions</Table.Th>
                        <Table.Th>Execution</Table.Th>
                        <Table.Th>Desired commit</Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {loaderData.targets.map((target) => (
                        <Table.Tr key={`${target.repositoryId}:${target.name}`}>
                          <Table.Td>
                            <Stack gap={0}>
                              <Anchor
                                component={Link}
                                fw={600}
                                to={`/repositories/${target.repositoryId}/targets/${encodeURIComponent(target.name)}`}
                              >
                                {target.title ?? target.name}
                              </Anchor>
                              {target.title ? (
                                <Text c="dimmed" size="xs">
                                  {target.name}
                                </Text>
                              ) : null}
                            </Stack>
                          </Table.Td>
                          <Table.Td>
                            <Anchor component={Link} to={`/repositories/${target.repositoryId}`}>
                              {target.repositoryName}
                            </Anchor>
                          </Table.Td>
                          <Table.Td>
                            <Stack gap={2}>
                              <Badge variant="light">
                                {target.actions.length} action
                                {target.actions.length === 1 ? "" : "s"}
                              </Badge>
                              <Text c="dimmed" lineClamp={2} maw={360} size="xs">
                                {target.actions.join(", ") || "No actions"}
                              </Text>
                            </Stack>
                          </Table.Td>
                          <Table.Td>
                            <Stack gap={0}>
                              <Text size="sm">
                                {target.placement ?? "Action-defined placement"}
                              </Text>
                              <Text c="dimmed" size="xs">
                                {target.runtime ?? "Action-defined runtime"}
                              </Text>
                            </Stack>
                          </Table.Td>
                          <Table.Td>
                            <Stack gap={0}>
                              <Code>{shortCommit(target.desiredCommitSha)}</Code>
                              {target.desiredCommitSha !== target.catalogCommitSha ? (
                                <Text c="dimmed" size="xs">
                                  differs from catalog
                                </Text>
                              ) : null}
                            </Stack>
                          </Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                </Table.ScrollContainer>
              </Paper>

              <Group justify="space-between">
                <Text c="dimmed" size="sm">
                  Showing {firstTarget}–{lastTarget} of {loaderData.filteredTargetCount}
                </Text>
                {loaderData.pageCount > 1 ? (
                  <Pagination
                    onChange={changePage}
                    total={loaderData.pageCount}
                    value={loaderData.page}
                    withEdges
                  />
                ) : null}
              </Group>
            </>
          )}
        </>
      )}
    </Stack>
  );
}

function TargetFilters({
  query,
  repositories,
  repositoryId,
}: Readonly<{
  query: string;
  repositories: Array<{ id: string; name: string }>;
  repositoryId: string;
}>) {
  return (
    <Paper p="md" withBorder>
      <Form method="get">
        <Group align="flex-end" wrap="wrap">
          <TextInput
            defaultValue={query}
            key={`query:${query}`}
            label="Search"
            name="q"
            placeholder="Target, action, or repository"
            style={{ flex: "1 1 280px" }}
          />
          <NativeSelect
            data={[
              { label: "All repositories", value: "" },
              ...repositories.map((repository) => ({
                label: repository.name,
                value: repository.id,
              })),
            ]}
            defaultValue={repositoryId}
            key={`repository:${repositoryId}`}
            label="Repository"
            name="repositoryId"
            style={{ flex: "0 1 260px" }}
          />
          <Button type="submit">Filter</Button>
          {query || repositoryId ? (
            <Button component={Link} to="/" variant="subtle">
              Clear
            </Button>
          ) : null}
        </Group>
      </Form>
    </Paper>
  );
}

function positiveInteger(value: string | null) {
  if (!value) return 1;

  const parsed = Number.parseInt(value, 10);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 1;
}

function shortCommit(commit: string) {
  return commit.slice(0, 12);
}
