import {
  Accordion,
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
  Switch,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { IconExternalLink, IconPlayerPlay } from "@tabler/icons-react";
import { createContext, useContext, useState, type ReactNode } from "react";
import { Form, Link, redirect, useNavigation } from "react-router";

import {
  decodeActionParameters,
  parameterDelivery,
  parameterFieldName,
  sortedEntries,
} from "~/action-run";
import {
  credentialInjectionLines,
  explainedEntries,
  explainedValue,
  provenanceSummary,
} from "~/action-explanation";
import { createServerAPIClient } from "~/api/client.server";
import {
  createRun,
  explainActiveCatalogAction,
  getRepositoryCatalog,
  listRepositories,
  type Action,
  type CatalogExplanation,
  type CredentialExplanation,
  type ExplainedString,
  type InputExplanation,
  type OutputExplanation,
  type Parameter,
  type Target,
} from "~/api/generated";
import { CommitReference } from "~/components/git-commit";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader } from "~/components/page";
import {
  CodeList,
  CommandBlock,
  MappingTable,
  ResourceRequirementsTable,
  TechnicalValue,
} from "~/components/technical-definition";
import { pageTitle } from "~/page-title";
import { targetFilterPath } from "~/target-filter";

import type { Route } from "./+types/action-run";

export function meta({ loaderData, params }: Route.MetaArgs) {
  const actionName =
    loaderData?.actionDefinition?.title ??
    (loaderData?.actionDefinition ? params.actionName : "Action");
  return [{ title: pageTitle(`Run ${actionName}`) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [repositoriesResult, catalogResult, explanationResult] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
    getRepositoryCatalog({ client, path: { repositoryId: params.repositoryId } }),
    explainActiveCatalogAction({
      client,
      path: {
        actionName: params.actionName,
        repositoryId: params.repositoryId,
        targetName: params.targetName,
      },
    }),
  ]);
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);
  const target = catalogResult.data?.targets.find(
    (item) => item.metadata.name === params.targetName,
  );

  return {
    actionDefinition: target?.spec.actions[params.actionName],
    apiAvailable:
      repositoriesResult.data !== undefined ||
      catalogResult.data !== undefined ||
      explanationResult.data !== undefined,
    catalog: catalogResult.data,
    explanation: explanationResult.data,
    explanationError:
      explanationResult.error?.detail ??
      explanationResult.error?.title ??
      "The effective execution context could not be loaded.",
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
        description={
          <Stack gap={2}>
            <Text c="dimmed">
              {targetLabel} · {loaderData.repository?.name ?? "Repository"}
            </Text>
            {actionDefinition.title ? (
              <Text c="dimmed" size="sm">
                Technical name <Code>{params.actionName}</Code>
              </Text>
            ) : null}
            {actionDefinition.description ? (
              <Text c="dimmed" size="sm">
                {actionDefinition.description}
              </Text>
            ) : null}
            {actionDefinition.documentationUrl ? (
              <Anchor
                href={actionDefinition.documentationUrl}
                rel="noreferrer"
                size="sm"
                target="_blank"
              >
                <Group gap={4} wrap="nowrap">
                  <span>Action documentation</span>
                  <IconExternalLink aria-hidden size={14} />
                </Group>
              </Anchor>
            ) : null}
          </Stack>
        }
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
        title={`Run ${actionDefinition.title ?? params.actionName}`}
      />

      {loaderData.explanation ? (
        <>
          <ActionContext explanation={loaderData.explanation} />
          <RunForm
            actionDefinition={actionDefinition}
            error={actionData?.error}
            submittedValues={actionData?.values}
            target={target}
            targetURL={targetURL}
          />
        </>
      ) : (
        <Alert color="red" title="Execution context unavailable">
          {loaderData.explanationError}
        </Alert>
      )}
    </Stack>
  );
}

