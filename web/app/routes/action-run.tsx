import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  NativeSelect,
  NumberInput,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { IconPlayerPlay } from "@tabler/icons-react";
import { Form, Link, redirect, useNavigation } from "react-router";

import {
  decodeActionParameters,
  effectiveAction,
  parameterDelivery,
  parameterFieldName,
  sortedEntries,
} from "~/action-run";
import { createServerAPIClient } from "~/api/client.server";
import {
  createRun,
  getRepositoryCatalog,
  listRepositories,
  type Action,
  type ArtifactDeclaration,
  type Parameter,
  type Target,
} from "~/api/generated";
import { CommitReference } from "~/components/git-commit";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader } from "~/components/page";
import {
  CommandBlock,
  MappingTable,
  ResourceRequirementsTable,
  TechnicalValue,
} from "~/components/technical-definition";
import { pageTitle } from "~/page-title";
import { targetFilterPath } from "~/target-filter";

import type { Route } from "./+types/action-run";

export function meta({ loaderData, params }: Route.MetaArgs) {
  const actionName = loaderData?.actionDefinition ? params.actionName : "Action";
  return [{ title: pageTitle(`Run ${actionName}`) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [repositoriesResult, catalogResult] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
    getRepositoryCatalog({ client, path: { repositoryId: params.repositoryId } }),
  ]);
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);
  const target = catalogResult.data?.targets.find(
    (item) => item.metadata.name === params.targetName,
  );

  return {
    actionDefinition: target?.spec.actions[params.actionName],
    apiAvailable: repositoriesResult.data !== undefined || catalogResult.data !== undefined,
    catalog: catalogResult.data,
    repository,
    repositoryId: params.repositoryId,
    target,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const client = createServerAPIClient(request, context);
  const catalogResult = await getRepositoryCatalog({
    client,
    path: { repositoryId: params.repositoryId },
  });
  const target = catalogResult.data?.targets.find(
    (candidate) => candidate.metadata.name === params.targetName,
  );
  const actionDefinition = target?.spec.actions[params.actionName];
  if (!target || !actionDefinition) {
    return { error: "This action is no longer available.", values: {} };
  }

  if (Object.keys(actionDefinition.inputs ?? {}).length > 0) {
    return {
      error:
        "This action requires artifact inputs and must be started from a compatible producing Run.",
      values: {},
    };
  }

  const submission = decodeActionParameters(formData, actionDefinition.parameters);
  if (submission.error) {
    return { error: submission.error, values: submission.values };
  }

  const result = await createRun({
    body: {
      action: params.actionName,
      inputs: {},
      parameters: submission.parameters,
      repositoryId: params.repositoryId,
      target: params.targetName,
    },
    client,
  });
  if (result.data) return redirect(`/runs/${result.data.id}`);

  return {
    error: result.error?.detail ?? result.error?.title ?? "The Run could not be created.",
    values: submission.values,
  };
}

export default function ActionRun({ actionData, loaderData, params }: Route.ComponentProps) {
  const targetURL = targetPath(loaderData.repositoryId, params.targetName);

  if (!loaderData.apiAvailable) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to target" to={targetURL} />
        <Alert color="red" title="API unavailable">
          The action definition could not be loaded.
        </Alert>
      </Stack>
    );
  }

  if (!loaderData.catalog || !loaderData.target || !loaderData.actionDefinition) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to target" to={targetURL} />
        <Alert color="red" title="Action not found">
          This action is not present in the active catalog revision.
        </Alert>
      </Stack>
    );
  }

  const target = loaderData.target;
  const actionDefinition = loaderData.actionDefinition;
  const targetLabel = target.metadata.title ?? target.metadata.name;

  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label={`Back to ${targetLabel}`} to={targetURL} />}
        badges={
          actionDefinition.intent ? (
            <Badge variant="outline">{actionDefinition.intent}</Badge>
          ) : null
        }
        description={`${targetLabel} · ${loaderData.repository?.name ?? "Repository"}`}
        details={
          <Text c="dimmed" size="sm">
            Runs at catalog{" "}
            <CommitReference
              commit={loaderData.catalog.commit}
              repositoryId={params.repositoryId}
            />{" "}
            · last relevant change{" "}
            <CommitReference
              commit={target.status.desiredCommit}
              repositoryId={params.repositoryId}
            />
          </Text>
        }
        title={`Run ${params.actionName}`}
      />

      <ActionContext actionDefinition={actionDefinition} target={target} />
      <RunForm
        actionDefinition={actionDefinition}
        error={actionData?.error}
        submittedValues={actionData?.values}
        targetURL={targetURL}
      />
    </Stack>
  );
}

