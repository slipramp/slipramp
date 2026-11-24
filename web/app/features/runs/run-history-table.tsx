import { Anchor, Badge, Group, Paper, Stack, Table, Text, Tooltip } from "@mantine/core";
import { Link } from "react-router";

import type { Run } from "~/api/generated";
import { CommitReference } from "~/features/commits/git-commit";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { runActivity } from "~/features/mascot/execution-activity";
import { runActionHistoryPath } from "~/features/runs/run-history-filters";
import { runSourceLabel } from "~/features/runs/run-source";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import { targetFilterPath } from "~/features/targets/target-filter";
import { formatDuration, formatTimestamp, shortIdentifier } from "~/shared/display";
import { RelativeTime } from "~/shared/display-values";
import { Identity } from "~/shared/identity";
import { useTimeZone } from "~/shared/time-zone-preference";

export function RunHistoryTable({
  emptyMessage,
  historyFilters,
  repositoryNames,
  runs,
  showRevision = false,
  showSource = false,
  showTarget = true,
}: Readonly<{
  emptyMessage: string;
  historyFilters?: URLSearchParams;
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
                    actionFilterTo={historyFilters && runActionHistoryPath(historyFilters, run)}
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
                    <RunRevision
                      repositoryName={repositoryNames?.get(run.repositoryId)}
                      run={run}
                    />
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
  actionFilterTo,
  run,
  showTarget,
}: Readonly<{ actionFilterTo?: string; run: Run; showTarget: boolean }>) {
  return (
    <Stack align="flex-start" gap={0}>
      <Anchor component={Link} fw={600} size="sm" to={`/runs/${run.id}`}>
        Run {shortIdentifier(run.id)}
      </Anchor>
      <Text c="dimmed" size="xs" style={{ whiteSpace: "nowrap" }}>
        {showTarget ? (
          <>
            <Anchor c="inherit" component={Link} size="xs" to={`/targets/${run.targetId}`}>
              {run.target}
            </Anchor>
            {" / "}
          </>
        ) : null}
        {actionFilterTo ? (
          <Anchor
            aria-label={`Filter runs by ${run.target} / ${run.action}`}
            c="inherit"
            component={Link}
            size="xs"
            to={actionFilterTo}
          >
            {run.action}
          </Anchor>
        ) : (
          run.action
        )}
      </Text>
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
      <Anchor component={Link} size="sm" to={targetFilterPath("runtime", run.runtime)}>
        {run.runtime}
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

function RunRevision({ repositoryName, run }: Readonly<{ repositoryName?: string; run: Run }>) {
  return (
    <Stack align="flex-start" gap={0}>
      <CommitReference commit={run.commit} repositoryId={run.repositoryId} />
      <Anchor c="dimmed" component={Link} size="xs" to={`/repositories/${run.repositoryId}`}>
        {repositoryName ?? shortIdentifier(run.repositoryId)}
      </Anchor>
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
