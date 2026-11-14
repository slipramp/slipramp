import {
  Alert,
  Anchor,
  Badge,
  Button,
  Group,
  Paper,
  Stack,
  Table,
  Text,
  Tooltip,
} from "@mantine/core";
import { IconListDetails } from "@tabler/icons-react";
import { Link } from "react-router";

import type { Run } from "~/api/generated";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime } from "~/shared/display-values";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { SectionHeader } from "~/shared/page";
import { formatDuration, formatTimestamp, shortIdentifier } from "~/shared/display";
import { runActivity } from "~/features/mascot/execution-activity";
import { runSourceLabel } from "~/features/runs/run-source";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import { Identity } from "~/shared/identity";
import { useTimeZone } from "~/shared/time-zone-preference";

export const recentRunLimit = 10;

export function RecentRuns({
  available,
  description,
  emptyMessage,
  linkExecution = false,
  plainEmpty = false,
  runs,
  showSource = false,
  showTarget = true,
  title = "Recent runs",
  viewAllTo,
}: Readonly<{
  available: boolean;
  description?: string;
  emptyMessage: string;
  linkExecution?: boolean;
  plainEmpty?: boolean;
  runs: Run[];
  showSource?: boolean;
  showTarget?: boolean;
  title?: string;
  viewAllTo: string;
}>) {
  const { timeZone } = useTimeZone();
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
        title={title}
      />
      {!available ? (
        <Alert color="red" title="Run history unavailable">
          Run history could not be loaded.
        </Alert>
      ) : runs.length === 0 ? (
        plainEmpty ? (
          <Text c="dimmed">{emptyMessage}</Text>
        ) : (
          <Paper p="md" withBorder>
            <Text c="dimmed">{emptyMessage}</Text>
          </Paper>
        )
      ) : (
        <Paper withBorder>
          <Table.ScrollContainer minWidth={showSource ? 1000 : 900} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Run</Table.Th>
                  <Table.Th>{showTarget ? "Workload" : "Action"}</Table.Th>
                  <Table.Th miw={180} w={180}>
                    Status
                  </Table.Th>
                  <Table.Th>Execution</Table.Th>
                  {showSource ? <Table.Th>Source</Table.Th> : null}
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
                    <Table.Td miw={180} w={180}>
                      <Group gap="xs" justify="space-between" wrap="nowrap">
                        <Badge color={runStatusColor(run)} variant="light">
                          {runStatusLabel(run)}
                        </Badge>
                        <KawaiiActivity activity={runActivity(run)} animationKey={run.id} compact />
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        {linkExecution ? (
                          <Anchor
                            component={Link}
                            size="sm"
                            to={`/clusters/${encodeURIComponent(run.cluster)}`}
                          >
                            {run.cluster}
                          </Anchor>
                        ) : (
                          <Text size="sm">{run.cluster}</Text>
                        )}
                        {linkExecution ? (
                          <Anchor
                            c="dimmed"
                            component={Link}
                            size="xs"
                            to={`/placements/${encodeURIComponent(run.placement)}`}
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
                    {showSource ? (
                      <Table.Td>
                        <RunSourceSummary run={run} timeZone={timeZone} />
                      </Table.Td>
                    ) : null}
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

function RunSourceSummary({ run, timeZone }: Readonly<{ run: Run; timeZone: string }>) {
  const principal =
    run.createdBy?.displayName ??
    (run.source.type === "git" || run.source.type === "schedule" ? "Automation" : "System");

  return (
    <Stack gap={4}>
      {run.source.type === "schedule" && run.source.schedule ? (
        <Tooltip
          label={`Trigger: ${run.source.schedule.triggerName} · Instance: ${formatTimestamp(run.source.schedule.scheduledAt, timeZone)}`}
          openDelay={350}
          withArrow
        >
          <Text
            aria-label={`Schedule. Trigger ${run.source.schedule.triggerName}. Instance ${formatTimestamp(run.source.schedule.scheduledAt, timeZone)}.`}
            component="span"
            display="inline-block"
            size="sm"
            style={{ alignSelf: "flex-start" }}
            tabIndex={0}
          >
            Schedule
          </Text>
        </Tooltip>
      ) : run.source.type === "promotion" && run.source.promotion ? (
        <Anchor component={Link} size="sm" to={`/promotions/${run.source.promotion.promotionId}`}>
          {runSourceLabel(run.source)}
        </Anchor>
      ) : (
        <Text size="sm">{runSourceLabel(run.source)}</Text>
      )}
      <Identity avatarSize={22} name={principal} nameProps={{ fw: 500, size: "xs" }} />
    </Stack>
  );
}