function ActionContext({ explanation }: Readonly<{ explanation: CatalogExplanation }>) {
  const [showProvenance, setShowProvenance] = useState(false);
  const environment = explainedEntries(explanation.process.environment);
  const nodeSelector = Object.entries(explanation.placement.nodeSelector.value ?? {}).toSorted(
    ([left], [right]) => left.localeCompare(right),
  );
  const requests = explainedMapValues(explanation.resources.requests);
  const limits = explainedMapValues(explanation.resources.limits);
  const resourceCount = new Set([...Object.keys(requests), ...Object.keys(limits)]).size;

  return (
    <ProvenanceVisibilityContext.Provider value={showProvenance}>
      <Stack gap="lg">
        <Group justify="flex-end">
          <Switch
            checked={showProvenance}
            label="Show provenance"
            onChange={(event) => setShowProvenance(event.currentTarget.checked)}
            size="sm"
          />
        </Group>
        <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg">
          <Paper p="lg" withBorder>
            <Stack gap="lg">
              <Stack gap={2}>
                <Title order={2}>What will run</Title>
                <Text c="dimmed" size="sm">
                  The Server resolved this context from the active catalog and current Placement
                  configuration. Parameter-derived arguments and environment values are added only
                  when the Run is created.
                </Text>
              </Stack>
              <Stack gap={4}>
                <CommandBlock command={explanation.process.command.value} />
                <SourceLabel value={explanation.process.command} />
              </Stack>
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <ExplainedTechnicalValue
                  explained={explanation.process.workingDirectory}
                  label="Process directory"
                />
                <ExplainedTechnicalValue
                  explained={explanation.process.repositoryMount}
                  label="Repository mount"
                />
                <ExplainedTechnicalValue
                  explained={explanation.process.targetPath}
                  label="Target path"
                />
                <TechnicalValue
                  description={
                    showProvenance ? provenanceSummary(explanation.runtime.name) : undefined
                  }
                  label="Runtime"
                  value={
                    <Anchor
                      component={Link}
                      to={targetFilterPath("runtime", explanation.runtime.name.value)}
                    >
                      {explanation.runtime.name.value}
                    </Anchor>
                  }
                />
                <ExplainedTechnicalValue explained={explanation.runtime.image} label="Image" />
                <ExplainedTechnicalValue
                  explained={explanation.timeout}
                  label="Timeout"
                  render={(value) => value || "No timeout"}
                />
              </SimpleGrid>
              <MappingTable
                empty="No runtime environment values"
                entries={environment.map(([name, value]) => [name, String(explainedValue(value))])}
                label="Command environment before parameters"
              />
              <MappingSourceLabels entries={environment} />
            </Stack>
          </Paper>

          <Paper p="lg" withBorder>
            <Stack gap="lg">
              <Stack gap={2}>
                <Title order={2}>Placement and access</Title>
                <Text c="dimmed" size="sm">
                  Placement references and credential injection are shown without secret values or
                  arbitrary pod configuration.
                </Text>
              </Stack>
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <TechnicalValue
                  description={
                    showProvenance ? provenanceSummary(explanation.placement.name) : undefined
                  }
                  label="Placement"
                  value={
                    <Anchor
                      component={Link}
                      to={targetFilterPath("placement", explanation.placement.name.value)}
                    >
                      {explanation.placement.name.value}
                    </Anchor>
                  }
                />
                <ExplainedTechnicalValue
                  explained={explanation.placement.cluster}
                  label="Cluster"
                />
                <ExplainedTechnicalValue
                  explained={explanation.placement.namespace}
                  label="Namespace"
                />
                <ExplainedTechnicalValue
                  explained={explanation.placement.serviceAccount}
                  label="Service account"
                />
                <ExplainedTechnicalValue
                  explained={explanation.placement.podExtensions}
                  label="Pod extensions configured"
                  render={(value) => (value ? "Yes" : "No")}
                />
              </SimpleGrid>
              <MappingTable empty="No node selector" entries={nodeSelector} label="Node selector" />
              <SourceLabel value={explanation.placement.nodeSelector} />
              <Accordion defaultValue={[]} multiple variant="contained">
                <Accordion.Item value="resources">
                  <Accordion.Control>
                    <DisclosureLabel count={resourceCount} label="Resources" />
                  </Accordion.Control>
                  <Accordion.Panel>
                    <Stack gap="sm">
                      <ResourceRequirementsTable
                        label="Requests and limits"
                        limits={limits}
                        requests={requests}
                      />
                      <ResourceSourceLabels explanation={explanation} />
                    </Stack>
                  </Accordion.Panel>
                </Accordion.Item>
                <Accordion.Item value="credentials">
                  <Accordion.Control>
                    <DisclosureLabel count={explanation.credentials.length} label="Credentials" />
                  </Accordion.Control>
                  <Accordion.Panel>
                    <Credentials credentials={explanation.credentials} />
                  </Accordion.Panel>
                </Accordion.Item>
              </Accordion>
            </Stack>
          </Paper>
        </SimpleGrid>

        <Paper p="lg" withBorder>
          <Stack gap="lg">
            <Stack gap={2}>
              <Title order={2}>Artifacts</Title>
              <Text c="dimmed" size="sm">
                Declared outputs and compatible inputs are pinned by the catalog explanation.
              </Text>
            </Stack>
            <ArtifactContext inputs={explanation.inputs} outputs={explanation.outputs} />
          </Stack>
        </Paper>
      </Stack>
    </ProvenanceVisibilityContext.Provider>
  );
}

