import { Alert, Anchor, Badge, Group, Paper, Stack, Table, Text, Tooltip } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listClusters,
  listRepositories,
  listRuns,
  type Cluster,
  type Repository,
  type Run,
  type RunSourceType,
} from "~/api/generated";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { CommitReference } from "~/features/commits/git-commit";
import { Identity } from "~/shared/identity";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { ListPagination, SearchParamFilters } from "~/shared/list-controls";
import { PageHeader } from "~/shared/page";
import { formatDuration, formatTimestamp, shortIdentifier } from "~/shared/display";
import { runActivity } from "~/features/mascot/execution-activity";
import { pageTitle } from "~/shared/page-title";
import { runStatusColor, runStatusLabel } from "~/features/runs/run-status";
import { runSourceLabel } from "~/features/runs/run-source";
import { pageFromSearchParam } from "~/shared/search-params";
import { targetFilterPath } from "~/features/targets/target-filter";
import { useTimeZone } from "~/shared/time-zone-preference";

import type { Route } from "./+types/runs";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Run history") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const repositoryId = optionalSearchValue(url, "repositoryId");
  const targetId = optionalSearchValue(url, "targetId");
  const action = optionalSearchValue(url, "action");
  const source = optionalRunSource(url.searchParams.get("source"));
  const triggerName = optionalSearchValue(url, "triggerName");
  const cluster = optionalSearchValue(url, "cluster");
  const phase = optionalRunPhase(url.searchParams.get("phase"));
  const reviewState = optionalRunReviewState(url.searchParams.get("reviewState"));
  const requestedPage = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const offset = (requestedPage - 1) * pageSize;
  const client = createServerAPIClient(request, context);
  const [initialRunsResult, repositoriesResult, clustersResult] = await Promise.all([
    listRuns({
      client,
      query: {
        action,
        cluster,
        limit: pageSize,
        offset,
        phase,
        reviewState,
        repositoryId,
        source,
        targetId,
        triggerName,
      },
    }),
    listRepositories({ client, query: { limit: 200 } }),
    listClusters({ client, query: { limit: 200 } }),
  ]);

  if (!initialRunsResult.data) {
    return {
      available: false,
      action,
      cluster,
      clusters: clustersResult.data?.items ?? [],
      error:
        initialRunsResult.error?.detail ??
        initialRunsResult.error?.title ??
        "Run history could not be loaded.",
      page: 1,
      phase,
      repositoryId,
      reviewState,
      repositories: repositoriesResult.data?.items ?? [],
      runs: [],
      source,
      targetId,
      total: 0,
      triggerName,
    };
  }

  const pageCount = Math.ceil(initialRunsResult.data.total / pageSize);
  const lastPage = Math.max(1, pageCount);
  if (requestedPage > lastPage) {
    if (lastPage === 1) {
      url.searchParams.delete("page");
    } else {
      url.searchParams.set("page", String(lastPage));
    }
    return redirect(`${url.pathname}${url.search}`);
  }

  return {
    available: true,
    action,
    cluster,
    clusters: clustersResult.data?.items ?? [],
    error: undefined,
    page: requestedPage,
    phase,
    repositoryId,
    reviewState,
    repositories: repositoriesResult.data?.items ?? [],
    runs: initialRunsResult.data.items,
    source,
    targetId,
    total: initialRunsResult.data.total,
    triggerName,
  };
}

export default function RunHistory({ loaderData }: Route.ComponentProps) {
  useActivityRefresh(loaderData.runs.some((run) => run.phase !== "finished"));
  const repositoryNames = new Map(
    loaderData.repositories.map((repository) => [repository.id, repository.name]),
  );
  return (
    <Stack gap="lg">
      <PageHeader description="Immutable action executions, newest first." title="Run history" />

      <HistoryFilters
        action={loaderData.action}
        cluster={loaderData.cluster}
        clusters={loaderData.clusters}
        phase={loaderData.phase}
        repositoryId={loaderData.repositoryId}
        repositories={loaderData.repositories}
        reviewState={loaderData.reviewState}
        source={loaderData.source}
        targetId={loaderData.targetId}
        triggerName={loaderData.triggerName}
      />

      {!loaderData.available ? (
        <Alert color="red" title="Run history unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : (
        <>
          <HistoryTable repositoryNames={repositoryNames} runs={loaderData.runs} />
          {loaderData.total > 0 ? (
            <ListPagination
              itemCount={loaderData.runs.length}
              noun="Runs"
              page={loaderData.page}
              pageSize={pageSize}
              total={loaderData.total}
            />
          ) : null}
        </>
      )}
    </Stack>
  );
}

