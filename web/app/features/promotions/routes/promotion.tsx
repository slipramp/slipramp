import {
  Alert,
  Anchor,
  Badge,
  Box,
  Button,
  Checkbox,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Tabs,
  Text,
  Textarea,
  TextInput,
  Title,
  UnstyledButton,
} from "@mantine/core";
import {
  IconDownload,
  IconExternalLink,
  IconGitCompare,
  IconInfoCircle,
  IconChecklist,
  IconLoader2,
  IconPlayerPlay,
  IconStack2,
  IconTruckDelivery,
  type TablerIcon,
} from "@tabler/icons-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import { proxyArtifactDownload } from "~/api/artifact-download.server";
import { createServerAPIClient, getServerAPIURL } from "~/api/client.server";
import {
  decidePromotion,
  getPromotion,
  type GitCommit,
  type ProductPrincipal,
  type Promotion,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { useActionSuccessToast } from "~/shared/action-notifications";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { CommitReference } from "~/features/commits/git-commit";
import { ContextValue } from "~/shared/context-value";
import { Identity } from "~/shared/identity";
import { KargoButton } from "~/features/promotions/kargo-link";
import {
  promotionDeliveryStatus,
  promotionExecutionStatus,
  promotionOverallStatus,
  promotionReviewStatus,
  type PromotionStatusPresentation,
} from "~/features/promotions/promotion-status";
import { PromotionStatusBadge } from "~/features/promotions/promotion-status-badge";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import { formValue, optionalFormValue } from "~/shared/form-data";
import {
  promotionDetailActivity,
  promotionDetailNeedsRefresh,
} from "~/features/mascot/execution-activity";
import {
  filterArgoDiffResources,
  parseArgoDiff,
  type ArgoDiffResource,
} from "~/features/promotions/promotion-diff";
import { pageTitle } from "~/shared/page-title";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import {
  defaultPromotionDetailView,
  promotionDetailSearchParamNames,
  promotionLifecycleStages,
  selectedPromotionDetailView,
  selectedPromotionLifecycleStage,
  updatePromotionDetailSearchParams,
  updatePromotionLifecycleSearchParams,
  type PromotionDetailView,
  type PromotionLifecycleStage,
} from "~/features/promotions/promotion-detail-url-state";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import styles from "./promotion.module.css";
import type { Route } from "./+types/promotion";

export function meta({ loaderData }: Route.MetaArgs) {
  const title = loaderData?.promotion
    ? `Promotion ${loaderData.promotion.externalPromotionId}`
    : "Promotion";
  return [{ title: pageTitle(title) }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [promotionResult, principal] = await Promise.all([
    getPromotion({ client, path: { promotionId: params.promotionId } }),
    getProductPrincipal(context),
  ]);
  const promotion = promotionResult.data;

  let gitDiff: ArtifactText = {};
  let argoDiff: ArtifactText = {};
  let argoResources: ArgoDiffResource[] | undefined;
  if (promotion && canReadSensitiveArtifacts(principal)) {
    [gitDiff, argoDiff] = await Promise.all([
      loadArtifactText(promotion.gitDiffArtifactId, request),
      loadArtifactText(promotion.argoDiffArtifactId, request),
    ]);
    if (argoDiff.contents !== undefined) {
      try {
        argoResources = parseArgoDiff(argoDiff.contents).resources;
      } catch (error) {
        argoDiff = {
          error: error instanceof Error ? error.message : "The Argo CD diff artifact is malformed.",
        };
      }
    }
  } else if (promotion?.gitDiffArtifactId || promotion?.argoDiffArtifactId) {
    const error = "Operator permission is required to read sensitive Promotion diffs.";
    gitDiff = { error };
    argoDiff = { error };
  }

  return {
    argoDiff,
    argoResources,
    error: promotionResult.error?.detail ?? promotionResult.error?.title,
    gitDiff,
    principal,
    promotion,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const decision = formValue(formData, "decision");
  if (decision !== "approved" && decision !== "rejected") {
    return { error: "Choose approve or reject." };
  }
  const comment = optionalFormValue(formData, "comment");
  const result = await decidePromotion({
    body: { comment, decision },
    client: createServerAPIClient(request, context),
    path: { promotionId: params.promotionId },
  });

  return result.data
    ? { message: `The Promotion was ${decision}.` }
    : {
        error:
          result.error?.detail ?? result.error?.title ?? "The Promotion decision was not recorded.",
      };
}

export default function PromotionDetail({ actionData, loaderData }: Route.ComponentProps) {
  useActionSuccessToast(actionData, "Decision recorded", actionData?.message);

  const canReadDiffs = canReadSensitiveArtifacts(loaderData.principal);
  const preparedDiffsLoaded =
    !canReadDiffs ||
    (loaderData.gitDiff.contents !== undefined && loaderData.argoResources !== undefined);
  useActivityRefresh(
    loaderData.promotion !== undefined &&
      promotionDetailNeedsRefresh(loaderData.promotion, preparedDiffsLoaded),
  );

  if (!loaderData.promotion) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to promotions" to="/promotions" />
        <Alert color="red" title="Promotion not found">
          {loaderData.error ?? "The Promotion could not be loaded."}
        </Alert>
      </Stack>
    );
  }

  const promotion = loaderData.promotion;
  const canWrite = canWritePromotions(loaderData.principal);
  const overallStatus = promotionOverallStatus(promotion);

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          promotion.kargoUrl || promotion.argoCdUrl ? (
            <Group gap="sm">
              {promotion.kargoUrl ? <KargoButton url={promotion.kargoUrl} /> : null}
              {promotion.argoCdUrl ? (
                <Button
                  component="a"
                  href={promotion.argoCdUrl}
                  leftSection={<IconExternalLink aria-hidden size={16} />}
                  rel="noreferrer"
                  size="compact-md"
                  target="_blank"
                  variant="light"
                >
                  Open in Argo CD
                </Button>
              ) : null}
            </Group>
          ) : undefined
        }
        back={<PageBackLink label="Back to promotions" to="/promotions" />}
        badges={<PromotionStatusBadge status={overallStatus} />}
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Promotion">
              <Code>{promotion.id.slice(0, 8)}</Code>
            </PageHeaderFact>
            <PageHeaderFact label="Created">
              <Group gap="xs" wrap="wrap">
                <Timestamp value={promotion.createdAt} />
                <Text c="dimmed">·</Text>
                <RelativeTime c="dimmed" value={promotion.createdAt} />
              </Group>
            </PageHeaderFact>
            <PageHeaderFact label="Source">
              {promotion.source === "kargo" ? "Kargo" : "Slipramp"}
            </PageHeaderFact>
            <PageHeaderFact label="Requested by">
              <Identity
                avatarSize={22}
                name={promotion.requestedBy.displayName}
                nameProps={{ fw: 500, size: "sm" }}
              />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={promotion.externalPromotionId}
      />

      {actionData?.error ? (
        <Alert color="red" title="Decision failed">
          {actionData.error}
        </Alert>
      ) : null}
      {promotion.message ? (
        <Alert color={promotion.status === "expired" ? "gray" : "red"} title="Promotion result">
          {promotion.message}
        </Alert>
      ) : null}
      {promotion.status === "creating" ? (
        <Alert className={styles.creationNotice} color="cyan" title="Waiting for review context">
          Kargo has not submitted the Git context required to prepare this review yet.
        </Alert>
      ) : null}
      <CreationDispatchNotice promotion={promotion} />
      <KargoOutcomeNotice promotion={promotion} />

      <PromotionWorkspace
        argoDiff={loaderData.argoDiff}
        argoResources={loaderData.argoResources}
        canWrite={canWrite}
        gitDiff={loaderData.gitDiff}
        promotion={promotion}
      />
    </Stack>
  );
}

