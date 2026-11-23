import { Alert, Badge, Button, Grid, Group, Paper, Stack, Text, ThemeIcon } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import {
  IconActivityHeartbeat,
  IconAlertTriangle,
  IconArrowRight,
  IconCheck,
  IconChecklist,
  IconClock,
  IconArrowsDiff,
  IconGitBranch,
  IconGitPullRequest,
  IconLoader,
  IconPlayerStop,
  IconPackageImport,
  IconServerOff,
  IconTargetArrow,
  type TablerIcon,
} from "@tabler/icons-react";
import { useEffect, useRef } from "react";
import { Link, useFetcher } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  acknowledgeInboxProblems,
  getInbox,
  getReadiness,
  listPromotionBranchDrift,
  listPromotionProjects,
  listPromotionStages,
  type Inbox as InboxProjection,
  type InboxItem,
  type ReadinessResponse,
} from "~/api/generated";
import { RelativeTime } from "~/shared/display-values";
import { EmptyState, PageHeader, SectionHeader } from "~/shared/page";
import { problemDetail } from "~/shared/form-data";
import { acknowledgementProblemIDs, inboxDestinationHref } from "~/features/inbox/inbox";
import { pageTitle } from "~/shared/page-title";
import { asReadinessResponse } from "~/features/inbox/readiness";
import { runActivityOptions } from "~/features/runs/run-status";
import { useActivityRefresh } from "~/features/shell/activity-refresh";

import type { Route } from "./+types/index";

type AcknowledgementActionResult = { error?: string };

export function meta() {
  return [{ title: pageTitle("Inbox") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [inboxResult, projectResult, branchDriftResult, newerFreightResult, readinessResult] =
    await Promise.all([
      getInbox({ client }),
      listPromotionProjects({ client, query: { limit: 1 } }),
      listPromotionBranchDrift({ client, query: { limit: 1, status: "drifted" } }),
      listPromotionStages({ client, query: { limit: 1, newerFreight: true } }),
      getReadiness({ client }),
    ]);

  return {
    inbox: inboxResult.data,
    inboxError: inboxResult.error
      ? problemDetail(inboxResult.error, "The operational Inbox could not be loaded.")
      : undefined,
    projectError: projectResult.error
      ? problemDetail(projectResult.error, "Kargo Project status could not be loaded.")
      : undefined,
    projectSummary: projectResult.data
      ? {
          freightDifferences: projectResult.data.summary.freightDifferenceCount,
          pendingReviews: projectResult.data.summary.pendingReviewCount,
          stages: projectResult.data.summary.stageCount,
          stale: projectResult.data.summary.staleStageCount,
          unhealthy: projectResult.data.summary.unhealthyStageCount,
        }
      : undefined,
    branchDriftError: branchDriftResult.error
      ? problemDetail(branchDriftResult.error, "Kargo Stage branch drift could not be loaded.")
      : undefined,
    branchDriftTotal: branchDriftResult.data?.total,
    newerFreightError: newerFreightResult.error
      ? problemDetail(newerFreightResult.error, "Newer Kargo Freight could not be loaded.")
      : undefined,
    newerFreightTotal: newerFreightResult.data?.total,
    readiness: asReadinessResponse(readinessResult.data ?? readinessResult.error),
  };
}

export async function action({
  context,
  request,
}: Route.ActionArgs): Promise<AcknowledgementActionResult> {
  const problemIDs = acknowledgementProblemIDs(await request.formData());
  if (!problemIDs) {
    return { error: "Choose one or more current Inbox problems to acknowledge." };
  }

  const result = await acknowledgeInboxProblems({
    body: { problemIds: problemIDs },
    client: createServerAPIClient(request, context),
  });

  return result.error === undefined
    ? {}
    : {
        error: problemDetail(result.error, "The Inbox problems could not be acknowledged."),
      };
}

export default function Inbox({ loaderData }: Route.ComponentProps) {
  useActivityRefresh(true, 30_000);

  const inbox = loaderData.inbox;

  return (
    <Stack gap="lg">
      <PageHeader
        description="Decisions and operational conditions that need your attention."
        title="Inbox"
      />

      <Grid gap="xl">
        <Grid.Col span={{ base: 12, lg: 8 }}>
          {inbox ? (
            <InboxContents inbox={inbox} />
          ) : (
            <Alert color="red" icon={<IconAlertTriangle size={18} />} title="Inbox unavailable">
              {loaderData.inboxError ?? "Slipramp could not load the operational Inbox."}{" "}
              {readinessDescription(loaderData.readiness)}
            </Alert>
          )}
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 4 }}>
          <Stack gap="lg">
            <RunActivity activity={inbox?.activity} />
            <KargoStatus
              branchDriftError={loaderData.branchDriftError}
              branchDriftTotal={loaderData.branchDriftTotal}
              error={loaderData.projectError}
              newerFreightError={loaderData.newerFreightError}
              newerFreightTotal={loaderData.newerFreightTotal}
              summary={loaderData.projectSummary}
            />
          </Stack>
        </Grid.Col>
      </Grid>
    </Stack>
  );
}

