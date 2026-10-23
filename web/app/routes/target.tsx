import {
  Accordion,
  Alert,
  Badge,
  Button,
  Code,
  Divider,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { IconPlayerPlay } from "@tabler/icons-react";
import { Link } from "react-router";

import { actionRunPath, effectiveAction, parameterDelivery, sortedEntries } from "~/action-run";
import { createServerAPIClient } from "~/api/client.server";
import {
  getRepositoryCatalog,
  listRepositories,
  type Action,
  type ArtifactDeclaration,
  type ArtifactInput,
  type Parameter,
  type Target,
} from "~/api/generated";
import { pageTitle } from "~/page-title";
import { CommitReference } from "~/components/git-commit";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, SectionHeader } from "~/components/page";
import { RunHistoryButton } from "~/components/run-history-button";

import type { Route } from "./+types/target";

export function meta({ loaderData }: Route.MetaArgs) {
  const targetName = loaderData?.target
    ? (loaderData.target.metadata.title ?? loaderData.target.metadata.name)
    : "Target";
  return [{ title: pageTitle(targetName) }];
}

export async function loader({ params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request);
  const [repositoriesResult, catalogResult] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
    getRepositoryCatalog({ client, path: { repositoryId: params.repositoryId } }),
  ]);
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);
  const target = catalogResult.data?.targets.find(
    (item) => item.metadata.name === params.targetName,
  );

  return {
    apiAvailable: repositoriesResult.data !== undefined || catalogResult.data !== undefined,
    catalog: catalogResult.data,
    repository,
    repositoryId: params.repositoryId,
    repositoryListAvailable: repositoriesResult.data !== undefined,
    target,
  };
}

export default function TargetDetail({ loaderData }: Route.ComponentProps) {
  if (!loaderData.apiAvailable) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to targets" to="/" />
        <Alert color="red" title="API unavailable">
          The target definition could not be loaded.
        </Alert>
      </Stack>
    );
  }

  if (loaderData.repositoryListAvailable && !loaderData.repository) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to targets" to="/" />
        <Alert color="red" title="Repository not found">
          This repository is not in the active repository list.
        </Alert>
      </Stack>
    );
  }

  if (!loaderData.catalog) {
    return (
      <Stack gap="md">
        <RepositoryLink
          repositoryId={loaderData.repositoryId}
          repositoryName={loaderData.repository?.name}
        />
        <Alert color="yellow" title="No active catalog">
          This repository does not have an accepted catalog revision.
        </Alert>
      </Stack>
    );
  }

  if (!loaderData.target) {
    return (
      <Stack gap="md">
        <RepositoryLink
          repositoryId={loaderData.repositoryId}
          repositoryName={loaderData.repository?.name}
        />
        <Alert color="red" title="Target not found">
          This target is not present in the active catalog revision.
        </Alert>
      </Stack>
    );
  }

  const target = loaderData.target;
  const actions = sortedEntries(target.spec.actions);

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          <RunHistoryButton
            to={`/runs?repositoryId=${loaderData.repositoryId}&target=${encodeURIComponent(target.metadata.name)}`}
          />
        }
        back={
          <RepositoryLink
            repositoryId={loaderData.repositoryId}
            repositoryName={loaderData.repository?.name}
          />
        }
        badges={
          <Badge variant="outline">
            {actions.length} action{actions.length === 1 ? "" : "s"}
          </Badge>
        }
        description={target.metadata.title ? target.metadata.name : "Operational target"}
        details={
          <Stack gap={0}>
            <Text c="dimmed" size="sm">
              {target.source.path} · document {target.source.document}
            </Text>
            <Text c="dimmed" size="xs">
              Desired <CommitReference commit={target.status.desiredCommit} /> · Catalog{" "}
              <CommitReference commit={loaderData.catalog.commit} />
            </Text>
          </Stack>
        }
        title={target.metadata.title ?? target.metadata.name}
      />

      <TargetDefaults target={target} />

      <Stack gap="md">
        <SectionHeader title="Actions" />
        <Accordion chevronPosition="left" variant="separated">
          {actions.map(([name, actionDefinition]) => (
            <ActionAccordionItem
              actionDefinition={actionDefinition}
              key={name}
              name={name}
              repositoryId={loaderData.repositoryId}
              target={target}
            />
          ))}
        </Accordion>
      </Stack>
    </Stack>
  );
}