function CreationDispatchNotice({ promotion }: Readonly<{ promotion: Promotion }>) {
  const dispatch = promotion.creationDispatch;
  if (dispatch.phase !== "failed") return null;

  return (
    <Alert color="red" title="Kargo object delivery failed">
      {dispatch.diagnostic ?? "Slipramp could not materialize the Kargo Promotion."}
    </Alert>
  );
}

function KargoOutcomeNotice({ promotion }: Readonly<{ promotion: Promotion }>) {
  if (!promotion.kargoOutcome || promotion.kargoOutcome.phase === "Succeeded") return null;

  return (
    <Alert color="red" title={`Kargo Promotion ${promotion.kargoOutcome.phase.toLowerCase()}`}>
      <Stack gap="xs">
        <Text>
          {promotion.kargoOutcome.diagnostic ??
            "Kargo reached an unsuccessful terminal phase without a diagnostic."}
        </Text>
        <Text c="dimmed" size="sm">
          Observed <Timestamp value={promotion.kargoOutcome.observedAt} />
        </Text>
      </Stack>
    </Alert>
  );
}

type PromotionLifecycleState = "active" | "complete" | "error" | "terminal" | "waiting";

type PromotionLifecycleStatus = Omit<PromotionStatusPresentation, "color"> & {
  color: string;
};

