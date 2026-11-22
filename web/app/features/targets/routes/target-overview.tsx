import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  Stack,
  Table,
  Text,
} from "@mantine/core";
import { IconExternalLink, IconPlayerPlay } from "@tabler/icons-react";
import { Link, useOutletContext } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getTarget,
  listRuns,
  type Action,
  type Run,
  type TargetReconciliationStatus,
  type TargetSyncRunCheckpoint,
} from "~/api/generated";
import { CommitReference } from "~/features/commits/git-commit";
import { actionRunPath, sortedEntries } from "~/features/runs/action-run";
import { RecentRuns } from "~/features/runs/recent-runs";
import { RunCreateButton } from "~/features/runs/run-create-dialog";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import {
  loadLatestScheduledRuns,
  scheduledActionDefinitions,
  scheduledActionsDescription,
  scheduledRunHistoryPath,
  scheduledRunPresentation,
  type ScheduledActionRun,
} from "~/features/targets/scheduled-actions";
import {
  reconciliationPresentation,
  showLastAttemptedSync,
  syncCheckpointColor,
  syncCheckpointResult,
} from "~/features/targets/target-reconciliation";
import { shortIdentifier } from "~/shared/display";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { SectionHeader } from "~/shared/page";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import type { Route } from "./+types/target-overview";
import type { TargetOutletContext } from "./target";

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

const activeRunPhases = ["queued", "assigned", "preparing", "running"] as const;
const activeRunPageSize = 100;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const targetResult = await getTarget({ client, path: { targetId: params.targetId } });
  if (!targetResult.data) {
    return { activeRuns: [], activeRunsAvailable: false, scheduledRuns: [] };
  }

  const definitions = scheduledActionDefinitions(targetResult.data.definition);
  const [scheduledRuns, activeRunsByPhase] = await Promise.all([
    loadLatestScheduledRuns(definitions, async (definition) => {
      const result = await listRuns({
        client,
        query: {
          action: definition.actionName,
          limit: 1,
          source: "schedule",
          targetId: params.targetId,
          triggerName: definition.triggerName,
        },
      });
      if (!result.data) throw new Error("scheduled Run history unavailable");

      return result.data.items[0];
    }),
    Promise.all(
      activeRunPhases.map(async (phase) => {
        const runs: Run[] = [];
        let offset = 0;

        while (true) {
          const result = await listRuns({
            client,
            query: {
              limit: activeRunPageSize,
              offset,
              phase,
              targetId: params.targetId,
            },
          });
          if (!result.data) return undefined;

          runs.push(...result.data.items);
          offset += result.data.items.length;
          if (offset >= result.data.total || result.data.items.length === 0) return runs;
        }
      }),
    ),
  ]);
  const activeRunsAvailable = activeRunsByPhase.every((runs) => runs !== undefined);
  const activeRunsByID = new Map<string, Run>();
  for (const runs of activeRunsByPhase) {
    for (const run of runs ?? []) activeRunsByID.set(run.id, run);
  }
  const activeRuns = [...activeRunsByID.values()]
    .filter((run) => run.phase !== "finished")
    .toSorted(
      (left, right) =>
        right.createdAt.localeCompare(left.createdAt) || right.id.localeCompare(left.id),
    );

  return { activeRuns, activeRunsAvailable, scheduledRuns };
}

export default function TargetOverview({ loaderData }: Route.ComponentProps) {
  const { target: record } = useOutletContext<TargetOutletContext>();
  const target = record.definition;
  const actions = sortedEntries(target.spec.actions);

  return (
    <Stack gap="lg">
      {target.spec.actions.sync ? (
        <TargetReconciliation
          archived={record.archivedAt !== undefined}
          repositoryId={record.repositoryId}
          status={target.status.reconciliation}
        />
      ) : null}

      <RecentRuns
        available={loaderData.activeRunsAvailable}
        emptyMessage="No active Runs for this Target."
        plainEmpty
        runs={loaderData.activeRuns}
        showSource
        showTarget={false}
        title="Active runs"
        viewAllTo={`/runs?targetId=${record.id}`}
      />

      {loaderData.scheduledRuns.length > 0 ? (
        <ScheduledActions
          archived={record.archivedAt !== undefined}
          records={loaderData.scheduledRuns}
          targetId={record.id}
        />
      ) : null}

      <Stack gap="md">
        <SectionHeader title="Actions" />
        <Stack gap="sm">
          {actions.map(([name, actionDefinition]) => (
            <ActionCard
              actionDefinition={actionDefinition}
              archived={record.archivedAt !== undefined}
              key={name}
              name={name}
              targetId={record.id}
            />
          ))}
        </Stack>
      </Stack>
    </Stack>
  );
}