function InboxContents({ inbox }: Readonly<{ inbox: InboxProjection }>) {
  const attentionCount = inbox.decisions.length + inbox.problems.length;

  if (attentionCount === 0) {
    return (
      <EmptyState
        action={
          <Button
            component={Link}
            leftSection={<IconTargetArrow aria-hidden size={16} />}
            to="/targets"
            variant="light"
          >
            Browse targets
          </Button>
        }
        description="There are no decisions or operational problems that currently need you."
        title="You’re all caught up"
      />
    );
  }

  return (
    <Stack gap="xl">
      {inbox.decisions.length > 0 ? (
        <InboxSection
          description="Requests waiting for an operator decision."
          items={inbox.decisions}
          title="Needs a decision"
        />
      ) : null}
      {inbox.problems.length > 0 ? (
        <InboxSection
          canAcknowledge={inbox.capabilities.acknowledgeProblems}
          description="Current conditions that may need investigation."
          items={inbox.problems}
          title="Problems"
        />
      ) : null}
    </Stack>
  );
}

function InboxSection({
  canAcknowledge = false,
  description,
  items,
  title,
}: Readonly<{
  canAcknowledge?: boolean;
  description: string;
  items: InboxItem[];
  title: string;
}>) {
  const fetcher = useFetcher<typeof action>();
  const acknowledgingAll = fetcher.state !== "idle";
  useAcknowledgementError(fetcher.data);

  return (
    <Stack gap="sm">
      <SectionHeader
        actions={
          <Group gap="xs">
            <Badge color="gray" variant="light">
              {items.length}
            </Badge>
            {canAcknowledge ? (
              <fetcher.Form method="post">
                {items.map((item) => (
                  <input key={item.id} name="problemId" type="hidden" value={item.id} />
                ))}
                <Button
                  leftSection={<IconCheck aria-hidden size={14} />}
                  loading={acknowledgingAll}
                  size="compact-sm"
                  type="submit"
                  variant="light"
                >
                  Acknowledge all
                </Button>
              </fetcher.Form>
            ) : null}
          </Group>
        }
        description={description}
        title={title}
      />
      <Stack gap="sm">
        {items.map((item) => (
          <InboxItemCard
            acknowledgementsEnabled={canAcknowledge}
            bulkAcknowledgementPending={acknowledgingAll}
            item={item}
            key={item.id}
          />
        ))}
      </Stack>
    </Stack>
  );
}

