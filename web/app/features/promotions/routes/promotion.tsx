import { LifecycleStep } from "~/shared/lifecycle-step";
import { CancelButton, CancellationDetail, CancellationStep } from "~/shared/cancellation-detail";
import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Box,
  Button,
  Checkbox,
  Code,
  Group,
  Paper,
  Stack,
  Tabs,
  Text,
  Textarea,
  TextInput,
  Tooltip,
  Title,
} from "@mantine/core";
import {
  IconDownload,
  IconExternalLink,
  IconGitCompare,
  IconInfoCircle,
  IconChecklist,
  IconLoader2,
  IconPlayerPlay,
  IconRefresh,
  IconStack2,
  IconTruckDelivery,
  type TablerIcon,
} from "@tabler/icons-react";
import { useEffect, useMemo } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import { proxyArtifactDownload } from "~/api/artifact-download.server";
import { createServerAPIClient, getServerAPIURL } from "~/api/client.server";
import {
  cancelPromotion,
  overridePromotionGate,
  decidePromotion,
  getPromotion,
  getPromotionStage,
  type ProductPrincipal,
  type Promotion,
  type PromotionFreightItem,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { useActionSuccessToast } from "~/shared/action-notifications";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { CommitDetails } from "~/features/commits/git-commit";
import { DetailFact, DetailGrid } from "~/shared/context-value";
import { Identity } from "~/shared/identity";
import { KargoButton } from "~/features/promotions/kargo-link";
import { FreightContents, FreightIdentifier } from "~/features/promotions/freight-display";
import {
  canWritePromotions,
  repeatPromotionFreight,
} from "~/features/promotions/promotion-creation";
import { PromotionCreateButton } from "~/features/promotions/promotion-create-dialog";
import { promotionStagePath } from "~/features/promotions/promotion-stage";
import { freightOriginLabel } from "~/features/promotions/promotion-freight";
import { loadLockStatus } from "~/features/gates/gate-policy.server";
import { LockWarning } from "~/features/gates/lock-warning";
import { ReleaseGateDialog, ReleaseGateSection } from "~/features/gates/release-gate-details";
import {
  promotionDeliveryStatus,
  promotionExecutionStatus,
  promotionIsActive,
  promotionOverallStatus,
  promotionReviewStatus,
  promotionRepeatLabel,
  type PromotionStatusPresentation,
} from "~/features/promotions/promotion-status";
import { PromotionStatusBadge } from "~/features/promotions/promotion-status-badge";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { PageBackLink } from "~/shared/page-back-link";
import {
  CopyableIdFact,
  DetailSection,
  PageHeader,
  PageHeaderFact,
  PageHeaderFacts,
} from "~/shared/page";
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
import { PromotionProjectLink } from "~/features/promotions/promotion-project-link";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import {
  defaultPromotionDetailView,
  promotionDetailSearchParamNames,
  selectedPromotionDetailView,
  selectedPromotionLifecycleStage,
  updatePromotionDetailSearchParams,
  updatePromotionLifecycleSearchParams,
  type PromotionDetailView,
  type PromotionPhaseStage,
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
  const stageLockRequest = promotion
    ? loadLockStatus({ context, request }, { kind: "stage", stageId: promotion.stageId })
    : undefined;

  const repeatLabel =
    promotion && canWritePromotions(principal) ? promotionRepeatLabel(promotion) : undefined;
  const repeatStageRequest =
    promotion && repeatLabel
      ? getPromotionStage({ client, path: { stageId: promotion.stageId } })
      : undefined;

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

  const [stageResult, lockStatus] = await Promise.all([repeatStageRequest, stageLockRequest]);
  const repeatStage = stageResult?.data?.stage;

  return {
    argoDiff,
    argoResources,
    error: promotionResult.error?.detail ?? promotionResult.error?.title,
    gitDiff,
    lockStatus,
    principal,
    promotion,
    repeatLabel,
    repeatOptions:
      repeatStage && promotion
        ? {
            ...repeatPromotionFreight(promotion, repeatStage),
            stage: {
              id: repeatStage.id,
              name: repeatStage.name,
              capabilities: repeatStage.capabilities,
              policy: repeatStage.policy,
            },
          }
        : undefined,
    repeatStageError: stageResult?.error?.detail ?? stageResult?.error?.title,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  if (formValue(formData, "intent") === "override-gate") {
    const result = await overridePromotionGate({
      client: createServerAPIClient(request, context),
      path: { promotionId: params.promotionId },
      body: {
        key: formValue(formData, "gateKey"),
        version: formValue(formData, "version"),
        reason: optionalFormValue(formData, "reason"),
      },
    });
    if (result.data) return { message: "Gate override recorded." };
    return { error: result.error?.detail ?? "The Gate override could not be recorded." };
  }
  if (formValue(formData, "intent") === "cancel") {
    const result = await cancelPromotion({
      client: createServerAPIClient(request, context),
      path: { promotionId: params.promotionId },
    });
    return result.data
      ? { message: "Cancellation recorded." }
      : {
          error:
            result.error?.detail ?? result.error?.title ?? "The Promotion could not be canceled.",
        };
  }
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
  useActionSuccessToast(actionData, "Promotion updated", actionData?.message);

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
  const canManageActivePromotion =
    canWrite && promotionIsActive(promotion) && promotion.cancellation.state === "not-requested";
  const overallStatus = promotionOverallStatus(promotion);

  return (
    <Stack gap="lg">
      {loaderData.lockStatus && (
        <LockWarning
          key={promotion.stageId}
          initial={loaderData.lockStatus}
          owner={{ kind: "stage", stageId: promotion.stageId }}
          gatesPath={`${promotionStagePath(promotion.stageId)}?view=gates`}
        />
      )}
      <PageHeader
        actions={
          canManageActivePromotion ||
          loaderData.repeatLabel ||
          promotion.kargoUrl ||
          promotion.argoCdUrl ? (
            <>
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
              {loaderData.repeatLabel &&
                (loaderData.repeatOptions ? (
                  <PromotionCreateButton
                    {...loaderData.repeatOptions}
                    repeatLabel={loaderData.repeatLabel}
                    formAction={promotionStagePath(promotion.stageId)}
                  />
                ) : (
                  <Tooltip
                    label={loaderData.repeatStageError ?? "The destination Stage is unavailable."}
                  >
                    <span>
                      <Button
                        disabled
                        leftSection={<IconRefresh aria-hidden size={16} />}
                        size="compact-md"
                      >
                        {loaderData.repeatLabel}
                      </Button>
                    </span>
                  </Tooltip>
                ))}
              {canManageActivePromotion ? <CancelButton>Cancel promotion</CancelButton> : null}
            </>
          ) : undefined
        }
        back={<PageBackLink label="Back to promotions" to="/promotions" />}
        badges={<PromotionStatusBadge status={overallStatus} />}
        details={
          <PageHeaderFacts>
            <CopyableIdFact label="Promotion ID" value={promotion.id} />
            <PageHeaderFact label="Stage">
              <PromotionStageLink fw={400} stage={promotion.stageSnapshot} />
            </PageHeaderFact>
            <PageHeaderFact label="Source">
              {promotion.source === "kargo" ? "Kargo" : "Slipramp"}
            </PageHeaderFact>
            <PageHeaderFact label="Created by">
              <Identity
                avatarSize={22}
                name={promotion.requestedBy.displayName}
                nameProps={{ fw: 500, size: "sm" }}
              />
            </PageHeaderFact>
            <PageHeaderFact label="Created">
              <RelativeTime value={promotion.createdAt} />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={promotion.externalPromotionId}
      />

      {actionData?.error ? (
        <Alert color="red" title="Promotion update failed">
          {actionData.error}
        </Alert>
      ) : null}
      {promotion.message ? (
        <Alert color={promotion.status === "expired" ? "gray" : "red"} title="Promotion result">
          {promotion.message}
        </Alert>
      ) : null}
      {promotion.status === "creating" &&
      promotion.cancellation.state === "not-requested" &&
      !promotion.kargoOutcome ? (
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
      <ReleaseGateDialog
        gates={promotion.gates}
        subject="Promotion"
        subjectId={promotion.id}
        localOwnerId={promotion.stageId}
        canOverride={canManageActivePromotion}
      />
    </Stack>
  );
}

function SelectedFreight({ freight }: Readonly<{ freight: PromotionFreightItem }>) {
  return (
    <DetailSection defaultExpanded title="Selected Freight">
      <DetailGrid>
        <DetailFact label="Freight ID">
          <FreightIdentifier freight={freight} />
        </DetailFact>
        <DetailFact label="Warehouse">{freightOriginLabel(freight.origin)}</DetailFact>
        <Box style={{ gridColumn: "1 / -1" }}>
          <DetailFact label="Contents">
            <FreightContents freight={freight} />
          </DetailFact>
        </Box>
      </DetailGrid>
    </DetailSection>
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
  if (
    !promotion.kargoOutcome ||
    promotion.kargoOutcome.phase === "Succeeded" ||
    promotion.cancellation.state === "completed"
  )
    return null;

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
  stage: PromotionPhaseStage;
  state: PromotionLifecycleState;
  status: PromotionLifecycleStatus;
  value?: string;
};

function PromotionLifecycle({
  onStageChange,
  promotion,
  selectedStage,
  showHeading = true,
  steps,
}: Readonly<{
  onStageChange: (stage: PromotionLifecycleStage) => void;
  promotion: Promotion;
  selectedStage: PromotionLifecycleStage;
  showHeading?: boolean;
  steps: PromotionLifecycleStep[];
}>) {
  const activeStage = currentPromotionLifecycleStage(steps);
  const selectedStep = steps.find((step) => step.stage === selectedStage);

  return (
    <Stack gap="md">
      {showHeading ? <Title order={2}>Lifecycle</Title> : null}
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
            <LifecycleStep
              key={step.stage}
              icon={Icon}
              label={step.label}
              time={step.value}
              status={stateLabel}
              color={stateColor}
              complete={complete}
              highlighted={active || terminal}
              selected={selectedStage === step.stage}
              onSelect={() => onStageChange(step.stage)}
              missingTime={active ? "In progress" : "Not reached"}
            />
          );
        })}
        <CancellationStep
          cancellation={promotion.cancellation}
          selected={selectedStage === "cancellation"}
          onSelect={() => onStageChange("cancellation")}
        />
      </Stack>
      {selectedStep ? (
        <PromotionLifecycleStageDetail
          detail={selectedStep.detail}
          promotion={promotion}
          stage={selectedStep.stage}
          status={selectedStep.status}
          value={selectedStep.value}
        />
      ) : (
        <CancellationDetail
          cancellation={promotion.cancellation}
          subject="Promotion"
          outcome={promotion.kargoOutcome?.phase}
          finishedAt={promotion.kargoOutcome?.observedAt}
        />
      )}
    </Stack>
  );
}

function promotionLifecycleSteps(promotion: Promotion): PromotionLifecycleStep[] {
  const delivery = promotionDeliveryStatus(promotion);
  const steps: PromotionLifecycleStep[] = [
    {
      detail: delivery.description,
      icon: IconTruckDelivery,
      label: "Kargo delivery",
      stage: "delivery",
      state: promotionLifecycleState(delivery),
      status: delivery,
      value: delivery.recordedAt,
    },
  ];

  if (promotion.reviewRequired) {
    const preparation = promotionPreparationStep(promotion);
    const review = promotionReviewStatus(promotion);
    const waiting =
      promotion.cancellation.state === "not-requested" &&
      (promotion.status === "creating" ||
        promotion.status === "preparing" ||
        preparation.state === "error");
    steps.push(preparation, {
      detail: review.description,
      icon: IconChecklist,
      label: "Review",
      stage: "review",
      state: waiting ? "waiting" : promotionLifecycleState(review),
      status: review,
      value: review.recordedAt ?? promotion.preparationCompletedAt,
    });
  }

  const execution = promotionExecutionStatus(promotion);
  steps.push({
    detail: execution.description,
    icon: IconPlayerPlay,
    label: "Kargo execution",
    stage: "execution",
    state: promotionLifecycleState(execution),
    status: execution,
    value: execution.recordedAt,
  });
  return steps;
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
    case "cancellation":
      return "Cancellation";
  }
}

function promotionPreparationStep(promotion: Promotion): PromotionLifecycleStep {
  const run = promotion.preparationRun;
  if (!run && promotion.cancellation.state !== "not-requested" && promotion.reviewRequired) {
    const detail = "Cancellation stopped review preparation before a Run started.";
    return {
      detail,
      icon: IconLoader2,
      label: "Review preparation",
      stage: "preparation",
      state: "terminal",
      status: { color: "gray", description: detail, label: "Stopped" },
    };
  }
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
  if (["Expired", "Stopped", "Closed", "Aborted", "Skipped"].includes(status.label))
    return "terminal";
  if (status.label === "Not required") return "complete";
  if (status.color === "green") return "complete";
  if (status.color === "blue" || status.color === "cyan" || status.color === "yellow") {
    return "active";
  }
  return "waiting";
}

function PromotionDetails({ promotion }: Readonly<{ promotion: Promotion }>) {
  const stage = promotion.stageSnapshot;
  const enrichment = stage.enrichment;
  const review = promotion.reviewContext;
  const git = review?.git;
  return (
    <>
      {review && git && (
        <DetailSection defaultExpanded title="Git revisions">
          <DetailGrid>
            <CommitDetails
              label="Base revision"
              commit={git.baseCommit}
              repositoryId={review.repositoryId}
              stacked
              titleOrder={3}
            />
            <CommitDetails
              label="Candidate revision"
              commit={git.candidateCommit}
              repositoryId={review.repositoryId}
              stacked
              titleOrder={3}
            />
          </DetailGrid>
        </DetailSection>
      )}
      <ReleaseGateSection gates={promotion.gates} title="Promotion gates" />
      {promotion.selectedFreight && <SelectedFreight freight={promotion.selectedFreight} />}
      <DetailSection title="Promotion context">
        <Stack gap="lg">
          <DetailGrid>
            <DetailFact label="Integration">
              <Anchor
                component={Link}
                to={`/promotions?integration=${encodeURIComponent(promotion.integration)}`}
              >
                {promotion.integration}
              </Anchor>
            </DetailFact>
            <DetailFact label="Project">
              <PromotionProjectLink fw={400} project={stage} />
            </DetailFact>
            <DetailFact label="Stage">
              <PromotionStageLink fw={400} stage={stage} />
            </DetailFact>
            <DetailFact label="Policy snapshot">
              {promotion.policySnapshot.effectiveMode}
            </DetailFact>
            {promotion.expiresAt && (
              <DetailFact label="Review deadline">
                <Timestamp value={promotion.expiresAt} />
              </DetailFact>
            )}
            {enrichment?.application && (
              <DetailFact label="Application">
                <Anchor
                  component={Link}
                  to={`/promotions?application=${encodeURIComponent(enrichment.application)}`}
                >
                  {enrichment.application}
                </Anchor>
              </DetailFact>
            )}
            {enrichment?.environment && (
              <DetailFact label="Environment">{enrichment.environment}</DetailFact>
            )}
            {review && (
              <>
                <DetailFact label="Repository">
                  <Anchor component={Link} to={`/repositories/${review.repositoryId}`}>
                    {review.repositorySnapshot.name}
                  </Anchor>
                </DetailFact>
                <DetailFact label="Target branch">{review.targetBranch}</DetailFact>
                <DetailFact label="Candidate branch">
                  {git?.candidateBranch ??
                    (promotionIsActive(promotion) &&
                    promotion.cancellation.state === "not-requested"
                      ? "Waiting for Kargo"
                      : "Not recorded")}
                </DetailFact>
              </>
            )}
          </DetailGrid>
          {!review && (
            <Text size="sm" c="dimmed">
              This Kargo-native Promotion uses the Stage and Freight directly.
            </Text>
          )}
        </Stack>
      </DetailSection>
    </>
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
  const activeView = selectedPromotionDetailView(searchParams, automaticView);
  const requestedView = searchParams.get(promotionDetailSearchParamNames.view);
  const lifecycleSteps = promotionLifecycleSteps(promotion);
  const activeLifecycleStage =
    promotion.cancellation.state === "not-requested"
      ? currentPromotionLifecycleStage(lifecycleSteps)
      : "cancellation";
  const selectedLifecycleStage = selectedPromotionLifecycleStage(
    searchParams,
    lifecycleSteps.map((step) => step.stage),
    activeLifecycleStage,
  );
  const requestedLifecycleStage = searchParams.get(promotionDetailSearchParamNames.stage);
  const selectedLifecycleStep = lifecycleSteps.find(
    (step) => step.stage === selectedLifecycleStage,
  );

  useEffect(() => {
    if (requestedView === null || requestedView === activeView) return;

    setSearchParams(updatePromotionDetailSearchParams(searchParams, activeView, automaticView), {
      preventScrollReset: true,
      replace: true,
    });
  }, [activeView, automaticView, requestedView, searchParams, setSearchParams]);

  useEffect(() => {
    if (requestedLifecycleStage === null || requestedLifecycleStage === selectedLifecycleStage) {
      return;
    }
    setSearchParams(updatePromotionLifecycleSearchParams(searchParams), {
      preventScrollReset: true,
      replace: true,
    });
  }, [requestedLifecycleStage, searchParams, selectedLifecycleStage, setSearchParams]);

  function changeView(value: string | null) {
    if (!value) return;
    setSearchParams(
      updatePromotionDetailSearchParams(searchParams, value as PromotionDetailView, automaticView),
      { preventScrollReset: true },
    );
  }

  function changeLifecycleStage(stage: PromotionLifecycleStage) {
    setSearchParams(updatePromotionLifecycleSearchParams(searchParams, stage), {
      preventScrollReset: true,
    });
  }

  const lifecycleProps = {
    onStageChange: changeLifecycleStage,
    promotion,
    selectedStage: selectedLifecycleStage,
    steps: lifecycleSteps,
  };

  return (
    <Box className={styles.workspace}>
      <Accordion className={styles.mobileLifecycle} variant="contained">
        <Accordion.Item value="lifecycle">
          <Accordion.Control>
            <Group justify="space-between" pr="sm" wrap="nowrap">
              <Text fw={650}>Lifecycle</Text>
              <Badge
                color={
                  selectedLifecycleStep?.status.color ??
                  (promotion.cancellation.state === "requested" ? "yellow" : "gray")
                }
                variant="light"
              >
                {promotionLifecycleStageLabel(selectedLifecycleStage)}
              </Badge>
            </Group>
          </Accordion.Control>
          <Accordion.Panel>
            <PromotionLifecycle {...lifecycleProps} showHeading={false} />
          </Accordion.Panel>
        </Accordion.Item>
      </Accordion>

      <Paper className={styles.reviewCard} withBorder>
        <Tabs keepMounted={false} onChange={changeView} value={activeView}>
          <Tabs.List aria-label="Promotion views" className={styles.reviewTabs}>
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
            <DiffDownloadButton artifactId={promotion.gitDiffArtifactId} />
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
            <DiffDownloadButton artifactId={promotion.argoDiffArtifactId} />
            <ArtifactError error={argoDiff.error} />
            {argoResources ? (
              <ArgoDiff resources={argoResources} />
            ) : !argoDiff.error ? (
              <DiffPending kind="Argo CD" promotion={promotion} />
            ) : null}
          </Tabs.Panel>

          <Tabs.Panel value="details">
            <PromotionDetails key={promotion.id} promotion={promotion} />
          </Tabs.Panel>
        </Tabs>
      </Paper>

      <Stack className={styles.sidebar} gap="md">
        <Paper className={styles.desktopLifecycle} p="md" withBorder>
          <PromotionLifecycle {...lifecycleProps} />
        </Paper>
        <DecisionPanel canWrite={canWrite} promotion={promotion} />
      </Stack>
    </Box>
  );
}

function DiffDownloadButton({ artifactId }: Readonly<{ artifactId?: string }>) {
  if (!artifactId) return null;
  return (
    <Button
      component="a"
      href={`/api/v1/artifacts/${artifactId}/download`}
      leftSection={<IconDownload aria-hidden size={16} />}
      ml="auto"
      size="compact-md"
      variant="light"
    >
      Download diff
    </Button>
  );
}

function DiffPending({ kind, promotion }: Readonly<{ kind: string; promotion: Promotion }>) {
  const preparing =
    promotion.cancellation.state === "not-requested" &&
    !promotion.kargoOutcome &&
    (promotion.status === "creating" || promotion.status === "preparing");
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
  if (
    !promotion.decision &&
    (promotion.status !== "pending" ||
      promotion.cancellation.state !== "not-requested" ||
      promotion.kargoOutcome)
  )
    return null;

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

function resourceKey(resource: ArgoDiffResource) {
  return [resource.group, resource.kind, resource.namespace, resource.name].join("/");
}
