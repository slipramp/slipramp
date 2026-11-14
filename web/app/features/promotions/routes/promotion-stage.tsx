import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Tabs,
  Text,
  Title,
} from "@mantine/core";
import { IconExternalLink, IconGitBranch, IconInfoCircle, IconPackage } from "@tabler/icons-react";
import { useEffect, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getPromotionStage,
  listPromotionBranchDrift,
  type PromotionBranchDriftRepository,
  type PromotionBranchDriftStage,
  type PromotionProjectStage,
  type PromotionStageCapability,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { BranchDriftCommit } from "~/features/promotions/branch-drift-commit";
import { PromotionCreateButton } from "~/features/promotions/promotion-create-dialog";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { buildStageFreightDetailRows } from "~/features/promotions/promotion-freight";
import { KargoButton } from "~/features/promotions/kargo-link";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { promotionProjectDetailPath } from "~/features/promotions/promotion-project-inventory-state";
import {
  StageCard,
  StageFreightDetails,
  StageHealthBadge,
  StagePolicyDialog,
} from "~/features/promotions/promotion-project-view";
import {
  canonicalPromotionStageSearchParams,
  promotionStageFreightPage,
  promotionStageFreightPageSize,
  selectedPromotionStageView,
  updatePromotionStageView,
  type PromotionStageView,
} from "~/features/promotions/promotion-stage";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { problemDetail } from "~/shared/form-data";
import { PageHeader, PageHeaderFact, PageHeaderFacts } from "~/shared/page";
import { PageBackLink } from "~/shared/page-back-link";
import { pageTitle } from "~/shared/page-title";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import type { Route } from "./+types/promotion-stage";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.detail?.stage.name ?? "Stage") }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [result, principal] = await Promise.all([
    getPromotionStage({ client, path: { stageId: params.stageId } }),
    getProductPrincipal(context),
  ]);
  let branchDrift: BranchDriftSummary | undefined;
  let branchDriftError: string | undefined;

  if (result.data?.stage.capabilities.branchDrift.status === "ready") {
    const detail = result.data;
    const branchDriftResult = await listPromotionBranchDrift({
      client,
      query: {
        application: detail.stage.enrichment?.application,
        integration: detail.integration,
        limit: 100,
        project: detail.project,
        repository: detail.stage.enrichment?.repository,
      },
    });

    if (branchDriftResult.data) {
      for (const repository of branchDriftResult.data.items) {
        const stage = repository.stages.find((item) => item.id === detail.stage.id);
        if (stage) {
          branchDrift = { repository, stage };
          break;
        }
      }
    } else {
      branchDriftError = problemDetail(
        branchDriftResult.error,
        "Branch drift could not be loaded.",
      );
    }
  }

  return {
    branchDrift,
    branchDriftError,
    detail: result.data,
    error: result.data
      ? undefined
      : problemDetail(result.error, "The Kargo Stage could not be loaded."),
    principal,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  return submitPromotionProjectAction(request, context);
}