type PromotionLifecycleStep = {
  detail: string;
  icon: TablerIcon;
  label: string;
  stage: PromotionLifecycleStage;
  state: PromotionLifecycleState;
  status: PromotionLifecycleStatus;
  value?: string;
};

function PromotionLifecycle({ promotion }: Readonly<{ promotion: Promotion }>) {
  const [searchParams, setSearchParams] = useSearchParams();
  const preparation = promotionPreparationStep(promotion);
  const reviewStatus = promotionReviewStatus(promotion);
  const deliveryStatus = promotionDeliveryStatus(promotion);
  const executionStatus = promotionExecutionStatus(promotion);
  const reviewWaiting =
    promotion.status === "creating" ||
    promotion.status === "preparing" ||
    preparation.state === "error";
  const steps: PromotionLifecycleStep[] = [
    {
      detail: deliveryStatus.description,
      icon: IconTruckDelivery,
      label: "Kargo delivery",
      stage: "delivery",
      state: promotionLifecycleState(deliveryStatus),
      status: deliveryStatus,
      value: deliveryStatus.recordedAt,
    },
    preparation,
    {
      detail: reviewStatus.description,
      icon: IconChecklist,
      label: "Review",
      stage: "review",
      state: reviewWaiting ? "waiting" : promotionLifecycleState(reviewStatus),
      status: reviewStatus,
      value:
        reviewStatus.recordedAt ??
        promotion.preparationCompletedAt ??
        (!promotion.reviewRequired ? promotion.createdAt : undefined),
    },
    {
      detail: executionStatus.description,
      icon: IconPlayerPlay,
      label: "Kargo execution",
      stage: "execution",
      state: promotionLifecycleState(executionStatus),
      status: executionStatus,
      value: executionStatus.recordedAt,
    },
  ];
  const activeStage = currentPromotionLifecycleStage(steps);
  const selectedStage = selectedPromotionLifecycleStage(searchParams, activeStage);
  const requestedStage = searchParams.get(promotionDetailSearchParamNames.stage);
  const selectedStep = steps[promotionLifecycleStages.indexOf(selectedStage)] ?? preparation;

  useEffect(() => {
    if (requestedStage === null || requestedStage === selectedStage) return;
    setSearchParams(updatePromotionLifecycleSearchParams(searchParams), {
      preventScrollReset: true,
      replace: true,
    });
  }, [requestedStage, searchParams, selectedStage, setSearchParams]);

  function selectStage(stage: PromotionLifecycleStage) {
    setSearchParams(updatePromotionLifecycleSearchParams(searchParams, stage), {
      preventScrollReset: true,
    });
  }

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2}>Lifecycle</Title>
        <Group className={styles.lifecycleMascot} justify="center">
          <KawaiiActivity
            activity={promotionDetailActivity(promotion)}
            animationKey={promotion.id}
            mascotOnly
          />
        </Group>
        <Stack className={styles.lifecycleSteps} gap={3}>
          {steps.map((step) => {
            const Icon = step.icon;
            const active = step.stage === activeStage && step.state === "active";
            const complete = step.state === "complete";
            const terminal = step.state === "error" || step.state === "terminal";
            const stateLabel = terminal
              ? step.status.label
              : active
                ? "Current"
                : complete
                  ? "Complete"
                  : "Waiting";
            const stateColor = terminal ? step.status.color : active ? "lilac" : "gray";
            return (
              <UnstyledButton
                aria-current={selectedStage === step.stage ? "step" : undefined}
                className={styles.lifecycleStep}
                data-selected={selectedStage === step.stage || undefined}
                key={step.stage}
                onClick={() => selectStage(step.stage)}
              >
                <Box className={styles.lifecycleIcon} data-complete={complete || undefined}>
                  <Icon aria-hidden size={15} stroke={2.2} />
                </Box>
                <Stack gap={0} style={{ flex: 1 }}>
                  <Text fw={650} size="sm">
                    {step.label}
                  </Text>
                  <Text c="dimmed" size="xs">
                    {step.value ? (
                      <Timestamp value={step.value} />
                    ) : active ? (
                      "In progress"
                    ) : (
                      "Not reached"
                    )}
                  </Text>
                </Stack>
                <Badge
                  color={stateColor}
                  size="xs"
                  variant={active || terminal ? "light" : "outline"}
                >
                  {stateLabel}
                </Badge>
              </UnstyledButton>
            );
          })}
        </Stack>
        <PromotionLifecycleStageDetail
          detail={selectedStep.detail}
          promotion={promotion}
          stage={selectedStep.stage}
          status={selectedStep.status}
          value={selectedStep.value}
        />
      </Stack>
    </Paper>
  );
}