function HistoryFilters({
  action,
  cluster,
  clusters,
  phase,
  repositoryId,
  repositories,
  reviewState,
  source,
  targetId,
  triggerName,
}: Readonly<{
  action?: string;
  cluster?: string;
  clusters: Cluster[];
  phase?: Run["phase"];
  repositoryId?: string;
  repositories: Repository[];
  reviewState?: Run["review"]["state"];
  source?: RunSourceType;
  targetId?: string;
  triggerName?: string;
}>) {
  return (
    <SearchParamFilters
      clearTo="/runs"
      filters={[
        {
          active: Boolean(repositoryId),
          data: [
            { label: "All repositories", value: "" },
            ...repositories.map((repository) => ({
              label: repository.name,
              value: repository.id,
            })),
          ],
          kind: "select",
          label: "Repository",
          name: "repositoryId",
          value: repositoryId,
        },
        {
          active: Boolean(targetId),
          kind: "text",
          label: "Target ID",
          name: "targetId",
          placeholder: "Target UUID",
          value: targetId,
        },
        {
          active: Boolean(action),
          kind: "text",
          label: "Action",
          maxLength: 63,
          name: "action",
          placeholder: "Action name",
          value: action,
        },
        {
          active: Boolean(source),
          clearWhenEmpty: ["triggerName"],
          data: triggerName
            ? [
                { label: "Git", value: "git" },
                { label: "Schedule", value: "schedule" },
              ]
            : [
                { label: "All sources", value: "" },
                { label: "Manual", value: "manual" },
                { label: "API", value: "api" },
                { label: "MCP", value: "mcp" },
                { label: "Git", value: "git" },
                { label: "Schedule", value: "schedule" },
                { label: "Promotion", value: "promotion" },
              ],
          kind: "select",
          label: "Source",
          name: "source",
          value: source,
        },
        {
          active: Boolean(triggerName),
          disabled: source !== "git" && source !== "schedule",
          kind: "text",
          label: "Trigger",
          maxLength: 63,
          name: "triggerName",
          placeholder: "Trigger name",
          value: triggerName,
        },
        {
          active: Boolean(cluster),
          data: [
            { label: "All clusters", value: "" },
            ...clusters.map((item) => ({ label: item.name, value: item.name })),
          ],
          kind: "select",
          label: "Cluster",
          name: "cluster",
          value: cluster,
        },
        {
          active: Boolean(phase),
          data: [
            { label: "Any phase", value: "" },
            { label: "Queued", value: "queued" },
            { label: "Assigned", value: "assigned" },
            { label: "Preparing", value: "preparing" },
            { label: "Running", value: "running" },
            { label: "Finished", value: "finished" },
          ],
          kind: "select",
          label: "Phase",
          name: "phase",
          value: phase,
        },
        {
          active: Boolean(reviewState),
          data: [
            { label: "Any review state", value: "" },
            { label: "Pending review", value: "pending" },
            { label: "Approved", value: "approved" },
            { label: "Rejected", value: "rejected" },
            { label: "Not required", value: "not-required" },
          ],
          kind: "select",
          label: "Review",
          name: "reviewState",
          value: reviewState,
        },
      ]}
    />
  );
}