export default function PromotionStage({ actionData, loaderData }: Route.ComponentProps) {
  useActivityRefresh(true, 30_000);

  const [searchParams, setSearchParams] = useSearchParams();
  const detail = loaderData.detail;
  const freightRows = detail ? buildStageFreightDetailRows(detail.stage) : [];
  const activeView = selectedPromotionStageView(searchParams);
  const freightPage = promotionStageFreightPage(searchParams, freightRows.length);
  const canonicalSearchParams = detail
    ? canonicalPromotionStageSearchParams(searchParams, freightRows.length)
    : searchParams;
  const currentSearch = searchParams.toString();
  const canonicalSearch = canonicalSearchParams.toString();

  useEffect(() => {
    if (!detail || canonicalSearch === currentSearch) return;
    setSearchParams(canonicalSearch, { preventScrollReset: true, replace: true });
  }, [canonicalSearch, currentSearch, detail, setSearchParams]);

  if (!detail) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to Stages" to="/promotion-stages" />
        <Alert color="red" title="Stage not found">
          {loaderData.error}
        </Alert>
      </Stack>
    );
  }

  const stage = detail.stage;
  const projectPath = promotionProjectDetailPath(detail);
  const canWrite = canWritePromotions(loaderData.principal);
  const policyStage = searchParams.get("policy") === stage.id ? stage : undefined;
  const branchDriftStatus = loaderData.branchDrift?.stage.status;
  const branchDriftTabStatus =
    branchDriftStatus && branchDriftStatus !== "current"
      ? branchDriftPresentation(branchDriftStatus)
      : undefined;
  const contextLabel = [stage.enrichment?.application, stage.enrichment?.environment]
    .filter(Boolean)
    .join(" · ");

  function editPolicy() {
    const next = new URLSearchParams(searchParams);
    next.set("policy", stage.id);
    setSearchParams(next, { preventScrollReset: true });
  }

  function closePolicy() {
    const next = new URLSearchParams(searchParams);
    next.delete("policy");
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  function changeView(view: string | null) {
    if (!view) return;
    setSearchParams(updatePromotionStageView(searchParams, view as PromotionStageView), {
      preventScrollReset: true,
    });
  }

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          <Group gap="sm">
            <Button
              component={Link}
              size="compact-md"
              to={`/promotions?stageId=${encodeURIComponent(stage.id)}`}
            >
              View Promotions
            </Button>
            {stage.kargoUrl ? <KargoButton url={stage.kargoUrl} /> : null}
            {stage.argoCdUrl ? (
              <Button
                component="a"
                href={stage.argoCdUrl}
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
        }
        back={<PageBackLink label="Back to Stages" to="/promotion-stages" />}
        badges={<StageHealthBadge stage={stage} />}
        description={contextLabel || "Kargo native Stage"}
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Project">
              <Anchor component={Link} to={projectPath}>
                {detail.project}
              </Anchor>
            </PageHeaderFact>
            <PageHeaderFact label="Integration">
              <Anchor
                component={Link}
                to={`/promotion-projects?integration=${encodeURIComponent(detail.integration)}`}
              >
                {detail.integration}
              </Anchor>
            </PageHeaderFact>
            <PageHeaderFact label="Cluster">
              <Anchor component={Link} to={`/clusters/${encodeURIComponent(detail.cluster)}`}>
                {detail.cluster}
              </Anchor>
            </PageHeaderFact>
            <PageHeaderFact label="Last observed">
              <RelativeTime value={stage.lastObservedAt} />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={stage.name}
      />

      <Tabs keepMounted={false} onChange={changeView} value={activeView} variant="outline">
        <Tabs.List aria-label="Stage views" style={{ flexWrap: "nowrap", overflowX: "auto" }}>
          <Tabs.Tab
            leftSection={<IconPackage aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="freight"
          >
            Freight
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconGitBranch aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="branch-drift"
          >
            <Group gap={6} wrap="nowrap">
              Branch drift
              {branchDriftTabStatus ? (
                <Badge color={branchDriftTabStatus.color} size="xs" variant="light">
                  {branchDriftTabStatus.label}
                </Badge>
              ) : null}
            </Group>
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconInfoCircle aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="details"
          >
            Details
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel pt="lg" value="freight">
          <Stack gap="lg">
            <StageCard
              canWrite={canWrite}
              onEditPolicy={editPolicy}
              showFreightDetails={false}
              showStageHeader={false}
              stage={stage}
            />
            <Stack gap="md">
              <Title order={2} size="h3">
                All Freight
              </Title>
              <StageFreightDetails
                canWrite={canWrite}
                page={freightPage}
                pageSize={promotionStageFreightPageSize}
                stage={stage}
              />
            </Stack>
          </Stack>
        </Tabs.Panel>

        <Tabs.Panel pt="lg" value="branch-drift">
          <BranchDriftDetails
            canWrite={canWrite}
            error={loaderData.branchDriftError}
            stage={stage}
            summary={loaderData.branchDrift}
          />
        </Tabs.Panel>

        <Tabs.Panel pt="lg" value="details">
          <Stack gap="lg">
            <ObservationDetails stage={stage} />
            <CapabilityDetails stage={stage} />
          </Stack>
        </Tabs.Panel>
      </Tabs>

      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
}

type BranchDriftSummary = {
  repository: PromotionBranchDriftRepository;
  stage: PromotionBranchDriftStage;
};

function BranchDriftDetails({
  canWrite,
  error,
  stage,
  summary,
}: Readonly<{
  canWrite: boolean;
  error?: string;
  stage: PromotionProjectStage;
  summary?: BranchDriftSummary;
}>) {
  const capability = stage.capabilities.branchDrift;
  const status = summary?.stage.status;
  const presentation = branchDriftPresentation(status);

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group justify="space-between" wrap="wrap">
          <Title order={2} size="h3">
            Branch drift
          </Title>
          <Group gap="xs" wrap="wrap">
            <Badge color={presentation.color} variant="light">
              {presentation.label}
            </Badge>
            {canWrite && summary?.stage.status === "drifted" ? (
              <PromotionCreateButton
                freight={summary.stage.freight.items}
                inPlace
                stage={summary.stage}
                unavailableLabel="This Stage has no current Freight to promote in place."
              />
            ) : null}
          </Group>
        </Group>

        {error ? (
          <Alert color="red" title="Branch drift unavailable">
            {error}
          </Alert>
        ) : capability.status !== "ready" ? (
          <Text c="dimmed" size="sm">
            {capability.reason ?? "Branch drift is not available for this Stage."}
          </Text>
        ) : !summary ? (
          <Text c="dimmed" size="sm">
            No cached branch drift result is available for this Stage.
          </Text>
        ) : (
          <>
            {summary.stage.message ? <Text size="sm">{summary.stage.message}</Text> : null}
            <Stack gap="md">
              <DetailFact label="Repository">{summary.repository.repository}</DetailFact>
              <DetailFact label="Last scanned">
                <RelativeTime value={summary.repository.scannedAt} />
              </DetailFact>
              <DetailFact label="Source updated">
                <BranchDriftCommit
                  branch={summary.repository.sourceBranch}
                  commit={summary.stage.sourceBranchCommit}
                />
              </DetailFact>
              <DetailFact label="Last rendered">
                <BranchDriftCommit
                  branch={summary.stage.targetBranch}
                  commit={summary.stage.acknowledgedRenderCommit}
                />
              </DetailFact>
            </Stack>
            {summary.stage.sourcePaths.length > 0 ? (
              <Stack gap={6}>
                <Text c="dimmed" size="xs" tt="uppercase">
                  Source paths
                </Text>
                <Code block>{summary.stage.sourcePaths.join("\n")}</Code>
              </Stack>
            ) : null}
          </>
        )}
      </Stack>
    </Paper>
  );
}

function branchDriftPresentation(status?: PromotionBranchDriftStage["status"]) {
  switch (status) {
    case "current":
      return { color: "green", label: "In sync" };
    case "drifted":
      return { color: "yellow", label: "Behind" };
    default:
      return { color: "gray", label: "Unknown" };
  }
}

function ObservationDetails({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2} size="h3">
          Observation
        </Title>
        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
          <DetailFact label="First observed">
            <Timestamp value={stage.firstObservedAt} />
          </DetailFact>
          <DetailFact label="Native auto-promotion">
            <Badge color={stage.autoPromotionEnabled ? "green" : "gray"} variant="light">
              {stage.autoPromotionEnabled ? "Enabled" : "Disabled"}
            </Badge>
          </DetailFact>
          <DetailFact label="Stage kind">
            {stage.controlFlow ? "Control flow" : "Promotion Stage"}
          </DetailFact>
          {stage.currentPromotion ? (
            <DetailFact label="Current Kargo Promotion">
              {promotionReference(stage.currentPromotion)}
            </DetailFact>
          ) : null}
          {stage.lastPromotion ? (
            <DetailFact label="Last Kargo Promotion">
              {promotionReference(stage.lastPromotion)}
            </DetailFact>
          ) : null}
          <DetailFact label="Kubernetes UID">
            <Code>{stage.kubernetesUid}</Code>
          </DetailFact>
          <DetailFact label="Resource version">
            <Code>{stage.resourceVersion}</Code>
          </DetailFact>
        </SimpleGrid>
      </Stack>
    </Paper>
  );
}

