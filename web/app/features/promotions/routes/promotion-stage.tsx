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
  Text,
  Title,
} from "@mantine/core";
import { IconExternalLink } from "@tabler/icons-react";
import type { ReactNode } from "react";
import { Link, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getPromotionStage,
  type PromotionProjectStage,
  type PromotionStageCapability,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { KargoObservationTime } from "~/features/promotions/kargo-observation-time";
import { KargoButton } from "~/features/promotions/kargo-link";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { promotionProjectDetailPath } from "~/features/promotions/promotion-project-inventory-state";
import {
  StageCard,
  StageHealthBadge,
  StagePolicyDialog,
} from "~/features/promotions/promotion-project-view";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { Timestamp } from "~/shared/display-values";
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

  return {
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
  const policyStage = searchParams.get("policy") === stage.id ? stage : undefined;
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

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          stage.kargoUrl || stage.argoCdUrl ? (
            <Group gap="sm">
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
          </PageHeaderFacts>
        }
        title={stage.name}
      />

      <StageCard
        canWrite={canWritePromotions(loaderData.principal)}
        linkName={false}
        onEditPolicy={editPolicy}
        showKargoLink={false}
        stage={stage}
      />

      <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg">
        <ObservationDetails stage={stage} />
        <CapabilityDetails stage={stage} />
      </SimpleGrid>

      <Group gap="sm" wrap="wrap">
        <Button
          component={Link}
          to={`/promotions?stageId=${encodeURIComponent(stage.id)}`}
          variant="light"
        >
          View Promotions
        </Button>
        <Button component={Link} to={projectPath} variant="light">
          View Project pipelines
        </Button>
        {stage.enrichment?.application ? (
          <Button
            component={Link}
            to={`/promotion-branch-drift?integration=${encodeURIComponent(detail.integration)}&project=${encodeURIComponent(detail.project)}&application=${encodeURIComponent(stage.enrichment.application)}`}
            variant="light"
          >
            View branch drift
          </Button>
        ) : null}
      </Group>

      <StagePolicyDialog actionData={actionData} onClose={closePolicy} stage={policyStage} />
    </Stack>
  );
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
          <DetailFact label="Last observed">
            <KargoObservationTime value={stage.lastObservedAt} />
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