function InboxItemCard({
  acknowledgementsEnabled,
  bulkAcknowledgementPending,
  item,
}: Readonly<{
  acknowledgementsEnabled: boolean;
  bulkAcknowledgementPending: boolean;
  item: InboxItem;
}>) {
  const fetcher = useFetcher<typeof action>();
  const presentation = itemPresentation(item.kind);
  const ItemIcon = presentation.icon;
  useAcknowledgementError(fetcher.data);

  return (
    <Paper className="inbox-item" data-tone={presentation.tone} p="md" withBorder>
      <Group align="flex-start" gap="md" wrap="nowrap">
        <ThemeIcon color={presentation.color} size={38} variant="light">
          <ItemIcon aria-hidden size={20} stroke={1.8} />
        </ThemeIcon>
        <Stack gap="xs" style={{ flex: 1, minWidth: 0 }}>
          <Group align="flex-start" justify="space-between" wrap="wrap">
            <Text fw={650} style={{ flex: "1 1 15rem", minWidth: 0 }}>
              {item.title}
            </Text>
            <Group gap="xs" style={{ marginInlineStart: "auto" }} wrap="wrap">
              <RelativeTime c="dimmed" size="xs" value={item.occurredAt} />
              {acknowledgementsEnabled ? (
                <fetcher.Form method="post">
                  <input name="problemId" type="hidden" value={item.id} />
                  <Button
                    disabled={bulkAcknowledgementPending}
                    loading={fetcher.state !== "idle"}
                    size="compact-sm"
                    type="submit"
                    variant="subtle"
                  >
                    Acknowledge
                  </Button>
                </fetcher.Form>
              ) : null}
              <Button
                component={Link}
                rightSection={<IconArrowRight aria-hidden size={14} />}
                size="compact-sm"
                to={inboxDestinationHref(item.destination)}
                variant="light"
              >
                {item.actionLabel}
              </Button>
            </Group>
          </Group>
          <Text c="dimmed" lineClamp={3} size="sm">
            {item.description}
          </Text>
        </Stack>
      </Group>
    </Paper>
  );
}

function useAcknowledgementError(actionData: AcknowledgementActionResult | undefined) {
  const shownActionData = useRef<AcknowledgementActionResult | undefined>(undefined);

  useEffect(() => {
    if (!actionData?.error || shownActionData.current === actionData) return;

    shownActionData.current = actionData;
    notifications.show({
      color: "red",
      message: actionData.error,
      title: "Acknowledgement failed",
    });
  }, [actionData]);
}

const runActivityStyles = {
  queued: { color: "gray", icon: IconClock },
  starting: { color: "cyan", icon: IconLoader },
  running: { color: "blue", icon: IconActivityHeartbeat },
  canceling: { color: "orange", icon: IconPlayerStop },
};

function RunActivity({ activity }: Readonly<{ activity?: InboxProjection["activity"] }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon color="lilac" size="md" variant="light">
            <IconActivityHeartbeat aria-hidden size={17} />
          </ThemeIcon>
          <Text fw={650}>In progress</Text>
        </Group>
        {activity ? (
          <SummaryLinks
            indicators={runActivityOptions.map(({ value, label }) => ({
              ...runActivityStyles[value],
              href: `/runs?activity=${value}`,
              label,
              value: activity[value],
            }))}
            emptyMessage="No runs in progress."
          />
        ) : (
          <Text c="dimmed" size="sm">
            Run activity is unavailable.
          </Text>
        )}
        <Button
          component={Link}
          fullWidth
          rightSection={<IconArrowRight aria-hidden size={14} />}
          to="/runs"
          variant="subtle"
        >
          View run history
        </Button>
      </Stack>
    </Paper>
  );
}