function RepositoryLink({
  repositoryId,
  repositoryName,
}: Readonly<{ repositoryId: string; repositoryName?: string }>) {
  return (
    <PageBackLink
      label={`Back to ${repositoryName ?? "repository"}`}
      to={`/repositories/${repositoryId}`}
    />
  );
}

function TargetDefaults({ target }: Readonly<{ target: Target }>) {
  const credentials = sortedEntries(target.spec.credentials);
  const requests = sortedEntries(target.spec.resources?.requests);
  const limits = sortedEntries(target.spec.resources?.limits);

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Stack gap={2}>
          <Title order={2}>Target defaults</Title>
          <Text c="dimmed" size="sm">
            Actions inherit these values unless they provide an override.
          </Text>
        </Stack>

        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
          <DefinitionValue label="Path" value={target.spec.path ?? "Repository root"} />
          <DefinitionValue label="Runtime" value={target.spec.runtime ?? "Set by action"} />
          <DefinitionValue label="Placement" value={target.spec.placement ?? "Set by action"} />
          <DefinitionValue label="Timeout" value={target.spec.timeout ?? "Placement default"} />
        </SimpleGrid>

        <DefinitionEntries
          empty="No watch paths"
          entries={(target.spec.watch ?? []).map((path) => [path, ""] as const)}
          label="Watch paths"
          valuesOnly
        />
        <DefinitionEntries
          empty="No target credential bindings"
          entries={credentials}
          label="Credential bindings"
        />
        <ResourceEntries limits={limits} requests={requests} />
      </Stack>
    </Paper>
  );
}

function ActionAccordionItem({
  actionDefinition,
  name,
  repositoryId,
  target,
}: Readonly<{
  actionDefinition: Action;
  name: string;
  repositoryId: string;
  target: Target;
}>) {
  const parameterCount = Object.keys(actionDefinition.parameters ?? {}).length;
  const artifactCount = Object.keys(actionDefinition.artifacts ?? {}).length;
  const inputCount = Object.keys(actionDefinition.inputs ?? {}).length;

  return (
    <Accordion.Item id={`action-${name}`} value={name}>
      <Group align="center" gap="sm" pr="sm" wrap="nowrap">
        <Accordion.Control style={{ flex: 1 }}>
          <Stack gap="xs">
            <Group justify="space-between">
              <Group gap="xs">
                <Text fw={600}>{name}</Text>
                {actionDefinition.intent ? (
                  <Badge variant="outline">{actionDefinition.intent}</Badge>
                ) : null}
              </Group>
              <Group gap="xs">
                {parameterCount > 0 ? (
                  <Badge variant="light">{parameterCount} parameters</Badge>
                ) : null}
                {artifactCount > 0 ? (
                  <Badge variant="light">{artifactCount} artifacts</Badge>
                ) : null}
                {inputCount > 0 ? <Badge variant="light">{inputCount} inputs</Badge> : null}
              </Group>
            </Group>
            <Text c="dimmed" lineClamp={1} size="sm">
              {actionDefinition.command.join(" ")}
            </Text>
          </Stack>
        </Accordion.Control>
        <Button
          component={Link}
          leftSection={<IconPlayerPlay aria-hidden size={15} />}
          size="xs"
          style={{ flexShrink: 0 }}
          to={actionRunPath(repositoryId, target.metadata.name, name)}
          variant="light"
        >
          Run
        </Button>
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
  target: Target;
}>) {
  const effective = effectiveAction(target, actionDefinition);
  const parameters = sortedEntries(actionDefinition.parameters);
  const artifacts = sortedEntries(actionDefinition.artifacts);
  const inputs = sortedEntries(actionDefinition.inputs);

  return (
    <Stack gap="lg">
      <Stack gap={4}>
        <Text c="dimmed" size="xs" tt="uppercase">
          Argument array
        </Text>
        <Code block>{JSON.stringify(actionDefinition.command, null, 2)}</Code>
      </Stack>

      <SimpleGrid cols={{ base: 1, sm: 3 }}>
        <EffectiveValue
          inherited={!actionDefinition.runtime}
          label="Runtime"
          value={effective.runtime ?? "Not configured"}
        />
        <EffectiveValue
          inherited={!actionDefinition.placement}
          label="Placement"
          value={effective.placement ?? "Not configured"}
        />
        <EffectiveValue
          inherited={!actionDefinition.timeout}
          label="Timeout"
          value={effective.timeout ?? "Placement default"}
        />
      </SimpleGrid>

      <DefinitionEntries
        empty="No credential bindings"
        entries={sortedEntries(effective.credentials)}
        label="Effective credential bindings"
      />
      <ResourceEntries
        limits={sortedEntries(effective.resources.limits)}
        requests={sortedEntries(effective.resources.requests)}
      />

      {inputs.length > 0 ? (
        <Alert color="blue" title="Artifact input required">
          Start this action from a compatible artifact on its producing Run.
        </Alert>
      ) : null}

      <Divider />
      <DefinitionTable
        columns={["Parameter", "Type", "Required", "Delivery", "Allowed values"]}
        empty="No parameters"
        rows={parameters.map(([parameterName, parameter]) =>
          parameterRow(parameterName, parameter),
        )}
        title="Parameters"
      />
      <DefinitionTable
        columns={["Artifact", "Path", "Media type", "Sensitive"]}
        empty="No declared artifacts"
        rows={artifacts.map(([artifactName, artifact]) => artifactRow(artifactName, artifact))}
        title="Artifacts"
      />
      <DefinitionTable
        columns={["Input", "Source action", "Mount path", "Compatibility"]}
        empty="No artifact inputs"
        rows={inputs.map(([inputName, input]) => inputRow(inputName, input))}
        title="Inputs"
      />
    </Stack>
  );
}

