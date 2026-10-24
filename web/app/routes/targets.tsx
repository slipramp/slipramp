import {
  Alert,
  Anchor,
  Badge,
  Button,
  Group,
  Menu,
  Paper,
  Stack,
  Table,
  Text,
} from "@mantine/core";
import { IconChevronDown, IconPlayerPlay } from "@tabler/icons-react";
import { Link, redirect } from "react-router";

import { actionRunPath } from "~/action-run";
import { createServerAPIClient } from "~/api/client.server";
import { listRepositories, listTargets } from "~/api/generated";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { CommitReference } from "~/components/git-commit";
import { EmptyState, PageHeader } from "~/components/page";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/targets";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Targets") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const query = url.searchParams.get("q")?.trim() ?? "";
  const repositoryId = url.searchParams.get("repositoryId")?.trim() ?? "";
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const offset = (requestedPage - 1) * pageSize;
  const [repositoriesResult, initialTargetsResult] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
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
    error: undefined,
    filteredTargetCount: initialTargetsResult.data.total,
    page: requestedPage,
    pageCount,
    query,
    repositories,
    repositoryId,
    targets: initialTargetsResult.data.items,
    totalTargetCount: initialTargetsResult.data.total,
  };
}

export default function TargetIndex({ loaderData }: Route.ComponentProps) {
  const filtered = Boolean(loaderData.query || loaderData.repositoryId);

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          loaderData.available ? (
            <Group gap="xs">
              <Badge size="lg" variant="light">
                {loaderData.totalTargetCount}{" "}
                {filtered ? "matching" : targetNoun(loaderData.totalTargetCount)}
              </Badge>
              <Badge size="lg" variant="outline">
                {loaderData.repositories.length} {repositoryNoun(loaderData.repositories.length)}
              </Badge>
            </Group>
          ) : null
        }
        description="Browse actions across every active repository catalog."
        title="Targets"
      />

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
            <EmptyState
              action={
                filtered ? (
                  <Anchor component={Link} to="/targets">
                    Clear filters
                  </Anchor>
                ) : (
                  <Anchor component={Link} to="/repositories">
                    Manage repositories
                  </Anchor>
                )
              }
              description={
                filtered
                  ? "Try a different target, action, or repository filter."
                  : "Connect a repository with an accepted catalog to populate this view."
              }
              title={filtered ? "No matching targets" : "No targets available"}
            />
          ) : (
            <>
              <Paper withBorder>
                <Table.ScrollContainer minWidth={820} type="native">
                  <Table highlightOnHover verticalSpacing="sm">
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>Target</Table.Th>
                        <Table.Th>Runtime / placement</Table.Th>
                        <Table.Th>Desired revision</Table.Th>
                        <Table.Th aria-label="Actions" />
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
                              <Anchor
                                c="dimmed"
                                component={Link}
                                size="xs"
                                to={`/repositories/${target.repositoryId}`}
                              >
                                {target.repositoryName}
                              </Anchor>
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
                              <CommitReference commit={target.desiredCommit} />
                              {target.desiredCommit.sha !== target.catalogCommit.sha ? (
                                <Text c="dimmed" size="xs">
                                  differs from catalog
                                </Text>
                              ) : null}
                            </Stack>
                          </Table.Td>
                          <Table.Td>
                            <Menu position="bottom-end" shadow="md" width={220}>
                              <Menu.Target>
                                <Button
                                  rightSection={<IconChevronDown aria-hidden size={14} />}
                                  size="compact-sm"
                                  variant="light"
                                >
                                  Run action
                                </Button>
                              </Menu.Target>
                              <Menu.Dropdown>
                                <Menu.Label>Choose an action</Menu.Label>
                                {target.actions.map((action) => (
                                  <Menu.Item
                                    component={Link}
                                    key={action}
                                    leftSection={<IconPlayerPlay aria-hidden size={14} />}
                                    to={actionRunPath(target.repositoryId, target.name, action)}
                                  >
                                    {action}
                                  </Menu.Item>
                                ))}
                              </Menu.Dropdown>
                            </Menu>
                          </Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                </Table.ScrollContainer>
              </Paper>

              <ListPagination
                itemCount={loaderData.targets.length}
                noun="targets"
                page={loaderData.page}
                pageSize={pageSize}
                total={loaderData.filteredTargetCount}
              />
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
    <SearchParamFilters
      clearTo="/targets"
      filters={[
        {
          active: Boolean(query),
          kind: "text",
          label: "Target",
          name: "q",
          placeholder: "Target, action, or repository",
          value: query,
        },
        {
          active: Boolean(repositoryId),
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
      ]}
    />
  );
}

function repositoryNoun(count: number) {
  return count === 1 ? "repository" : "repositories";
}

function targetNoun(count: number) {
  return count === 1 ? "target" : "targets";
}
