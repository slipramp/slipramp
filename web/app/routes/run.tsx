import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Menu,
  Modal,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Timeline,
  Title,
} from "@mantine/core";
import {
  IconBan,
  IconCheck,
  IconChevronDown,
  IconClock,
  IconClockExclamation,
  IconLoader2,
  IconMaximize,
  IconOctagon,
  IconPlayerPlay,
  IconRefresh,
  IconServer2,
  IconX,
  type TablerIcon,
} from "@tabler/icons-react";
import { useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { Form, Link, redirect, useNavigation } from "react-router";

import {
  ANSI_DEFAULT_BACKGROUND,
  ANSI_DEFAULT_FOREGROUND,
  parseAnsi,
  type AnsiTextSegment,
} from "~/ansi";
import { createServerAPIClient } from "~/api/client.server";
import {
  cancelRun,
  createRun,
  getRun,
  listRepositories,
  listRunArtifacts,
  listRuns,
  rerunRun,
  type Artifact,
  type LogEvent,
  type Run,
  type RunArtifactConsumer,
  type RunArtifactInput,
} from "~/api/generated";
import { useActivityRefresh } from "~/components/activity-refresh";
import { ContextValue } from "~/components/context-value";
import { RelativeTime, Timestamp } from "~/components/display";
import { CommitDetails, CommitReference } from "~/components/git-commit";
import { Identity } from "~/components/identity";
import { KawaiiActivity } from "~/components/kawaii-activity";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, SectionHeader } from "~/components/page";
import {
  formatBinarySize,
  formatDuration,
  formatElapsedDuration,
  shortIdentifier,
} from "~/display";
import { pageTitle } from "~/page-title";
import { runActivity, runResultActivity } from "~/execution-activity";
import { runStatusColor } from "~/run-status";
import { runSourceLabel } from "~/run-source";
import { targetFilterPath } from "~/target-filter";

import styles from "./run.module.css";
import type { Route } from "./+types/run";

const elapsedTimerIntervalMs = 1000;
const runRevalidationIntervalMs = 2000;

