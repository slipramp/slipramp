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
import { notifications } from "@mantine/notifications";
import {
  IconActivityHeartbeat,
  IconAlertTriangle,
  IconArrowRight,
  IconCheck,
  IconCircleCheck,
  IconGitBranch,
  IconGitPullRequest,
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
  type Inbox as InboxProjection,
  type InboxItem,
  type ReadinessResponse,
} from "~/api/generated";
import { RelativeTime } from "~/components/display";
import { EmptyState, PageHeader, SectionHeader } from "~/components/page";
import { problemDetail } from "~/form-data";
import { acknowledgementProblemIDs, inboxDestinationHref } from "~/inbox";
import { pageTitle } from "~/page-title";
import { asReadinessResponse } from "~/readiness";

import type { Route } from "./+types/index";

type AcknowledgementActionResult = { error?: string };

export function meta() {
  return [{ title: pageTitle("Inbox") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [inboxResult, readinessResult] = await Promise.all([
    getInbox({ client }),
    getReadiness({ client }),
  ]);

  return {
    inbox: inboxResult.data,
    inboxError: inboxResult.error
      ? problemDetail(inboxResult.error, "The operational Inbox could not be loaded.")
      : undefined,
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
  const inbox = loaderData.inbox;
  const attentionCount = inbox ? inbox.decisions.length + inbox.problems.length : undefined;

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          attentionCount === undefined ? (
            <Badge
              color="red"
              leftSection={<IconAlertTriangle aria-hidden size={15} />}
              size="lg"
              variant="light"
            >
              Unavailable
            </Badge>
          ) : (
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
          )
        }
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
          <RunActivity activity={inbox?.activity} />
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

function itemPresentation(kind: InboxItem["kind"]): {
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
