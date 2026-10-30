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

import { actionRunPath } from "~/features/runs/action-run";
import { createServerAPIClient } from "~/api/client.server";
import { listRepositories, listTargets, type TargetReconciliationStatus } from "~/api/generated";
import { ListPagination, SearchParamFilters } from "~/shared/list-controls";
import { CommitReference } from "~/features/commits/git-commit";
import { EmptyState, PageHeader } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";
import { pageFromSearchParam } from "~/shared/search-params";
import { activeTargetReconciliationPresentation } from "~/features/targets/target-reconciliation";

import type { Route } from "./+types/targets";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Targets") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const query = url.searchParams.get("q")?.trim() ?? "";
  const repositoryId = url.searchParams.get("repositoryId")?.trim() ?? "";
  const runtime = url.searchParams.get("runtime")?.trim() ?? "";
  const placement = url.searchParams.get("placement")?.trim() ?? "";
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
        runtime: runtime || undefined,
        placement: placement || undefined,
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
      runtime,
      placement,
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
    runtime,
    placement,
    targets: initialTargetsResult.data.items,
    totalTargetCount: initialTargetsResult.data.total,
  };
}

export default function TargetIndex({ loaderData }: Route.ComponentProps) {
  const filtered = Boolean(
    loaderData.query || loaderData.repositoryId || loaderData.runtime || loaderData.placement,
  );

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
            runtime={loaderData.runtime}
            placement={loaderData.placement}
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
                  ? "Try a different target, action, repository, Runtime, or Placement filter."
                  : "Connect a repository with an accepted catalog to populate this view."
              }
              title={filtered ? "No matching targets" : "No targets available"}
            />
          ) : (
            <>
              <Paper withBorder>
                <Table.ScrollContainer minWidth={1080} type="native">
                  <Table highlightOnHover verticalSpacing="sm">
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>Target</Table.Th>
                        <Table.Th>Runtime / placement</Table.Th>
                        <Table.Th>Last changed</Table.Th>
                        <Table.Th>Sync reconciliation</Table.Th>
                        <Table.Th>Actions</Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {loaderData.targets.map((target) => (
                        <Table.Tr key={target.id}>
                          <Table.Td>
                            <Stack gap={0}>
                              <Anchor component={Link} fw={600} to={`/targets/${target.id}`}>
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
                              {target.runtime ? (
                                <Anchor
                                  component={Link}
                                  size="sm"
                                  to={targetFilterPath(loaderData, "runtime", target.runtime)}
                                >
                                  {target.runtime}
                                </Anchor>
                              ) : (
                                <Text size="sm">Action-defined runtime</Text>
                              )}
                              {target.placement ? (
                                <Anchor
                                  c="dimmed"
                                  component={Link}
                                  size="xs"
                                  to={targetFilterPath(loaderData, "placement", target.placement)}
                                >
                                  {target.placement}
                                </Anchor>
                              ) : (
                                <Text c="dimmed" size="xs">
                                  Action-defined placement
                                </Text>
                              )}
                            </Stack>
                          </Table.Td>
                          <Table.Td>
                            <CommitReference
                              commit={target.desiredCommit}
                              repositoryId={target.repositoryId}
                            />
                          </Table.Td>
                          <Table.Td>
                            <TargetReconciliationBadge
                              actions={target.actions}
                              status={target.reconciliation}
                            />
                          </Table.Td>
                          <Table.Td>
                            <Menu position="bottom-start" shadow="md" width={220}>
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
                                    to={actionRunPath(target.id, action)}
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

function TargetReconciliationBadge({
  actions,
  status,
}: Readonly<{
  actions: readonly string[];
  status: TargetReconciliationStatus;
}>) {
  const presentation = activeTargetReconciliationPresentation(actions, status);

  if (!presentation) {
    return (
      <Text c="dimmed" size="sm">
        —
      </Text>
    );
  }

  return (
    <Badge color={presentation.color} title={presentation.description} variant="light">
      {presentation.label}
    </Badge>
  );
}

function TargetFilters({
  query,
  repositories,
  repositoryId,
  runtime,
  placement,
}: Readonly<{
  query: string;
  repositories: Array<{ id: string; name: string }>;
  repositoryId: string;
  runtime: string;
  placement: string;
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
        {
          active: Boolean(runtime),
          kind: "text",
          label: "Runtime",
          name: "runtime",
          placeholder: "Runtime name",
          value: runtime,
        },
        {
          active: Boolean(placement),
          kind: "text",
          label: "Placement",
          name: "placement",
          placeholder: "Placement name",
          value: placement,
        },
      ]}
    />
  );
}

function targetFilterPath(
  filters: Readonly<{
    query: string;
    repositoryId: string;
    runtime: string;
    placement: string;
  }>,
  name: "runtime" | "placement",
  value: string,
) {
  const searchParams = new URLSearchParams();
  if (filters.query) searchParams.set("q", filters.query);
  if (filters.repositoryId) searchParams.set("repositoryId", filters.repositoryId);
  if (filters.runtime) searchParams.set("runtime", filters.runtime);
  if (filters.placement) searchParams.set("placement", filters.placement);
  searchParams.set(name, value);

  return `/targets?${searchParams.toString()}`;
}

function repositoryNoun(count: number) {
  return count === 1 ? "repository" : "repositories";
}

function targetNoun(count: number) {
  return count === 1 ? "target" : "targets";
}