function HistoryTable({
  repositoryNames,
  runs,
}: Readonly<{ repositoryNames: Map<string, string>; runs: Run[] }>) {
  const { timeZone } = useTimeZone();

  if (runs.length === 0) {
    return (
      <Paper p="md" withBorder>
        <Text c="dimmed">No Runs match these filters.</Text>
      </Paper>
    );
  }

  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={1080} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Run / workload</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Execution</Table.Th>
              <Table.Th>Revision</Table.Th>
              <Table.Th>Source / author</Table.Th>
              <Table.Th>Created</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {runs.map((run) => (
              <Table.Tr key={run.id}>
                <Table.Td>
                  <Anchor component={Link} fw={600} to={`/runs/${run.id}`}>
                    {run.target} · {run.action}
                  </Anchor>
                  <Group gap="xs">
                    <Anchor c="dimmed" component={Link} size="xs" to={`/targets/${run.targetId}`}>
                      {repositoryNames.get(run.repositoryId) ?? shortIdentifier(run.repositoryId)}
                    </Anchor>
                    <Text c="dimmed" size="xs">
                      Run {shortIdentifier(run.id)}
                    </Text>
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs" justify="space-between" wrap="nowrap">
                    <Group gap="xs" wrap="nowrap">
                      <Badge color={runStatusColor(run)} variant="light">
                        {runStatusLabel(run)}
                      </Badge>
                      {run.observationState === "unknown" ? (
                        <Badge color="yellow" variant="outline">
                          Unknown
                        </Badge>
                      ) : null}
                    </Group>
                    <KawaiiActivity activity={runActivity(run)} animationKey={run.id} compact />
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <Anchor
                      component={Link}
                      size="sm"
                      to={`/clusters/${encodeURIComponent(run.cluster)}`}
                    >
                      {run.cluster}
                    </Anchor>
                    <Anchor
                      c="dimmed"
                      component={Link}
                      size="xs"
                      to={targetFilterPath("placement", run.placement)}
                    >
                      {run.placement}
                    </Anchor>
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <CommitReference commit={run.commit} repositoryId={run.repositoryId} />
                    <Text c="dimmed" size="xs">
                      Catalog {shortIdentifier(run.catalogRevisionId)}
                    </Text>
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    {run.source.type === "schedule" && run.source.schedule ? (
                      <Tooltip
                        label={`Trigger: ${run.source.schedule.triggerName} · Instance: ${formatTimestamp(run.source.schedule.scheduledAt, timeZone)}`}
                        openDelay={350}
                        withArrow
                      >
                        <Text
                          aria-label={`Schedule. Trigger ${run.source.schedule.triggerName}. Instance ${formatTimestamp(run.source.schedule.scheduledAt, timeZone)}.`}
                          component="span"
                          display="inline-block"
                          size="sm"
                          style={{ alignSelf: "flex-start" }}
                          tabIndex={0}
                        >
                          Schedule
                        </Text>
                      </Tooltip>
                    ) : run.source.type === "promotion" && run.source.promotion ? (
                      <Anchor
                        component={Link}
                        size="sm"
                        to={`/promotions/${run.source.promotion.promotionId}`}
                      >
                        {runSourceLabel(run.source)}
                      </Anchor>
                    ) : (
                      <Text size="sm">{runSourceLabel(run.source)}</Text>
                    )}
                    <Identity
                      avatarSize={22}
                      name={
                        run.createdBy?.displayName ??
                        (run.source.type === "git" || run.source.type === "schedule"
                          ? "Automation"
                          : "System")
                      }
                      nameProps={{ c: "dimmed", fw: 500, size: "xs" }}
                    />
                  </Stack>
                </Table.Td>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  <Stack gap={0}>
                    <RelativeTime size="sm" value={run.createdAt} />
                    {run.startedAt && run.finishedAt ? (
                      <Text c="dimmed" size="xs">
                        {formatDuration(run.startedAt, run.finishedAt)} runtime
                      </Text>
                    ) : run.startedAt ? (
                      <Timestamp c="dimmed" size="xs" value={run.startedAt} />
                    ) : (
                      <Text c="dimmed" size="xs">
                        Not started
                      </Text>
                    )}
                  </Stack>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function optionalSearchValue(url: URL, name: string) {
  return url.searchParams.get(name)?.trim() || undefined;
}

function optionalRunPhase(value: string | null): Run["phase"] | undefined {
  switch (value) {
    case "queued":
    case "assigned":
    case "preparing":
    case "running":
    case "finished":
      return value;
    default:
      return undefined;
  }
}

function optionalRunReviewState(value: string | null): Run["review"]["state"] | undefined {
  switch (value) {
    case "not-required":
    case "pending":
    case "approved":
    case "rejected":
      return value;
    default:
      return undefined;
  }
}

function optionalRunSource(value: string | null): RunSourceType | undefined {
  switch (value) {
    case "manual":
    case "api":
    case "mcp":
    case "git":
    case "schedule":
    case "promotion":
      return value;
    default:
      return undefined;
  }
}