function CapabilityDetails({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2} size="h3">
          Slipramp capabilities
        </Title>
        <Capability label="Promotion" value={stage.capabilities.promotion} />
        <Capability label="Review" value={stage.capabilities.review} />
        <Capability label="Branch drift" value={stage.capabilities.branchDrift} />
      </Stack>
    </Paper>
  );
}

function Capability({
  label,
  value,
}: Readonly<{ label: string; value: PromotionStageCapability }>) {
  const presentation = capabilityPresentation(value.status);
  return (
    <Stack gap={3}>
      <Group justify="space-between" wrap="wrap">
        <Text fw={600}>{label}</Text>
        <Badge color={presentation.color} variant="light">
          {presentation.label}
        </Badge>
      </Group>
      {value.reason ? (
        <Text c="dimmed" size="sm">
          {value.reason}
        </Text>
      ) : null}
    </Stack>
  );
}

function capabilityPresentation(status: PromotionStageCapability["status"]) {
  switch (status) {
    case "ready":
      return { color: "green", label: "Ready" };
    case "not-applicable":
      return { color: "gray", label: "Not applicable" };
    case "not-configured":
      return { color: "gray", label: "Not configured" };
    case "unavailable":
      return { color: "red", label: "Unavailable" };
  }
}

function DetailFact({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap={3}>
      <Text c="dimmed" size="xs" tt="uppercase">
        {label}
      </Text>
      <Text component="div" size="sm">
        {children}
      </Text>
    </Stack>
  );
}

function promotionReference(reference: { name: string; phase?: string }) {
  return reference.phase ? `${reference.name} · ${reference.phase}` : reference.name;
}