function currentPromotionLifecycleStage(
  steps: Array<{ stage: PromotionLifecycleStage; state: PromotionLifecycleState }>,
) {
  for (let index = steps.length - 1; index >= 0; index -= 1) {
    const step = steps[index];
    if (step && step.state !== "waiting" && step.state !== "complete") return step.stage;
  }
  for (let index = steps.length - 1; index >= 0; index -= 1) {
    const step = steps[index];
    if (step?.state === "complete") return step.stage;
  }
  return "delivery";
}

function PromotionLifecycleStageDetail({
  detail,
  promotion,
  stage,
  status,
  value,
}: Readonly<{
  detail: string;
  promotion: Promotion;
  stage: PromotionLifecycleStage;
  status: PromotionLifecycleStatus;
  value?: string;
}>) {
  return (
    <Paper bg="var(--mantine-color-default-hover)" p="sm" radius="md">
      <Stack gap={5}>
        <Group gap="xs" justify="space-between" wrap="wrap">
          <Text fw={700} size="sm">
            {promotionLifecycleStageLabel(stage)}
          </Text>
          <Badge color={status.color} variant="light">
            {status.label}
          </Badge>
        </Group>
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
        <PromotionLifecycleRelevantDetail promotion={promotion} stage={stage} />
      </Stack>
    </Paper>
  );
}

function PromotionLifecycleRelevantDetail({
  promotion,
  stage,
}: Readonly<{
  promotion: Promotion;
  stage: PromotionLifecycleStage;
}>) {
  if (stage === "preparation" && promotion.preparationRun) {
    const run = promotion.preparationRun;
    return (
      <Stack gap={2}>
        <Anchor component={Link} fw={600} size="xs" to={`/runs/${run.id}`}>
          Run {run.id.slice(0, 8)}
        </Anchor>
        <Text c="dimmed" size="xs">
          {run.target} · {run.action}
        </Text>
      </Stack>
    );
  }
  if (stage === "review" && promotion.decision) {
    return (
      <Stack gap={4}>
        <Group gap={4} wrap="wrap">
          <Text c="dimmed" size="xs">
            Decided by
          </Text>
          <Identity
            avatarSize={18}
            name={promotion.decision.decidedBy.displayName}
            nameProps={{ size: "xs" }}
          />
        </Group>
        {promotion.decision.comment ? (
          <Text c="dimmed" size="xs">
            {promotion.decision.comment}
          </Text>
        ) : null}
      </Stack>
    );
  }
  if (stage === "delivery" && promotion.creationDispatch.diagnostic) {
    return (
      <Text c="dimmed" size="xs">
        {promotion.creationDispatch.diagnostic}
      </Text>
    );
  }
  if (stage === "execution" && promotion.kargoOutcome?.diagnostic) {
    return (
      <Text c="dimmed" size="xs">
        {promotion.kargoOutcome.diagnostic}
      </Text>
    );
  }
  return null;
}

