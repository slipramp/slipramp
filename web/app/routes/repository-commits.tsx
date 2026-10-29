import { Alert, Badge, Button, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { IconGitCommit } from "@tabler/icons-react";
import { Link, useOutletContext } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listCommits, type RepositoryCommit } from "~/api/generated";
import { Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { SectionHeader } from "~/components/page";
import { repositoryDetailListLimit } from "~/repository-detail";

import type { Route } from "./+types/repository-commits";
import type { RepositoryOutletContext } from "./repository";

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const result = await listCommits({
    client,
    query: { limit: repositoryDetailListLimit, repositoryId: params.repositoryId },
  });

  return {
    available: result.data !== undefined,
    commits: result.data?.items ?? [],
  };
}

export default function RepositoryCommits({ loaderData }: Route.ComponentProps) {
  const { repository } = useOutletContext<RepositoryOutletContext>();
  const index = repository.commitHistoryIndex;
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
        description={`The latest ${repositoryDetailListLimit} commits indexed from the tracked branch.`}
        title="Commit history"
      />
      <Paper p="md" withBorder>
        <Stack gap="md">
          {!loaderData.available ? (
            <Alert color="red" title="Commit history unavailable">
              Indexed commits could not be loaded from the Slipramp API.
            </Alert>
          ) : tracksHistory ? (
            <>
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
              <CommitTable commits={loaderData.commits} />
            </>
          ) : (
            <Text c="dimmed">
              Enable complete tracked branch history for this repository to browse its commits.
            </Text>
          )}
        </Stack>
      </Paper>
    </Stack>
  );
}

function CommitTable({ commits }: Readonly<{ commits: RepositoryCommit[] }>) {
  if (commits.length === 0) {
    return <Text c="dimmed">No commits have been indexed yet.</Text>;
  }

  return (
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
              <CommitReference commit={item.commit} repositoryId={item.repositoryId} />
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
  );
}
