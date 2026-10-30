import { Button, Group, Paper, Stack, Text, Title } from "@mantine/core";
import { IconListDetails } from "@tabler/icons-react";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getRepositoryCatalog, type CatalogRevision } from "~/api/generated";
import { Timestamp } from "~/shared/display-values";
import { CommitReference } from "~/features/commits/git-commit";
import { SectionHeader } from "~/shared/page";

import type { Route } from "./+types/repository-overview";

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const result = await getRepositoryCatalog({
    client,
    path: { repositoryId: params.repositoryId },
  });

  return {
    catalog: result.data,
    repositoryId: params.repositoryId,
  };
}

export default function RepositoryOverview({ loaderData }: Route.ComponentProps) {
  const catalog = loaderData.catalog;

  if (!catalog) {
    return (
      <Paper p="lg" withBorder>
        <Stack gap="xs">
          <Title order={2}>No active catalog</Title>
          <Text c="dimmed">
            Slipramp has not accepted a catalog revision for this repository yet. The repository
            refresh status above shows whether indexing is pending or failed.
          </Text>
        </Stack>
      </Paper>
    );
  }

  return (
    <Stack gap="lg">
      <CatalogSummary catalog={catalog} repositoryId={loaderData.repositoryId} />
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
    </Stack>
  );
}

function CatalogSummary({
  catalog,
  repositoryId,
}: Readonly<{ catalog: CatalogRevision; repositoryId: string }>) {
  return (
    <Paper p="md" withBorder>
      <Group gap="xl">
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Active commit
          </Text>
          <CommitReference commit={catalog.commit} repositoryId={repositoryId} />
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