export function meta({ loaderData }: Route.MetaArgs) {
  const runName = loaderData?.run ? `Run ${loaderData.run.id.slice(0, 8)}` : "Run";
  return [{ title: pageTitle(runName) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [runResult, artifactResult, repositoriesResult] = await Promise.all([
    getRun({ client, path: { runId: params.runId } }),
    listRunArtifacts({ client, path: { runId: params.runId } }),
    listRepositories({ client, query: { limit: 200 } }),
  ]);
  const planArtifact = artifactResult.data?.items.find((artifact) => artifact.name === "plan");
  const planRunsResult = planArtifact
    ? await listRuns({ client, query: { inputArtifactId: planArtifact.id, limit: 100 } })
    : undefined;

  return {
    artifacts: artifactResult.data?.items,
    planRuns: planRunsResult?.data?.items ?? [],
    planRunsAvailable: planRunsResult === undefined || planRunsResult.data !== undefined,
    repositoryName: repositoriesResult.data?.items.find(
      (repository) => repository.id === runResult.data?.repositoryId,
    )?.name,
    rerunKey: crypto.randomUUID(),
    run: runResult.data,
    runId: params.runId,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const intent = formData.get("intent");
  if (intent === "cancel") {
    const result = await cancelRun({
      client: createServerAPIClient(request, context),
      path: { runId: params.runId },
    });
    if (result.data) {
      return redirect(`/runs/${result.data.id}`);
    }

    return {
      error: result.error?.detail ?? result.error?.title ?? "The Run could not be canceled.",
    };
  }

  if (intent === "rerun-exact" || intent === "rerun-latest") {
    const idempotencyKey = formData.get("idempotencyKey");
    const result = await rerunRun({
      body: { mode: intent === "rerun-exact" ? "exact" : "latest" },
      client: createServerAPIClient(request, context),
      headers:
        typeof idempotencyKey === "string" ? { "Idempotency-Key": idempotencyKey } : undefined,
      path: { runId: params.runId },
    });
    if (result.data) {
      return redirect(`/runs/${result.data.id}`);
    }

    return {
      error: result.error?.detail ?? result.error?.title ?? "The Run could not be started again.",
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

  const client = createServerAPIClient(request, context);
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
  useActivityRefresh(
    loaderData.run?.phase !== "finished" ||
      loaderData.planRuns.some((run) => run.phase !== "finished"),
    runRevalidationIntervalMs,
  );

  if (!loaderData.run) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to run history" to="/runs" />
        <Alert color="red" title="Run not found">
          The Run could not be loaded.
        </Alert>
      </Stack>
    );
  }

  const run = loaderData.run;
  const planArtifact = loaderData.artifacts?.find((artifact) => artifact.name === "plan");
  const applyConsumer = run.artifactConsumers.plan?.find((consumer) => consumer.action === "apply");
  const planCanBeApplied =
    run.outcome === "succeeded" &&
    planArtifact?.state === "sealed" &&
    planArtifact.retentionState === "retained" &&
    applyConsumer !== undefined;
  const resultActivity = runResultActivity(run);
  const activity = resultActivity ?? runActivity(run);
  const actor = run.createdBy?.displayName ?? (run.source.type === "git" ? "Automation" : "System");
  const rerunSubmitting =
    navigation.state === "submitting" &&
    (navigation.formData?.get("intent") === "rerun-exact" ||
      navigation.formData?.get("intent") === "rerun-latest");
  const hasArtifactInputs = Object.keys(run.inputs).length > 0;
  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          run.phase === "finished" ? (
            <Group gap="sm" wrap="wrap">
              {planCanBeApplied ? (
                <ArtifactAction
                  artifact={planArtifact}
                  consumer={applyConsumer}
                  prominent
                  submitting={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("artifactId") === planArtifact.id &&
                    navigation.formData?.get("actionName") === applyConsumer.action
                  }
                />
              ) : null}
              <Menu position="bottom-end" shadow="md" width={220}>
                <Menu.Target>
                  <Button
                    leftSection={<IconRefresh aria-hidden size={16} />}
                    loading={rerunSubmitting}
                    rightSection={<IconChevronDown aria-hidden size={14} />}
                    size="compact-md"
                    variant={planCanBeApplied ? "light" : undefined}
                  >
                    Run again
                  </Button>
                </Menu.Target>
                <Menu.Dropdown>
                  <Menu.Label>Choose revision</Menu.Label>
                  <Form method="post">
                    <input name="intent" type="hidden" value="rerun-exact" />
                    <input
                      name="idempotencyKey"
                      type="hidden"
                      value={`${loaderData.rerunKey}:exact`}
                    />
                    <Menu.Item leftSection={<IconRefresh aria-hidden size={16} />} type="submit">
                      Same revision
                    </Menu.Item>
                  </Form>
                  <Form method="post">
                    <input name="intent" type="hidden" value="rerun-latest" />
                    <input
                      name="idempotencyKey"
                      type="hidden"
                      value={`${loaderData.rerunKey}:latest`}
                    />
                    <Menu.Item
                      disabled={hasArtifactInputs}
                      leftSection={<IconPlayerPlay aria-hidden size={16} />}
                      title={
                        hasArtifactInputs
                          ? "Runs with artifact inputs stay pinned to the producing revision."
                          : "Resolve this target, action, and its parameters from the latest catalog revision."
                      }
                      type="submit"
                    >
                      Latest revision
                    </Menu.Item>
                  </Form>
                </Menu.Dropdown>
              </Menu>
            </Group>
          ) : run.cancellationRequestedAt === undefined ? (
            <Form method="post">
              <input name="intent" type="hidden" value="cancel" />
              <Button
                color="red"
                leftSection={<IconOctagon aria-hidden size={16} />}
                loading={
                  navigation.state === "submitting" &&
                  navigation.formData?.get("intent") === "cancel"
                }
                size="compact-md"
                type="submit"
                variant="subtle"
              >
                Cancel run
              </Button>
            </Form>
          ) : null
        }
        back={<PageBackLink label="Back to run history" to="/runs" />}
        badges={
          <>
            <Badge color={runStatusColor(run)} variant="light">
              {run.outcome ?? run.phase}
            </Badge>
            {run.phase !== "finished" && run.cancellationRequestedAt !== undefined ? (
              <Badge color="yellow" variant="outline">
                Cancellation requested
              </Badge>
            ) : null}
          </>
        }
        description={
          <Group gap="xs">
            <Text c="dimmed">Run {run.id.slice(0, 8)}</Text>
            <Text c="dimmed">·</Text>
            <Timestamp c="dimmed" value={run.createdAt} />
            <Text c="dimmed">·</Text>
            <RelativeTime c="dimmed" value={run.createdAt} />
          </Group>
        }
        details={
          <Group gap="xs" wrap="wrap">
            <Text c="dimmed" size="sm">
              Source: {runSourceLabel(run.source)}
            </Text>
            <Text c="dimmed" size="sm">
              ·
            </Text>
            <Identity avatarSize={22} name={actor} nameProps={{ fw: 500, size: "sm" }} />
          </Group>
        }
        title={
          <>
            <Anchor
              component={Link}
              inherit
              to={`/repositories/${run.repositoryId}/targets/${encodeURIComponent(run.target)}`}
            >
              {run.target}
            </Anchor>
            <Text c="dimmed" component="span" inherit>
              {" / "}
            </Text>
            <Text component="span" inherit>
              {run.action}
            </Text>
          </>
        }
      />

      <KawaiiActivity activity={activity} animationKey={run.id} />

      {actionData?.error ? (
        <Alert color="red" title="Run operation failed">
          {actionData.error}
        </Alert>
      ) : null}

      {run.phase !== "finished" && run.observationState === "unknown" ? (
        <Alert color="yellow" title="Execution observation unavailable">
          The cluster controller is unreachable or has not reported this Run recently. Its durable
          phase remains {run.phase} until observations resume.
        </Alert>
      ) : null}

      {run.cancellationRequestedAt ? (
        <Alert color="yellow" title="Cancellation requested">
          <Group gap="xs" wrap="wrap">
            <Text span>This Run was asked to stop</Text>
            <RelativeTime span value={run.cancellationRequestedAt} />
            {run.canceledBy ? (
              <>
                <Text span>by</Text>
                <Identity
                  avatarSize={22}
                  name={run.canceledBy.displayName}
                  nameProps={{ fw: 500, size: "sm" }}
                />
              </>
            ) : null}
          </Group>
        </Alert>
      ) : null}

      {run.phase === "finished" &&
      (run.outcomeReason || run.outcomeMessage || run.exitCode !== undefined) ? (
        <Alert
          className={resultActivity ? styles.terminalNotice : undefined}
          color={runStatusColor(run)}
          title={run.outcomeReason ?? resultActivity?.title ?? "Run finished"}
        >
          <Stack gap={4}>
            {run.outcomeMessage ? <Text>{run.outcomeMessage}</Text> : null}
            {run.exitCode !== undefined ? (
              <Text size="sm">
                Process exit code <Code>{run.exitCode}</Code>
              </Text>
            ) : null}
          </Stack>
        </Alert>
      ) : null}

      <CommitDetails commit={run.commit} repositoryId={run.repositoryId} />
      <LiveLog key={run.id} runId={run.id} />
      {planArtifact ? (
        <PlanLineage
          available={loaderData.planRunsAvailable}
          plan={planArtifact}
          runs={loaderData.planRuns}
        />
      ) : null}
      <RunOverview repositoryName={loaderData.repositoryName} run={run} />
      <ParameterTable parameters={run.parameters} />

      <InputTable inputs={run.inputs} repositoryId={run.repositoryId} />
      <ArtifactTable
        artifacts={loaderData.artifacts ?? []}
        consumers={run.artifactConsumers}
        producerSucceeded={run.outcome === "succeeded"}
      />
    </Stack>
  );
}

function PlanLineage({
  available,
  plan,
  runs,
}: Readonly<{ available: boolean; plan: Artifact; runs: Run[] }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <SectionHeader
          actions={
            <Group gap="sm">
              <Badge variant="light">
                {runs.length} {runs.length === 1 ? "run" : "runs"}
              </Badge>
              {plan.state === "sealed" && plan.retentionState === "retained" ? (
                <Anchor href={`/api/v1/artifacts/${plan.id}/download`} size="sm">
                  Download plan
                </Anchor>
              ) : null}
            </Group>
          }
          description="Apply runs stay linked to the exact retained plan they consume."
          title="Plan"
        />
        {!available ? (
          <Alert color="red" title="Plan history unavailable">
            Runs created from this plan could not be loaded.
          </Alert>
        ) : runs.length === 0 ? (
          <Text c="dimmed" size="sm">
            No runs have been created from this plan.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={540} type="native">
            <Table verticalSpacing="xs">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Run</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Created</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {runs.map((consumerRun) => (
                  <Table.Tr key={consumerRun.id}>
                    <Table.Td>
                      <Anchor component={Link} fw={600} to={`/runs/${consumerRun.id}`}>
                        {consumerRun.action} · {shortIdentifier(consumerRun.id)}
                      </Anchor>
                    </Table.Td>
                    <Table.Td>
                      <Badge color={runStatusColor(consumerRun)} variant="light">
                        {consumerRun.outcome ?? consumerRun.phase}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <RelativeTime size="sm" value={consumerRun.createdAt} />
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

function RunOverview({ repositoryName, run }: Readonly<{ repositoryName?: string; run: Run }>) {
  return (
    <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg">
      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <Title order={2}>Execution context</Title>
          <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="lg">
            <Fact label="Repository">
              <Anchor component={Link} to={`/repositories/${run.repositoryId}`}>
                {repositoryName ?? shortIdentifier(run.repositoryId)}
              </Anchor>
            </Fact>
            <Fact label="Runtime">
              <Anchor component={Link} to={targetFilterPath("runtime", run.runtime)}>
                {run.runtime}
              </Anchor>
            </Fact>
            <Fact label="Cluster">
              <Anchor component={Link} to={`/clusters/${encodeURIComponent(run.cluster)}`}>
                {run.cluster}
              </Anchor>
            </Fact>
            <Fact label="Placement">
              <Anchor component={Link} to={targetFilterPath("placement", run.placement)}>
                {run.placement}
              </Anchor>
            </Fact>
            <Fact label="Catalog revision">
              <CommitReference commit={run.commit} repositoryId={run.repositoryId} />
            </Fact>
            <Fact label="Run ID">
              <Code style={{ overflowWrap: "anywhere" }}>{run.id}</Code>
            </Fact>
            {run.rerunOfRunId && run.rerunMode ? (
              <Fact label="Run again">
                <Anchor component={Link} to={`/runs/${run.rerunOfRunId}`}>
                  {run.rerunMode === "exact" ? "Same revision as" : "Latest revision from"} Run{" "}
                  {shortIdentifier(run.rerunOfRunId)}
                </Anchor>
              </Fact>
            ) : null}
            <Fact label="Job UID">
              {run.jobUid ? (
                <Code style={{ overflowWrap: "anywhere" }}>{run.jobUid}</Code>
              ) : (
                <Text c="dimmed" size="sm">
                  Not materialized
                </Text>
              )}
            </Fact>
          </SimpleGrid>
          {run.lastObservedAt ? (
            <Group gap={4}>
              <Text c="dimmed" size="xs">
                Controller observation {run.observationState ?? "current"} · last seen
              </Text>
              <RelativeTime c="dimmed" size="xs" value={run.lastObservedAt} />
            </Group>
          ) : null}
        </Stack>
      </Paper>

      <Paper p="md" withBorder>
        <RunLifecycle run={run} />
      </Paper>
    </SimpleGrid>
  );
}

function Fact({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap={3}>
      <Text c="dimmed" fw={700} size="xs" tt="uppercase">
        {label}
      </Text>
      <ContextValue size="sm">{children}</ContextValue>
    </Stack>
  );
}

function RunLifecycle({ run }: Readonly<{ run: Run }>) {
  const activeStep = lifecycleActiveStep(run);
  const currentTime = useCurrentTime(run.phase !== "finished");
  const steps: Array<{
    detail: ReactNode;
    icon: TablerIcon;
    label: string;
    value?: string;
  }> = [
    {
      detail: lifecycleDuration(
        run.createdAt,
        run.assignedAt,
        activeStep === 0,
        "Waiting for assignment",
        "Waited",
        currentTime,
      ),
      icon: IconClock,
      label: "Queued",
      value: run.createdAt,
    },
    {
      detail: lifecycleDuration(
        run.assignedAt,
        run.preparingAt,
        activeStep === 1,
        "Waiting for Job materialization",
        "Job materialized in",
        currentTime,
      ),
      icon: IconServer2,
      label: "Assigned",
      value: run.assignedAt,
    },
    {
      detail: lifecycleDuration(
        run.preparingAt,
        run.startedAt,
        activeStep === 2,
        "Preparing checkout and inputs",
        "Prepared in",
        currentTime,
      ),
      icon: IconLoader2,
      label: "Preparing",
      value: run.preparingAt,
    },
    {
      detail: lifecycleDuration(
        run.startedAt,
        run.finishedAt,
        activeStep === 3,
        "Action is executing",
        "Executed for",
        currentTime,
      ),
      icon: IconPlayerPlay,
      label: "Running",
      value: run.startedAt,
    },
    {
      detail: run.finishedAt
        ? `Run ${run.outcome ?? "finished"}${run.exitCode === undefined ? "" : ` with exit code ${run.exitCode}`}`
        : "Terminal outcome not recorded",
      icon: finishedIcon(run),
      label: "Finished",
      value: run.finishedAt,
    },
  ];
  const summary = [
    run.assignedAt ? `Queue ${formatDuration(run.createdAt, run.assignedAt)}` : undefined,
    run.startedAt ? `To start ${formatDuration(run.createdAt, run.startedAt)}` : undefined,
    run.startedAt && run.finishedAt
      ? `Runtime ${formatDuration(run.startedAt, run.finishedAt)}`
      : undefined,
  ].filter((value): value is string => value !== undefined);

  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Group gap="xs">
            {summary.map((value) => (
              <Badge key={value} size="sm" tt="none" variant="light">
                {value}
              </Badge>
            ))}
          </Group>
        }
        title="Lifecycle"
      />
      <Timeline
        active={activeStep}
        bulletSize={28}
        className="lifecycle-timeline"
        color={run.phase === "finished" ? runStatusColor(run) : "lilac"}
        lineWidth={2}
      >
        {steps.map((step, index) => (
          <LifecycleStep
            active={activeStep === index && run.phase !== "finished"}
            complete={step.value !== undefined && (index < activeStep || run.phase === "finished")}
            icon={step.icon}
            key={step.label}
            label={step.label}
            status={index === 4 && run.outcome ? run.outcome : undefined}
            value={step.value}
          >
            {step.detail}
          </LifecycleStep>
        ))}
      </Timeline>
    </Stack>
  );
}

function LifecycleStep({
  active,
  children,
  complete,
  icon: Icon,
  label,
  status,
  value,
}: Readonly<{
  active: boolean;
  children: ReactNode;
  complete: boolean;
  icon: TablerIcon;
  label: string;
  status?: Run["outcome"];
  value?: string;
}>) {
  const state = status ?? (active ? "Current" : complete ? "Complete" : "Waiting");
  const color = status ? runOutcomeColor(status) : active ? "lilac" : "gray";

  return (
    <Timeline.Item
      bullet={<Icon aria-hidden size={15} stroke={2.2} />}
      className="lifecycle-step"
      title={
        <Group gap="xs" justify="space-between" wrap="nowrap">
          <Text fw={650} size="sm">
            {label}
          </Text>
          <Badge color={color} size="xs" variant={active || status ? "light" : "outline"}>
            {state}
          </Badge>
        </Group>
      }
    >
      {value ? (
        <Group gap={6} wrap="wrap">
          <Timestamp c="dimmed" size="xs" value={value} />
          <Text c="dimmed" size="xs">
            {children}
          </Text>
        </Group>
      ) : (
        <Text c="dimmed" size="xs">
          Not reached
        </Text>
      )}
    </Timeline.Item>
  );
}

function runOutcomeColor(outcome: NonNullable<Run["outcome"]>) {
  switch (outcome) {
    case "succeeded":
      return "green";
    case "failed":
      return "red";
    case "canceled":
      return "gray";
    case "timed-out":
      return "orange";
  }
}

function lifecycleDuration(
  start: string | undefined,
  end: string | undefined,
  active: boolean,
  activeLabel: string,
  completeLabel: string,
  currentTime: Date | undefined,
) {
  if (!start) return "Not reached";
  if (end) return `${completeLabel} ${formatDuration(start, end)}`;
  if (active) {
    const elapsed = currentTime ? ` · ${formatElapsedDuration(start, currentTime)} elapsed` : "";
    return `${activeLabel}${elapsed}`;
  }
  return "Transition timestamp unavailable";
}

function useCurrentTime(active: boolean) {
  const [currentTime, setCurrentTime] = useState<Date>();

  useEffect(() => {
    if (!active) return;

    const initialTimer = window.setTimeout(() => setCurrentTime(new Date()), 0);
    const timer = window.setInterval(() => setCurrentTime(new Date()), elapsedTimerIntervalMs);
    return () => {
      window.clearTimeout(initialTimer);
      window.clearInterval(timer);
    };
  }, [active]);

  return currentTime;
}

function finishedIcon(run: Run): TablerIcon {
  switch (run.outcome) {
    case "succeeded":
      return IconCheck;
    case "failed":
      return IconX;
    case "canceled":
      return IconBan;
    case "timed-out":
      return IconClockExclamation;
    default:
      return IconCheck;
  }
}

function lifecycleActiveStep(run: Run) {
  switch (run.phase) {
    case "queued":
      return 0;
    case "assigned":
      return 1;
    case "preparing":
      return 2;
    case "running":
      return 3;
    case "finished":
      return 4;
  }
}

function ParameterTable({ parameters }: Readonly<{ parameters: Run["parameters"] }>) {
  const entries = Object.entries(parameters).toSorted(([left], [right]) =>
    left.localeCompare(right),
  );
  if (entries.length === 0) return null;

  return (
    <Accordion className="run-parameters" defaultValue={null} order={2} variant="contained">
      <Accordion.Item value="parameters">
        <Accordion.Control>
          <Group gap="sm">
            <Text fw={650} size="lg">
              Parameters
            </Text>
            <Badge variant="light">{entries.length}</Badge>
          </Group>
        </Accordion.Control>
        <Accordion.Panel>
          <Table.ScrollContainer minWidth={520} type="native">
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Value</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {entries.map(([name, value]) => (
                  <Table.Tr key={name}>
                    <Table.Td>{name}</Table.Td>
                    <Table.Td>
                      <Code>{String(value)}</Code>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  );
}

function InputTable({
  inputs,
  repositoryId,
}: Readonly<{ inputs: Record<string, RunArtifactInput>; repositoryId: string }>) {
  const entries = Object.entries(inputs).toSorted(([left], [right]) => left.localeCompare(right));
  if (entries.length === 0) return null;

  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Title order={2}>Inputs</Title>
        <Table.ScrollContainer minWidth={720} type="native">
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
                  <Table.Td>{formatBinarySize(input.sizeBytes)}</Table.Td>
                  <Table.Td>
                    <Code>{shortIdentifier(input.sha256, 12)}</Code>
                  </Table.Td>
                  <Table.Td>
                    <Stack gap={0}>
                      <CommitReference commit={input.commit} repositoryId={repositoryId} />
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
  const [fullscreenOpened, setFullscreenOpened] = useState(false);
  const [receivedBytes, setReceivedBytes] = useState(0);
  const [status, setStatus] = useState("Connecting");
  const fullscreenOutputRef = useRef<HTMLElement>(null);
  const outputRef = useRef<HTMLElement>(null);
  const logSegments = useMemo(() => parseAnsi(contents), [contents]);

  useEffect(() => {
    if (contents.length === 0) return;

    const outputs = fullscreenOpened
      ? [outputRef.current, fullscreenOutputRef.current]
      : [outputRef.current];
    for (const output of outputs) {
      if (output !== null) {
        output.scrollTop = output.scrollHeight;
      }
    }
  }, [contents, fullscreenOpened]);

  useEffect(() => {
    const decoder = new TextDecoder();
    let nextOffsetBytes = 0;
    let pendingContents = "";
    let pendingOffsetBytes = 0;
    let renderFrame: number | undefined;
    let reconnectTimer: number | undefined;
    let source: EventSource | undefined;
    let stopped = false;

    const flushPendingContents = () => {
      renderFrame = undefined;
      if (pendingContents.length === 0) return;

      const appendedContents = pendingContents;
      pendingContents = "";
      setContents((current) => current + appendedContents);
      setReceivedBytes(pendingOffsetBytes);
    };

    const scheduleRender = () => {
      if (renderFrame === undefined) {
        renderFrame = window.requestAnimationFrame(flushPendingContents);
      }
    };

    const finishRendering = () => {
      if (renderFrame !== undefined) {
        window.cancelAnimationFrame(renderFrame);
      }
      flushPendingContents();
    };

    const connect = () => {
      const streamURL = new URL(`/api/v1/runs/${runId}/logs`, window.location.origin);
      streamURL.searchParams.set("offsetBytes", String(nextOffsetBytes));
      source = new EventSource(streamURL);
      source.addEventListener("open", () => setStatus("Live"));
      source.addEventListener("chunk", (event) => {
        const payload = JSON.parse((event as MessageEvent<string>).data) as LogEvent;
        const binary = atob(payload.contentsBase64);
        const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
        nextOffsetBytes = payload.nextOffsetBytes;
        pendingOffsetBytes = nextOffsetBytes;
        pendingContents += decoder.decode(bytes, { stream: true });
        scheduleRender();
      });
      source.addEventListener("sealed", () => {
        stopped = true;
        pendingContents += decoder.decode();
        finishRendering();
        setStatus("Retained");
        source?.close();
      });
      source.addEventListener("failed", () => {
        stopped = true;
        pendingContents += decoder.decode();
        finishRendering();
        setStatus("Incomplete");
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
      if (renderFrame !== undefined) {
        window.cancelAnimationFrame(renderFrame);
      }
    };
  }, [runId]);

  return (
    <>
      <Paper p="md" withBorder>
        <Stack gap="sm">
          <Group justify="space-between">
            <Title order={2}>Logs</Title>
            <Group gap="xs">
              <Text c="dimmed" size="xs">
                {formatBinarySize(receivedBytes)} received
              </Text>
              <Badge variant="light">{status}</Badge>
              <Button
                leftSection={<IconMaximize aria-hidden size={15} />}
                onClick={() => setFullscreenOpened(true)}
                size="compact-sm"
                variant="subtle"
              >
                Full screen
              </Button>
            </Group>
          </Group>
          <LogOutput contents={contents} logSegments={logSegments} outputRef={outputRef} />
        </Stack>
      </Paper>

      <Modal
        classNames={{ body: "run-log-modal-body", content: "run-log-modal-content" }}
        fullScreen
        onClose={() => setFullscreenOpened(false)}
        opened={fullscreenOpened}
        title="Run logs"
      >
        <Stack className="run-log-fullscreen-layout" gap="sm">
          <Group gap="xs">
            <Text c="dimmed" size="xs">
              {formatBinarySize(receivedBytes)} received
            </Text>
            <Badge variant="light">{status}</Badge>
          </Group>
          <LogOutput
            contents={contents}
            fullscreen
            logSegments={logSegments}
            outputRef={fullscreenOutputRef}
          />
        </Stack>
      </Modal>
    </>
  );
}

function LogOutput({
  contents,
  fullscreen = false,
  logSegments,
  outputRef,
}: Readonly<{
  contents: string;
  fullscreen?: boolean;
  logSegments: AnsiTextSegment[];
  outputRef: RefObject<HTMLElement | null>;
}>) {
  return (
    <Code
      className={fullscreen ? "run-log run-log-fullscreen" : "run-log"}
      ref={outputRef}
      block
      mah={fullscreen ? undefined : 480}
      style={{
        backgroundColor: ANSI_DEFAULT_BACKGROUND,
        color: ANSI_DEFAULT_FOREGROUND,
        overflow: "auto",
        tabSize: 4,
        whiteSpace: "pre-wrap",
      }}
    >
      {contents.length === 0
        ? "Waiting for durable log output…"
        : logSegments.map((segment, index) => (
            <span key={index} style={segment.style}>
              {segment.text}
            </span>
          ))}
    </Code>
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
          <Table.ScrollContainer minWidth={760} type="native">
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
                    <Table.Td>{formatBinarySize(artifact.sizeBytes)}</Table.Td>
                    <Table.Td>
                      <Code>{artifact.sha256 ? shortIdentifier(artifact.sha256, 12) : "—"}</Code>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text>
                          {artifact.provenance.target} · {artifact.provenance.action}
                        </Text>
                        <Text c="dimmed" size="xs">
                          <CommitReference
                            commit={artifact.provenance.commit}
                            repositoryId={artifact.provenance.repositoryId}
                          />{" "}
                          · Catalog {shortIdentifier(artifact.provenance.catalogRevisionId)}
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
  prominent = false,
  submitting,
}: Readonly<{
  artifact: Artifact;
  consumer: RunArtifactConsumer;
  prominent?: boolean;
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
      <Button
        leftSection={prominent ? <IconPlayerPlay aria-hidden size={16} /> : undefined}
        loading={submitting}
        size={prominent ? "compact-md" : "compact-sm"}
        type="submit"
        variant={prominent ? "filled" : "light"}
      >
        {label}
      </Button>
    </Form>
  );
}
