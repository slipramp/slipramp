import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { IconExternalLink, IconPlayerPlay } from "@tabler/icons-react";
import type { ReactNode } from "react";
import { Link, useOutletContext, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getTarget,
  listRuns,
  type Action,
  type ArtifactDeclaration,
  type ArtifactInput,
  type Parameter,
  type TargetDefinition,
  type TargetReconciliationStatus,
  type TargetSyncRunCheckpoint,
} from "~/api/generated";
import { CommitReference } from "~/features/commits/git-commit";
import {
  actionRunPath,
  effectiveAction,
  parameterDelivery,
  sortedEntries,
} from "~/features/runs/action-run";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import {
  loadLatestScheduledRuns,
  scheduledActionDefinitions,
  scheduledActionsDescription,
  scheduledRunHistoryPath,
  scheduledRunPresentation,
  type ScheduledActionRun,
} from "~/features/targets/scheduled-actions";
import { targetFilterPath } from "~/features/targets/target-filter";
import {
  reconciliationPresentation,
  showLastAttemptedSync,
  syncCheckpointColor,
  syncCheckpointResult,
} from "~/features/targets/target-reconciliation";
import { shortIdentifier } from "~/shared/display";
import { Timestamp } from "~/shared/display-values";
import { SectionHeader } from "~/shared/page";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";
import {
  CommandBlock,
  MappingTable,
  ResourceRequirementsTable,
  TechnicalValue,
} from "~/shared/technical-definition";

import type { Route } from "./+types/target-overview";
import type { TargetOutletContext } from "./target";

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const targetResult = await getTarget({ client, path: { targetId: params.targetId } });
  if (!targetResult.data) return { scheduledRuns: [] };

  const definitions = scheduledActionDefinitions(targetResult.data.definition);
  const scheduledRuns = await loadLatestScheduledRuns(definitions, async (definition) => {
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
  });

  return { scheduledRuns };
}

