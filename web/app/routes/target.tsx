import {
  Alert,
  Anchor,
  Badge,
  Card,
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
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getRepositoryCatalog,
  listRepositories,
  type Action,
  type ArtifactDeclaration,
  type ArtifactInput,
  type Parameter,
  type ResourceRequirements,
  type Target,
} from "~/api/generated";

import type { Route } from "./+types/target";

export async function loader({ params }: Route.LoaderArgs) {
  const client = createServerAPIClient();
  const [repositoriesResult, catalogResult] = await Promise.all([
    listRepositories({ client }),
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
      <Alert color="red" title="API unavailable">
        The target definition could not be loaded.
      </Alert>
    );
  }

  if (loaderData.repositoryListAvailable && !loaderData.repository) {
    return (
      <Alert color="red" title="Repository not found">
        This repository is not in the active repository list.
      </Alert>
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
      <Stack gap="xs">
        <RepositoryLink
          repositoryId={loaderData.repositoryId}
          repositoryName={loaderData.repository?.name}
        />
        <Group align="flex-start" justify="space-between">
          <Stack gap={2}>
            <Title order={1}>{target.metadata.title ?? target.metadata.name}</Title>
            {target.metadata.title ? <Text c="dimmed">{target.metadata.name}</Text> : null}
            <Text c="dimmed" size="sm">
              {target.source.path} · document {target.source.document}
            </Text>
          </Stack>
          <Stack align="flex-end" gap="xs">
            <Badge size="lg" variant="outline">
              {actions.length} actions
            </Badge>
            <Text c="dimmed" size="xs">
              Desired <Code>{shortCommit(target.status.desiredCommitSha)}</Code>
            </Text>
            <Text c="dimmed" size="xs">
              Catalog <Code>{shortCommit(loaderData.catalog.commitSha)}</Code>
            </Text>
          </Stack>
        </Group>
      </Stack>

      <TargetDefaults target={target} />

      <Stack gap="md">
        <Title order={2}>Actions</Title>
        {actions.map(([name, actionDefinition]) => (
          <ActionDetail
            actionDefinition={actionDefinition}
            key={name}
            name={name}
            target={target}
          />
        ))}
      </Stack>
    </Stack>
  );
}

function RepositoryLink({
  repositoryId,
  repositoryName,
}: Readonly<{ repositoryId: string; repositoryName?: string }>) {
  return (
    <Anchor component={Link} size="sm" to={`/repositories/${repositoryId}`}>
      ← {repositoryName ?? "Repository"}
    </Anchor>
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

function ActionDetail({
  actionDefinition,
  name,
  target,
}: Readonly<{ actionDefinition: Action; name: string; target: Target }>) {
  const effective = resolveAction(target, actionDefinition);
  const parameters = sortedEntries(actionDefinition.parameters);
  const artifacts = sortedEntries(actionDefinition.artifacts);
  const inputs = sortedEntries(actionDefinition.inputs);

  return (
    <Card padding="lg" withBorder>
      <Stack gap="lg">
        <Group justify="space-between">
          <Title order={3}>{name}</Title>
          {actionDefinition.intent ? (
            <Badge variant="outline">{actionDefinition.intent}</Badge>
          ) : null}
        </Group>

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
    </Card>
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
        <Table.ScrollContainer minWidth={640}>
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

function resolveAction(target: Target, actionDefinition: Action) {
  return {
    credentials: {
      ...target.spec.credentials,
      ...actionDefinition.credentials,
    },
    placement: actionDefinition.placement ?? target.spec.placement,
    resources: {
      limits: {
        ...target.spec.resources?.limits,
        ...actionDefinition.resources?.limits,
      },
      requests: {
        ...target.spec.resources?.requests,
        ...actionDefinition.resources?.requests,
      },
    } satisfies ResourceRequirements,
    runtime: actionDefinition.runtime ?? target.spec.runtime,
    timeout: actionDefinition.timeout ?? target.spec.timeout,
  };
}

function parameterRow(name: string, parameter: Parameter) {
  let delivery = "Not mapped";

  if (parameter.environment) {
    delivery = `Environment ${parameter.environment}`;
  } else if (parameter.argument) {
    delivery = parameter.argument.name
      ? `Argument ${parameter.argument.name}`
      : "Positional argument";
  }

  return [
    name,
    parameter.type,
    parameter.required ? "Yes" : "No",
    delivery,
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

function sortedEntries<T>(values: Record<string, T> | undefined) {
  return Object.entries(values ?? {}).toSorted(([left], [right]) => left.localeCompare(right));
}

function shortCommit(commit: string) {
  return commit.slice(0, 12);
}
