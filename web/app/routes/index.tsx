import {
  Alert,
  Badge,
  Button,
  Grid,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  ThemeIcon,
} from "@mantine/core";
import {
  IconActivityHeartbeat,
  IconAlertTriangle,
  IconArrowRight,
  IconCircleCheck,
  IconGitBranch,
  IconGitPullRequest,
  IconServerOff,
  IconTargetArrow,
  type TablerIcon,
} from "@tabler/icons-react";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getCurrentPrincipal,
  getReadiness,
  listClusters,
  listPromotions,
  listRepositories,
  listRuns,
  type ReadinessResponse,
} from "~/api/generated";
import { RelativeTime } from "~/components/display";
import { EmptyState, PageHeader, SectionHeader } from "~/components/page";
import { buildInbox, type InboxItem, type InboxItemKind } from "~/inbox";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/index";

const domainPageLimit = 100;

export function meta() {
  return [{ title: pageTitle("Inbox") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [
    principalResult,
    readinessResult,
    repositoriesResult,
    clustersResult,
    pendingPromotionsResult,
    errorPromotionsResult,
    queuedRunsResult,
    assignedRunsResult,
    preparingRunsResult,
    runningRunsResult,
  ] = await Promise.all([
    getCurrentPrincipal({ client }),
    getReadiness({ client }),
    listRepositories({
      client,
      query: { limit: 200, refreshStatus: "failed" },
    }),
    listClusters({
      client,
      query: { connection: "unreachable", limit: 200 },
    }),
    listPromotions({
      client,
      query: { limit: domainPageLimit, status: "pending" },
    }),
    listPromotions({
      client,
      query: { limit: domainPageLimit, status: "error" },
    }),
    listRuns({ client, query: { limit: 1, phase: "queued" } }),
    listRuns({ client, query: { limit: domainPageLimit, phase: "assigned" } }),
    listRuns({ client, query: { limit: domainPageLimit, phase: "preparing" } }),
    listRuns({ client, query: { limit: domainPageLimit, phase: "running" } }),
  ]);

  const runResults = [assignedRunsResult, preparingRunsResult, runningRunsResult];
  const promotions = [
    ...(pendingPromotionsResult.data?.items ?? []),
    ...(errorPromotionsResult.data?.items ?? []),
  ];
  const activeRuns = runResults.flatMap((result) => result.data?.items ?? []);
  const inbox = buildInbox({
    activeRuns,
    principal: principalResult.data,
    promotions,
    readiness: readinessReport(readinessResult.data ?? readinessResult.error),
    repositories: repositoriesResult.data?.items ?? [],
    unreachableClusters: clustersResult.data?.items ?? [],
  });

  const unavailableSources = new Set<string>();
  if (!principalResult.data) unavailableSources.add("your permissions");
  if (!readinessReport(readinessResult.data ?? readinessResult.error)) {
    unavailableSources.add("server readiness");
  }
  if (!repositoriesResult.data) unavailableSources.add("repository health");
  if (!clustersResult.data) unavailableSources.add("cluster health");
  if (!pendingPromotionsResult.data || !errorPromotionsResult.data) {
    unavailableSources.add("Promotions");
  }
  if (
    !queuedRunsResult.data ||
    !assignedRunsResult.data ||
    !preparingRunsResult.data ||
    !runningRunsResult.data
  ) {
    unavailableSources.add("Runs");
  }

  const limitedSources = new Set<string>();
  if (
    (pendingPromotionsResult.data?.total ?? 0) >
      (pendingPromotionsResult.data?.items.length ?? 0) ||
    (errorPromotionsResult.data?.total ?? 0) > (errorPromotionsResult.data?.items.length ?? 0)
  ) {
    limitedSources.add("Promotions");
  }
  if ((repositoriesResult.data?.total ?? 0) > (repositoriesResult.data?.items.length ?? 0)) {
    limitedSources.add("repositories");
  }
  if ((clustersResult.data?.total ?? 0) > (clustersResult.data?.items.length ?? 0)) {
    limitedSources.add("clusters");
  }
  if (runResults.some((result) => (result.data?.total ?? 0) > (result.data?.items.length ?? 0))) {
    limitedSources.add("active Runs");
  }

  const activity =
    queuedRunsResult.data &&
    assignedRunsResult.data &&
    preparingRunsResult.data &&
    runningRunsResult.data
      ? {
          queued: queuedRunsResult.data.total,
          running: runningRunsResult.data.total,
          starting: assignedRunsResult.data.total + preparingRunsResult.data.total,
        }
      : undefined;

  return {
    activity,
    decisions: inbox.decisions,
    limitedSources: [...limitedSources],
    problems: inbox.problems,
    unavailableSources: [...unavailableSources],
  };
}

export default function Inbox({ loaderData }: Route.ComponentProps) {
  const attentionCount = loaderData.decisions.length + loaderData.problems.length;

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          <Badge
            color={attentionCount === 0 ? "green" : "yellow"}
            leftSection={
              attentionCount === 0 ? <IconCircleCheck aria-hidden size={15} /> : undefined
            }
            size="lg"
            variant="light"
          >
            {attentionCount === 0
              ? "All clear"
              : `${attentionCount} ${attentionCount === 1 ? "item" : "items"}`}
          </Badge>
        }
        description="Decisions and operational conditions that need your attention."
        title="Inbox"
      />

      {loaderData.unavailableSources.length > 0 ? (
        <Alert color="yellow" icon={<IconAlertTriangle size={18} />} title="Inbox is incomplete">
          Slipramp could not load {humanList(loaderData.unavailableSources)}. Available items are
          still shown below.
        </Alert>
      ) : null}

      {loaderData.limitedSources.length > 0 ? (
        <Alert color="blue" title="Some results are not shown">
          This first version limits the number of {humanList(loaderData.limitedSources)} loaded into
          the Inbox. Use the linked domain views for the complete history.
        </Alert>
      ) : null}

      <Grid gap="xl">
        <Grid.Col span={{ base: 12, lg: 8 }}>
          {attentionCount === 0 ? (
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
          ) : (
            <Stack gap="xl">
              {loaderData.decisions.length > 0 ? (
                <InboxSection
                  description="Requests waiting for an operator decision."
                  items={loaderData.decisions}
                  title="Needs a decision"
                />
              ) : null}
              {loaderData.problems.length > 0 ? (
                <InboxSection
                  description="Current conditions that may need investigation."
                  items={loaderData.problems}
                  title="Problems"
                />
              ) : null}
            </Stack>
          )}
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 4 }}>
          <RunActivity activity={loaderData.activity} />
        </Grid.Col>
      </Grid>
    </Stack>
  );
}