function promotionLifecycleStageLabel(stage: PromotionLifecycleStage) {
  switch (stage) {
    case "preparation":
      return "Review preparation";
    case "review":
      return "Review";
    case "delivery":
      return "Kargo delivery";
    case "execution":
      return "Kargo execution";
  }
}

function promotionPreparationStep(promotion: Promotion): PromotionLifecycleStep {
  const run = promotion.preparationRun;
  if (!run) {
    const deliveryComplete =
      promotion.source === "kargo" || promotion.creationDispatch.phase === "delivered";
    const waiting =
      deliveryComplete && (promotion.status === "creating" || promotion.status === "preparing");
    return {
      detail: waiting
        ? "Waiting for the preparation Run to be created."
        : promotion.status === "creating"
          ? "The preparation Run starts after the Kargo Promotion callback."
          : "This Promotion does not require a preparation Run.",
      icon: IconLoader2,
      label: "Review preparation",
      stage: "preparation",
      state: waiting ? "active" : promotion.status === "creating" ? "waiting" : "complete",
      status: {
        color: waiting ? "cyan" : "gray",
        description: waiting
          ? "Waiting for the preparation Run"
          : promotion.status === "creating"
            ? "Waiting for Kargo delivery and callback"
            : "No preparation Run is required",
        label: waiting
          ? "Waiting"
          : promotion.status === "creating"
            ? "Not started"
            : "Not required",
      },
      value: waiting ? (promotion.creationDispatch.deliveredAt ?? promotion.createdAt) : undefined,
    };
  }

  const label = runStatusLabel(run);
  const succeeded = run.phase === "finished" && run.outcome === "succeeded";
  const failed = run.phase === "finished" && !succeeded;
  return {
    detail: succeeded
      ? "The preparation Run produced and validated the review artifacts."
      : failed
        ? `The preparation Run finished with outcome ${run.outcome ?? "unknown"}.`
        : `The preparation Run is ${label}.`,
    icon: IconLoader2,
    label: "Review preparation",
    stage: "preparation",
    state: succeeded ? "complete" : failed ? "error" : "active",
    status: {
      color: runStatusColor(run),
      description: `Preparation Run ${label}`,
      label,
    },
    value: run.finishedAt ?? run.startedAt ?? run.preparingAt ?? run.assignedAt ?? run.createdAt,
  };
}

function promotionLifecycleState(status: PromotionStatusPresentation): PromotionLifecycleState {
  if (status.color === "red") return "error";
  if (status.label === "Expired") return "terminal";
  if (status.label === "Not required") return "complete";
  if (status.color === "green") return "complete";
  if (status.color === "blue" || status.color === "cyan" || status.color === "yellow") {
    return "active";
  }
  return "waiting";
}

