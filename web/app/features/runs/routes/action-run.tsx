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
  Switch,
  Table,
  Text,
} from "@mantine/core";
import { IconExternalLink, IconPlayerPlay } from "@tabler/icons-react";
import { createContext, useContext, useState, type ReactNode } from "react";
import { Form, Link, redirect, useNavigation } from "react-router";

import { decodeActionParameters, sortedEntries } from "~/features/runs/action-run";
import { RunParameterFields } from "~/features/runs/run-parameter-fields";
import {
  credentialInjectionLines,
  explainedEntries,
  explainedValue,
  provenanceSummary,
} from "~/features/targets/action-explanation";
import { createServerAPIClient } from "~/api/client.server";
import {
  createRun,
  explainTargetAction,
  getTarget,
  type Action,
  type CatalogExplanation,
  type CredentialExplanation,
  type ExplainedString,
  type InputExplanation,
  type OutputExplanation,
  type TargetDefinition,
} from "~/api/generated";
import { CommitReference } from "~/features/commits/git-commit";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import {
  CodeList,
  MappingTable,
  ResourceRequirementsTable,
  TechnicalValue,
} from "~/shared/technical-definition";
import { pageTitle } from "~/shared/page-title";
import { targetFilterPath } from "~/features/targets/target-filter";

import type { Route } from "./+types/action-run";