function DefinitionValue({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <Stack gap={0}>
      <Text c="dimmed" size="xs" tt="uppercase">
        {label}
      </Text>
      <Text size="sm">{value}</Text>
    </Stack>
  );
}

function EffectiveValue({
  inherited,
  label,
  value,
}: Readonly<{ inherited: boolean; label: string; value: string }>) {
  return (
    <Stack gap={2}>
      <Text c="dimmed" size="xs" tt="uppercase">
        {label}
      </Text>
      <Text fw={500} size="sm">
        {value}
      </Text>
      <Text c="dimmed" size="xs">
        {inherited ? "Inherited" : "Action override"}
      </Text>
    </Stack>
  );
}

function DefinitionEntries({
  empty,
  entries,
  label,
  valuesOnly = false,
}: Readonly<{
  empty: string;
  entries: ReadonlyArray<readonly [string, string]>;
  label: string;
  valuesOnly?: boolean;
}>) {
  return (
    <Stack gap="xs">
      <Text c="dimmed" size="xs" tt="uppercase">
        {label}
      </Text>
      {entries.length === 0 ? (
        <Text c="dimmed" size="sm">
          {empty}
        </Text>
      ) : (
        <Group gap="xs">
          {entries.map(([name, value]) => (
            <Badge key={name} variant="light">
              {valuesOnly ? name : `${name} → ${value}`}
            </Badge>
          ))}
        </Group>
      )}
    </Stack>
  );
}

function ResourceEntries({
  limits,
  requests,
}: Readonly<{
  limits: ReadonlyArray<readonly [string, string]>;
  requests: ReadonlyArray<readonly [string, string]>;
}>) {
  const entries = [
    ...requests.map(([name, value]) => [`Request ${name}`, value] as const),
    ...limits.map(([name, value]) => [`Limit ${name}`, value] as const),
  ];

  return <DefinitionEntries empty="No resource values" entries={entries} label="Resources" />;
}

function DefinitionTable({
  columns,
  empty,
  rows,
  title,
}: Readonly<{ columns: string[]; empty: string; rows: string[][]; title: string }>) {
  return (
    <Stack gap="xs">
      <Title order={4}>{title}</Title>
      {rows.length === 0 ? (
        <Text c="dimmed" size="sm">
          {empty}
        </Text>
      ) : (
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
      )}
    </Stack>
  );
}

function parameterRow(name: string, parameter: Parameter) {
  return [
    name,
    parameter.type,
    parameter.required ? "Yes" : "No",
    parameterDelivery(parameter),
    parameter.values?.join(", ") ?? "—",
  ];
}

function artifactRow(name: string, artifact: ArtifactDeclaration) {
  return [
    name,
    artifact.path,
    artifact.mediaType ?? "Unspecified",
    artifact.sensitive ? "Yes" : "No",
  ];
}

function inputRow(name: string, input: ArtifactInput) {
  return [
    name,
    input.artifact.fromAction,
    input.mountPath,
    input.artifact.compatibility ?? "exact-catalog-revision",
  ];
}
