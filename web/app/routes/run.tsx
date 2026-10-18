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
  Title,
} from "@mantine/core";
import { useEffect, useRef, useState } from "react";
import { Form, Link, redirect, useNavigation, useRevalidator } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  cancelRun,
  createRun,
  getRun,
  listRunArtifacts,
  type Artifact,
  type RunArtifactConsumer,
  type RunArtifactInput,
} from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/run";

const runRevalidationIntervalMs = 2000;

export function meta({ loaderData }: Route.MetaArgs) {
  const runName = loaderData?.run ? `Run ${loaderData.run.id.slice(0, 8)}` : "Run";
  return [{ title: pageTitle(runName) }];
}

export async function loader({ params }: Route.LoaderArgs) {
  const client = createServerAPIClient();
  const [runResult, artifactResult] = await Promise.all([
    getRun({ client, path: { runId: params.runId } }),
    listRunArtifacts({ client, path: { runId: params.runId } }),
  ]);

  return {
    artifacts: artifactResult.data?.items,
    run: runResult.data,
    runId: params.runId,
  };
}

export async function action({ params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const intent = formData.get("intent");
  if (intent === "cancel") {
    const result = await cancelRun({
      client: createServerAPIClient(),
      path: { runId: params.runId },
    });
    if (result.data) {
      return redirect(`/runs/${result.data.id}`);
    }

    return {
      error: result.error?.detail ?? result.error?.title ?? "The Run could not be canceled.",
    };
  }

  if (intent !== "apply-artifact") {
    return { error: "The requested Run operation is not supported." };
  }

  const artifactId = formData.get("artifactId");
  const artifactName = formData.get("artifactName");
  const actionName = formData.get("actionName");
  const inputName = formData.get("inputName");
  if (
    typeof artifactId !== "string" ||
    typeof artifactName !== "string" ||
    typeof actionName !== "string" ||
    typeof inputName !== "string"
  ) {
    return { error: "The artifact selection is incomplete." };
  }

  const client = createServerAPIClient();
  const producerResult = await getRun({ client, path: { runId: params.runId } });
  const producer = producerResult.data;
  const consumer = producer?.artifactConsumers[artifactName]?.find(
    (candidate) => candidate.action === actionName && candidate.input === inputName,
  );
  if (!producer || producer.outcome !== "succeeded" || !consumer) {
    return { error: "This artifact is not available for the requested action." };
  }

  const result = await createRun({
    body: {
      action: consumer.action,
      inputs: { [consumer.input]: artifactId },
      parameters: {},
      repositoryId: producer.repositoryId,
      source: { type: "manual" },
      target: producer.target,
    },
    client,
  });
  if (result.data) {
    return redirect(`/runs/${result.data.id}`);
  }

  return {
    error: result.error?.detail ?? result.error?.title ?? "The consuming Run could not be created.",
  };
}

export default function RunDetail({ actionData, loaderData }: Route.ComponentProps) {
  const navigation = useNavigation();
  const { revalidate, state: revalidationState } = useRevalidator();
  const phase = loaderData.run?.phase;

  useEffect(() => {
    if (phase === undefined || phase === "finished" || revalidationState !== "idle") return;

    const timer = window.setTimeout(() => void revalidate(), runRevalidationIntervalMs);
    return () => window.clearTimeout(timer);
  }, [phase, revalidate, revalidationState]);

  if (!loaderData.run) {
    return (
      <Alert color="red" title="Run not found">
        The Run could not be loaded.
      </Alert>
    );
  }

  const run = loaderData.run;
  return (
    <Stack gap="lg">
      <Group justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Run {run.id.slice(0, 8)}</Title>
          <Text c="dimmed">
            {run.target} · {run.action} · <Code>{run.commitSha.slice(0, 12)}</Code>
          </Text>
        </Stack>
        <Group gap="sm">
          {run.phase !== "finished" && run.cancellationRequestedAt === undefined ? (
            <Form method="post">
              <input name="intent" type="hidden" value="cancel" />
              <Button
                color="red"
                loading={
                  navigation.state === "submitting" &&
                  navigation.formData?.get("intent") === "cancel"
                }
                type="submit"
                variant="light"
              >
                Cancel Run
              </Button>
            </Form>
          ) : null}
          {run.phase !== "finished" && run.cancellationRequestedAt !== undefined ? (
            <Badge color="yellow" size="lg" variant="light">
              Cancellation requested
            </Badge>
          ) : null}
          <Badge color={run.outcome === "failed" ? "red" : undefined} size="lg">
            {run.outcome ?? run.phase}
          </Badge>
        </Group>
      </Group>

      {actionData?.error ? (
        <Alert color="red" title="Run operation failed">
          {actionData.error}
        </Alert>
      ) : null}

      {run.observationState === "unknown" ? (
        <Alert color="yellow" title="Execution observation unavailable">
          The cluster controller is unreachable or has not reported this Run recently. Its durable
          phase remains {run.phase} until observations resume.
        </Alert>
      ) : null}

      <InputTable inputs={run.inputs} />
      <LiveLog runId={run.id} />
      <ArtifactTable
        artifacts={loaderData.artifacts ?? []}
        consumers={run.artifactConsumers}
        producerSucceeded={run.outcome === "succeeded"}
      />
    </Stack>
  );
}

function InputTable({ inputs }: Readonly<{ inputs: Record<string, RunArtifactInput> }>) {
  const entries = Object.entries(inputs).toSorted(([left], [right]) => left.localeCompare(right));
  if (entries.length === 0) return null;

  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Title order={2}>Inputs</Title>
        <Table.ScrollContainer minWidth={720}>
          <Table>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Producer</Table.Th>
                <Table.Th>Size</Table.Th>
                <Table.Th>Digest</Table.Th>
                <Table.Th>Pin</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {entries.map(([name, input]) => (
                <Table.Tr key={name}>
                  <Table.Td>
                    <Group gap="xs">
                      <Text>{name}</Text>
                      {input.sensitive ? <Badge color="orange">Sensitive</Badge> : null}
                    </Group>
                    <Text c="dimmed" size="xs">
                      {input.mediaType} · {input.retentionState}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Anchor component={Link} to={`/runs/${input.producerRunId}`}>
                      {input.producerRunId.slice(0, 8)}
                    </Anchor>
                  </Table.Td>
                  <Table.Td>{formatBytes(input.sizeBytes)}</Table.Td>
                  <Table.Td>
                    <Code>{input.sha256.slice(0, 12)}</Code>
                  </Table.Td>
                  <Table.Td>
                    <Stack gap={0}>
                      <Code>{input.commitSha.slice(0, 12)}</Code>
                      <Text c="dimmed" size="xs">
                        Catalog {input.catalogRevisionId.slice(0, 8)}
                      </Text>
                    </Stack>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Stack>
    </Paper>
  );
}

function LiveLog({ runId }: Readonly<{ runId: string }>) {
  const [contents, setContents] = useState("");
  const [status, setStatus] = useState("Connecting");
  const outputRef = useRef<HTMLElement>(null);

  useEffect(() => {
    if (contents.length === 0) return;

    const output = outputRef.current;
    if (output !== null) {
      output.scrollTop = output.scrollHeight;
    }
  }, [contents]);

  useEffect(() => {
    const decoder = new TextDecoder();
    let nextOffsetBytes = 0;
    let reconnectTimer: number | undefined;
    let source: EventSource | undefined;
    let stopped = false;

    const connect = () => {
      const streamURL = new URL(`/api/v1/runs/${runId}/logs`, window.location.origin);
      streamURL.searchParams.set("offsetBytes", String(nextOffsetBytes));
      source = new EventSource(streamURL);
      source.addEventListener("open", () => setStatus("Live"));
      source.addEventListener("chunk", (event) => {
        const payload = JSON.parse((event as MessageEvent<string>).data) as {
          contentsBase64: string;
          nextOffsetBytes: number;
        };
        const binary = atob(payload.contentsBase64);
        const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
        nextOffsetBytes = payload.nextOffsetBytes;
        setContents((current) => current + decoder.decode(bytes, { stream: true }));
      });
      source.addEventListener("sealed", () => {
        stopped = true;
        setContents((current) => current + decoder.decode());
        setStatus("Retained");
        source?.close();
      });
      source.addEventListener("error", () => {
        source?.close();
        if (stopped || reconnectTimer !== undefined) return;

        setStatus("Reconnecting");
        reconnectTimer = window.setTimeout(() => {
          reconnectTimer = undefined;
          connect();
        }, 1000);
      });
    };

    connect();

    return () => {
      stopped = true;
      source?.close();
      if (reconnectTimer !== undefined) {
        window.clearTimeout(reconnectTimer);
      }
    };
  }, [runId]);

  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Group justify="space-between">
          <Title order={2}>Logs</Title>
          <Badge variant="light">{status}</Badge>
        </Group>
        <Code ref={outputRef} block mah={480} style={{ overflow: "auto", whiteSpace: "pre-wrap" }}>
          {contents || "Waiting for durable log output…"}
        </Code>
      </Stack>
    </Paper>
  );
}

function ArtifactTable({
  artifacts,
  consumers,
  producerSucceeded,
}: Readonly<{
  artifacts: Artifact[];
  consumers: Record<string, RunArtifactConsumer[]>;
  producerSucceeded: boolean;
}>) {
  const navigation = useNavigation();

  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Title order={2}>Artifacts</Title>
        {artifacts.length === 0 ? (
          <Text c="dimmed">No artifacts have been published.</Text>
        ) : (
          <Table.ScrollContainer minWidth={760}>
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>State</Table.Th>
                  <Table.Th>Size</Table.Th>
                  <Table.Th>Digest</Table.Th>
                  <Table.Th>Provenance</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {artifacts.map((artifact) => (
                  <Table.Tr key={artifact.id}>
                    <Table.Td>
                      <Group gap="xs">
                        <Text>{artifact.name}</Text>
                        {artifact.sensitive ? <Badge color="orange">Sensitive</Badge> : null}
                      </Group>
                      <Text c="dimmed" size="xs">
                        {artifact.mediaType}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text>{artifact.state}</Text>
                        <Text c="dimmed" size="xs">
                          {artifact.retentionState}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>{formatBytes(artifact.sizeBytes)}</Table.Td>
                    <Table.Td>
                      <Code>{artifact.sha256?.slice(0, 12) ?? "—"}</Code>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text>
                          {artifact.provenance.target} · {artifact.provenance.action}
                        </Text>
                        <Text c="dimmed" size="xs">
                          <Code>{artifact.provenance.commitSha.slice(0, 12)}</Code> · Catalog{" "}
                          {artifact.provenance.catalogRevisionId.slice(0, 8)}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="sm" wrap="nowrap">
                        {artifact.state === "sealed" && artifact.retentionState === "retained" ? (
                          <Anchor href={`/api/v1/artifacts/${artifact.id}/download`}>
                            Download
                          </Anchor>
                        ) : (
                          <Text c="dimmed">Unavailable</Text>
                        )}
                        {producerSucceeded &&
                        artifact.state === "sealed" &&
                        artifact.retentionState === "retained"
                          ? (consumers[artifact.name] ?? []).map((consumer) => (
                              <ArtifactAction
                                artifact={artifact}
                                consumer={consumer}
                                key={`${artifact.id}-${consumer.action}`}
                                submitting={
                                  navigation.state === "submitting" &&
                                  navigation.formData?.get("artifactId") === artifact.id &&
                                  navigation.formData?.get("actionName") === consumer.action
                                }
                              />
                            ))
                          : null}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Stack>
    </Paper>
  );
}

function ArtifactAction({
  artifact,
  consumer,
  submitting,
}: Readonly<{
  artifact: Artifact;
  consumer: RunArtifactConsumer;
  submitting: boolean;
}>) {
  const label =
    artifact.name === "plan" && consumer.action === "apply"
      ? "Apply this plan"
      : `Run ${consumer.action}`;

  return (
    <Form method="post">
      <input name="intent" type="hidden" value="apply-artifact" />
      <input name="artifactId" type="hidden" value={artifact.id} />
      <input name="artifactName" type="hidden" value={artifact.name} />
      <input name="actionName" type="hidden" value={consumer.action} />
      <input name="inputName" type="hidden" value={consumer.input} />
      <Button loading={submitting} size="compact-sm" type="submit" variant="light">
        {label}
      </Button>
    </Form>
  );
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}
