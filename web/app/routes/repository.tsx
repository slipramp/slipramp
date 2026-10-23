import {
  Alert,
  Badge,
  Button,
  Divider,
  Group,
  NativeSelect,
  Paper,
  Stack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { IconGitCommit, IconListDetails, IconTrash } from "@tabler/icons-react";
import { Form, Link, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getRepositoryCatalog,
  listCommits,
  listRepositories,
  listRuns,
  type CatalogRevision,
  type Repository,
  type RepositoryCommit,
  type RepositoryCommitIndexingMode,
  updateRepository,
} from "~/api/generated";
import { Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, SectionHeader } from "~/components/page";
import { RecentRuns, recentRunLimit } from "~/components/recent-runs";
import { pageTitle } from "~/page-title";
import { formValue } from "~/form-data";

import type { Route } from "./+types/repository";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.repository?.name ?? "Repository") }];
}

export async function loader({ params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request);
  const [repositoriesResult, catalogResult, runsResult, commitsResult] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
    getRepositoryCatalog({ client, path: { repositoryId: params.repositoryId } }),
    listRuns({
      client,
      query: { limit: recentRunLimit, repositoryId: params.repositoryId },
    }),
    listCommits({
      client,
      query: { limit: 5, repositoryId: params.repositoryId },
    }),
  ]);
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);

  return {
    apiAvailable: repositoriesResult.data !== undefined || catalogResult.data !== undefined,
    catalog: catalogResult.data,
    repository,
    repositoryId: params.repositoryId,
    repositoryListAvailable: repositoriesResult.data !== undefined,
    recentRuns: runsResult.data?.items ?? [],
    recentCommits: commitsResult.data?.items ?? [],
    runsAvailable: runsResult.data !== undefined,
  };
}

export async function action({ params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const result = await updateRepository({
    body: {
      commitIndexing: formValue(formData, "commitIndexing") as RepositoryCommitIndexingMode,
    },
    client: createServerAPIClient(request),
    path: { repositoryId: params.repositoryId },
  });

  if (result.data) return { updated: true };

  return {
    error: result.error.detail ?? result.error.title ?? "The repository could not be updated.",
  };
}

export default function RepositoryCatalog({ actionData, loaderData }: Route.ComponentProps) {
  if (!loaderData.apiAvailable) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to repositories" to="/repositories" />
        <Alert color="red" title="API unavailable">
          The repository catalog could not be loaded.
        </Alert>
      </Stack>
    );
  }

  if (loaderData.repositoryListAvailable && !loaderData.repository) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to repositories" to="/repositories" />
        <Alert color="red" title="Repository not found">
          This repository is not in the active repository list.
        </Alert>
      </Stack>
    );
  }

  const repository = loaderData.repository;
  const catalog = loaderData.catalog;

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          repository ? (
            <Button
              color="red"
              component={Link}
              leftSection={<IconTrash size={16} />}
              size="compact-md"
              to={`/repositories/${repository.id}/delete`}
              variant="subtle"
            >
              Remove
            </Button>
          ) : null
        }
        back={<PageBackLink label="Back to repositories" to="/repositories" />}
        badges={repository ? <RefreshBadge repository={repository} /> : null}
        description={repository ? `${repository.cloneUrl} · ${repository.branch}` : "Repository"}
        title={repository?.name ?? "Repository"}
      />

      {repository?.refresh.failureReason ? (
        <Alert color="red" title="Latest refresh failed">
          {repository.refresh.failureReason}
        </Alert>
      ) : null}

      {!catalog ? (
        <Paper p="lg" withBorder>
          <Stack gap="xs">
            <Title order={2}>No active catalog</Title>
            <Text c="dimmed">
              Slipramp has not accepted a catalog revision for this repository yet. The previous
              refresh status above shows whether indexing is pending or failed.
            </Text>
          </Stack>
        </Paper>
      ) : (
        <>
          <CatalogSummary catalog={catalog} />
          <Stack gap="md">
            <SectionHeader
              actions={
                catalog.targets.length > 0 ? (
                  <Button
                    component={Link}
                    leftSection={<IconListDetails size={16} />}
                    size="compact-md"
                    to={`/targets?repositoryId=${encodeURIComponent(loaderData.repositoryId)}`}
                    variant="light"
                  >
                    View targets
                  </Button>
                ) : null
              }
              title="Targets"
            />
            <Paper p="md" withBorder>
              <Stack gap={2}>
                <Text fw={600}>
                  {catalog.targets.length} active target
                  {catalog.targets.length === 1 ? "" : "s"}
                </Text>
                <Text c="dimmed" size="sm">
                  {catalog.targets.length === 0
                    ? "This catalog contains no targets."
                    : "Search and browse this repository’s targets in the main target index."}
                </Text>
              </Stack>
            </Paper>
          </Stack>
        </>
      )}

      {repository ? (
        <CommitHistorySection
          actionData={actionData}
          commits={loaderData.recentCommits}
          repository={repository}
        />
      ) : null}

      <RecentRuns
        available={loaderData.runsAvailable}
        description={`The latest ${recentRunLimit} executions from this repository.`}
        emptyMessage="No Runs have been created from this repository."
        runs={loaderData.recentRuns}
        viewAllTo={`/runs?repositoryId=${encodeURIComponent(loaderData.repositoryId)}`}
      />
    </Stack>
  );
}

