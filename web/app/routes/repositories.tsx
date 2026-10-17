import { Anchor, Badge, Code, Paper, Stack, Table, Text, Title } from "@mantine/core";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listRepositories } from "~/api/generated";

import type { Route } from "./+types/repositories";

export async function loader() {
  const result = await listRepositories({ client: createServerAPIClient() });

  return {
    available: result.data !== undefined,
    repositories: result.data?.items ?? [],
  };
}

export default function Repositories({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="md">
      <Stack gap={2}>
        <Title order={1}>Repositories</Title>
        <Text c="dimmed">Tracked Git branches and their latest catalog refresh.</Text>
      </Stack>

      <Paper withBorder>
        {!loaderData.available ? (
          <Text c="dimmed" p="md">
            Repositories are unavailable while the API is disconnected.
          </Text>
        ) : loaderData.repositories.length === 0 ? (
          <Text c="dimmed" p="md">
            No repositories have been registered yet.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={760}>
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Repository</Table.Th>
                  <Table.Th>Branch</Table.Th>
                  <Table.Th>Refresh</Table.Th>
                  <Table.Th>Commit</Table.Th>
                  <Table.Th>Last attempt</Table.Th>
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
                    <Table.Td>{repository.branch}</Table.Td>
                    <Table.Td>
                      <RefreshBadge status={repository.refresh.status} />
                    </Table.Td>
                    <Table.Td>
                      {repository.refresh.commitSha ? (
                        <Code>{shortCommit(repository.refresh.commitSha)}</Code>
                      ) : (
                        <Text c="dimmed">—</Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      {repository.refresh.attemptedAt ? (
                        <Text
                          c="dimmed"
                          component="time"
                          dateTime={repository.refresh.attemptedAt}
                          size="sm"
                        >
                          {formatTimestamp(repository.refresh.attemptedAt)}
                        </Text>
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

function shortCommit(commit: string) {
  return commit.slice(0, 12);
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
