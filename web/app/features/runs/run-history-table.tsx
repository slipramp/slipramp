import { Anchor, Badge, Group, Paper, Stack, Table, Text, Tooltip } from "@mantine/core";
import { Link } from "react-router";

import type { Run } from "~/api/generated";
import { CommitReference } from "~/features/commits/git-commit";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { runActivity } from "~/features/mascot/execution-activity";
import { runSourceLabel } from "~/features/runs/run-source";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import { formatDuration, formatTimestamp, shortIdentifier } from "~/shared/display";
import { RelativeTime } from "~/shared/display-values";
import { Identity } from "~/shared/identity";
import { useTimeZone } from "~/shared/time-zone-preference";

export function RunHistoryTable({
  emptyMessage,
  repositoryNames,
  runs,
  showRevision = false,
  showSource = false,
  showTarget = true,
}: Readonly<{
  emptyMessage: string;
  repositoryNames?: ReadonlyMap<string, string>;
  runs: Run[];
  showRevision?: boolean;
  showSource?: boolean;
  showTarget?: boolean;
}>) {
  const { timeZone } = useTimeZone();

  if (runs.length === 0) {
    return (
      <Paper p="md" withBorder>
        <Text c="dimmed">{emptyMessage}</Text>
      </Paper>
    );
  }

  const minWidth = 820 + (showRevision ? 180 : 0) + (showSource ? 180 : 0);

  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={minWidth} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{showTarget ? "Workload / run" : "Action / run"}</Table.Th>
              <Table.Th miw={240} w={240}>
                Status
              </Table.Th>
              <Table.Th>Execution</Table.Th>
              {showRevision ? <Table.Th>Revision</Table.Th> : null}
              {showSource ? <Table.Th>Source / actor</Table.Th> : null}
              <Table.Th>Timing</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {runs.map((run) => (
              <Table.Tr key={run.id}>
                <Table.Td>
                  <RunSummary
                    repositoryName={
                      repositoryNames
                        ? (repositoryNames.get(run.repositoryId) ??
                          shortIdentifier(run.repositoryId))
                        : undefined
                    }
                    run={run}
                    showTarget={showTarget}
                  />
                </Table.Td>
                <Table.Td>
                  <RunStatus run={run} />
                </Table.Td>
                <Table.Td>
                  <RunExecution run={run} />
                </Table.Td>
                {showRevision ? (
                  <Table.Td>
                    <RunRevision run={run} />
                  </Table.Td>
                ) : null}
                {showSource ? (
                  <Table.Td>
                    <RunSource run={run} timeZone={timeZone} />
                  </Table.Td>
                ) : null}
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  <RunTiming run={run} />
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function RunSummary({
  repositoryName,
  run,
  showTarget,
}: Readonly<{ repositoryName?: string; run: Run; showTarget: boolean }>) {
  return (
    <Stack gap={0}>
      {showTarget ? (
        <Anchor component={Link} fw={600} size="sm" to={`/targets/${run.targetId}`}>
          {run.target}
        </Anchor>
      ) : null}
      <Text
        c={showTarget ? "dimmed" : undefined}
        fw={showTarget ? undefined : 600}
        size={showTarget ? "xs" : "sm"}
      >
        {run.action}
      </Text>
      <Group gap="xs" wrap="wrap">
        <Anchor c="dimmed" component={Link} size="xs" to={`/runs/${run.id}`}>
          Run {shortIdentifier(run.id)}
        </Anchor>
        {repositoryName ? (
          <>
            <Text c="dimmed" size="xs">
              ·
            </Text>
            <Anchor c="dimmed" component={Link} size="xs" to={`/repositories/${run.repositoryId}`}>
              {repositoryName}
            </Anchor>
          </>
        ) : null}
      </Group>
    </Stack>
  );
}

function RunStatus({ run }: Readonly<{ run: Run }>) {
  return (
    <Group gap="xs" justify="space-between" miw="max-content" wrap="nowrap">
      <Stack align="flex-start" gap={4}>
        <Badge color={runStatusColor(run)} variant="light">
          {runStatusLabel(run)}
        </Badge>
        {run.observationState === "unknown" ? (
          <Badge color="yellow" variant="outline">
            Controller updates unavailable
          </Badge>
        ) : null}
      </Stack>
      <KawaiiActivity activity={runActivity(run)} animationKey={run.id} compact />
    </Group>
  );
}

function RunExecution({ run }: Readonly<{ run: Run }>) {
  return (
    <Stack gap={0}>
      <Anchor component={Link} size="sm" to={`/clusters/${encodeURIComponent(run.cluster)}`}>
        {run.cluster}
      </Anchor>
      <Anchor
        c="dimmed"
        component={Link}
        size="xs"
        to={`/placements/${encodeURIComponent(run.placement)}`}
      >
        {run.placement}
      </Anchor>
    </Stack>
  );
}

function RunRevision({ run }: Readonly<{ run: Run }>) {
  return (
    <Stack gap={0}>
      <CommitReference commit={run.commit} repositoryId={run.repositoryId} />
      <Text c="dimmed" size="xs">
        Catalog {shortIdentifier(run.catalogRevisionId)}
      </Text>
    </Stack>
  );
}

function RunSource({ run, timeZone }: Readonly<{ run: Run; timeZone: string }>) {
  const principal =
    run.createdBy?.displayName ??
    (run.source.type === "git" || run.source.type === "schedule" ? "Automation" : "System");

  return (
    <Stack gap={0}>
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
      <Identity avatarSize={22} name={principal} nameProps={{ c: "dimmed", fw: 500, size: "xs" }} />
    </Stack>
  );
}

function RunTiming({ run }: Readonly<{ run: Run }>) {
  let runtime = "Not started";
  if (run.startedAt) {
    runtime = `${formatDuration(run.startedAt, run.finishedAt)} ${run.finishedAt ? "runtime" : "elapsed"}`;
  }

  return (
    <Stack gap={0}>
      <RelativeTime size="sm" value={run.createdAt} />
      <Text c="dimmed" size="xs">
        {runtime}
      </Text>
    </Stack>
  );
}