function CommitHistorySection({
  actionData,
  commits,
  repository,
}: Readonly<{
  actionData: Route.ComponentProps["actionData"];
  commits: RepositoryCommit[];
  repository: Repository;
}>) {
  const index = repository.commitHistoryIndex;
  const navigation = useNavigation();
  const colors = { failed: "red", pending: "yellow", succeeded: "green" } as const;
  const tracksHistory = repository.commitIndexing === "trackedBranchHistory";

  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          tracksHistory ? (
            <Button
              component={Link}
              leftSection={<IconGitCommit size={16} />}
              size="compact-md"
              to={`/commits?repositoryId=${encodeURIComponent(repository.id)}`}
              variant="light"
            >
              View all commits
            </Button>
          ) : null
        }
        title="Commit history"
      />
      <Paper p="md" withBorder>
        <Stack gap="md">
          {actionData?.error ? (
            <Alert color="red" title="Commit indexing was not updated">
              {actionData.error}
            </Alert>
          ) : null}
          {actionData?.updated ? (
            <Alert color="green" title="Commit indexing updated">
              The new mode will be used by subsequent repository refreshes.
            </Alert>
          ) : null}
          <Form method="post">
            <Group align="flex-end" wrap="wrap">
              <NativeSelect
                data={[
                  { label: "Disabled", value: "disabled" },
                  { label: "Observed branch heads", value: "observedHeads" },
                  { label: "Complete tracked branch history", value: "trackedBranchHistory" },
                ]}
                defaultValue={repository.commitIndexing}
                description="Exact SHAs used by Runs and catalogs are retained in every mode. Disabling indexing does not erase metadata already collected."
                label="Indexing mode"
                name="commitIndexing"
                style={{ flex: "1 1 28rem" }}
              />
              <Button loading={navigation.state === "submitting"} type="submit">
                Save
              </Button>
            </Group>
          </Form>

          {tracksHistory ? (
            <>
              <Divider />
              {index?.failureReason ? (
                <Alert color="red" title="History indexing failed">
                  {index.failureReason}
                </Alert>
              ) : null}
              <Group justify="space-between">
                <Stack gap={0}>
                  <Text fw={600}>
                    {index?.commitCount ?? 0} indexed commit
                    {(index?.commitCount ?? 0) === 1 ? "" : "s"}
                  </Text>
                  <Text c="dimmed" size="sm">
                    {index?.status === "pending"
                      ? "Catching up to the latest observed branch head."
                      : index?.status === "failed"
                        ? "The previous successful index remains available."
                        : "Reachable from the latest indexed branch head."}
                  </Text>
                </Stack>
                {index ? (
                  <Badge color={colors[index.status]} variant="light">
                    {index.status}
                  </Badge>
                ) : null}
              </Group>
              {commits.length === 0 ? (
                <Text c="dimmed">No commits have been indexed yet.</Text>
              ) : (
                <Table>
                  <Table.Tbody>
                    {commits.map((item) => (
                      <Table.Tr key={item.commit.sha}>
                        <Table.Td>
                          <Text fw={600} lineClamp={1} title={item.commit.title}>
                            {item.commit.title ?? "Untitled commit"}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <CommitReference commit={item.commit} />
                        </Table.Td>
                        <Table.Td>
                          {(item.commit.committedAt ?? item.commit.authoredAt) ? (
                            <Timestamp
                              c="dimmed"
                              size="sm"
                              value={item.commit.committedAt ?? item.commit.authoredAt ?? ""}
                            />
                          ) : null}
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              )}
            </>
          ) : null}
        </Stack>
      </Paper>
    </Stack>
  );
}

function RefreshBadge({ repository }: Readonly<{ repository: Repository }>) {
  const colors = {
    failed: "red",
    pending: "yellow",
    succeeded: "green",
  } as const;

  return (
    <Badge color={colors[repository.refresh.status]} variant="light">
      Refresh {repository.refresh.status}
    </Badge>
  );
}

function CatalogSummary({ catalog }: Readonly<{ catalog: CatalogRevision }>) {
  return (
    <Paper p="md" withBorder>
      <Group gap="xl">
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Active commit
          </Text>
          <CommitReference commit={catalog.commit} />
        </Stack>
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Accepted
          </Text>
          <Timestamp size="sm" value={catalog.createdAt} />
        </Stack>
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Definitions
          </Text>
          <Text size="sm">
            {catalog.targets.length} targets · {catalog.runtimes.length} runtimes
          </Text>
        </Stack>
      </Group>
    </Paper>
  );
}
