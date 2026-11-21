import { LifecycleStep } from "~/shared/lifecycle-step";
import { CancellationDetail, CancellationStep } from "~/shared/cancellation-detail";
import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Box,
  Button,
  Code,
  Group,
  Menu,
  Paper,
  Stack,
  Table,
  Tabs,
  Text,
  Textarea,
  Title,
  Tooltip,
} from "@mantine/core";
import {
  IconBan,
  IconCheck,
  IconChevronDown,
  IconClock,
  IconClockExclamation,
  IconExternalLink,
  IconFileAnalytics,
  IconInfoCircle,
  IconLoader2,
  IconOctagon,
  IconPlayerPlay,
  IconRefresh,
  IconServer2,
  IconTerminal2,
  IconX,
  type TablerIcon,
} from "@tabler/icons-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Form, Link, redirect, useNavigation, useSearchParams } from "react-router";

import { findArtifactConsumer, otherTargetActions } from "~/features/runs/action-run";
import { RunCreateMenu } from "~/features/runs/run-create-dialog";
import { createServerAPIClient } from "~/api/client.server";
import { canReadArtifactContents } from "~/features/artifacts/artifact-access";
import {
  cancelRun,
  overrideRunGate,
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
import { DetailFact, DetailGrid } from "~/shared/context-value";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { CommitDetails, CommitReference } from "~/features/commits/git-commit";
import { Identity } from "~/shared/identity";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import { AnsiblePlaybookReportView } from "~/features/runs/ansible-playbook-report-view";
import { RunLogViewer } from "~/features/runs/run-log-viewer";
import { PyinfraDeployReportView } from "~/features/runs/pyinfra-deploy-report-view";
import { StepReportView } from "~/features/runs/step-report-view";
import { InfrastructurePlanReportView } from "~/features/runs/terraform-plan-report-view";
import {
  formatBinarySize,
  formatDuration,
  formatElapsedDuration,
  formatTimestamp,
  shortIdentifier,
} from "~/shared/display";
import { formValue, optionalFormValue } from "~/shared/form-data";
import { pageTitle } from "~/shared/page-title";
import { useTimeZone } from "~/shared/time-zone-preference";
import { runActivity, runResultActivity } from "~/features/mascot/execution-activity";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import { ReleaseGateDetails } from "~/features/gates/release-gate-details";
import { runSourceLabel } from "~/features/runs/run-source";
import { reportDetailRequestState, type ReportLoad } from "~/features/runs/run-reports";
import {
  runDetailSearchParamNames,
  selectedRunDetailValue,
  updateRunDetailSearchParams,
} from "~/features/runs/run-detail-url-state";
import { targetFilterPath } from "~/features/targets/target-filter";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import styles from "./run.module.css";
import type { Route } from "./+types/run";

const elapsedTimerIntervalMs = 1000;
const runRevalidationIntervalMs = 2000;

export function meta({ loaderData }: Route.MetaArgs) {
  const runName = loaderData?.run ? `Run ${loaderData.run.id.slice(0, 8)}` : "Run";
  return [{ title: pageTitle(runName) }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [runResult, artifactResult, principal] = await Promise.all([
    getRun({ client, path: { runId: params.runId } }),
    listRunArtifacts({ client, path: { runId: params.runId } }),
    getProductPrincipal(context),
  ]);
  const artifacts = artifactResult.data?.items ?? [];
  const lineageArtifacts = artifacts.filter((artifact) => artifact.kind === "output");
  const [lineageResults, reportLoads, targetResult] = await Promise.all([
    Promise.all(
      lineageArtifacts.map(async (artifact) => ({
        artifactId: artifact.id,
        result: await listRuns({
          client,
          query: { inputArtifactId: artifact.id, limit: 100 },
        }),
      })),
    ),
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
    artifactRuns: Object.fromEntries(
      lineageResults.map(({ artifactId, result }) => [artifactId, result.data?.items ?? []]),
    ),
    artifactRunsAvailable: Object.fromEntries(
      lineageResults.map(({ artifactId, result }) => [artifactId, result.data !== undefined]),
    ),
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
  if (formValue(formData, "intent") === "override-gate") {
    const result = await overrideRunGate({
      client: createServerAPIClient(request, context),
      path: { runId: params.runId, gateName: formValue(formData, "gateName") },
      body: {
        definitionDigest: formValue(formData, "definitionDigest"),
        reason: formValue(formData, "reason"),
      },
    });
    if (result.response?.status === 204) return { message: "Gate override recorded." };
    return { error: result.error?.detail ?? "The Gate override could not be recorded." };
  }
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
  const { timeZone } = useTimeZone();
  useActivityRefresh(
    loaderData.run?.phase !== "finished" ||
      Object.values(loaderData.artifactRuns).some((runs) =>
        runs.some((run) => run.phase !== "finished"),
      ),
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
  const actor =
    run.createdBy?.displayName ??
    (run.source.type === "git" || run.source.type === "schedule" ? "Automation" : "System");
  const scheduledSource = run.source.type === "schedule" ? run.source.schedule : undefined;
  const scheduledSourceLabel = scheduledSource
    ? `${scheduledSource.triggerName} · ${formatTimestamp(scheduledSource.scheduledAt, timeZone)}`
    : undefined;
  const rerunSubmitting =
    navigation.state === "submitting" &&
    (navigation.formData?.get("intent") === "rerun-exact" ||
      navigation.formData?.get("intent") === "rerun-latest");
  const hasArtifactInputs = Object.keys(run.inputs).length > 0;
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
          ) : run.cancellation.state === "not-requested" ? (
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
        badges={<RunStatusBadge run={run} />}
        description={
          run.actionDescription || run.actionDocumentationUrl ? (
            <Text c="dimmed">
              {run.actionDescription}
              {run.actionDocumentationUrl ? (
                <Anchor
                  aria-label="Open action documentation"
                  display="inline-flex"
                  href={run.actionDocumentationUrl}
                  ml={6}
                  rel="noreferrer"
                  target="_blank"
                  title="Open action documentation"
                >
                  <IconExternalLink aria-hidden size={14} />
                </Anchor>
              ) : null}
            </Text>
          ) : undefined
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
              <RelativeTime value={run.createdAt} />
            </PageHeaderFact>
            <PageHeaderFact label="Source">
              {scheduledSource ? (
                <Tooltip label={scheduledSourceLabel} openDelay={350}>
                  <Text component="span" inherit>
                    Schedule
                  </Text>
                </Tooltip>
              ) : (
                runSourceLabel(run.source)
              )}
            </PageHeaderFact>
            <PageHeaderFact label="Created by">
              <Identity avatarSize={22} name={actor} nameProps={{ fw: 500, size: "sm" }} />
            </PageHeaderFact>
            <PageHeaderFact label="Revision">
              <CommitReference commit={run.commit} repositoryId={run.repositoryId} />
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

      <RunWorkspace
        artifactRuns={loaderData.artifactRuns}
        artifactRunsAvailable={loaderData.artifactRunsAvailable}
        artifacts={loaderData.artifacts}
        consumers={run.artifactConsumers}
        logArtifact={logArtifact}
        principal={loaderData.principal}
        reportLoads={loaderData.reportLoads}
        repositoryName={loaderData.repositoryName}
        run={run}
      />
    </Stack>
  );
}

const lifecycleStages: Run["phase"][] = ["queued", "assigned", "preparing", "running", "finished"];

type RunLifecycleStage = Run["phase"] | "cancellation";
const stagesWithCancellation: RunLifecycleStage[] = [...lifecycleStages, "cancellation"];

function RunWorkspace({
  artifactRuns,
  artifactRunsAvailable,
  artifacts,
  consumers,
  logArtifact,
  principal,
  reportLoads,
  repositoryName,
  run,
}: Readonly<{
  artifactRuns: Record<string, Run[]>;
  artifactRunsAvailable: Record<string, boolean>;
  artifacts: Artifact[];
  consumers: Record<string, RunArtifactConsumer[]>;
  logArtifact?: Artifact;
  principal?: ProductPrincipal;
  reportLoads: ReportLoad[];
  repositoryName?: string;
  run: Run;
}>) {
  const [searchParams, setSearchParams] = useSearchParams();
  const reportArtifacts = useMemo(
    () => artifacts.filter((artifact) => artifact.report !== undefined),
    [artifacts],
  );
  const outputValues = useMemo(
    () => [...reportArtifacts.map((artifact) => reportOutputValue(artifact.id)), "log", "details"],
    [reportArtifacts],
  );
  const automaticOutput = defaultRunOutput(run, reportArtifacts, reportLoads, logArtifact);
  const selectedOutput = selectedRunDetailValue(
    searchParams,
    runDetailSearchParamNames.output,
    outputValues,
    automaticOutput,
  );
  const hasCancellation = run.cancellation.state !== "not-requested";
  const availableStages = hasCancellation ? stagesWithCancellation : lifecycleStages;
  const selectedStage = selectedRunDetailValue(
    searchParams,
    runDetailSearchParamNames.stage,
    availableStages,
    hasCancellation ? "cancellation" : run.phase,
  ) as RunLifecycleStage;
  const requestedOutput = searchParams.get(runDetailSearchParamNames.output);
  const requestedStage = searchParams.get(runDetailSearchParamNames.stage);

  useEffect(() => {
    const invalidOutput = requestedOutput !== null && !outputValues.includes(requestedOutput);
    const invalidStage =
      requestedStage !== null && !(availableStages as readonly string[]).includes(requestedStage);
    if (!invalidOutput && !invalidStage) return;

    setSearchParams(
      updateRunDetailSearchParams(searchParams, {
        [runDetailSearchParamNames.output]: invalidOutput
          ? undefined
          : (requestedOutput ?? undefined),
        [runDetailSearchParamNames.stage]: invalidStage ? undefined : (requestedStage ?? undefined),
      }),
      { preventScrollReset: true, replace: true },
    );
  }, [
    availableStages,
    outputValues,
    requestedOutput,
    requestedStage,
    searchParams,
    setSearchParams,
  ]);

  function selectOutput(output: string | null) {
    if (!output) return;
    setSearchParams(
      updateRunDetailSearchParams(searchParams, {
        [runDetailSearchParamNames.output]: output,
      }),
      { preventScrollReset: true },
    );
  }

  function selectStage(stage: RunLifecycleStage) {
    setSearchParams(
      updateRunDetailSearchParams(searchParams, {
        [runDetailSearchParamNames.stage]: stage,
      }),
      { preventScrollReset: true },
    );
  }

  const canReview = principal?.role === "operator" || principal?.role === "admin";
  const lifecycleProps = { canReview, onStageChange: selectStage, run, selectedStage };

  return (
    <Box className={styles.workspace}>
      <Accordion className={styles.mobileLifecycle} variant="contained">
        <Accordion.Item value="lifecycle">
          <Accordion.Control>
            <Group justify="space-between" pr="sm" wrap="nowrap">
              <Text fw={650}>Lifecycle</Text>
              <Badge color={runStatusColor(run)} variant="light">
                {lifecycleStageLabel(selectedStage)}
              </Badge>
            </Group>
          </Accordion.Control>
          <Accordion.Panel>
            <RunLifecycle {...lifecycleProps} showHeading={false} />
          </Accordion.Panel>
        </Accordion.Item>
      </Accordion>

      <Paper className={styles.outputCard} withBorder>
        <Tabs keepMounted onChange={selectOutput} value={selectedOutput}>
          <Tabs.List className={styles.outputTabs}>
            {reportArtifacts.map((artifact) => (
              <Tabs.Tab
                key={artifact.id}
                leftSection={<IconFileAnalytics aria-hidden size={16} />}
                value={reportOutputValue(artifact.id)}
              >
                {artifact.name}
              </Tabs.Tab>
            ))}
            <Tabs.Tab leftSection={<IconTerminal2 aria-hidden size={16} />} value="log">
              Run log
            </Tabs.Tab>
            <Tabs.Tab leftSection={<IconInfoCircle aria-hidden size={16} />} value="details">
              Run details
            </Tabs.Tab>
          </Tabs.List>

          {reportArtifacts.map((artifact) => {
            const load = reportLoads.find((candidate) => candidate.artifactId === artifact.id);
            return (
              <Tabs.Panel
                className={styles.outputPanel}
                key={artifact.id}
                value={reportOutputValue(artifact.id)}
              >
                {load ? (
                  <RunReportOutput artifact={artifact} load={load} principal={principal} />
                ) : (
                  <Alert color="red" title="Report could not be loaded">
                    Slipramp did not return an availability record for this report Artifact.
                  </Alert>
                )}
              </Tabs.Panel>
            );
          })}

          <Tabs.Panel className={styles.outputPanel} value="log">
            <RunLogViewer
              artifact={logArtifact}
              embedded
              initialSearchQuery={searchParams.get(runDetailSearchParamNames.logQuery) ?? ""}
              key={`${run.id}:${searchParams.get(runDetailSearchParamNames.logQuery) ?? ""}`}
              runFinished={run.phase === "finished"}
              runId={run.id}
            />
          </Tabs.Panel>

          <Tabs.Panel className={styles.outputPanel} value="details">
            <RunDetails
              artifactRuns={artifactRuns}
              artifactRunsAvailable={artifactRunsAvailable}
              artifacts={artifacts}
              consumers={consumers}
              principal={principal}
              repositoryName={repositoryName}
              run={run}
            />
          </Tabs.Panel>
        </Tabs>
      </Paper>

      <Paper className={styles.desktopLifecycle} p="md" withBorder>
        <RunLifecycle {...lifecycleProps} />
      </Paper>
    </Box>
  );
}

function RunReportOutput({
  artifact,
  load,
  principal,
}: Readonly<{ artifact: Artifact; load: ReportLoad; principal?: ProductPrincipal }>) {
  const canReadContents = canReadArtifactContents(artifact, principal);
  switch (artifact.report?.kind) {
    case "terraform-plan":
    case "opentofu-plan":
      return (
        <InfrastructurePlanReportView
          artifact={artifact}
          canReadContents={canReadContents}
          embedded
          load={load}
        />
      );
    case "pyinfra-deploy":
      return (
        <PyinfraDeployReportView
          artifact={artifact}
          canReadContents={canReadContents}
          embedded
          load={load}
        />
      );
    case "ansible-playbook":
      return (
        <AnsiblePlaybookReportView
          artifact={artifact}
          canReadContents={canReadContents}
          embedded
          load={load}
        />
      );
    case "step-report":
      return (
        <StepReportView
          artifact={artifact}
          canReadContents={canReadContents}
          embedded
          load={load}
        />
      );
    default:
      return (
        <Alert color="orange" title="Unsupported report">
          This report kind does not have a Run detail renderer.
        </Alert>
      );
  }
}

function RunDetails({
  artifactRuns,
  artifactRunsAvailable,
  artifacts,
  consumers,
  principal,
  repositoryName,
  run,
}: Readonly<{
  artifactRuns: Record<string, Run[]>;
  artifactRunsAvailable: Record<string, boolean>;
  artifacts: Artifact[];
  consumers: Record<string, RunArtifactConsumer[]>;
  principal?: ProductPrincipal;
  repositoryName?: string;
  run: Run;
}>) {
  return (
    <Stack gap="lg">
      <SectionHeader title="Run details" />
      <CommitDetails commit={run.commit} repositoryId={run.repositoryId} titleOrder={3} />
      <ReleaseGateDetails
        gates={run.gates}
        subject="Run"
        subjectId={run.id}
        canOverride={
          run.phase === "queued" &&
          run.cancellation.state === "not-requested" &&
          (principal?.role === "operator" || principal?.role === "admin")
        }
      />
      <ParameterTable parameters={run.parameters} />
      <RunOverview repositoryName={repositoryName} run={run} />
      <InputTable inputs={run.inputs} repositoryId={run.repositoryId} />
      <ArtifactTable
        artifactRuns={artifactRuns}
        artifactRunsAvailable={artifactRunsAvailable}
        artifacts={artifacts}
        consumers={consumers}
        principal={principal}
        producerSucceeded={run.outcome === "succeeded"}
      />
    </Stack>
  );
}

function reportOutputValue(artifactId: string) {
  return `report:${artifactId}`;
}

function defaultRunOutput(
  run: Run,
  reportArtifacts: Artifact[],
  reportLoads: ReportLoad[],
  logArtifact?: Artifact,
) {
  if (run.phase !== "finished") return "log";
  const readableReport = reportArtifacts.find(
    (artifact) =>
      reportLoads.find((load) => load.artifactId === artifact.id)?.state === "available",
  );
  if (readableReport) return reportOutputValue(readableReport.id);
  return logArtifact ? "log" : "details";
}

function RunStatusBadge({ run }: Readonly<{ run: Run }>) {
  return (
    <Badge color={runStatusColor(run)} variant="light">
      {runStatusLabel(run)}
    </Badge>
  );
}

function runQueueExplanation(run: Run): ReactNode {
  switch (run.schedulingState) {
    case "awaiting-review":
      return "This Run must be approved before it can enter the eligible FIFO queue.";
    case "awaiting-gates":
      return "Each Gate needs a fresh pass or an override before assignment.";
    case "target-busy":
      return run.blockingRunId ? (
        <>
          The Target is at capacity behind active Run{" "}
          <Anchor component={Link} to={`/runs/${run.blockingRunId}`}>
            {shortIdentifier(run.blockingRunId)}
          </Anchor>
          .
        </>
      ) : (
        "The Target has reached its active Run limit."
      );
    case "cluster-unreachable":
      return "The Cluster Controller is not currently reachable by the Server.";
    case "cluster-capacity":
      return "The Cluster or this Run's Placement has reached its active Run limit.";
    case "eligible":
      return "This Run satisfies the current review, Gate, Target, and capacity constraints.";
    default:
      return "Slipramp has not recorded a current queue scheduling reason.";
  }
}

function QueueSnapshot({ run }: Readonly<{ run: Run }>) {
  if (run.queueAgeSeconds === undefined || !run.schedulingSnapshotAt) return null;
  return (
    <>
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
    </>
  );
}

function RunReviewDetails({ canReview, run }: Readonly<{ canReview: boolean; run: Run }>) {
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
    <Stack align="flex-start" gap="sm">
      <Group gap="xs" wrap="wrap">
        <Text fw={650} size="xs">
          Review
        </Text>
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
    <RunCreateMenu
      actions={actions}
      buttonIcon
      label="Run another action"
      position="bottom-end"
      size="compact-md"
      targetId={targetId}
      width={260}
    />
  );
}

function RunOverview({ repositoryName, run }: Readonly<{ repositoryName?: string; run: Run }>) {
  return (
    <Stack gap="lg">
      <Title order={2}>Execution context</Title>
      <DetailGrid>
        <DetailFact label="Repository">
          <Anchor component={Link} to={`/repositories/${run.repositoryId}`}>
            {repositoryName ?? shortIdentifier(run.repositoryId)}
          </Anchor>
        </DetailFact>
        <DetailFact label="Runtime">
          <Anchor component={Link} to={targetFilterPath("runtime", run.runtime)}>
            {run.runtime}
          </Anchor>
        </DetailFact>
        <DetailFact label="Cluster">
          <Anchor component={Link} to={`/clusters/${encodeURIComponent(run.cluster)}`}>
            {run.cluster}
          </Anchor>
        </DetailFact>
        <DetailFact label="Placement">
          <Anchor component={Link} to={targetFilterPath("placement", run.placement)}>
            {run.placement}
          </Anchor>
        </DetailFact>
        <DetailFact label="Catalog revision">
          <Code>{run.catalogRevisionId}</Code>
        </DetailFact>
        {run.rerunOfRunId && run.rerunMode ? (
          <DetailFact label="Run again">
            <Anchor component={Link} to={`/runs/${run.rerunOfRunId}`}>
              {run.rerunMode === "exact" ? "Same revision as" : "Latest revision from"} Run{" "}
              {shortIdentifier(run.rerunOfRunId)}
            </Anchor>
          </DetailFact>
        ) : null}
        <DetailFact label="Controller observation">
          <Stack gap={2}>
            <Text size="sm">{run.observationState ?? "Not active"}</Text>
            {run.lastObservedAt ? (
              <Group gap={4} wrap="wrap">
                <Text c="dimmed" size="xs">
                  Last seen
                </Text>
                <RelativeTime c="dimmed" size="xs" value={run.lastObservedAt} />
              </Group>
            ) : (
              <Text c="dimmed" size="xs">
                No Controller observation recorded
              </Text>
            )}
          </Stack>
        </DetailFact>
      </DetailGrid>
      <Accordion defaultValue={null} variant="contained">
        <Accordion.Item value="job-details">
          <Accordion.Control>
            <Text fw={600}>Job and image details</Text>
          </Accordion.Control>
          <Accordion.Panel>
            <DetailGrid>
              <DetailFact label="Job UID">
                {run.jobUid ? (
                  <Code style={{ overflowWrap: "anywhere" }}>{run.jobUid}</Code>
                ) : (
                  <Text c="dimmed" size="sm">
                    Not materialized
                  </Text>
                )}
              </DetailFact>
              {run.images?.action ? (
                <DetailFact label="Action image ID">
                  <Code style={{ wordBreak: "break-all" }}>{run.images.action}</Code>
                </DetailFact>
              ) : null}
              {run.images?.checkout ? (
                <DetailFact label="Checkout image ID">
                  <Code style={{ wordBreak: "break-all" }}>{run.images.checkout}</Code>
                </DetailFact>
              ) : null}
              {run.images?.harness ? (
                <DetailFact label="Harness image ID">
                  <Code style={{ wordBreak: "break-all" }}>{run.images.harness}</Code>
                </DetailFact>
              ) : null}
            </DetailGrid>
          </Accordion.Panel>
        </Accordion.Item>
      </Accordion>
    </Stack>
  );
}

function RunLifecycle({
  canReview,
  onStageChange,
  run,
  selectedStage,
  showHeading = true,
}: Readonly<{
  canReview: boolean;
  onStageChange: (stage: RunLifecycleStage) => void;
  run: Run;
  selectedStage: RunLifecycleStage;
  showHeading?: boolean;
}>) {
  const activeStep = lifecycleStages.indexOf(run.phase);
  const currentTime = useCurrentTime(run.phase !== "finished");
  const steps: Array<{
    detail: string;
    icon: TablerIcon;
    label: string;
    stage: Run["phase"];
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
      stage: "queued",
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
      stage: "assigned",
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
      stage: "preparing",
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
      stage: "running",
      value: run.startedAt,
    },
    {
      detail: run.finishedAt
        ? `Run ${run.outcome ?? "finished"}${run.exitCode === undefined ? "" : ` with exit code ${run.exitCode}`}`
        : "Terminal outcome not recorded",
      icon: finishedIcon(run),
      label: "Finished",
      stage: "finished",
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
      <Stack gap="xs">
        {showHeading ? <Title order={2}>Lifecycle</Title> : null}
        <Group gap="xs">
          {summary.map((value) => (
            <Badge key={value} size="sm" variant="light">
              {value}
            </Badge>
          ))}
        </Group>
      </Stack>
      <Group justify="center">
        <KawaiiActivity
          activity={runActivity(run) ?? runResultActivity(run)}
          animationKey={run.id}
          mascotOnly
        />
      </Group>
      <Stack className={styles.lifecycleSteps} gap={3}>
        {steps.map((step, index) => {
          const Icon = step.icon;
          const active = activeStep === index && run.phase !== "finished";
          const complete =
            step.value !== undefined && (index < activeStep || run.phase === "finished");
          const status = index === 4 && run.outcome ? run.outcome : undefined;
          const state =
            status ??
            (active
              ? "Current"
              : complete
                ? "Complete"
                : run.phase === "finished"
                  ? "Skipped"
                  : "Waiting");
          const color = status ? runOutcomeColor(status) : active ? "lilac" : "gray";
          return (
            <LifecycleStep
              key={step.stage}
              icon={Icon}
              label={step.label}
              time={step.value}
              status={state}
              color={color}
              complete={complete}
              highlighted={active || Boolean(status)}
              selected={selectedStage === step.stage}
              onSelect={() => onStageChange(step.stage)}
            />
          );
        })}
        <CancellationStep
          cancellation={run.cancellation}
          selected={selectedStage === "cancellation"}
          onSelect={() => onStageChange("cancellation")}
        />
      </Stack>
      {selectedStage === "cancellation" ? (
        <CancellationDetail
          cancellation={run.cancellation}
          subject="Run"
          outcome={run.outcome}
          finishedAt={run.finishedAt}
        />
      ) : (
        <LifecycleStageDetail
          canReview={canReview}
          detail={
            steps[lifecycleStages.indexOf(selectedStage)]?.detail ?? "No lifecycle detail recorded."
          }
          run={run}
          stage={selectedStage}
          value={steps[lifecycleStages.indexOf(selectedStage)]?.value}
        />
      )}
    </Stack>
  );
}

function LifecycleStageDetail({
  canReview,
  detail,
  run,
  stage,
  value,
}: Readonly<{
  canReview: boolean;
  detail: string;
  run: Run;
  stage: Run["phase"];
  value?: string;
}>) {
  return (
    <Paper bg="var(--mantine-color-default-hover)" p="sm" radius="md">
      <Stack gap={5}>
        <Text fw={700} size="sm">
          {lifecycleStageLabel(stage)}
        </Text>
        {value ? (
          <Timestamp c="dimmed" size="xs" value={value} />
        ) : (
          <Text c="dimmed" size="xs">
            No recorded time
          </Text>
        )}
        <Text c="dimmed" size="xs">
          {detail}
        </Text>
        <LifecycleRelevantDetail canReview={canReview} run={run} stage={stage} />
        <LifecycleObservationDetail run={run} stage={stage} />
      </Stack>
    </Paper>
  );
}

function QueuedLifecycleDetails({ canReview, run }: Readonly<{ canReview: boolean; run: Run }>) {
  const showQueue = run.phase === "queued" && run.schedulingState !== undefined;
  const showReview = run.review.state !== "not-required";
  if (!showQueue && !showReview) return null;

  return (
    <Stack gap="sm">
      {showQueue ? (
        <Stack gap={4}>
          {run.schedulingState === "awaiting-review" && showReview ? null : (
            <Text size="xs">{runQueueExplanation(run)}</Text>
          )}
          <QueueSnapshot run={run} />
        </Stack>
      ) : null}
      {showReview ? <RunReviewDetails canReview={canReview} run={run} /> : null}
    </Stack>
  );
}

function LifecycleRelevantDetail({
  canReview,
  run,
  stage,
}: Readonly<{ canReview: boolean; run: Run; stage: Run["phase"] }>) {
  switch (stage) {
    case "queued":
      return <QueuedLifecycleDetails canReview={canReview} run={run} />;
    case "assigned":
      return (
        <Text size="xs">
          Controller in <Code>{run.cluster}</Code>
          {run.jobUid ? ` accepted Job ${run.jobUid}` : " was selected for delivery"}.
        </Text>
      );
    case "preparing":
      return (
        <Text size="xs">
          {run.jobUid ? `Job ${run.jobUid}` : "The execution Job"} prepared checkout, inputs, and
          the Action container.
        </Text>
      );
    case "running":
      return (
        <Text size="xs">
          Action <Code>{run.action}</Code> executed using Runtime <Code>{run.runtime}</Code>.
        </Text>
      );
    case "finished":
      return run.outcomeReason || run.outcomeMessage ? (
        <Stack gap={4}>
          {run.outcomeReason ? <Text size="xs">{run.outcomeReason}</Text> : null}
          {run.outcomeMessage ? <Text size="xs">{run.outcomeMessage}</Text> : null}
        </Stack>
      ) : null;
  }
}

function LifecycleObservationDetail({ run, stage }: Readonly<{ run: Run; stage: Run["phase"] }>) {
  if (run.phase === "finished" || run.observationState !== "unknown" || stage !== run.phase) {
    return null;
  }

  return (
    <Stack gap={4}>
      <Group gap="xs">
        <Badge color="yellow" size="xs" variant="light">
          Observation unavailable
        </Badge>
      </Group>
      <Text size="xs">
        The cluster Controller is unreachable or has not reported this Run recently. Its durable
        phase remains {run.phase} until observations resume.
      </Text>
    </Stack>
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

function lifecycleStageLabel(stage: RunLifecycleStage) {
  return `${stage.slice(0, 1).toLocaleUpperCase()}${stage.slice(1)}`;
}

function ParameterTable({ parameters }: Readonly<{ parameters: Run["parameters"] }>) {
  const entries = Object.entries(parameters).toSorted(([left], [right]) =>
    left.localeCompare(right),
  );
  if (entries.length === 0) return null;

  return (
    <Stack gap="sm">
      <Title order={2}>Parameters</Title>
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
    </Stack>
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
  artifactRuns,
  artifactRunsAvailable,
  artifacts,
  consumers,
  principal,
  producerSucceeded,
}: Readonly<{
  artifactRuns: Record<string, Run[]>;
  artifactRunsAvailable: Record<string, boolean>;
  artifacts: Artifact[];
  consumers: Record<string, RunArtifactConsumer[]>;
  principal?: ProductPrincipal;
  producerSucceeded: boolean;
}>) {
  const navigation = useNavigation();

  return (
    <Stack gap="sm">
      <Title order={2}>Artifacts</Title>
      {artifacts.length === 0 ? (
        <Text c="dimmed">No artifacts have been published.</Text>
      ) : (
        <Table.ScrollContainer minWidth={560} type="native">
          <Table>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>State</Table.Th>
                <Table.Th>Size</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {artifacts.map((artifact) => {
                const downstreamRuns = artifactRuns[artifact.id] ?? [];
                return (
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
                    <Table.Td>
                      <Stack gap={0}>
                        <Text>{formatBinarySize(artifact.sizeBytes)}</Text>
                        <Text c="dimmed" ff="monospace" size="xs">
                          SHA-256 {artifact.sha256 ? shortIdentifier(artifact.sha256, 12) : "—"}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Stack align="flex-start" gap="xs">
                        <Group gap="sm" wrap="wrap">
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
                        {artifact.kind === "output" ? (
                          <ArtifactLineageDetails
                            available={artifactRunsAvailable[artifact.id] ?? false}
                            runs={downstreamRuns}
                          />
                        ) : null}
                      </Stack>
                    </Table.Td>
                  </Table.Tr>
                );
              })}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  );
}

function ArtifactLineageDetails({
  available,
  runs,
}: Readonly<{ available: boolean; runs: Run[] }>) {
  return (
    <Box className={styles.artifactLineage} component="details">
      <Box component="summary">Downstream Runs · {runs.length.toLocaleString()}</Box>
      <Stack gap="xs" mt="xs">
        {!available ? (
          <Text c="red" size="xs">
            Artifact history could not be loaded.
          </Text>
        ) : runs.length === 0 ? (
          <Text c="dimmed" size="xs">
            No Runs have consumed this Artifact.
          </Text>
        ) : (
          runs.map((consumerRun) => (
            <Group gap="xs" key={consumerRun.id} wrap="wrap">
              <Anchor component={Link} size="xs" to={`/runs/${consumerRun.id}`}>
                {consumerRun.action} · {shortIdentifier(consumerRun.id)}
              </Anchor>
              <Badge color={runStatusColor(consumerRun)} size="xs" variant="light">
                {runStatusLabel(consumerRun)}
              </Badge>
              <RelativeTime c="dimmed" size="xs" value={consumerRun.createdAt} />
            </Group>
          ))
        )}
      </Stack>
    </Box>
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
