import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  SimpleGrid,
  Stack,
  Tabs,
  Text,
} from "@mantine/core";
import {
  IconExternalLink,
  IconGitBranch,
  IconInfoCircle,
  IconListDetails,
  IconLock,
  IconPackage,
} from "@tabler/icons-react";
import { useEffect, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getPromotionStage,
  listPromotionBranchDrift,
  listPromotions,
  type Promotion,
  type PromotionBranchDriftRepository,
  type PromotionBranchDriftStage,
  type PromotionProjectStage,
  type PromotionStageCapability,
  type PromotionStatus,
} from "~/api/generated";
import { canManageGatePolicy, gateRefreshInterval } from "~/features/gates/gate-policy";
import { GatePolicyPanel, GlobalGateSummary } from "~/features/gates/gate-policy-panel";
import { loadLocalGatePolicies, submitGatePolicy } from "~/features/gates/gate-policy.server";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { BranchDriftCommit } from "~/features/promotions/branch-drift-commit";
import {
  InPlacePromotionLink,
  StagePromotionDialog,
} from "~/features/promotions/promotion-create-dialog";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { buildStageFreightDetailRows } from "~/features/promotions/promotion-freight";
import { KargoButton } from "~/features/promotions/kargo-link";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { promotionProjectDetailPath } from "~/features/promotions/promotion-project-inventory-state";
import { promotionIsActive } from "~/features/promotions/promotion-status";
import { PromotionTable } from "~/features/promotions/promotion-table";
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
  shouldRevalidatePromotionStage,
  updatePromotionStageView,
  type PromotionStageView,
} from "~/features/promotions/promotion-stage";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { problemDetail } from "~/shared/form-data";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import { PageBackLink } from "~/shared/page-back-link";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/promotion-stage";

const activePromotionStatuses = [
  "creating",
  "preparing",
  "pending",
  "approved",
] as const satisfies readonly PromotionStatus[];
const activePromotionPageSize = 100;

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.detail?.stage.name ?? "Stage") }];
}

export const shouldRevalidate = shouldRevalidatePromotionStage;

export async function loader({ context, params, request, url }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [result, gatePolicies, principal, activePromotionsByStatus] = await Promise.all([
    getPromotionStage({ client, path: { stageId: params.stageId } }),
    selectedPromotionStageView(url.searchParams) === "gates"
      ? loadLocalGatePolicies({ context, request }, { kind: "stage", stageId: params.stageId })
      : undefined,
    getProductPrincipal(context),
    Promise.all(
      activePromotionStatuses.map(async (status) => {
        const promotions: Promotion[] = [];
        let offset = 0;

        while (true) {
          const promotionsResult = await listPromotions({
            client,
            query: {
              limit: activePromotionPageSize,
              offset,
              stageId: params.stageId,
              status,
            },
          });
          if (!promotionsResult.data) return undefined;

          promotions.push(...promotionsResult.data.items);
          offset += promotionsResult.data.items.length;
          if (offset >= promotionsResult.data.total || promotionsResult.data.items.length === 0) {
            return promotions;
          }
        }
      }),
    ),
  ]);
  const activePromotionsAvailable = activePromotionsByStatus.every(
    (promotions) => promotions !== undefined,
  );
  const activePromotionsByID = new Map<string, Promotion>();
  for (const promotions of activePromotionsByStatus) {
    for (const promotion of promotions ?? []) activePromotionsByID.set(promotion.id, promotion);
  }
  const activePromotions = [...activePromotionsByID.values()]
    .filter(promotionIsActive)
    .toSorted(
      (left, right) =>
        right.createdAt.localeCompare(left.createdAt) || right.id.localeCompare(left.id),
    );
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
    activePromotions,
    activePromotionsAvailable,
    gatePolicies,
    branchDrift,
    branchDriftError,
    detail: result.data,
    error: result.data
      ? undefined
      : problemDetail(result.error, "The Kargo Stage could not be loaded."),
    principal,
  };
}

export async function action(args: Route.ActionArgs) {
  const { context, request, params } = args;
  const form = await request.formData();
  if (form.has("gateAction"))
    return submitGatePolicy(args, { kind: "stage", stageId: params.stageId }, form);
  return submitPromotionProjectAction(request, context, form);
}