export function meta({ loaderData, params }: Route.MetaArgs) {
  const actionName =
    loaderData?.actionDefinition?.title ??
    (loaderData?.actionDefinition ? params.actionName : "Action");
  return [{ title: pageTitle(`Run ${actionName}`) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const dialog = new URL(request.url).searchParams.get("display") === "dialog";
  const [targetResult, explanationResult] = await Promise.all([
    getTarget({ client, path: { targetId: params.targetId } }),
    dialog
      ? Promise.resolve(undefined)
      : explainTargetAction({
          client,
          path: {
            actionName: params.actionName,
            targetId: params.targetId,
          },
        }),
  ]);
  const target = targetResult.data?.definition;

  return {
    actionDefinition: target?.spec.actions[params.actionName],
    error:
      targetResult.error?.detail ??
      targetResult.error?.title ??
      "The action definition could not be loaded.",
    explanation: explanationResult?.data,
    explanationError: explanationResult
      ? (explanationResult.error?.detail ??
        explanationResult.error?.title ??
        "The effective execution context could not be loaded.")
      : undefined,
    record: targetResult.data,
    target,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const mode = formData.get("mode");
  if (mode !== null && mode !== "view" && mode !== "background") {
    return { error: "Choose how to open the created Run.", values: {} };
  }
  const client = createServerAPIClient(request, context);
  const targetResult = await getTarget({
    client,
    path: { targetId: params.targetId },
  });
  const target = targetResult.data?.definition;
  const actionDefinition = target?.spec.actions[params.actionName];
  if (!target || targetResult.data?.archivedAt || !actionDefinition) {
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
      targetId: params.targetId,
    },
    client,
  });
  if (result.data) {
    if (mode === "background") return { runId: result.data.id };
    return redirect(`/runs/${result.data.id}`);
  }

  return {
    error: result.error?.detail ?? result.error?.title ?? "The Run could not be created.",
    values: submission.values,
  };
}

export default function ActionRun({ actionData, loaderData, params }: Route.ComponentProps) {
  const targetURL = targetPath(params.targetId);

  if (!loaderData.record) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to target" to={targetURL} />
        <Alert color="red" title="API unavailable">
          {loaderData.error}
        </Alert>
      </Stack>
    );
  }

  if (loaderData.record.archivedAt || !loaderData.target || !loaderData.actionDefinition) {
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
        actions={
          actionDefinition.documentationUrl ? (
            <Button
              component="a"
              href={actionDefinition.documentationUrl}
              rel="noreferrer"
              rightSection={<IconExternalLink aria-hidden size={14} />}
              size="compact-md"
              target="_blank"
              variant="default"
            >
              Documentation
            </Button>
          ) : null
        }
        back={<PageBackLink label={`Back to ${targetLabel}`} to={targetURL} />}
        badges={
          actionDefinition.intent ? (
            <Badge variant="outline">{actionDefinition.intent}</Badge>
          ) : null
        }
        description={
          actionDefinition.description ?? "Create an immutable execution of this action."
        }
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Target">
              <Anchor component={Link} to={targetURL}>
                {targetLabel}
              </Anchor>
            </PageHeaderFact>
            <PageHeaderFact label="Repository">
              <Anchor component={Link} to={`/repositories/${loaderData.record.repositoryId}`}>
                {loaderData.record.repositoryName}
              </Anchor>
            </PageHeaderFact>
            {actionDefinition.title ? (
              <PageHeaderFact label="Action">
                <Code>{params.actionName}</Code>
              </PageHeaderFact>
            ) : null}
            <PageHeaderFact label="Catalog">
              <CommitReference
                commit={loaderData.record.catalogCommit}
                repositoryId={loaderData.record.repositoryId}
              />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={`Run ${actionDefinition.title ?? params.actionName}`}
      />

      {loaderData.explanation ? (
        <>
          <RunForm
            actionDefinition={actionDefinition}
            error={actionData?.error}
            submittedValues={actionData?.values}
            target={target}
            targetURL={targetURL}
          />
          <ActionContext explanation={loaderData.explanation} />
          <ArtifactSection explanation={loaderData.explanation} />
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
      <Paper component="section" style={{ overflow: "hidden" }} withBorder>
        <Stack gap={0}>
          <Stack gap="lg" p="lg">
            <SectionHeader
              actions={
                <Switch
                  checked={showProvenance}
                  label="Show provenance"
                  onChange={(event) => setShowProvenance(event.currentTarget.checked)}
                  size="sm"
                />
              }
              title="Execution context"
            />
            <Stack gap={4}>
              <Text c="dimmed" fw={700} size="xs" tt="uppercase">
                Command
              </Text>
              <Code block className="technical-code-block technical-code-list">
                {explanation.process.command.value.join(" ")}
              </Code>
              <SourceLabel value={explanation.process.command} />
            </Stack>
            <ExplainedTechnicalValue explained={explanation.runtime.image} label="Image" />
            <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
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
              <TechnicalValue
                description={
                  showProvenance ? provenanceSummary(explanation.placement.name) : undefined
                }
                label="Placement"
                value={
                  <Anchor
                    component={Link}
                    to={`/placements/${encodeURIComponent(explanation.placement.name.value)}`}
                  >
                    {explanation.placement.name.value}
                  </Anchor>
                }
              />
              <ExplainedTechnicalValue
                explained={explanation.placement.cluster}
                label="Cluster"
                render={(cluster) => (
                  <Anchor component={Link} to={`/clusters/${encodeURIComponent(cluster)}`}>
                    {cluster}
                  </Anchor>
                )}
              />
              {!explanation.placement.podExtensions.resolved ||
              explanation.placement.podExtensions.value ? (
                <ExplainedTechnicalValue
                  explained={explanation.placement.podExtensions}
                  label="Pod extensions configured"
                  render={(value) => (value ? "Yes" : "No")}
                />
              ) : null}
            </SimpleGrid>
            {!explanation.placement.nodeSelector.resolved ? (
              <ExplainedTechnicalValue
                explained={explanation.placement.nodeSelector}
                label="Node selector"
              />
            ) : nodeSelector.length > 0 ? (
              <>
                <MappingTable
                  empty="No node selector"
                  entries={nodeSelector}
                  label="Node selector"
                />
                <SourceLabel value={explanation.placement.nodeSelector} />
              </>
            ) : null}
          </Stack>
          <Accordion
            defaultValue={[]}
            multiple
            style={{ borderTop: "1px solid var(--slipramp-border)" }}
          >
            <Accordion.Item value="execution-details">
              <Accordion.Control>
                <Text fw={600}>Execution details</Text>
              </Accordion.Control>
              <Accordion.Panel>
                <Stack gap="lg">
                  <SimpleGrid cols={{ base: 1, sm: 2 }}>
                    <ExplainedTechnicalValue
                      explained={explanation.placement.namespace}
                      label="Namespace"
                    />
                    <ExplainedTechnicalValue
                      explained={explanation.placement.serviceAccount}
                      label="Service account"
                    />
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
                    <ExplainedTechnicalValue
                      explained={explanation.timeout}
                      label="Timeout"
                      render={(value) => value || "No timeout"}
                    />
                  </SimpleGrid>
                  <CodeList
                    empty="No runtime environment values"
                    label="Command environment before parameters"
                    values={environment.map(
                      ([name, value]) => `${name}=${String(explainedValue(value))}`,
                    )}
                  />
                  <MappingSourceLabels entries={environment} />
                </Stack>
              </Accordion.Panel>
            </Accordion.Item>
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
            <Accordion.Item style={{ borderBottom: 0 }} value="credentials">
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
    </ProvenanceVisibilityContext.Provider>
  );
}

function ArtifactSection({ explanation }: Readonly<{ explanation: CatalogExplanation }>) {
  return (
    <Paper component="section" p="lg" withBorder>
      <Stack gap="lg">
        <SectionHeader title="Artifacts" />
        <ArtifactContext inputs={explanation.inputs} outputs={explanation.outputs} />
      </Stack>
    </Paper>
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

  if (credentials.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        No credential requirements
      </Text>
    );
  }

  return (
    <Paper className="technical-data-table" withBorder>
      <Table.ScrollContainer minWidth={760} type="native">
        <Table horizontalSpacing="sm" verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th style={{ whiteSpace: "nowrap" }} w="1%">
                Requirement
              </Table.Th>
              <Table.Th w="1%">Binding</Table.Th>
              <Table.Th w="1%">Secret reference</Table.Th>
              <Table.Th>Runtime injection</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {credentials.map((credential) => (
              <Table.Tr key={credential.requirement}>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  <Code style={{ whiteSpace: "nowrap" }}>{credential.requirement}</Code>
                </Table.Td>
                <Table.Td>
                  <TableValue
                    provenance={showProvenance ? provenanceSummary(credential.binding) : undefined}
                    value={String(explainedValue(credential.binding))}
                  />
                </Table.Td>
                <Table.Td>
                  <TableValue
                    provenance={showProvenance ? provenanceSummary(credential.source) : undefined}
                    value={
                      credential.source.resolved
                        ? credential.source.value.secretName
                        : (credential.source.unresolvedReason ?? "Unresolved")
                    }
                  />
                </Table.Td>
                <Table.Td>
                  <CredentialInjection
                    description={
                      showProvenance ? provenanceSummary(credential.injection) : undefined
                    }
                    values={credentialInjectionLines(credential.injection.value)}
                  />
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function TableValue({ provenance, value }: Readonly<{ provenance?: string; value: string }>) {
  return (
    <Stack gap={2}>
      <Code style={{ whiteSpace: "nowrap" }}>{value}</Code>
      {provenance ? (
        <Text c="dimmed" size="xs">
          {provenance}
        </Text>
      ) : null}
    </Stack>
  );
}

function CredentialInjection({
  description,
  values,
}: Readonly<{ description?: string; values: string[] }>) {
  return (
    <Stack gap={2}>
      {values.length === 0 ? (
        <Text c="dimmed" size="sm">
          No injection convention
        </Text>
      ) : (
        <Stack gap="sm">
          {values.map((value) => (
            <CredentialInjectionLine key={value} value={value} />
          ))}
        </Stack>
      )}
      {description ? (
        <Text c="dimmed" size="xs">
          {description}
        </Text>
      ) : null}
    </Stack>
  );
}

function CredentialInjectionLine({ value }: Readonly<{ value: string }>) {
  const separator = " ← ";
  const separatorIndex = value.indexOf(separator);
  const content =
    separatorIndex === -1
      ? value
      : `${value.slice(0, separatorIndex)}\n← ${value.slice(separatorIndex + separator.length)}`;

  return (
    <Code block className="technical-code-block technical-code-list">
      {content}
    </Code>
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
  target: TargetDefinition;
  targetURL: string;
}>) {
  const navigation = useNavigation();
  const [showTechnicalDetails, setShowTechnicalDetails] = useState(false);
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
    <Paper component="section" p="lg" withBorder>
      <Form method="post">
        <Stack gap="lg">
          <Stack gap="md">
            <SectionHeader
              actions={
                parameters.length > 0 ? (
                  <Switch
                    checked={showTechnicalDetails}
                    label="Show technical details"
                    onChange={(event) => setShowTechnicalDetails(event.currentTarget.checked)}
                    size="sm"
                  />
                ) : null
              }
              title="Parameters"
            />
            {error ? (
              <Alert color="red" title="Run not created">
                {error}
              </Alert>
            ) : null}
            <RunParameterFields
              declarations={actionDefinition.parameters}
              showTechnicalDetails={showTechnicalDetails}
              submittedValues={submittedValues}
            />
          </Stack>
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

function ArtifactContext({
  inputs,
  outputs,
}: Readonly<{ inputs: InputExplanation[]; outputs: OutputExplanation[] }>) {
  if (inputs.length === 0 && outputs.length === 0) {
    return <Text c="dimmed">This Action has no declared artifacts.</Text>;
  }

  return (
    <Stack gap="xl">
      {outputs.length > 0 ? <ArtifactOutputs outputs={outputs} /> : null}
      {inputs.length > 0 ? <ArtifactInputs inputs={inputs} /> : null}
    </Stack>
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
        <Paper className="technical-data-table" withBorder>
          <Table.ScrollContainer minWidth={680} type="native">
            <Table horizontalSpacing="sm" verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Artifact</Table.Th>
                  <Table.Th>Output</Table.Th>
                  <Table.Th>Compatible consumers</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {outputs.map((output) => {
                  const artifact = output.artifact.value;
                  return (
                    <Table.Tr key={output.name}>
                      <Table.Td>
                        <Stack gap={4}>
                          <Group gap="xs" wrap="wrap">
                            <Code>{output.name}</Code>
                            {artifact.sensitive ? (
                              <Badge color="orange" size="xs" variant="light">
                                Sensitive
                              </Badge>
                            ) : null}
                          </Group>
                          <SourceLabel value={output.artifact} />
                          <Text c={artifact.description ? undefined : "dimmed"} size="sm">
                            {artifact.description ?? "No description"}
                          </Text>
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        <Stack gap="sm">
                          <ArtifactTableField label="Path">
                            <Code style={{ whiteSpace: "nowrap" }}>{artifact.path}</Code>
                          </ArtifactTableField>
                          <ArtifactTableField label="Media type">
                            {artifact.mediaType ?? <Text c="dimmed">Unspecified</Text>}
                          </ArtifactTableField>
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        {output.consumers.length > 0 ? (
                          <Stack gap={2}>
                            {output.consumers.map((consumer) => (
                              <Text key={`${consumer.action}:${consumer.mountPath}`} size="sm">
                                <Code>{consumer.action}</Code> → {consumer.mountPath}
                              </Text>
                            ))}
                          </Stack>
                        ) : (
                          <Text c="dimmed">—</Text>
                        )}
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
        <Paper className="technical-data-table" withBorder>
          <Table.ScrollContainer minWidth={680} type="native">
            <Table horizontalSpacing="sm" verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Input</Table.Th>
                  <Table.Th>Producer</Table.Th>
                  <Table.Th>Consumption</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {inputs.map((item) => {
                  const input = item.input.value;
                  return (
                    <Table.Tr key={item.name}>
                      <Table.Td>
                        <Stack gap={2}>
                          <Code>{item.name}</Code>
                          <SourceLabel value={item.input} />
                          <Text
                            c={item.producer.value.description ? undefined : "dimmed"}
                            size="sm"
                          >
                            {item.producer.value.description ?? "No description"}
                          </Text>
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        <Stack gap="sm">
                          <ArtifactTableField label="Action">
                            <Code>{input.artifact.fromAction}</Code>
                          </ArtifactTableField>
                          <ArtifactTableField label="Path">
                            <Code style={{ whiteSpace: "nowrap" }}>{item.producer.value.path}</Code>
                          </ArtifactTableField>
                          <SourceLabel value={item.producer} />
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        <Stack gap="sm">
                          <ArtifactTableField label="Mount path">
                            <Code style={{ whiteSpace: "nowrap" }}>{input.mountPath}</Code>
                          </ArtifactTableField>
                          <ArtifactTableField label="Compatibility">
                            <Text size="sm">
                              {item.compatibility.resolved
                                ? item.compatibility.value
                                : (item.compatibility.unresolvedReason ?? "Unresolved")}
                            </Text>
                          </ArtifactTableField>
                          <SourceLabel value={item.compatibility} />
                        </Stack>
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

function ArtifactTableField({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap={2}>
      <Text c="dimmed" fw={600} size="xs">
        {label}
      </Text>
      {children}
    </Stack>
  );
}

function targetPath(targetId: string) {
  return `/targets/${targetId}`;
}