function PromotionContext({ promotion }: Readonly<{ promotion: Promotion }>) {
  const stage = promotion.stageSnapshot;
  const enrichment = stage.enrichment;
  const review = promotion.reviewContext;
  const policy = promotion.policySnapshot;

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2}>Promotion context</Title>
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
          <Fact
            label="Integration"
            value={
              <Anchor
                component={Link}
                to={`/promotions?integration=${encodeURIComponent(promotion.integration)}`}
              >
                {promotion.integration}
              </Anchor>
            }
          />
          <Fact
            label="Stage"
            value={
              <PromotionStageLink
                label={`${stage.project}/${stage.name}`}
                stage={{ id: promotion.stageId, name: stage.name }}
              />
            }
          />
          <Fact label="Policy snapshot" value={policy.effectiveMode} />
          {promotion.expiresAt ? (
            <Fact label="Review expires" value={<Timestamp value={promotion.expiresAt} />} />
          ) : null}
        </SimpleGrid>
        {enrichment ? (
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
            {enrichment.application ? (
              <Fact
                label="Application"
                value={
                  <Anchor
                    component={Link}
                    to={`/promotions?application=${encodeURIComponent(enrichment.application)}`}
                  >
                    {enrichment.application}
                  </Anchor>
                }
              />
            ) : null}
            {enrichment.environment ? (
              <Fact label="Environment" value={enrichment.environment} />
            ) : null}
          </SimpleGrid>
        ) : null}
        {review ? (
          <>
            <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
              <Fact
                label="Repository"
                value={
                  <Anchor component={Link} to={`/repositories/${review.repositoryId}`}>
                    {review.repositorySnapshot.name}
                  </Anchor>
                }
              />
              <Fact label="Target branch" value={review.targetBranch} />
              <Fact
                label="Candidate branch"
                value={review.git?.candidateBranch ?? "Waiting for Kargo"}
              />
            </SimpleGrid>
            {review.git ? (
              <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
                <Revision
                  commit={review.git.baseCommit}
                  label="Base revision"
                  repositoryId={review.repositoryId}
                />
                <Revision
                  commit={review.git.candidateCommit}
                  label="Candidate revision"
                  repositoryId={review.repositoryId}
                />
              </SimpleGrid>
            ) : null}
          </>
        ) : (
          <Alert color="blue" title="Kargo-native Promotion">
            This Promotion has no Slipramp Repository review context. Kargo received only the exact
            Stage and Freight identity.
          </Alert>
        )}
      </Stack>
    </Paper>
  );
}

function PromotionWorkspace({
  argoDiff,
  argoResources,
  canWrite,
  gitDiff,
  promotion,
}: Readonly<{
  argoDiff: ArtifactText;
  argoResources?: ArgoDiffResource[];
  canWrite: boolean;
  gitDiff: ArtifactText;
  promotion: Promotion;
}>) {
  const [searchParams, setSearchParams] = useSearchParams();
  const automaticView = defaultPromotionDetailView(
    promotion.gitDiffArtifactId,
    promotion.argoDiffArtifactId,
  );
  const [initialAutomaticView] = useState(automaticView);
  const activeView = selectedPromotionDetailView(searchParams, initialAutomaticView);
  const requestedView = searchParams.get(promotionDetailSearchParamNames.view);

  useEffect(() => {
    if (requestedView === null || requestedView === activeView) return;

    setSearchParams(
      updatePromotionDetailSearchParams(searchParams, activeView, initialAutomaticView),
      {
        preventScrollReset: true,
        replace: true,
      },
    );
  }, [activeView, initialAutomaticView, requestedView, searchParams, setSearchParams]);

  function changeView(value: string | null) {
    if (!value) return;
    setSearchParams(
      updatePromotionDetailSearchParams(
        searchParams,
        value as PromotionDetailView,
        initialAutomaticView,
      ),
      { preventScrollReset: true },
    );
  }

  return (
    <Box className={styles.workspace}>
      <Paper className={styles.reviewCard} withBorder>
        <Tabs keepMounted={false} onChange={changeView} value={activeView}>
          <Tabs.List className={styles.reviewTabs}>
            <Tabs.Tab leftSection={<IconGitCompare aria-hidden size={16} />} value="git">
              Git diff
            </Tabs.Tab>
            <Tabs.Tab leftSection={<IconStack2 aria-hidden size={16} />} value="argo">
              Argo CD
            </Tabs.Tab>
            <Tabs.Tab leftSection={<IconInfoCircle aria-hidden size={16} />} value="details">
              Promotion details
            </Tabs.Tab>
          </Tabs.List>

          <Tabs.Panel className={styles.reviewPanel} value="git">
            <SectionHeader
              actions={
                promotion.gitDiffArtifactId ? (
                  <Button
                    component="a"
                    href={`/api/v1/artifacts/${promotion.gitDiffArtifactId}/download`}
                    leftSection={<IconDownload aria-hidden size={16} />}
                    size="compact-md"
                    variant="light"
                  >
                    Download diff
                  </Button>
                ) : undefined
              }
              description={
                promotion.gitDiffArtifact?.description ??
                "Exact base-to-candidate changes prepared for this Promotion."
              }
              title="Git diff"
            />
            <ArtifactError error={gitDiff.error} />
            {gitDiff.contents !== undefined ? (
              gitDiff.contents.length === 0 ? (
                <Text c="dimmed">No Git changes.</Text>
              ) : (
                <Code block className={styles.gitDiff}>
                  {gitDiff.contents}
                </Code>
              )
            ) : !gitDiff.error ? (
              <DiffPending kind="Git" promotion={promotion} />
            ) : null}
          </Tabs.Panel>

          <Tabs.Panel className={styles.reviewPanel} value="argo">
            <SectionHeader
              actions={
                promotion.argoDiffArtifactId ? (
                  <Button
                    component="a"
                    href={`/api/v1/artifacts/${promotion.argoDiffArtifactId}/download`}
                    leftSection={<IconDownload aria-hidden size={16} />}
                    size="compact-md"
                    variant="light"
                  >
                    Download diff
                  </Button>
                ) : undefined
              }
              description={
                promotion.argoDiffArtifact?.description ??
                "Rendered resource changes Argo CD would apply for this Promotion."
              }
              title="Argo CD resources"
            />
            <ArtifactError error={argoDiff.error} />
            {argoResources ? (
              <ArgoDiff resources={argoResources} />
            ) : !argoDiff.error ? (
              <DiffPending kind="Argo CD" promotion={promotion} />
            ) : null}
          </Tabs.Panel>

          <Tabs.Panel className={styles.reviewPanel} value="details">
            <Stack gap="lg">
              <SectionHeader
                description="Review source, policy, revisions, and preparation details captured for this Promotion."
                title="Promotion details"
              />
              <PromotionContext promotion={promotion} />
            </Stack>
          </Tabs.Panel>
        </Tabs>
      </Paper>

      <Stack className={styles.sidebar} gap="md">
        <PromotionLifecycle promotion={promotion} />
        <DecisionPanel canWrite={canWrite} promotion={promotion} />
      </Stack>
    </Box>
  );
}

