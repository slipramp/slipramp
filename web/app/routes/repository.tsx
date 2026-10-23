import { Alert, Badge, Button, Group, Paper, Stack, Text, Title } from "@mantine/core";
import { IconListDetails, IconTrash } from "@tabler/icons-react";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getRepositoryCatalog,
  listRepositories,
  listRuns,
  type CatalogRevision,
  type Repository,
} from "~/api/generated";
import { Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, SectionHeader } from "~/components/page";
import { RecentRuns, recentRunLimit } from "~/components/recent-runs";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/repository";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.repository?.name ?? "Repository") }];
}

export async function loader({ params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request);
  const [repositoriesResult, catalogResult, runsResult] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
    getRepositoryCatalog({ client, path: { repositoryId: params.repositoryId } }),
    listRuns({
      client,
      query: { limit: recentRunLimit, repositoryId: params.repositoryId },
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
    runsAvailable: runsResult.data !== undefined,
  };
}

export default function RepositoryCatalog({ loaderData }: Route.ComponentProps) {
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
                    to={`/?repositoryId=${encodeURIComponent(loaderData.repositoryId)}`}
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