function InboxSection({
  description,
  items,
  title,
}: Readonly<{ description: string; items: InboxItem[]; title: string }>) {
  return (
    <Stack gap="sm">
      <SectionHeader
        actions={
          <Badge color="gray" variant="light">
            {items.length}
          </Badge>
        }
        description={description}
        title={title}
      />
      <Stack gap="sm">
        {items.map((item) => (
          <InboxItemCard item={item} key={item.id} />
        ))}
      </Stack>
    </Stack>
  );
}

function InboxItemCard({ item }: Readonly<{ item: InboxItem }>) {
  const presentation = itemPresentation(item.kind);
  const ItemIcon = presentation.icon;

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
            <Group gap="xs" style={{ marginInlineStart: "auto" }} wrap="nowrap">
              {item.occurredAt ? (
                <RelativeTime c="dimmed" size="xs" value={item.occurredAt} />
              ) : null}
              <Button
                component={Link}
                rightSection={<IconArrowRight aria-hidden size={14} />}
                size="compact-sm"
                to={item.href}
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

function RunActivity({
  activity,
}: Readonly<{ activity?: { queued: number; running: number; starting: number } }>) {
  const active = activity ? activity.queued + activity.starting + activity.running : undefined;

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group justify="space-between">
          <Group gap="xs">
            <ThemeIcon color="lilac" size="md" variant="light">
              <IconActivityHeartbeat aria-hidden size={17} />
            </ThemeIcon>
            <Text fw={650}>In progress</Text>
          </Group>
          <Badge color="gray" variant="light">
            {active ?? "—"}
          </Badge>
        </Group>
        <SimpleGrid cols={3} spacing="xs">
          <ActivityCount label="Queued" value={activity?.queued} />
          <ActivityCount label="Starting" value={activity?.starting} />
          <ActivityCount label="Running" value={activity?.running} />
        </SimpleGrid>
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

function ActivityCount({ label, value }: Readonly<{ label: string; value?: number }>) {
  return (
    <Paper className="inbox-stat" p="sm">
      <Stack align="center" gap={2}>
        <Text fw={700} size="xl">
          {value ?? "—"}
        </Text>
        <Text c="dimmed" size="xs">
          {label}
        </Text>
      </Stack>
    </Paper>
  );
}

function itemPresentation(kind: InboxItemKind): {
  color: "orange" | "red" | "yellow";
  icon: TablerIcon;
  tone: "critical" | "warning";
} {
  switch (kind) {
    case "promotion-review":
      return { color: "yellow", icon: IconGitPullRequest, tone: "warning" };
    case "run-observation":
      return { color: "orange", icon: IconActivityHeartbeat, tone: "warning" };
    case "repository-refresh":
      return { color: "red", icon: IconGitBranch, tone: "critical" };
    case "cluster-unreachable":
      return { color: "red", icon: IconServerOff, tone: "critical" };
    case "promotion-error":
    case "dependency-unavailable":
      return { color: "red", icon: IconAlertTriangle, tone: "critical" };
  }
}

function readinessReport(value: unknown): ReadinessResponse | undefined {
  if (
    typeof value !== "object" ||
    value === null ||
    !("status" in value) ||
    !("database" in value) ||
    !("artifactStorage" in value)
  ) {
    return undefined;
  }

  return value as ReadinessResponse;
}

function humanList(items: string[]) {
  if (items.length === 0) return "";
  if (items.length === 1) return items[0];
  if (items.length === 2) return items.join(" and ");
  return `${items.slice(0, -1).join(", ")}, and ${items.at(-1)}`;
}
