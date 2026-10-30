import { Alert, Anchor, Badge, Button, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { IconListDetails } from "@tabler/icons-react";
import { Link } from "react-router";

import type { Run } from "~/api/generated";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime } from "~/shared/display-values";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { SectionHeader } from "~/shared/page";
import { formatDuration, shortIdentifier } from "~/shared/display";
import { runActivity } from "~/features/mascot/execution-activity";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import { targetFilterPath } from "~/features/targets/target-filter";

export const recentRunLimit = 10;

export function RecentRuns({
  available,
  description,
  emptyMessage,
  linkPlacement = false,
  runs,
  showTarget = true,
  viewAllTo,
}: Readonly<{
  available: boolean;
  description: string;
  emptyMessage: string;
  linkPlacement?: boolean;
  runs: Run[];
  showTarget?: boolean;
  viewAllTo: string;
}>) {
  useActivityRefresh(runs.some((run) => run.phase !== "finished"));

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
                  <Table.Th>{showTarget ? "Workload" : "Action"}</Table.Th>
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
                      {showTarget ? (
                        <Stack gap={0}>
                          <Text size="sm">{run.target}</Text>
                          <Text c="dimmed" size="xs">
                            {run.action}
                          </Text>
                        </Stack>
                      ) : (
                        <Text fw={600} size="sm">
                          {run.action}
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="space-between" wrap="nowrap">
                        <Badge color={runStatusColor(run)} variant="light">
                          {runStatusLabel(run)}
                        </Badge>
                        <KawaiiActivity activity={runActivity(run)} animationKey={run.id} compact />
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text size="sm">{run.cluster}</Text>
                        {linkPlacement ? (
                          <Anchor
                            c="dimmed"
                            component={Link}
                            size="xs"
                            to={targetFilterPath("placement", run.placement)}
                          >
                            {run.placement}
                          </Anchor>
                        ) : (
                          <Text c="dimmed" size="xs">
                            {run.placement}
                          </Text>
                        )}
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