const ProvenanceVisibilityContext = createContext(false);

function DisclosureLabel({ count, label }: Readonly<{ count: number; label: string }>) {
  return (
    <Group gap="xs">
      <Text fw={600}>{label}</Text>
      <Badge color="gray" size="sm" variant="light">
        {count}
      </Badge>
    </Group>
  );
}

type Explained<T> = {
  value: T;
  resolved: boolean;
  provenance: ExplainedString["provenance"];
  unresolvedReason?: string;
};

function ExplainedTechnicalValue<T>({
  explained,
  fallback = "Unresolved",
  label,
  render,
}: Readonly<{
  explained: Explained<T>;
  fallback?: string;
  label: string;
  render?: (value: T) => ReactNode;
}>) {
  const showProvenance = useContext(ProvenanceVisibilityContext);
  const value = explained.resolved
    ? render
      ? render(explained.value)
      : String(explained.value)
    : (explained.unresolvedReason ?? fallback);

  return (
    <TechnicalValue
      description={showProvenance ? provenanceSummary(explained) : undefined}
      label={label}
      value={value}
    />
  );
}

function SourceLabel({ value }: Readonly<{ value: Pick<Explained<unknown>, "provenance"> }>) {
  const showProvenance = useContext(ProvenanceVisibilityContext);
  if (!showProvenance) return null;

  return (
    <Text c="dimmed" size="xs">
      Source: {provenanceSummary(value)}
    </Text>
  );
}

function MappingSourceLabels({
  entries,
}: Readonly<{ entries: ReadonlyArray<readonly [string, ExplainedString]> }>) {
  const showProvenance = useContext(ProvenanceVisibilityContext);
  if (!showProvenance || entries.length === 0) return null;

  return (
    <Stack gap={2}>
      {entries.map(([name, value]) => (
        <Text c="dimmed" key={name} size="xs">
          <Code>{name}</Code> · {provenanceSummary(value)}
        </Text>
      ))}
    </Stack>
  );
}

function ResourceSourceLabels({ explanation }: Readonly<{ explanation: CatalogExplanation }>) {
  const entries = [
    ...explainedEntries(explanation.resources.requests).map(
      ([name, value]) => [`Request ${name}`, value] as const,
    ),
    ...explainedEntries(explanation.resources.limits).map(
      ([name, value]) => [`Limit ${name}`, value] as const,
    ),
  ];

  return <MappingSourceLabels entries={entries} />;
}

function Credentials({ credentials }: Readonly<{ credentials: CredentialExplanation[] }>) {
  const showProvenance = useContext(ProvenanceVisibilityContext);

  return (
    <Stack gap="xs">
      {credentials.length === 0 ? (
        <Text c="dimmed" size="sm">
          No credential requirements
        </Text>
      ) : (
        <Stack gap="xs">
          {credentials.map((credential) => {
            const injection = credentialInjectionLines(credential.injection.value);
            return (
              <Paper
                className="technical-data-table"
                key={credential.requirement}
                p="sm"
                withBorder
              >
                <Stack gap="sm">
                  <Group gap="xs" wrap="wrap">
                    <Code>{credential.requirement}</Code>
                    <Text c="dimmed" size="sm">
                      uses binding
                    </Text>
                    <Code>{String(explainedValue(credential.binding))}</Code>
                  </Group>
                  <TechnicalValue
                    description={showProvenance ? provenanceSummary(credential.source) : undefined}
                    label="Placement Secret reference"
                    value={
                      credential.source.resolved
                        ? credential.source.value.secretName
                        : (credential.source.unresolvedReason ?? "Unresolved")
                    }
                  />
                  <CodeList
                    empty="No injection convention"
                    label="Runtime injection"
                    values={injection}
                  />
                  <SourceLabel value={credential.binding} />
                  <SourceLabel value={credential.injection} />
                </Stack>
              </Paper>
            );
          })}
        </Stack>
      )}
    </Stack>
  );
}

function explainedMapValues(values: Record<string, ExplainedString>) {
  return Object.fromEntries(
    explainedEntries(values).map(([name, value]) => [name, String(explainedValue(value))]),
  );
}