function ScheduledActions({
  archived,
  records,
  targetId,
}: Readonly<{
  archived: boolean;
  records: ScheduledActionRun[];
  targetId: string;
}>) {
  useActivityRefresh(
    records.some(
      (record) =>
        record.state === "available" && record.run !== undefined && record.run.phase !== "finished",
    ),
  );

  return (
    <Stack gap="md">
      <SectionHeader
        description={scheduledActionsDescription(archived)}
        title="Scheduled actions"
      />
      {archived ? (
        <Alert color="gray" title="Schedules inactive">
          This Target is archived. Schedule history is retained, but new occurrences are not
          evaluated.
        </Alert>
      ) : null}
      {records.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No scheduled actions are defined for this Target.</Text>
        </Paper>
      ) : (
        <Paper withBorder>
          <Table.ScrollContainer minWidth={960} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Action</Table.Th>
                  <Table.Th>Trigger</Table.Th>
                  <Table.Th>Schedule</Table.Th>
                  <Table.Th>Latest Run</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Scheduled occurrence</Table.Th>
                  <Table.Th>History</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {records.map((record) => {
                  const presentation = scheduledRunPresentation(record);
                  const latestRun = record.state === "available" ? record.run : undefined;
                  const scheduledAt = latestRun?.source.schedule?.scheduledAt;

                  return (
                    <Table.Tr
                      key={`${record.definition.actionName}:${record.definition.triggerName}`}
                    >
                      <Table.Td>
                        <Stack gap={0}>
                          <Text fw={600} size="sm">
                            {record.definition.actionTitle ?? record.definition.actionName}
                          </Text>
                          {record.definition.actionTitle ? (
                            <Text c="dimmed" ff="monospace" size="xs">
                              {record.definition.actionName}
                            </Text>
                          ) : null}
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        <Text ff="monospace" size="sm">
                          {record.definition.triggerName}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Stack gap={0}>
                          <Code>{record.definition.cron}</Code>
                          <Text c="dimmed" size="xs">
                            UTC
                          </Text>
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        {latestRun ? (
                          <Anchor component={Link} fw={600} to={`/runs/${latestRun.id}`}>
                            {shortIdentifier(latestRun.id)}
                          </Anchor>
                        ) : (
                          <Text c="dimmed">—</Text>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Badge color={presentation.color} variant="light">
                          {presentation.label}
                        </Badge>
                      </Table.Td>
                      <Table.Td>
                        {scheduledAt ? (
                          <Timestamp size="sm" value={scheduledAt} />
                        ) : (
                          <Text c="dimmed">—</Text>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Anchor
                          component={Link}
                          fw={600}
                          size="sm"
                          to={scheduledRunHistoryPath(targetId, record.definition)}
                        >
                          View history
                        </Anchor>
                      </Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      )}
    </Stack>
  );
}

function TargetReconciliation({
  archived,
  repositoryId,
  status,
}: Readonly<{
  archived: boolean;
  repositoryId: string;
  status: TargetReconciliationStatus;
}>) {
  const presentation = reconciliationPresentation(status, archived);

  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Badge color={presentation.color} size="lg" variant="light">
            {presentation.label}
          </Badge>
        }
        title="Sync reconciliation"
      />

      <Stack gap="sm">
        {showLastAttemptedSync(status) ? (
          <SyncCheckpoint
            checkpoint={status.lastAttemptedSync}
            empty="No sync Run has been assigned."
            label="Last attempt"
            repositoryId={repositoryId}
            timestamp="assignedAt"
            timestampLabel="Assigned"
          />
        ) : null}
        <SyncCheckpoint
          checkpoint={status.lastSuccessfulSync}
          empty="No sync Run has succeeded."
          label="Last success"
          repositoryId={repositoryId}
          timestamp="finishedAt"
          timestampLabel="Finished"
        />
      </Stack>
    </Stack>
  );
}

function SyncCheckpoint({
  checkpoint,
  empty,
  label,
  repositoryId,
  timestamp,
  timestampLabel,
}: Readonly<{
  checkpoint?: TargetSyncRunCheckpoint;
  empty: string;
  label: string;
  repositoryId: string;
  timestamp: "assignedAt" | "finishedAt";
  timestampLabel: string;
}>) {
  const timestampValue = checkpoint?.[timestamp];

  return (
    <Group gap="md" justify="space-between" wrap="wrap">
      <Text c="dimmed" fw={650} size="xs" tt="uppercase">
        {label}
      </Text>
      {checkpoint ? (
        <Group gap="sm" justify="flex-end" wrap="wrap">
          {timestampValue ? (
            <Text c="dimmed" size="xs">
              {timestampLabel} <RelativeTime inherit value={timestampValue} />
            </Text>
          ) : null}
          <Badge color={syncCheckpointColor(checkpoint)} variant="light">
            {syncCheckpointResult(checkpoint)}
          </Badge>
          {checkpoint.relevanceCommit ? (
            <CommitReference commit={checkpoint.relevanceCommit} repositoryId={repositoryId} />
          ) : (
            <Text c="dimmed" fs="italic" size="sm">
              Unknown commit
            </Text>
          )}
          <Anchor component={Link} fw={650} size="sm" to={`/runs/${checkpoint.runId}`}>
            View Run
          </Anchor>
        </Group>
      ) : (
        <Text c="dimmed" size="sm">
          {empty}
        </Text>
      )}
    </Group>
  );
}

function ActionCard({
  actionDefinition,
  archived,
  name,
  targetId,
}: Readonly<{
  actionDefinition: Action;
  archived: boolean;
  name: string;
  targetId: string;
}>) {
  const inputCount = Object.keys(actionDefinition.inputs ?? {}).length;

  return (
    <Paper className="target-action-card" id={`action-${name}`} p="md" withBorder>
      <Group align="center" gap="md" justify="space-between" wrap="wrap">
        <Stack gap="xs" style={{ flex: "1 1 24rem", minWidth: 0 }}>
          <Group gap="sm" wrap="wrap">
            <Text fw={700} size="lg">
              {actionDefinition.title ?? name}
            </Text>
            {actionDefinition.title ? <Code>{name}</Code> : null}
            <Badge variant="outline">{actionDefinition.intent}</Badge>
            {inputCount > 0 ? (
              <Text c="dimmed" size="sm">
                {inputCount} input{inputCount === 1 ? "" : "s"}
              </Text>
            ) : null}
          </Group>
          {actionDefinition.description || actionDefinition.documentationUrl ? (
            <Text c="dimmed" size="sm">
              {actionDefinition.description}
              {actionDefinition.documentationUrl ? (
                <Anchor
                  aria-label="Open action documentation"
                  display="inline-flex"
                  href={actionDefinition.documentationUrl}
                  ml={6}
                  rel="noreferrer"
                  target="_blank"
                  title="Open action documentation"
                >
                  <IconExternalLink aria-hidden size={14} />
                </Anchor>
              ) : null}
            </Text>
          ) : null}
          <Text c="dimmed" ff="monospace" lineClamp={1} size="sm">
            {actionDefinition.command.join(" ")}
          </Text>
        </Stack>
        {!archived ? (
          <Group className="target-action-buttons" gap="xs" wrap="nowrap">
            <Button
              component={Link}
              leftSection={<IconPlayerPlay aria-hidden size={16} />}
              to={actionRunPath(targetId, name)}
            >
              Run action
            </Button>
            <RunCreateButton
              actionName={name}
              actionTitle={actionDefinition.title}
              label="Quick run"
              targetId={targetId}
              variant="light"
            />
          </Group>
        ) : null}
      </Group>
    </Paper>
  );
}
