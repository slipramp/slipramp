import { Alert, Anchor, Badge, Button, Paper, Stack, Table, Text } from "@mantine/core";
import { IconListDetails } from "@tabler/icons-react";
import { Link } from "react-router";

import type { Run } from "~/api/generated";
import { RelativeTime } from "~/components/display";
import { SectionHeader } from "~/components/page";
import { formatDuration, shortIdentifier } from "~/display";
import { runStatusColor } from "~/run-status";

export const recentRunLimit = 10;

export function RecentRuns({
  available,
  description,
  emptyMessage,
  runs,
  viewAllTo,
}: Readonly<{
  available: boolean;
  description: string;
  emptyMessage: string;
  runs: Run[];
  viewAllTo: string;
}>) {
  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Button
            component={Link}
            leftSection={<IconListDetails size={16} />}
            size="compact-md"
            to={viewAllTo}
            variant="light"
          >
            View all runs
          </Button>
        }
        description={description}
        title="Recent runs"
      />
      {!available ? (
        <Alert color="red" title="Run history unavailable">
          Recent execution history could not be loaded.
        </Alert>
      ) : runs.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">{emptyMessage}</Text>
        </Paper>
      ) : (
        <Paper withBorder>
          <Table.ScrollContainer minWidth={900} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Run</Table.Th>
                  <Table.Th>Workload</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Execution</Table.Th>
                  <Table.Th>Created</Table.Th>
                  <Table.Th>Runtime</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {runs.map((run) => (
                  <Table.Tr key={run.id}>
                    <Table.Td>
                      <Anchor component={Link} fw={600} to={`/runs/${run.id}`}>
                        {shortIdentifier(run.id)}
                      </Anchor>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text size="sm">{run.target}</Text>
                        <Text c="dimmed" size="xs">
                          {run.action}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Badge color={runStatusColor(run)} variant="light">
                        {run.outcome ?? run.phase}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text size="sm">{run.cluster}</Text>
                        <Text c="dimmed" size="xs">
                          {run.placement}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <RelativeTime size="sm" value={run.createdAt} />
                    </Table.Td>
                    <Table.Td>
                      <Text c="dimmed" size="sm">
                        {run.startedAt
                          ? formatDuration(run.startedAt, run.finishedAt)
                          : "Not started"}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      )}
    </Stack>
  );
}