function RunForm({
  actionDefinition,
  error,
  submittedValues,
  target,
  targetURL,
}: Readonly<{
  actionDefinition: Action;
  error?: string;
  submittedValues?: Record<string, string>;
  target: Target;
  targetURL: string;
}>) {
  const navigation = useNavigation();
  const inputs = sortedEntries(actionDefinition.inputs);
  const parameters = sortedEntries(actionDefinition.parameters);

  if (inputs.length > 0) {
    return (
      <Alert color="blue" title="Artifact input required">
        <Stack gap="xs">
          <Text>
            Start this action from a compatible artifact on its producing Run so the exact commit
            and catalog revision stay pinned.
          </Text>
          {inputs.map(([name, input]) => {
            const description =
              target.spec.actions[input.artifact.fromAction]?.artifacts?.[name]?.description;
            return (
              <Text key={name} size="sm">
                <Code>{name}</Code>
                {description ? ` — ${description}` : ""}
              </Text>
            );
          })}
        </Stack>
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
    description: (
      <Stack gap={2}>
        {parameter.title ? (
          <Text c="dimmed" size="xs">
            Technical name <Code>{name}</Code>
          </Text>
        ) : null}
        {parameter.description ? (
          <Text c="dimmed" size="xs">
            {parameter.description}
          </Text>
        ) : null}
        <Text c="dimmed" size="xs">
          {parameter.required ? "Required" : "Optional"} · {parameterDelivery(parameter)}
        </Text>
      </Stack>
    ),
    label: parameter.title ?? name,
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

function ArtifactContext({
  inputs,
  outputs,
}: Readonly<{ inputs: InputExplanation[]; outputs: OutputExplanation[] }>) {
  if (inputs.length === 0 && outputs.length === 0) {
    return <Text c="dimmed">This Action has no declared artifacts.</Text>;
  }

  return (
    <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg">
      <ArtifactOutputs outputs={outputs} />
      <ArtifactInputs inputs={inputs} />
    </SimpleGrid>
  );
}

function ArtifactOutputs({ outputs }: Readonly<{ outputs: OutputExplanation[] }>) {
  return (
    <Stack gap="xs">
      <Text c="dimmed" fw={700} size="xs" tt="uppercase">
        Declared outputs
      </Text>
      {outputs.length === 0 ? (
        <Text c="dimmed" size="sm">
          No output artifacts
        </Text>
      ) : (
        <Stack gap="xs">
          {outputs.map((output) => {
            const artifact = output.artifact.value;
            return (
              <Paper className="technical-data-table" key={output.name} p="sm" withBorder>
                <Stack gap="sm">
                  <Group gap="xs" justify="space-between" wrap="wrap">
                    <Code>{output.name}</Code>
                    {artifact.sensitive ? (
                      <Badge color="orange" size="xs" variant="outline">
                        Sensitive
                      </Badge>
                    ) : null}
                  </Group>
                  {artifact.description ? (
                    <Text c="dimmed" size="sm">
                      {artifact.description}
                    </Text>
                  ) : null}
                  <TechnicalValue label="Path" value={artifact.path} />
                  <TechnicalValue label="Media type" value={artifact.mediaType ?? "Unspecified"} />
                  {output.consumers.length > 0 ? (
                    <CodeList
                      empty="No compatible consumers"
                      label="Compatible consumers"
                      values={output.consumers.map(
                        (consumer) => `${consumer.action} → ${consumer.mountPath}`,
                      )}
                    />
                  ) : null}
                  <SourceLabel value={output.artifact} />
                </Stack>
              </Paper>
            );
          })}
        </Stack>
      )}
    </Stack>
  );
}

function ArtifactInputs({ inputs }: Readonly<{ inputs: InputExplanation[] }>) {
  return (
    <Stack gap="xs">
      <Text c="dimmed" fw={700} size="xs" tt="uppercase">
        Required inputs
      </Text>
      {inputs.length === 0 ? (
        <Text c="dimmed" size="sm">
          No artifact inputs
        </Text>
      ) : (
        <Stack gap="xs">
          {inputs.map((item) => {
            const input = item.input.value;
            return (
              <Paper className="technical-data-table" key={item.name} p="sm" withBorder>
                <Stack gap="sm">
                  <Code>{item.name}</Code>
                  {item.producer.value.description ? (
                    <Text c="dimmed" size="sm">
                      {item.producer.value.description}
                    </Text>
                  ) : null}
                  <TechnicalValue label="Producer Action" value={input.artifact.fromAction} />
                  <TechnicalValue label="Producer path" value={item.producer.value.path} />
                  <TechnicalValue label="Mount path" value={input.mountPath} />
                  <ExplainedTechnicalValue explained={item.compatibility} label="Compatibility" />
                  <SourceLabel value={item.input} />
                  <SourceLabel value={item.producer} />
                </Stack>
              </Paper>
            );
          })}
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
