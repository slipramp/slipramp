import {
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
import { IconPlus } from "@tabler/icons-react";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listRepositories } from "~/api/generated";
import { Datestamp, RelativeTime } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/repositories";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Repositories") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const query = url.searchParams.get("q")?.trim() ?? "";
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const result = await listRepositories({
    client: createServerAPIClient(request, context),
    query: { limit: pageSize, offset: (page - 1) * pageSize, q: query || undefined },
  });

  if (result.data) {
    const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    available: result.data !== undefined,
    page,
    query,
    repositories: result.data?.items ?? [],
    total: result.data?.total ?? 0,
  };
}

export default function Repositories({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="md">
      <PageHeader
        actions={
          <Button component={Link} leftSection={<IconPlus size={17} />} to="/repositories/new">
            Connect repository
          </Button>
        }
        description="Tracked Git branches and their latest catalog refresh."
        title="Repositories"
      />

      <SearchParamFilters active={Boolean(loaderData.query)} clearTo="/repositories">
        <TextInput
          defaultValue={loaderData.query}
          key={loaderData.query}
          label="Search repositories"
          name="q"
          placeholder="Name, URL, or branch"
          style={{ flex: "1 1 320px" }}
          type="search"
        />
      </SearchParamFilters>

      <Paper withBorder>
        {!loaderData.available ? (
          <Text c="dimmed" p="md">
            Repositories are unavailable while the API is disconnected.
          </Text>
        ) : loaderData.repositories.length === 0 ? (
          <Text c="dimmed" p="md">
            {loaderData.query
              ? "No repositories match this search."
              : "No repositories have been registered yet."}
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={760} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Repository</Table.Th>
                  <Table.Th>Tracked ref</Table.Th>
                  <Table.Th>Catalog</Table.Th>
                  <Table.Th>Refresh activity</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {loaderData.repositories.map((repository) => (
                  <Table.Tr key={repository.id}>
                    <Table.Td>
                      <Stack gap={0}>
                        <Anchor component={Link} fw={600} to={`/repositories/${repository.id}`}>
                          {repository.name}
                        </Anchor>
                        <Text c="dimmed" lineClamp={1} size="xs">
                          {repository.cloneUrl}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        <Code>{repository.branch}</Code>
                        <Datestamp c="dimmed" size="xs" value={repository.createdAt} />
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Stack align="flex-start" gap={4}>
                        <RefreshBadge status={repository.refresh.status} />
                        {repository.refresh.commit ? (
                          <CommitReference commit={repository.refresh.commit} />
                        ) : (
                          <Text c="dimmed" size="xs">
                            No accepted commit
                          </Text>
                        )}
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      {repository.refresh.attemptedAt ? (
                        <Stack gap={0}>
                          <RelativeTime size="sm" value={repository.refresh.attemptedAt} />
                          {repository.refresh.succeededAt ? (
                            <Group gap={4}>
                              <Text c="dimmed" size="xs">
                                Last success
                              </Text>
                              <RelativeTime
                                c="dimmed"
                                size="xs"
                                value={repository.refresh.succeededAt}
                              />
                            </Group>
                          ) : null}
                        </Stack>
                      ) : (
                        <Text c="dimmed">Not attempted</Text>
                      )}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Paper>
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

function RefreshBadge({ status }: Readonly<{ status: "pending" | "succeeded" | "failed" }>) {
  const colors = {
    failed: "red",
    pending: "yellow",
    succeeded: "green",
  } as const;

  return (
    <Badge color={colors[status]} variant="light">
      {status}
    </Badge>
  );
}
