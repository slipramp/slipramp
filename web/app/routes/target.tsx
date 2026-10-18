import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Divider,
  Group,
  NativeSelect,
  NumberInput,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, Link, redirect, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  createRun,
  getRepositoryCatalog,
  listRepositories,
  type Action,
  type ArtifactDeclaration,
  type ArtifactInput,
  type Parameter,
  type ParameterValue,
  type ResourceRequirements,
  type Target,
} from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/target";

export function meta({ loaderData }: Route.MetaArgs) {
  const targetName = loaderData?.target
    ? (loaderData.target.metadata.title ?? loaderData.target.metadata.name)
    : "Target";
  return [{ title: pageTitle(targetName) }];
}

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

export async function action({ params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const actionName = stringFormValue(formData, "actionName");
  if (!actionName) {
    return runFormError("", "The selected action is missing.");
  }

  const catalogResult = await getRepositoryCatalog({
    client: createServerAPIClient(),
    path: { repositoryId: params.repositoryId },
  });
  const target = catalogResult.data?.targets.find(
    (candidate) => candidate.metadata.name === params.targetName,
  );
  const actionDefinition = target?.spec.actions[actionName];
  if (!target || !actionDefinition) {
    return runFormError(actionName, "The selected action is no longer available.");
  }

  if (Object.keys(actionDefinition.inputs ?? {}).length > 0) {
    return runFormError(
      actionName,
      "This action requires artifact inputs and must be started from a compatible producing Run.",
    );
  }

  const submission = decodeParameters(formData, actionDefinition.parameters);
  if (submission.error) {
    return runFormError(actionName, submission.error, submission.values);
  }

  const result = await createRun({
    body: {
      action: actionName,
      inputs: {},
      parameters: submission.parameters,
      repositoryId: params.repositoryId,
      source: { type: "manual" },
      target: params.targetName,
    },
    client: createServerAPIClient(),
  });
  if (result.data) {
    return redirect(`/runs/${result.data.id}`);
  }

  return runFormError(
    actionName,
    result.error?.detail ?? result.error?.title ?? "The Run could not be created.",
    submission.values,
  );
}

export default function TargetDetail({ actionData, loaderData }: Route.ComponentProps) {
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
            <Anchor
              component={Link}
              size="sm"
              to={`/runs?repositoryId=${loaderData.repositoryId}&target=${encodeURIComponent(target.metadata.name)}`}
            >
              View Run history
            </Anchor>
          </Stack>
        </Group>
      </Stack>

      <TargetDefaults target={target} />

      <Stack gap="md">
        <Title order={2}>Actions</Title>
        <Accordion defaultValue={actionData?.actionName} variant="separated">
          {actions.map(([name, actionDefinition]) => (
            <ActionAccordionItem
              actionDefinition={actionDefinition}
              error={actionData?.actionName === name ? actionData.error : undefined}
              key={name}
              name={name}
              submittedValues={actionData?.actionName === name ? actionData.values : undefined}
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

function ActionAccordionItem({
  actionDefinition,
  error,
  name,
  submittedValues,
  target,
}: Readonly<{
  actionDefinition: Action;
  error?: string;
  name: string;
  submittedValues?: Record<string, string>;
  target: Target;
}>) {
  const parameterCount = Object.keys(actionDefinition.parameters ?? {}).length;
  const artifactCount = Object.keys(actionDefinition.artifacts ?? {}).length;
  const inputCount = Object.keys(actionDefinition.inputs ?? {}).length;

  return (
    <Accordion.Item value={name}>
      <Accordion.Control>
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
              {artifactCount > 0 ? <Badge variant="light">{artifactCount} artifacts</Badge> : null}
              {inputCount > 0 ? <Badge variant="light">{inputCount} inputs</Badge> : null}
            </Group>
          </Group>
          <Text c="dimmed" lineClamp={1} size="sm">
            {actionDefinition.command.join(" ")}
          </Text>
        </Stack>
      </Accordion.Control>
      <Accordion.Panel>
        <ActionDetail
          actionDefinition={actionDefinition}
          error={error}
          name={name}
          submittedValues={submittedValues}
          target={target}
        />
      </Accordion.Panel>
    </Accordion.Item>
  );
}

function ActionDetail({
  actionDefinition,
  error,
  name,
  submittedValues,
  target,
}: Readonly<{
  actionDefinition: Action;
  error?: string;
  name: string;
  submittedValues?: Record<string, string>;
  target: Target;
}>) {
  const effective = resolveAction(target, actionDefinition);
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

      <ActionRunForm
        actionDefinition={actionDefinition}
        error={error}
        name={name}
        submittedValues={submittedValues}
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
  );
}

function ActionRunForm({
  actionDefinition,
  error,
  name,
  submittedValues,
}: Readonly<{
  actionDefinition: Action;
  error?: string;
  name: string;
  submittedValues?: Record<string, string>;
}>) {
  const navigation = useNavigation();
  const inputs = sortedEntries(actionDefinition.inputs);
  const parameters = sortedEntries(actionDefinition.parameters);
  const isSubmitting =
    navigation.state === "submitting" && navigation.formData?.get("actionName") === name;

  if (inputs.length > 0) {
    return (
      <Alert color="blue" title="Artifact input required">
        Start this action from a compatible artifact on its producing Run.
      </Alert>
    );
  }

  return (
    <Paper p="md" withBorder>
      <Form method="post">
        <Stack gap="md">
          <Stack gap={2}>
            <Title order={4}>Run action</Title>
            <Text c="dimmed" size="sm">
              Create a Run from the active catalog revision and its exact Git commit.
            </Text>
          </Stack>

          {error ? (
            <Alert color="red" title="Run not created">
              {error}
            </Alert>
          ) : null}

          <input name="actionName" type="hidden" value={name} />
          {parameters.length === 0 ? (
            <Text c="dimmed" size="sm">
              This action has no parameters.
            </Text>
          ) : (
            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              {parameters.map(([parameterName, parameter]) => (
                <ParameterControl
                  key={parameterName}
                  name={parameterName}
                  parameter={parameter}
                  submittedValue={submittedValues?.[parameterName]}
                />
              ))}
            </SimpleGrid>
          )}

          <Group justify="flex-end">
            <Button loading={isSubmitting} type="submit">
              Run {name}
            </Button>
          </Group>
        </Stack>
      </Form>
    </Paper>
  );
}

function ParameterControl({
  name,
  parameter,
  submittedValue,
}: Readonly<{ name: string; parameter: Parameter; submittedValue?: string }>) {
  const common = {
    description: parameter.required ? "Required" : "Optional",
    label: name,
    name: parameterFieldName(name),
    required: parameter.required,
  };

  switch (parameter.type) {
    case "boolean":
      return (
        <NativeSelect
          {...common}
          data={selectOptions(parameter.required, ["true", "false"])}
          defaultValue={submittedValue ?? ""}
        />
      );
    case "enum":
      return (
        <NativeSelect
          {...common}
          data={selectOptions(parameter.required, parameter.values ?? [])}
          defaultValue={submittedValue ?? ""}
        />
      );
    case "number":
      return <NumberInput {...common} defaultValue={submittedValue ?? ""} />;
    case "string":
      return <TextInput {...common} defaultValue={submittedValue ?? ""} />;
  }
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

function decodeParameters(formData: FormData, declarations: Record<string, Parameter> | undefined) {
  const parameters: Record<string, ParameterValue> = {};
  const values: Record<string, string> = {};

  for (const [name, declaration] of sortedEntries(declarations)) {
    const value = stringFormValue(formData, parameterFieldName(name));
    values[name] = value;

    if (value === "") {
      if (declaration.required) {
        return {
          error: `Parameter “${name}” is required.`,
          parameters,
          values,
        };
      }

      continue;
    }

    switch (declaration.type) {
      case "boolean":
        if (value !== "true" && value !== "false") {
          return {
            error: `Parameter “${name}” must be true or false.`,
            parameters,
            values,
          };
        }
        parameters[name] = value === "true";
        break;
      case "enum":
        if (!declaration.values?.includes(value)) {
          return {
            error: `Parameter “${name}” must be one of its allowed values.`,
            parameters,
            values,
          };
        }
        parameters[name] = value;
        break;
      case "number": {
        const number = Number(value);
        if (!Number.isFinite(number)) {
          return {
            error: `Parameter “${name}” must be a number.`,
            parameters,
            values,
          };
        }
        parameters[name] = number;
        break;
      }
      case "string":
        parameters[name] = value;
        break;
    }
  }

  return { error: undefined, parameters, values };
}

function runFormError(actionName: string, error: string, values: Record<string, string> = {}) {
  return { actionName, error, values };
}

function parameterFieldName(name: string) {
  return `parameter:${name}`;
}

function selectOptions(required: boolean | undefined, values: string[]) {
  return [
    {
      disabled: required,
      label: required ? "Select a value" : "Not set",
      value: "",
    },
    ...values.map((value) => ({ label: value, value })),
  ];
}

function stringFormValue(formData: FormData, name: string) {
  const value = formData.get(name);
  return typeof value === "string" ? value : "";
}

function sortedEntries<T>(values: Record<string, T> | undefined) {
  return Object.entries(values ?? {}).toSorted(([left], [right]) => left.localeCompare(right));
}

function shortCommit(commit: string) {
  return commit.slice(0, 12);
}