function KargoStatus({
  branchDriftError,
  branchDriftTotal,
  error,
  newerFreightError,
  newerFreightTotal,
  summary,
}: Readonly<{
  branchDriftError?: string;
  branchDriftTotal?: number;
  error?: string;
  newerFreightError?: string;
  newerFreightTotal?: number;
  summary?: {
    freightDifferences: number;
    pendingReviews: number;
    stages: number;
    stale: number;
    unhealthy: number;
  };
}>) {
  const indicators = [
    {
      color: "orange",
      href: "/promotion-stages?attention=unhealthy",
      icon: IconAlertTriangle,
      label: "Non-ready Stages",
      value: summary?.unhealthy,
    },
    {
      color: "orange",
      href: "/promotion-stages?attention=stale",
      icon: IconClock,
      label: "Stale Stages",
      value: summary?.stale,
    },
    {
      color: "yellow",
      href: "/promotion-stages?attention=pending-review",
      icon: IconChecklist,
      label: "Pending reviews",
      value: summary?.pendingReviews,
    },
    {
      color: "yellow",
      href: "/promotion-pipelines?alignment=different",
      icon: IconArrowsDiff,
      label: "Freight differences",
      value: summary?.freightDifferences,
    },
    {
      color: "green",
      href: "/promotion-stages?attention=newer-freight",
      icon: IconPackageImport,
      label: "Newer Freight available",
      value: newerFreightTotal,
    },
    {
      color: "orange",
      href: "/promotion-branch-drift?status=drifted",
      icon: IconGitBranch,
      label: "Stage branches behind",
      value: branchDriftTotal,
    },
  ] satisfies SummaryIndicator[];

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon color="lilac" size="md" variant="light">
            <IconGitBranch aria-hidden size={17} />
          </ThemeIcon>
          <Text fw={650}>Kargo Projects</Text>
        </Group>
        <SummaryLinks indicators={indicators} emptyMessage="No Kargo conditions need attention." />
        {error ? (
          <Text c="red" size="xs">
            {error}
          </Text>
        ) : null}
        {branchDriftError ? (
          <Text c="red" size="xs">
            {branchDriftError}
          </Text>
        ) : null}
        {newerFreightError ? (
          <Text c="red" size="xs">
            {newerFreightError}
          </Text>
        ) : null}
        <Button
          component={Link}
          fullWidth
          rightSection={<IconArrowRight aria-hidden size={14} />}
          to="/promotion-stages"
          variant="subtle"
        >
          View all Stages{summary ? ` · ${summary.stages}` : ""}
        </Button>
      </Stack>
    </Paper>
  );
}

type SummaryIndicator = {
  color: string;
  href: string;
  icon: TablerIcon;
  label: string;
  value?: number;
};

function SummaryLinks({
  indicators,
  emptyMessage,
}: Readonly<{ indicators: SummaryIndicator[]; emptyMessage: string }>) {
  const visible = indicators.filter((indicator) => indicator.value !== 0);
  if (!visible.length) {
    return (
      <Text c="dimmed" size="sm">
        {emptyMessage}
      </Text>
    );
  }
  return (
    <Stack gap="xs">
      {visible.map(({ color, href, icon: Icon, label, value }) => (
        <Button
          key={href}
          component={Link}
          fz="sm"
          fullWidth
          justify="space-between"
          leftSection={<Icon aria-hidden size={16} />}
          rightSection={
            <Badge color={color} size="lg" variant="light">
              {value ?? "—"}
            </Badge>
          }
          size="md"
          to={href}
          variant="default"
        >
          {label}
        </Button>
      ))}
    </Stack>
  );
}

function itemPresentation(kind: InboxItem["kind"]): {
  color: "orange" | "red" | "yellow";
  icon: TablerIcon;
  tone: "critical" | "warning";
} {
  switch (kind) {
    case "promotion-review":
      return { color: "yellow", icon: IconGitPullRequest, tone: "warning" };
    case "run-review":
      return { color: "yellow", icon: IconChecklist, tone: "warning" };
    case "run-observation":
      return { color: "orange", icon: IconActivityHeartbeat, tone: "warning" };
    case "repository-refresh":
      return { color: "red", icon: IconGitBranch, tone: "critical" };
    case "cluster-unreachable":
      return { color: "red", icon: IconServerOff, tone: "critical" };
    case "git-trigger-failure":
    case "promotion-error":
      return { color: "red", icon: IconAlertTriangle, tone: "critical" };
  }
}

function readinessDescription(readiness: ReadinessResponse | undefined) {
  if (!readiness) {
    return "Server readiness could not be determined.";
  }

  const unavailable = [
    readiness.database === "unavailable" ? "PostgreSQL" : undefined,
    readiness.artifactStorage === "unavailable" ? "artifact storage" : undefined,
  ].filter((dependency): dependency is string => dependency !== undefined);
  if (unavailable.length > 0) {
    return `Readiness reports ${humanList(unavailable)} unavailable.`;
  }

  return "Server dependencies are ready; retry the request or inspect Server logs.";
}

function humanList(items: string[]) {
  if (items.length === 0) return "";
  if (items.length === 1) return items[0];
  if (items.length === 2) return items.join(" and ");
  return `${items.slice(0, -1).join(", ")}, and ${items.at(-1)}`;
}