function DiffPending({ kind, promotion }: Readonly<{ kind: string; promotion: Promotion }>) {
  const preparing = promotion.status === "creating" || promotion.status === "preparing";
  return (
    <Text c="dimmed">
      {preparing
        ? `${kind} changes will appear when review preparation finishes.`
        : `This Promotion does not include a prepared ${kind} diff.`}
    </Text>
  );
}

function ArgoDiff({ resources }: Readonly<{ resources: ArgoDiffResource[] }>) {
  const [searchParams, setSearchParams] = useSearchParams();
  const query = searchParams.get("resourceQuery") ?? "";
  const showVersionMetadata = searchParams.get("showVersionMetadata") === "true";
  const filtered = useMemo(
    () => filterArgoDiffResources(resources, query, showVersionMetadata),
    [query, resources, showVersionMetadata],
  );
  const hiddenVersionCount = resources.filter((resource) => resource.versionMetadataOnly).length;

  function updateFilter(name: "resourceQuery" | "showVersionMetadata", value?: string) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(name, value);
    else next.delete(name);
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  return (
    <Stack gap="md">
      <Group align="flex-end">
        <TextInput
          className={styles.resourceQuery}
          label="Filter by kind or name"
          onChange={(event) => updateFilter("resourceQuery", event.currentTarget.value)}
          placeholder="Deployment or external-dns"
          value={query}
        />
        <Checkbox
          checked={showVersionMetadata}
          label={`Show version-metadata-only changes (${hiddenVersionCount})`}
          onChange={(event) =>
            updateFilter("showVersionMetadata", event.currentTarget.checked ? "true" : undefined)
          }
        />
      </Group>
      {filtered.length === 0 ? (
        <Text c="dimmed">
          {resources.length === 0
            ? "No Argo CD resource changes."
            : "No resources match the filter."}
        </Text>
      ) : (
        filtered.map((resource) => (
          <Paper key={resourceKey(resource)} p="sm" withBorder>
            <Stack gap="xs">
              <Group gap="xs">
                <Badge variant="light">{resource.kind}</Badge>
                <Text fw={600}>{resource.name}</Text>
                {resource.namespace ? (
                  <Text c="dimmed" size="sm">
                    {resource.namespace}
                  </Text>
                ) : null}
                {resource.versionMetadataOnly ? (
                  <Badge color="gray" variant="outline">
                    Version metadata only
                  </Badge>
                ) : null}
              </Group>
              <Code block style={{ maxHeight: 480, overflow: "auto", whiteSpace: "pre" }}>
                {resource.diff || "No textual diff."}
              </Code>
            </Stack>
          </Paper>
        ))
      )}
    </Stack>
  );
}