export default function PromotionStage({ actionData, loaderData }: Route.ComponentProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const detail = loaderData.detail;
  const freightRows = detail ? buildStageFreightDetailRows(detail.stage) : [];
  const activeView = selectedPromotionStageView(searchParams);
  const scopeRefresh =
    activeView === "gates"
      ? gateRefreshInterval(
          loaderData.gatePolicies?.global.scope,
          loaderData.gatePolicies?.policy.scope,
        )
      : 30_000;
  useActivityRefresh(
    true,
    loaderData.activePromotions.length > 0 ? Math.min(3_000, scopeRefresh) : scopeRefresh,
  );
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
          canWrite || stage.kargoUrl || stage.argoCdUrl ? (
            <>
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
              {canWrite ? <InPlacePromotionLink label="Sync" stage={stage} /> : null}
            </>
          ) : undefined
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
            leftSection={<IconLock aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="gates"
          >
            Gates
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
            <ActivePromotions
              available={loaderData.activePromotionsAvailable}
              promotions={loaderData.activePromotions}
              stageId={stage.id}
            />
            <Stack gap="md">
              <SectionHeader title="Current Freight" />
              <StageCard
                canWrite={canWrite}
                onEditPolicy={editPolicy}
                showFreightDetails={false}
                showStageHeader={false}
                stage={stage}
              />
            </Stack>
            <Stack gap="md">
              <SectionHeader title="All Freight" />
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

        <Tabs.Panel pt="lg" value="gates">
          {loaderData.gatePolicies && (
            <Stack gap="lg">
              <GlobalGateSummary policy={loaderData.gatePolicies.global} />
              <SectionHeader
                title="Stage policy"
                description="Applies to Promotions released through Slipramp for this Stage."
              />
              <GatePolicyPanel
                policy={loaderData.gatePolicies.policy}
                canManage={canManageGatePolicy(loaderData.principal?.role, "stage")}
                subjectName={stage.name}
              />
              <Stack gap="sm">
                <Text fw={600} size="sm">
                  Repository Gates
                </Text>
                {stage.gates.length > 0 ? (
                  <Code block>{stage.gates.join("\n")}</Code>
                ) : (
                  <Text c="dimmed" size="sm">
                    No attachments.
                  </Text>
                )}
              </Stack>
            </Stack>
          )}
        </Tabs.Panel>
        <Tabs.Panel pt="lg" value="details">
          <Stack gap="lg">
            <ObservationDetails stage={stage} />
            <CapabilityDetails stage={stage} />
          </Stack>
        </Tabs.Panel>
      </Tabs>

      {canWrite ? <StagePromotionDialog stage={stage} /> : null}
      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
}

type BranchDriftSummary = {
  repository: PromotionBranchDriftRepository;
  stage: PromotionBranchDriftStage;
};

function ActivePromotions({
  available,
  promotions,
  stageId,
}: Readonly<{ available: boolean; promotions: Promotion[]; stageId: string }>) {
  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Button
            component={Link}
            leftSection={<IconListDetails aria-hidden size={16} />}
            size="compact-md"
            to={`/promotions?stageId=${encodeURIComponent(stageId)}`}
            variant="light"
          >
            View all promotions
          </Button>
        }
        title="Active promotions"
      />
      {!available ? (
        <Alert color="red" title="Promotions unavailable">
          Active Promotions could not be loaded.
        </Alert>
      ) : promotions.length === 0 ? (
        <Text c="dimmed">No active Promotions for this Stage.</Text>
      ) : (
        <PromotionTable promotions={promotions} />
      )}
    </Stack>
  );
}

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
    <Stack gap="md">
      <SectionHeader
        actions={
          <Group gap="xs" wrap="wrap">
            <Badge color={presentation.color} variant="light">
              {presentation.label}
            </Badge>
            {canWrite && summary?.stage.status === "drifted" ? (
              <InPlacePromotionLink label="Re-promote" stage={stage} />
            ) : null}
          </Group>
        }
        title="Branch drift"
      />

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
    <Stack gap="md">
      <SectionHeader title="Observation" />
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
  );
}

function CapabilityDetails({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  return (
    <Stack gap="md">
      <SectionHeader title="Slipramp capabilities" />
      <Capability label="Promotion" value={stage.capabilities.promotion} />
      <Capability label="Review" value={stage.capabilities.review} />
      <Capability label="Branch drift" value={stage.capabilities.branchDrift} />
    </Stack>
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