function ActionContext({
  actionDefinition,
  target,
}: Readonly<{ actionDefinition: Action; target: Target }>) {
  const effective = effectiveAction(target, actionDefinition);
  const credentials = sortedEntries(effective.credentials);
  const artifacts = sortedEntries(actionDefinition.artifacts);

  return (
    <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg">
      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <Stack gap={2}>
            <Title order={2}>What will run</Title>
            <Text c="dimmed" size="sm">
              The command and execution context are frozen into the new Run.
            </Text>
          </Stack>
          <CommandBlock command={actionDefinition.command} />
          <TechnicalValue label="Target path" value={target.spec.path ?? "Repository root"} />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TechnicalValue
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
            <TechnicalValue
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
            <TechnicalValue label="Timeout" value={effective.timeout ?? "Placement default"} />
          </SimpleGrid>
        </Stack>
      </Paper>

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <Stack gap={2}>
            <Title order={2}>Access and outputs</Title>
            <Text c="dimmed" size="sm">
              Logical credential bindings are resolved by the selected placement; secret values are
              never shown here.
            </Text>
          </Stack>
          <MappingTable
            empty="No credential bindings"
            entries={credentials}
            label="Credential bindings"
          />
          <ResourceRequirementsTable
            empty="Placement defaults"
            limits={effective.resources.limits}
            requests={effective.resources.requests}
          />
          <ArtifactOutputs artifacts={artifacts} />
        </Stack>
      </Paper>
    </SimpleGrid>
  );
}

function RunForm({
  actionDefinition,
  error,
  submittedValues,
  targetURL,
}: Readonly<{
  actionDefinition: Action;
  error?: string;
  submittedValues?: Record<string, string>;
  targetURL: string;
}>) {
  const navigation = useNavigation();
  const inputs = sortedEntries(actionDefinition.inputs);
  const parameters = sortedEntries(actionDefinition.parameters);

  if (inputs.length > 0) {
    return (
      <Alert color="blue" title="Artifact input required">
        This action consumes {inputs.map(([name]) => name).join(", ")}. Start it from a compatible
        artifact on its producing Run so the exact commit and catalog revision stay pinned.
      </Alert>
    );
  }

  return (
    <Paper p="lg" withBorder>
      <Form method="post">
        <Stack gap="lg">
          <Stack gap={2}>
            <Title order={2}>Parameters</Title>
            <Text c="dimmed" size="sm">
              Review the effective context above, then provide values for this immutable execution.
            </Text>
          </Stack>
          {error ? (
            <Alert color="red" title="Run not created">
              {error}
            </Alert>
          ) : null}
          {parameters.length === 0 ? (
            <Text c="dimmed">This action has no parameters.</Text>
          ) : (
            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              {parameters.map(([name, parameter]) => (
                <ParameterControl
                  key={name}
                  name={name}
                  parameter={parameter}
                  submittedValue={submittedValues?.[name]}
                />
              ))}
            </SimpleGrid>
          )}
          <Group justify="flex-end">
            <Button component={Link} to={targetURL} variant="default">
              Cancel
            </Button>
            <Button
              leftSection={<IconPlayerPlay aria-hidden size={16} />}
              loading={navigation.state === "submitting"}
              type="submit"
            >
              Create Run
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
    description: `${parameter.required ? "Required" : "Optional"} · ${parameterDelivery(parameter)}`,
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

function ArtifactOutputs({
  artifacts,
}: Readonly<{ artifacts: ReadonlyArray<readonly [string, ArtifactDeclaration]> }>) {
  return (
    <Stack gap="xs">
      <Text c="dimmed" fw={700} size="xs" tt="uppercase">
        Declared outputs
      </Text>
      {artifacts.length === 0 ? (
        <Text c="dimmed" size="sm">
          No output artifacts
        </Text>
      ) : (
        <Stack gap="xs">
          {artifacts.map(([name, artifact]) => (
            <Paper className="technical-data-table" key={name} p="sm" withBorder>
              <Stack gap="sm">
                <Group gap="xs" justify="space-between" wrap="wrap">
                  <Code>{name}</Code>
                  {artifact.sensitive ? (
                    <Badge color="orange" size="xs" variant="outline">
                      Sensitive
                    </Badge>
                  ) : null}
                </Group>
                <TechnicalValue label="Path" value={artifact.path} />
                <TechnicalValue label="Media type" value={artifact.mediaType ?? "Unspecified"} />
              </Stack>
            </Paper>
          ))}
        </Stack>
      )}
    </Stack>
  );
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

function targetPath(repositoryId: string, targetName: string) {
  return `/repositories/${repositoryId}/targets/${encodeURIComponent(targetName)}`;
}
