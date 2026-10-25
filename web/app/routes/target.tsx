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
import { Link } from "react-router";

import { actionRunPath, effectiveAction, parameterDelivery, sortedEntries } from "~/action-run";
import { createServerAPIClient } from "~/api/client.server";
import {
  getTarget,
  listRuns,
  type Action,
  type ArtifactDeclaration,
  type ArtifactInput,
  type Parameter,
  type TargetDefinition,
} from "~/api/generated";
import { pageTitle } from "~/page-title";
import { targetFilterPath } from "~/target-filter";
import { CommitReference } from "~/components/git-commit";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/components/page";
import { RecentRuns, recentRunLimit } from "~/components/recent-runs";
import { RunHistoryButton } from "~/components/run-history-button";
import {
  CodeList,
  CommandBlock,
  MappingTable,
  ResourceRequirementsTable,
  TechnicalValue,
} from "~/components/technical-definition";

import type { Route } from "./+types/target";

export function meta({ loaderData }: Route.MetaArgs) {
  const targetName = loaderData?.target
    ? (loaderData.target.definition.metadata.title ?? loaderData.target.definition.metadata.name)
    : "Target";
  return [{ title: pageTitle(targetName) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [targetResult, runsResult] = await Promise.all([
    getTarget({ client, path: { targetId: params.targetId } }),
    listRuns({
      client,
      query: {
        limit: recentRunLimit,
        targetId: params.targetId,
      },
    }),
  ]);

  return {
    error:
      targetResult.error?.detail ??
      targetResult.error?.title ??
      "The target definition could not be loaded.",
    recentRuns: runsResult.data?.items ?? [],
    runsAvailable: runsResult.data !== undefined,
    target: targetResult.data,
  };
}

export default function TargetDetail({ loaderData }: Route.ComponentProps) {
  if (!loaderData.target) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to targets" to="/targets" />
        <Alert color="red" title="Target not found">
          {loaderData.error}
        </Alert>
      </Stack>
    );
  }

  const record = loaderData.target;
  const target = record.definition;
  const actions = sortedEntries(target.spec.actions);

  return (
    <Stack gap="lg">
      <PageHeader
        actions={<RunHistoryButton to={`/runs?targetId=${record.id}`} />}
        back={<PageBackLink label="Back to targets" to="/targets" />}
        badges={
          <>
            {record.archivedAt ? <Badge color="gray">Archived</Badge> : null}
            <Badge variant="outline">
              {actions.length} action{actions.length === 1 ? "" : "s"}
            </Badge>
          </>
        }
        description="Operational target"
        details={
          <PageHeaderFacts>
            {target.metadata.title ? (
              <PageHeaderFact label="Target">
                <Code>{target.metadata.name}</Code>
              </PageHeaderFact>
            ) : null}
            <PageHeaderFact label="Repository">
              <Anchor component={Link} to={`/repositories/${record.repositoryId}`}>
                {record.repositoryName}
              </Anchor>
            </PageHeaderFact>
            <PageHeaderFact label="Last relevant change">
              <CommitReference
                commit={target.status.desiredCommit}
                repositoryId={record.repositoryId}
              />
            </PageHeaderFact>
            <PageHeaderFact label="Catalog">
              <CommitReference commit={record.catalogCommit} repositoryId={record.repositoryId} />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={target.metadata.title ?? target.metadata.name}
      />

      {record.archivedAt ? (
        <Alert color="gray" title="Archived target">
          This definition is retained for Run history. It cannot start new Runs unless it becomes
          active in its repository catalog again.
        </Alert>
      ) : null}

      <Stack gap="md">
        <SectionHeader
          description="Each action resolves the target defaults below into an immutable execution."
          title="Actions"
        />
        <Accordion chevronPosition="left" className="target-actions" variant="separated">
          {actions.map(([name, actionDefinition]) => (
            <ActionAccordionItem
              actionDefinition={actionDefinition}
              archived={record.archivedAt !== undefined}
              key={name}
              name={name}
              targetId={record.id}
              target={target}
            />
          ))}
        </Accordion>
      </Stack>

      <RecentRuns
        available={loaderData.runsAvailable}
        description={`The latest ${recentRunLimit} executions of this target.`}
        emptyMessage="No Runs have been created for this target."
        linkPlacement
        runs={loaderData.recentRuns}
        showTarget={false}
        viewAllTo={`/runs?targetId=${record.id}`}
      />

      <TargetDefaults target={target} />
    </Stack>
  );
}

function TargetDefaults({ target }: Readonly<{ target: TargetDefinition }>) {
  const credentials = sortedEntries(target.spec.credentials);

  return (
    <Paper p="lg" withBorder>
      <Stack gap="lg">
        <Stack gap={2}>
          <Title order={2}>Target defaults</Title>
          <Text c="dimmed" size="sm">
            Actions inherit these values unless they provide an override.
          </Text>
        </Stack>

        <TechnicalValue label="Path" value={target.spec.path ?? "Repository root"} />
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <TechnicalValue
            label="Runtime"
            value={
              target.spec.runtime ? (
                <Anchor component={Link} to={targetFilterPath("runtime", target.spec.runtime)}>
                  {target.spec.runtime}
                </Anchor>
              ) : (
                "Set by action"
              )
            }
          />
          <TechnicalValue
            label="Placement"
            value={
              target.spec.placement ? (
                <Anchor component={Link} to={targetFilterPath("placement", target.spec.placement)}>
                  {target.spec.placement}
                </Anchor>
              ) : (
                "Set by action"
              )
            }
          />
          <TechnicalValue label="Timeout" value={target.spec.timeout ?? "Placement default"} />
        </SimpleGrid>

        <CodeList empty="No watch paths" label="Watch paths" values={target.spec.watch ?? []} />
        <MappingTable
          empty="No target credential bindings"
          entries={credentials}
          label="Credential bindings"
        />
        <ResourceRequirementsTable
          limits={target.spec.resources?.limits}
          requests={target.spec.resources?.requests}
        />
      </Stack>
    </Paper>
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
  const parameterCount = Object.keys(actionDefinition.parameters ?? {}).length;
  const artifactCount = Object.keys(actionDefinition.artifacts ?? {}).length;
  const inputCount = Object.keys(actionDefinition.inputs ?? {}).length;
  const summary = [
    parameterCount > 0 ? `${parameterCount} parameter${parameterCount === 1 ? "" : "s"}` : null,
    artifactCount > 0 ? `${artifactCount} artifact${artifactCount === 1 ? "" : "s"}` : null,
    inputCount > 0 ? `${inputCount} input${inputCount === 1 ? "" : "s"}` : null,
  ].filter(Boolean);

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
              {summary.length > 0 ? (
                <Text c="dimmed" size="sm">
                  {summary.join(" · ")}
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
            "Git change",
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
