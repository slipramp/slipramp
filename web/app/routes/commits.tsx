import { Alert, Anchor, Paper, Stack, Table, Text } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listCommits,
  listRepositories,
  type Repository,
  type RepositoryCommit,
} from "~/api/generated";
import { commitPath } from "~/commit-path";
import { Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { Identity } from "~/components/identity";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/commits";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Commit history") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const query = url.searchParams.get("q")?.trim() ?? "";
  const repositoryId = url.searchParams.get("repositoryId")?.trim() || undefined;
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const [commitsResult, repositoriesResult] = await Promise.all([
    listCommits({
      client,
      query: {
        limit: pageSize,
        offset: (page - 1) * pageSize,
        q: query || undefined,
        repositoryId,
      },
    }),
    listRepositories({ client, query: { limit: 200 } }),
  ]);

  if (commitsResult.data) {
    const lastPage = Math.max(1, Math.ceil(commitsResult.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    available: commitsResult.data !== undefined,
    commits: commitsResult.data?.items ?? [],
    page,
    query,
    repositories:
      repositoriesResult.data?.items.filter(
        (repository) => repository.commitIndexing === "trackedBranchHistory",
      ) ?? [],
    repositoryId,
    total: commitsResult.data?.total ?? 0,
  };
}

export default function Commits({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="md">
      <PageHeader
        description="Commits reachable from branches configured for complete history indexing."
        title="Commit history"
      />

      <CommitFilters
        query={loaderData.query}
        repositories={loaderData.repositories}
        repositoryId={loaderData.repositoryId}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Commit history unavailable">
          Indexed commits could not be loaded from the Slipramp API.
        </Alert>
      ) : (
        <CommitTable
          commits={loaderData.commits}
          filtered={Boolean(loaderData.query || loaderData.repositoryId)}
          hasIndexedRepositories={loaderData.repositories.length > 0}
        />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.commits.length}
          noun="commits"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function CommitFilters({
  query,
  repositories,
  repositoryId,
}: Readonly<{
  query: string;
  repositories: Repository[];
  repositoryId?: string;
}>) {
  return (
    <SearchParamFilters
      clearTo="/commits"
      filters={[
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
          active: Boolean(query),
          kind: "text",
          label: "Commit",
          maxLength: 200,
          name: "q",
          placeholder: "SHA, title, or author",
          value: query,
        },
      ]}
    />
  );
}

function CommitTable({
  commits,
  filtered,
  hasIndexedRepositories,
}: Readonly<{
  commits: RepositoryCommit[];
  filtered: boolean;
  hasIndexedRepositories: boolean;
}>) {
  if (commits.length === 0) {
    return (
      <Paper p="md" withBorder>
        <Text c="dimmed">
          {filtered
            ? "No indexed commits match these filters."
            : hasIndexedRepositories
              ? "Commit history is empty while the first branch index is pending."
              : "Enable complete tracked branch history on a repository to browse its commits here."}
        </Text>
      </Paper>
    );
  }

  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={760} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Commit</Table.Th>
              <Table.Th>Repository</Table.Th>
              <Table.Th>Author</Table.Th>
              <Table.Th>Committed</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {commits.map((item) => (
              <Table.Tr key={`${item.repositoryId}:${item.commit.sha}`}>
                <Table.Td>
                  <Stack gap={2} maw={520}>
                    <Anchor
                      component={Link}
                      fw={600}
                      lineClamp={1}
                      title={item.commit.title}
                      to={commitPath(item.repositoryId, item.commit.sha)}
                    >
                      {item.commit.title ?? "Untitled commit"}
                    </Anchor>
                    <CommitReference commit={item.commit} repositoryId={item.repositoryId} />
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Anchor component={Link} to={`/repositories/${item.repositoryId}`}>
                    {item.repositoryName}
                  </Anchor>
                </Table.Td>
                <Table.Td>
                  {item.commit.author ? (
                    <Identity avatarSize={24} name={item.commit.author} />
                  ) : (
                    <Text c="dimmed">Unknown</Text>
                  )}
                </Table.Td>
                <Table.Td>
                  {(item.commit.committedAt ?? item.commit.authoredAt) ? (
                    <Timestamp
                      size="sm"
                      value={item.commit.committedAt ?? item.commit.authoredAt ?? ""}
                    />
                  ) : (
                    <Text c="dimmed">Unknown</Text>
                  )}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}