export default function TargetOverview({ loaderData }: Route.ComponentProps) {
  const { target: record } = useOutletContext<TargetOutletContext>();
  const [searchParams, setSearchParams] = useSearchParams();
  const target = record.definition;
  const actions = sortedEntries(target.spec.actions);
  const requestedAction = searchParams.get("action");
  const expandedAction = actions.some(([name]) => name === requestedAction)
    ? requestedAction
    : null;

  function changeExpandedAction(name: string | null) {
    const next = new URLSearchParams(searchParams);
    if (name) next.set("action", name);
    else next.delete("action");
    setSearchParams(next, { preventScrollReset: true });
  }

  return (
    <Stack gap="lg">
      {target.spec.actions.sync ? (
        <TargetReconciliation
          archived={record.archivedAt !== undefined}
          repositoryId={record.repositoryId}
          status={target.status.reconciliation}
        />
      ) : null}

      {loaderData.scheduledRuns.length > 0 ? (
        <ScheduledActions
          archived={record.archivedAt !== undefined}
          records={loaderData.scheduledRuns}
          targetId={record.id}
        />
      ) : null}

      <Stack gap="md">
        <SectionHeader
          description="Each action resolves the Target defaults into an immutable execution."
          title="Actions"
        />
        <Accordion
          chevronPosition="left"
          className="target-actions"
          onChange={changeExpandedAction}
          value={expandedAction}
          variant="separated"
        >
          {actions.map(([name, actionDefinition]) => (
            <ActionAccordionItem
              actionDefinition={actionDefinition}
              archived={record.archivedAt !== undefined}
              key={name}
              name={name}
              target={target}
              targetId={record.id}
            />
          ))}
        </Accordion>
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
          {timestampValue ? (
            <Text c="dimmed" size="xs">
              {timestampLabel} <Timestamp inherit value={timestampValue} />
            </Text>
          ) : null}
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

function ActionAccordionItem({
  actionDefinition,
  archived,
  name,
  targetId,
  target,
}: Readonly<{
  actionDefinition: Action;
  archived: boolean;
  name: string;
  targetId: string;
  target: TargetDefinition;
}>) {
  const inputCount = Object.keys(actionDefinition.inputs ?? {}).length;

  return (
    <Accordion.Item className="target-action-card" id={`action-${name}`} value={name}>
      <Group align="center" className="target-action-row" gap="sm" wrap="wrap">
        <Accordion.Control style={{ flex: "1 1 24rem", minWidth: 0 }}>
          <Stack gap="xs">
            <Group gap="sm" wrap="wrap">
              <Text fw={700} size="lg">
                {actionDefinition.title ?? name}
              </Text>
              {actionDefinition.title ? <Code>{name}</Code> : null}
              {actionDefinition.intent ? (
                <Badge variant="outline">{actionDefinition.intent}</Badge>
              ) : null}
              {inputCount > 0 ? (
                <Text c="dimmed" size="sm">
                  {inputCount} input{inputCount === 1 ? "" : "s"}
                </Text>
              ) : null}
            </Group>
            {actionDefinition.description ? (
              <Text c="dimmed" size="sm">
                {actionDefinition.description}
              </Text>
            ) : null}
            <Text c="dimmed" ff="monospace" lineClamp={1} size="sm">
              {actionDefinition.command.join(" ")}
            </Text>
          </Stack>
        </Accordion.Control>
        {!archived ? (
          <Button
            className="target-action-button"
            component={Link}
            leftSection={<IconPlayerPlay aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            to={actionRunPath(targetId, name)}
          >
            Run action
          </Button>
        ) : null}
      </Group>
      <Accordion.Panel>
        <ActionDetail actionDefinition={actionDefinition} target={target} />
      </Accordion.Panel>
    </Accordion.Item>
  );
}

function ActionDetail({
  actionDefinition,
  target,
}: Readonly<{
  actionDefinition: Action;
  target: TargetDefinition;
}>) {
  const effective = effectiveAction(target, actionDefinition);
  const parameters = sortedEntries(actionDefinition.parameters);
  const artifacts = sortedEntries(actionDefinition.artifacts);
  const inputs = sortedEntries(actionDefinition.inputs);
  const triggers = actionDefinition.triggers ?? [];

  return (
    <Stack gap="lg">
      {actionDefinition.documentationUrl ? (
        <Anchor href={actionDefinition.documentationUrl} rel="noreferrer" target="_blank">
          <Group gap={4} wrap="nowrap">
            <span>Action documentation</span>
            <IconExternalLink aria-hidden size={14} />
          </Group>
        </Anchor>
      ) : null}
      <CommandBlock command={actionDefinition.command} />

      <SimpleGrid cols={{ base: 1, sm: 3 }}>
        <EffectiveValue
          inherited={!actionDefinition.runtime}
          label="Runtime"
          value={
            effective.runtime ? (
              <Anchor component={Link} to={targetFilterPath("runtime", effective.runtime)}>
                {effective.runtime}
              </Anchor>
            ) : (
              "Not configured"
            )
          }
        />
        <EffectiveValue
          inherited={!actionDefinition.placement}
          label="Placement"
          value={
            effective.placement ? (
              <Anchor component={Link} to={targetFilterPath("placement", effective.placement)}>
                {effective.placement}
              </Anchor>
            ) : (
              "Not configured"
            )
          }
        />
        <EffectiveValue
          inherited={!actionDefinition.timeout}
          label="Timeout"
          value={effective.timeout ?? "Placement default"}
        />
      </SimpleGrid>

      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
        <MappingTable
          empty="No credential bindings"
          entries={sortedEntries(effective.credentials)}
          label="Effective credential bindings"
        />
        <ResourceRequirementsTable
          limits={effective.resources.limits}
          requests={effective.resources.requests}
        />
      </SimpleGrid>

      {inputs.length > 0 ? (
        <Alert color="blue" title="Artifact input required">
          Start this action from a compatible artifact on its producing Run.
        </Alert>
      ) : null}

      {parameters.length > 0 ? (
        <DefinitionTable
          columns={["Parameter", "Description", "Type", "Required", "Delivery", "Allowed values"]}
          rows={parameters.map(([parameterName, parameter]) =>
            parameterRow(parameterName, parameter),
          )}
          title="Parameters"
        />
      ) : null}
      {artifacts.length > 0 ? (
        <DefinitionTable
          columns={["Artifact", "Description", "Path", "Media type", "Sensitive"]}
          rows={artifacts.map(([artifactName, artifact]) => artifactRow(artifactName, artifact))}
          title="Artifacts"
        />
      ) : null}
      {inputs.length > 0 ? (
        <DefinitionTable
          columns={["Input", "Description", "Source action", "Mount path", "Compatibility"]}
          rows={inputs.map(([inputName, input]) => inputRow(inputName, input, target))}
          title="Inputs"
        />
      ) : null}
      {triggers.length > 0 ? (
        <DefinitionTable
          columns={["Trigger", "Event", "Static parameters"]}
          rows={triggers.map((trigger) => [
            trigger.name,
            trigger.schedule ? `UTC cron · ${trigger.schedule.cron}` : "Git change",
            trigger.parameters ? JSON.stringify(trigger.parameters) : "—",
          ])}
          title="Triggers"
        />
      ) : null}
    </Stack>
  );
}

function EffectiveValue({
  inherited,
  label,
  value,
}: Readonly<{ inherited: boolean; label: string; value: ReactNode }>) {
  return (
    <TechnicalValue
      description={inherited ? "Inherited from target" : "Action override"}
      label={label}
      value={value}
    />
  );
}

function DefinitionTable({
  columns,
  rows,
  title,
}: Readonly<{ columns: string[]; rows: string[][]; title: string }>) {
  return (
    <Stack gap="xs">
      <Title order={4}>{title}</Title>
      <Paper className="technical-data-table" withBorder>
        <Table.ScrollContainer minWidth={640} type="native">
          <Table verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                {columns.map((column) => (
                  <Table.Th key={column}>{column}</Table.Th>
                ))}
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((row) => (
                <Table.Tr key={row[0]}>
                  {row.map((value, index) => (
                    <Table.Td key={`${columns[index]}:${value}`}>{value}</Table.Td>
                  ))}
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>
    </Stack>
  );
}

function parameterRow(name: string, parameter: Parameter) {
  return [
    parameter.title ? `${parameter.title} (${name})` : name,
    parameter.description ?? "—",
    parameter.type,
    parameter.required ? "Yes" : "No",
    parameterDelivery(parameter),
    parameter.values?.join(", ") ?? "—",
  ];
}

function artifactRow(name: string, artifact: ArtifactDeclaration) {
  return [
    name,
    artifact.description ?? "—",
    artifact.path,
    artifact.mediaType ?? "Unspecified",
    artifact.sensitive ? "Yes" : "No",
  ];
}

function inputRow(name: string, input: ArtifactInput, target: TargetDefinition) {
  const description =
    target.spec.actions[input.artifact.fromAction]?.artifacts?.[name]?.description ?? "—";

  return [
    name,
    description,
    input.artifact.fromAction,
    input.mountPath,
    input.artifact.compatibility ?? "exact-catalog-revision",
  ];
}
