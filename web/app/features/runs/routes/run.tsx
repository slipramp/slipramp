import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Menu,
  Paper,
  Popover,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Textarea,
  Timeline,
  Title,
} from "@mantine/core";
import {
  IconBan,
  IconCheck,
  IconChevronDown,
  IconClock,
  IconClockExclamation,
  IconExternalLink,
  IconLoader2,
  IconOctagon,
  IconPlayerPlay,
  IconRefresh,
  IconServer2,
  IconX,
  type TablerIcon,
} from "@tabler/icons-react";
import { useEffect, useState, type ReactNode } from "react";
import { Form, Link, redirect, useNavigation } from "react-router";

import {
  actionRunPath,
  findArtifactConsumer,
  otherTargetActions,
} from "~/features/runs/action-run";
import { createServerAPIClient } from "~/api/client.server";
import { canReadArtifactContents } from "~/features/artifacts/artifact-access";
import {
  cancelRun,
  createRun,
  decideRunReview,
  getArtifactReport,
  getRun,
  getTarget,
  listRunArtifacts,
  listRuns,
  rerunRun,
  type Artifact,
  type ProductPrincipal,
  type Run,
  type RunArtifactConsumer,
  type RunArtifactInput,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { ContextValue } from "~/shared/context-value";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { CommitDetails, CommitReference } from "~/features/commits/git-commit";
import { Identity } from "~/shared/identity";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import { RunLogViewer } from "~/features/runs/run-log-viewer";
import { PyinfraDeployReportView } from "~/features/runs/pyinfra-deploy-report-view";
import { InfrastructurePlanReportView } from "~/features/runs/terraform-plan-report-view";
import {
  formatBinarySize,
  formatDuration,
  formatElapsedDuration,
  shortIdentifier,
} from "~/shared/display";
import { optionalFormValue } from "~/shared/form-data";
import { pageTitle } from "~/shared/page-title";
import { runActivity, runResultActivity } from "~/features/mascot/execution-activity";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import { runSourceLabel } from "~/features/runs/run-source";
import { reportDetailRequestState, type ReportLoad } from "~/features/runs/run-reports";
import { targetFilterPath } from "~/features/targets/target-filter";

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
  const [runResult, artifactResult, principal] = await Promise.all([
    getRun({ client, path: { runId: params.runId } }),
    listRunArtifacts({ client, path: { runId: params.runId } }),
    getProductPrincipal(context),
  ]);
  const artifacts = artifactResult.data?.items ?? [];
  const applyArtifact = runResult.data
    ? findArtifactConsumer(artifacts, runResult.data.artifactConsumers, "apply")?.artifact
    : undefined;
  const [consumerRunsResult, reportLoads, targetResult] = await Promise.all([
    applyArtifact
      ? listRuns({ client, query: { inputArtifactId: applyArtifact.id, limit: 100 } })
      : undefined,
    Promise.all(
      artifacts
        .filter((artifact) => artifact.report !== undefined)
        .map(async (artifact): Promise<ReportLoad> => {
          const requestState = reportDetailRequestState(artifact, principal);
          if (requestState !== "request") {
            return { artifactId: artifact.id, state: requestState };
          }

          const result = await getArtifactReport({
            client,
            path: { artifactId: artifact.id },
          });
          if (result.data) {
            return { artifactId: artifact.id, detail: result.data, state: "available" };
          }

          if (result.response?.status === 403) {
            return { artifactId: artifact.id, state: "restricted" };
          }
          if (result.response?.status === 404) {
            return { artifactId: artifact.id, state: "unavailable" };
          }

          return { artifactId: artifact.id, state: "error" };
        }),
    ),
    runResult.data ? getTarget({ client, path: { targetId: runResult.data.targetId } }) : undefined,
  ]);
  const activeTarget = targetResult?.data?.archivedAt ? undefined : targetResult?.data?.definition;

  return {
    artifactRuns: consumerRunsResult?.data?.items ?? [],
    artifactRunsAvailable:
      consumerRunsResult === undefined || consumerRunsResult.data !== undefined,
    artifacts,
    otherActions: otherTargetActions(activeTarget?.spec.actions, runResult.data?.action ?? ""),
    principal,
    reportLoads,
    repositoryName: targetResult?.data?.repositoryName,
    rerunKey: crypto.randomUUID(),
    run: runResult.data,
    runId: params.runId,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const intent = formData.get("intent");
  if (intent === "approve-review" || intent === "reject-review") {
    const comment = optionalFormValue(formData, "comment");
    const result = await decideRunReview({
      body: { comment, decision: intent === "approve-review" ? "approved" : "rejected" },
      client: createServerAPIClient(request, context),
      path: { runId: params.runId },
    });
    if (result.data) {
      return redirect(`/runs/${result.data.id}`);
    }

    return {
      conflict: result.response?.status === 409,
      error:
        result.error?.detail ??
        result.error?.title ??
        "The Run review decision could not be recorded.",
    };
  }

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
      targetId: producer.targetId,
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
      loaderData.artifactRuns.some((run) => run.phase !== "finished"),
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
  const applySelection = findArtifactConsumer(loaderData.artifacts, run.artifactConsumers, "apply");
  const logArtifact = loaderData.artifacts.find((artifact) => artifact.kind === "log");
  const artifactCanBeApplied =
    run.outcome === "succeeded" &&
    applySelection?.artifact.state === "sealed" &&
    applySelection.artifact.retentionState === "retained";
  const resultActivity = runResultActivity(run);
  const activity = resultActivity ?? runActivity(run);
  const actor = run.createdBy?.displayName ?? (run.source.type === "git" ? "Automation" : "System");
  const rerunSubmitting =
    navigation.state === "submitting" &&
    (navigation.formData?.get("intent") === "rerun-exact" ||
      navigation.formData?.get("intent") === "rerun-latest");
  const hasArtifactInputs = Object.keys(run.inputs).length > 0;
  const canReview =
    loaderData.principal?.role === "operator" || loaderData.principal?.role === "admin";
  const actionConflict =
    actionData !== undefined && "conflict" in actionData && actionData.conflict === true;
  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          run.phase === "finished" ? (
            <Group gap="sm" wrap="wrap">
              {artifactCanBeApplied && applySelection ? (
                <ArtifactAction
                  artifact={applySelection.artifact}
                  consumer={applySelection.consumer}
                  prominent
                  submitting={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("artifactId") === applySelection.artifact.id &&
                    navigation.formData?.get("actionName") === applySelection.consumer.action
                  }
                />
              ) : null}
              {loaderData.otherActions.length > 0 ? (
                <OtherActionMenu actions={loaderData.otherActions} targetId={run.targetId} />
              ) : null}
              <Menu position="bottom-end" shadow="md" width={220}>
                <Menu.Target>
                  <Button
                    leftSection={<IconRefresh aria-hidden size={16} />}
                    loading={rerunSubmitting}
                    rightSection={<IconChevronDown aria-hidden size={14} />}
                    size="compact-md"
                    variant={artifactCanBeApplied ? "light" : undefined}
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
            <RunStatusBadge run={run} />
            {run.phase !== "finished" && run.cancellationRequestedAt !== undefined ? (
              <Badge color="yellow" variant="outline">
                Cancellation requested
              </Badge>
            ) : null}
          </>
        }
        details={
          <PageHeaderFacts>
            {run.actionTitle ? (
              <PageHeaderFact label="Action">
                <Code>{run.action}</Code>
              </PageHeaderFact>
            ) : null}
            <PageHeaderFact label="Run">
              <Code>{run.id.slice(0, 8)}</Code>
            </PageHeaderFact>
            <PageHeaderFact label="Created">
              <Group gap="xs" wrap="wrap">
                <Timestamp value={run.createdAt} />
                <Text c="dimmed">·</Text>
                <RelativeTime c="dimmed" value={run.createdAt} />
              </Group>
            </PageHeaderFact>
            <PageHeaderFact label="Source">{runSourceLabel(run.source)}</PageHeaderFact>
            <PageHeaderFact label="Created by">
              <Identity avatarSize={22} name={actor} nameProps={{ fw: 500, size: "sm" }} />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={
          <>
            <Anchor component={Link} inherit to={`/targets/${run.targetId}`}>
              {run.target}
            </Anchor>
            <Text c="dimmed" component="span" inherit>
              {" / "}
            </Text>
            <Text component="span" inherit>
              {run.actionTitle ?? run.action}
            </Text>
          </>
        }
      />

      {run.actionDescription || run.actionDocumentationUrl ? (
        <Paper p="md" withBorder>
          <Stack align="flex-start" gap="xs">
            {run.actionDescription ? <Text>{run.actionDescription}</Text> : null}
            {run.actionDocumentationUrl ? (
              <Anchor href={run.actionDocumentationUrl} rel="noreferrer" target="_blank">
                <Group gap={4} wrap="nowrap">
                  <span>Action documentation</span>
                  <IconExternalLink aria-hidden size={14} />
                </Group>
              </Anchor>
            ) : null}
          </Stack>
        </Paper>
      ) : null}

      <KawaiiActivity activity={activity} animationKey={run.id} />

      {actionData?.error ? (
        <Alert color="red" title="Run operation failed">
          <Stack gap={4}>
            <Text>{actionData.error}</Text>
            {actionConflict ? (
              <Text size="sm">
                The Run changed before the decision could be recorded. Its refreshed state is shown
                on this page.
              </Text>
            ) : null}
          </Stack>
        </Alert>
      ) : null}

      <RunReviewPanel canReview={canReview} run={run} />

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
      {loaderData.artifacts
        .filter((artifact) => artifact.report !== undefined)
        .map((artifact) => {
          const load = loaderData.reportLoads.find(
            (candidate) => candidate.artifactId === artifact.id,
          );
          if (!load) return null;

          if (
            artifact.report?.kind === "terraform-plan" ||
            artifact.report?.kind === "opentofu-plan"
          ) {
            return (
              <InfrastructurePlanReportView
                artifact={artifact}
                canReadContents={canReadArtifactContents(artifact, loaderData.principal)}
                key={artifact.id}
                load={load}
              />
            );
          }
          if (artifact.report?.kind === "pyinfra-deploy") {
            return (
              <PyinfraDeployReportView
                artifact={artifact}
                canReadContents={canReadArtifactContents(artifact, loaderData.principal)}
                key={artifact.id}
                load={load}
              />
            );
          }

          return null;
        })}
      <RunLogViewer artifact={logArtifact} key={run.id} runId={run.id} />
      <RunOverview repositoryName={loaderData.repositoryName} run={run} />
      <ParameterTable parameters={run.parameters} />

      <InputTable inputs={run.inputs} repositoryId={run.repositoryId} />
      <ArtifactTable
        artifacts={loaderData.artifacts}
        consumers={run.artifactConsumers}
        principal={loaderData.principal}
        producerSucceeded={run.outcome === "succeeded"}
      />
      {applySelection ? (
        <ArtifactLineage
          artifact={applySelection.artifact}
          available={loaderData.artifactRunsAvailable}
          canReadContents={canReadArtifactContents(applySelection.artifact, loaderData.principal)}
          runs={loaderData.artifactRuns}
        />
      ) : null}
    </Stack>
  );
}

function RunStatusBadge({ run }: Readonly<{ run: Run }>) {
  const label = runStatusLabel(run);
  if (
    run.phase !== "queued" ||
    !run.schedulingState ||
    run.queueAgeSeconds === undefined ||
    !run.schedulingSnapshotAt
  ) {
    return (
      <Badge color={runStatusColor(run)} variant="light">
        {label}
      </Badge>
    );
  }

  let explanation: ReactNode;
  switch (run.schedulingState) {
    case "awaiting-review":
      explanation = "This Run must be approved before it can enter the eligible FIFO queue.";
      break;
    case "target-busy":
      explanation = run.blockingRunId ? (
        <>
          The Target is serialized behind active Run{" "}
          <Anchor component={Link} to={`/runs/${run.blockingRunId}`}>
            {shortIdentifier(run.blockingRunId)}
          </Anchor>
          .
        </>
      ) : (
        "Another active Run is serializing this Target."
      );
      break;
    case "cluster-unreachable":
      explanation = "The Cluster Controller is not currently reachable by the Server.";
      break;
    case "cluster-capacity":
      explanation = "The Cluster or this Run's Placement has reached its active Run limit.";
      break;
    case "eligible":
      explanation = "This Run satisfies the current review, Target, and capacity constraints.";
      break;
  }

  return (
    <Popover position="bottom-start" shadow="md" width="min(380px, calc(100vw - 2rem))" withArrow>
      <Popover.Target>
        <Badge
          aria-label={`${label}. Show queue details`}
          color={runStatusColor(run)}
          component="button"
          rightSection={<IconChevronDown aria-hidden size={12} />}
          style={{ cursor: "pointer" }}
          type="button"
          variant="light"
        >
          {label}
        </Badge>
      </Popover.Target>
      <Popover.Dropdown>
        <Stack gap="xs">
          <Text fw={700} size="sm">
            Queue details
          </Text>
          <Text size="sm">{explanation}</Text>
          <Group gap="xs" wrap="wrap">
            <Text size="xs">
              Queued for {formatDuration(new Date(0), new Date(run.queueAgeSeconds * 1000))}
            </Text>
            {run.eligiblePosition !== undefined ? (
              <Text size="xs">· Eligible position {run.eligiblePosition}</Text>
            ) : null}
          </Group>
          <Group gap={4} wrap="wrap">
            <Text c="dimmed" size="xs">
              Snapshot
            </Text>
            <Timestamp c="dimmed" size="xs" value={run.schedulingSnapshotAt} />
            <Text c="dimmed" size="xs">
              · Point-in-time state, not an ETA
            </Text>
          </Group>
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}

function RunReviewPanel({ canReview, run }: Readonly<{ canReview: boolean; run: Run }>) {
  const navigation = useNavigation();
  if (run.review.state === "not-required") return null;

  const pending = run.review.state === "pending";
  const canDecide = canReview && pending && run.phase === "queued";
  const decision =
    !pending && run.review.decidedBy && run.review.decidedAt
      ? { at: run.review.decidedAt, by: run.review.decidedBy }
      : undefined;
  const reviewLabel = pending
    ? "Awaiting review"
    : run.review.state === "approved"
      ? "Approved"
      : "Rejected";

  return (
    <Alert
      color={pending ? "yellow" : run.review.state === "approved" ? "green" : "red"}
      title={pending ? "Review required" : "Run review"}
    >
      <Stack align="flex-start" gap="sm">
        <Group gap="xs" wrap="wrap">
          <Badge
            color={pending ? "yellow" : run.review.state === "approved" ? "green" : "red"}
            variant="light"
          >
            {reviewLabel}
          </Badge>
          {decision ? (
            <>
              <Text size="sm">
                {reviewLabel} by {decision.by.displayName}
              </Text>
              <Text c="dimmed">·</Text>
              <Timestamp size="sm" value={decision.at} />
            </>
          ) : null}
        </Group>

        {pending && run.phase === "queued" ? (
          <Text size="sm">
            This Git-triggered Run will stay queued and cannot be assigned to a Controller until an
            operator approves it.
          </Text>
        ) : pending ? (
          <Text size="sm">
            No review decision was recorded before this Run left the queued state.
          </Text>
        ) : run.review.state === "approved" ? (
          <Text size="sm">
            Approval made this Run eligible for ordinary assignment. It does not indicate that
            execution succeeded.
          </Text>
        ) : (
          <Text size="sm">Rejection canceled this Run before it was assigned.</Text>
        )}

        {run.review.comment ? (
          <Text size="sm" style={{ whiteSpace: "pre-wrap" }}>
            {run.review.comment}
          </Text>
        ) : null}

        {canDecide ? (
          <Form method="post" style={{ width: "100%" }}>
            <Stack gap="sm">
              <Textarea
                label="Comment"
                maxLength={4096}
                minRows={3}
                name="comment"
                placeholder="Optional context for the immutable decision"
              />
              <Group gap="sm" justify="flex-end">
                <Button
                  color="red"
                  leftSection={<IconX aria-hidden size={16} />}
                  loading={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("intent") === "reject-review"
                  }
                  name="intent"
                  size="compact-md"
                  type="submit"
                  value="reject-review"
                  variant="light"
                >
                  Reject
                </Button>
                <Button
                  color="green"
                  leftSection={<IconCheck aria-hidden size={16} />}
                  loading={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("intent") === "approve-review"
                  }
                  name="intent"
                  size="compact-md"
                  type="submit"
                  value="approve-review"
                >
                  Approve
                </Button>
              </Group>
            </Stack>
          </Form>
        ) : pending && run.phase === "queued" && !canReview ? (
          <Text c="dimmed" size="sm">
            Run review permission is required to decide this review.
          </Text>
        ) : null}
      </Stack>
    </Alert>
  );
}

function OtherActionMenu({
  actions,
  targetId,
}: Readonly<{
  actions: ReturnType<typeof otherTargetActions>;
  targetId: string;
}>) {
  return (
    <Menu position="bottom-end" shadow="md" width={260}>
      <Menu.Target>
        <Button
          leftSection={<IconPlayerPlay aria-hidden size={16} />}
          rightSection={<IconChevronDown aria-hidden size={14} />}
          size="compact-md"
          variant="light"
        >
          Run another action
        </Button>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Label>Choose an action</Menu.Label>
        {actions.map((actionChoice) => (
          <Menu.Item
            component={Link}
            key={actionChoice.name}
            leftSection={<IconPlayerPlay aria-hidden size={16} />}
            to={actionRunPath(targetId, actionChoice.name)}
          >
            {actionChoice.title ? (
              <Stack gap={0}>
                <Text size="sm">{actionChoice.title}</Text>
                <Text c="dimmed" ff="monospace" size="xs">
                  {actionChoice.name}
                </Text>
              </Stack>
            ) : (
              actionChoice.name
            )}
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  );
}

function ArtifactLineage({
  artifact,
  available,
  canReadContents,
  runs,
}: Readonly<{
  artifact: Artifact;
  available: boolean;
  canReadContents: boolean;
  runs: Run[];
}>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <SectionHeader
          actions={
            <Group align="center" gap="xs" style={{ alignSelf: "center" }} wrap="wrap">
              <Badge variant="light">
                {runs.length} {runs.length === 1 ? "run" : "runs"}
              </Badge>
              {canReadContents &&
              artifact.state === "sealed" &&
              artifact.retentionState === "retained" ? (
                <Anchor href={`/api/v1/artifacts/${artifact.id}/download`} size="sm">
                  Download Artifact
                </Anchor>
              ) : null}
            </Group>
          }
          title="Artifact lineage"
        />
        {!available ? (
          <Alert color="red" title="Artifact history unavailable">
            Runs created from this Artifact could not be loaded.
          </Alert>
        ) : runs.length === 0 ? (
          <Text c="dimmed" size="sm">
            No Runs have been created from this Artifact.
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
                        {runStatusLabel(consumerRun)}
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
          <SimpleGrid cols={1} spacing="lg">
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
            {run.rerunOfRunId && run.rerunMode ? (
              <Fact label="Run again">
                <Anchor component={Link} to={`/runs/${run.rerunOfRunId}`}>
                  {run.rerunMode === "exact" ? "Same revision as" : "Latest revision from"} Run{" "}
                  {shortIdentifier(run.rerunOfRunId)}
                </Anchor>
              </Fact>
            ) : null}
            <Accordion defaultValue={null} variant="contained">
              <Accordion.Item value="job-details">
                <Accordion.Control>
                  <Text fw={600}>Job details</Text>
                </Accordion.Control>
                <Accordion.Panel>
                  <SimpleGrid cols={1} spacing="lg">
                    <Fact label="Job UID">
                      {run.jobUid ? (
                        <Code style={{ overflowWrap: "anywhere" }}>{run.jobUid}</Code>
                      ) : (
                        <Text c="dimmed" size="sm">
                          Not materialized
                        </Text>
                      )}
                    </Fact>
                    {run.images?.action ? (
                      <Fact label="Action image ID">
                        <Code style={{ wordBreak: "break-all" }}>{run.images.action}</Code>
                      </Fact>
                    ) : null}
                    {run.images?.checkout ? (
                      <Fact label="Checkout image ID">
                        <Code style={{ wordBreak: "break-all" }}>{run.images.checkout}</Code>
                      </Fact>
                    ) : null}
                    {run.images?.harness ? (
                      <Fact label="Harness image ID">
                        <Code style={{ wordBreak: "break-all" }}>{run.images.harness}</Code>
                      </Fact>
                    ) : null}
                  </SimpleGrid>
                </Accordion.Panel>
              </Accordion.Item>
            </Accordion>
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
                    {input.description ? (
                      <Text c="dimmed" size="xs">
                        {input.description}
                      </Text>
                    ) : null}
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

function ArtifactTable({
  artifacts,
  consumers,
  principal,
  producerSucceeded,
}: Readonly<{
  artifacts: Artifact[];
  consumers: Record<string, RunArtifactConsumer[]>;
  principal?: ProductPrincipal;
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
                      {artifact.description ? (
                        <Text c="dimmed" size="xs">
                          {artifact.description}
                        </Text>
                      ) : null}
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
                        {artifact.state === "sealed" &&
                        artifact.retentionState === "retained" &&
                        canReadArtifactContents(artifact, principal) ? (
                          <Anchor href={`/api/v1/artifacts/${artifact.id}/download`}>
                            Download
                          </Anchor>
                        ) : artifact.state === "sealed" &&
                          artifact.retentionState === "retained" &&
                          artifact.sensitive ? (
                          <Text c="dimmed">Restricted</Text>
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
  const label = prominent
    ? (consumer.actionTitle ?? `Run ${consumer.action}`)
    : `Run ${consumer.actionTitle ?? consumer.action}`;

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
        title={consumer.actionTitle ? `Action ${consumer.action}` : undefined}
        type="submit"
        variant={prominent ? "filled" : "light"}
      >
        {label}
      </Button>
    </Form>
  );
}