function DecisionPanel({
  canWrite,
  promotion,
}: Readonly<{ canWrite: boolean; promotion: Promotion }>) {
  const navigation = useNavigation();
  if (!promotion.decision && promotion.status !== "pending") return null;

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2}>Decision</Title>
        {promotion.decision ? (
          <Stack gap="xs">
            <Group gap="xs">
              <Badge color={promotion.decision.decision === "approved" ? "green" : "red"}>
                {promotion.decision.decision}
              </Badge>
              <Identity avatarSize={26} name={promotion.decision.decidedBy.displayName} />
              <Text c="dimmed">·</Text>
              <Timestamp value={promotion.decision.decidedAt} />
            </Group>
            {promotion.decision.comment ? <Text>{promotion.decision.comment}</Text> : null}
            <Text c="dimmed" size="sm">
              This attribution is immutable.
            </Text>
          </Stack>
        ) : canWrite ? (
          <Form method="post">
            <Stack gap="sm">
              <Textarea
                label="Comment"
                maxLength={4096}
                minRows={3}
                name="comment"
                placeholder="Optional context for the immutable decision"
              />
              <Group justify="flex-end">
                <Button
                  color="red"
                  loading={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("decision") === "rejected"
                  }
                  name="decision"
                  type="submit"
                  value="rejected"
                  variant="light"
                >
                  Reject
                </Button>
                <Button
                  color="green"
                  loading={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("decision") === "approved"
                  }
                  name="decision"
                  type="submit"
                  value="approved"
                >
                  Approve
                </Button>
              </Group>
            </Stack>
          </Form>
        ) : (
          <Text c="dimmed">Operator permission is required to decide this Promotion.</Text>
        )}
      </Stack>
    </Paper>
  );
}

function Fact({ label, value }: Readonly<{ label: string; value: ReactNode }>) {
  return (
    <Stack gap={0}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <ContextValue>{value}</ContextValue>
    </Stack>
  );
}

function Revision({
  commit,
  label,
  repositoryId,
}: Readonly<{ commit?: GitCommit; label: string; repositoryId: string }>) {
  return (
    <Stack gap={2}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <ContextValue>
        {commit ? (
          <CommitReference commit={commit} repositoryId={repositoryId} />
        ) : (
          "Waiting for Kargo"
        )}
      </ContextValue>
    </Stack>
  );
}

function ArtifactError({ error }: Readonly<{ error?: string }>) {
  return error ? (
    <Alert color="yellow" title="Diff unavailable">
      {error}
    </Alert>
  ) : null;
}

type ArtifactText = { contents?: string; error?: string };

async function loadArtifactText(
  artifactId: string | undefined,
  request: Request,
): Promise<ArtifactText> {
  if (!artifactId) return {};

  const response = await proxyArtifactDownload(artifactId, request, getServerAPIURL());
  if (response.ok) {
    return { contents: await response.text() };
  }

  try {
    const problem = (await response.json()) as { detail?: string; title?: string };
    return { error: problem.detail ?? problem.title ?? "The diff artifact could not be read." };
  } catch {
    return { error: "The diff artifact could not be read." };
  }
}

function canReadSensitiveArtifacts(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}

function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}

function resourceKey(resource: ArgoDiffResource) {
  return [resource.group, resource.kind, resource.namespace, resource.name].join("/");
}
