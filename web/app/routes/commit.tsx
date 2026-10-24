import { Alert, Anchor, Badge, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getCommit, listPromotions, listRuns, type Promotion, type Run } from "~/api/generated";
import { useKawaiiActivityRefresh } from "~/components/activity-refresh";
import { RelativeTime } from "~/components/display";
import { CommitDetails } from "~/components/git-commit";
import { Identity } from "~/components/identity";
import { KawaiiActivity } from "~/components/kawaii-activity";
import { ListPagination } from "~/components/list-controls";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, SectionHeader } from "~/components/page";
import { abbreviateMiddle, shortIdentifier } from "~/display";
import { promotionActivity, runActivity } from "~/execution-activity";
import { pageTitle } from "~/page-title";
import { runSourceLabel } from "~/run-source";
import { runStatusColor } from "~/run-status";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/commit";

const pageSize = 10;

export function meta({ loaderData }: Route.MetaArgs) {
  const title = loaderData?.commit
    ? `Commit ${shortIdentifier(loaderData.commit.commit.sha, 12)}`
    : "Commit";
  return [{ title: pageTitle(title) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const runsPage = pageFromSearchParam(url.searchParams.get("runsPage"), pageSize);
  const promotionsPage = pageFromSearchParam(url.searchParams.get("promotionsPage"), pageSize);
  const client = createServerAPIClient(request, context);
  const [commitResult, runsResult, promotionsResult] = await Promise.all([
    getCommit({
      client,
      path: { commitSha: params.commitSha, repositoryId: params.repositoryId },
    }),
    listRuns({
      client,
      query: {
        commitSha: params.commitSha,
        limit: pageSize,
        offset: (runsPage - 1) * pageSize,
        repositoryId: params.repositoryId,
      },
    }),
    listPromotions({
      client,
      query: {
        commitSha: params.commitSha,
        limit: pageSize,
        offset: (promotionsPage - 1) * pageSize,
        repositoryId: params.repositoryId,
      },
    }),
  ]);

  if (commitResult.data) {
    const lastRunsPage = Math.max(1, Math.ceil((runsResult.data?.total ?? 0) / pageSize));
    const lastPromotionsPage = Math.max(
      1,
      Math.ceil((promotionsResult.data?.total ?? 0) / pageSize),
    );
    let redirectNeeded = false;
    if (runsPage > lastRunsPage) {
      setPage(url, "runsPage", lastRunsPage);
      redirectNeeded = true;
    }
    if (promotionsPage > lastPromotionsPage) {
      setPage(url, "promotionsPage", lastPromotionsPage);
      redirectNeeded = true;
    }
    if (redirectNeeded) return redirect(`${url.pathname}${url.search}`);
  }

  return {
    commit: commitResult.data,
    commitError: commitResult.error?.detail ?? commitResult.error?.title,
    promotions: promotionsResult.data?.items ?? [],
    promotionsAvailable: promotionsResult.data !== undefined,
    promotionsPage,
    promotionsTotal: promotionsResult.data?.total ?? 0,
    runs: runsResult.data?.items ?? [],
    runsAvailable: runsResult.data !== undefined,
    runsPage,
    runsTotal: runsResult.data?.total ?? 0,
  };
}

export default function Commit({ loaderData }: Route.ComponentProps) {
  useKawaiiActivityRefresh(
    loaderData.runs.some((run) => run.phase !== "finished") ||
      loaderData.promotions.some((promotion) => promotionActivity(promotion) !== undefined),
  );

  if (!loaderData.commit) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to commits" to="/commits" />
        <Alert color="red" title="Commit unavailable">
          {loaderData.commitError ?? "This commit could not be loaded."}
        </Alert>
      </Stack>
    );
  }

  const { commit } = loaderData;

  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label="Back to commits" to="/commits" />}
        description={
          <Anchor component={Link} to={`/repositories/${commit.repositoryId}`}>
            {commit.repositoryName}
          </Anchor>
        }
        title={`Commit ${shortIdentifier(commit.commit.sha, 12)}`}
      />

      <CommitDetails commit={commit.commit} />

      <Stack gap="md">
        <SectionHeader
          description="Action executions pinned to this exact revision."
          title="Runs"
        />
        {!loaderData.runsAvailable ? (
          <Alert color="red" title="Runs unavailable">
            Associated Runs could not be loaded.
          </Alert>
        ) : (
          <RunAssociations runs={loaderData.runs} />
        )}
        {loaderData.runsAvailable && loaderData.runsTotal > 0 ? (
          <ListPagination
            itemCount={loaderData.runs.length}
            noun="Runs"
            page={loaderData.runsPage}
            pageParam="runsPage"
            pageSize={pageSize}
            total={loaderData.runsTotal}
          />
        ) : null}
      </Stack>

      <Stack gap="md">
        <SectionHeader
          description="Promotion reviews that use this revision as their base or candidate."
          title="Promotions"
        />
        {!loaderData.promotionsAvailable ? (
          <Alert color="red" title="Promotions unavailable">
            Associated Promotions could not be loaded.
          </Alert>
        ) : (
          <PromotionAssociations commitSha={commit.commit.sha} promotions={loaderData.promotions} />
        )}
        {loaderData.promotionsAvailable && loaderData.promotionsTotal > 0 ? (
          <ListPagination
            itemCount={loaderData.promotions.length}
            noun="Promotions"
            page={loaderData.promotionsPage}
            pageParam="promotionsPage"
            pageSize={pageSize}
            total={loaderData.promotionsTotal}
          />
        ) : null}
      </Stack>
    </Stack>
  );
}

function RunAssociations({ runs }: Readonly<{ runs: Run[] }>) {
  if (runs.length === 0) {
    return (
      <Paper p="md" withBorder>
        <Text c="dimmed">No Runs are associated with this commit.</Text>
      </Paper>
    );
  }

  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={720} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Run</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Source / actor</Table.Th>
              <Table.Th>Created</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {runs.map((run) => (
              <Table.Tr key={run.id}>
                <Table.Td>
                  <Stack gap={0}>
                    <Anchor component={Link} fw={600} to={`/runs/${run.id}`}>
                      {run.target} · {run.action}
                    </Anchor>
                    <Text c="dimmed" size="xs">
                      Run {shortIdentifier(run.id)} · {run.cluster}
                    </Text>
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs" wrap="nowrap">
                    <KawaiiActivity activity={runActivity(run)} compact />
                    <Badge color={runStatusColor(run)} variant="light">
                      {run.outcome ?? run.phase}
                    </Badge>
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <Text size="sm">{runSourceLabel(run.source)}</Text>
                    <Identity
                      avatarSize={22}
                      name={run.createdBy?.displayName ?? "System"}
                      nameProps={{ c: "dimmed", fw: 500, size: "xs" }}
                    />
                  </Stack>
                </Table.Td>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  <RelativeTime size="sm" value={run.createdAt} />
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function PromotionAssociations({
  commitSha,
  promotions,
}: Readonly<{ commitSha: string; promotions: Promotion[] }>) {
  if (promotions.length === 0) {
    return (
      <Paper p="md" withBorder>
        <Text c="dimmed">No Promotions are associated with this commit.</Text>
      </Paper>
    );
  }

  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={720} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Promotion / Stage</Table.Th>
              <Table.Th>Role</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Requested</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {promotions.map((promotion) => (
              <Table.Tr key={promotion.id}>
                <Table.Td>
                  <Stack gap={0}>
                    <Anchor component={Link} fw={600} to={`/promotions/${promotion.id}`}>
                      {abbreviateMiddle(promotion.externalPromotionId, 30)}
                    </Anchor>
                    <Text c="dimmed" size="xs">
                      {snapshotString(promotion.stageSnapshot, "name") ??
                        shortIdentifier(promotion.stageId)}
                    </Text>
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Badge variant="outline">{promotionCommitRole(promotion, commitSha)}</Badge>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs" wrap="nowrap">
                    <KawaiiActivity activity={promotionActivity(promotion)} compact />
                    <Badge color={promotionStatusColor(promotion.status)} variant="light">
                      {promotionStatusLabel(promotion.status)}
                    </Badge>
                  </Group>
                </Table.Td>
                <Table.Td style={{ whiteSpace: "nowrap" }}>
                  <Stack gap={0}>
                    <Identity
                      avatarSize={22}
                      name={promotion.requestedBy.displayName}
                      nameProps={{ fw: 500, size: "sm" }}
                    />
                    <RelativeTime c="dimmed" size="xs" value={promotion.createdAt} />
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

function promotionCommitRole(promotion: Promotion, commitSha: string) {
  const base = promotion.baseCommit?.sha === commitSha;
  const candidate = promotion.candidateCommit?.sha === commitSha;
  if (base && candidate) return "Base and candidate";
  return candidate ? "Candidate" : "Base";
}

function promotionStatusColor(status: Promotion["status"]) {
  switch (status) {
    case "approved":
      return "green";
    case "creating":
      return "cyan";
    case "error":
    case "rejected":
      return "red";
    case "pending":
      return "yellow";
    case "preparing":
      return "blue";
    case "expired":
      return "gray";
  }
}

function promotionStatusLabel(status: Promotion["status"]) {
  switch (status) {
    case "approved":
      return "Approved";
    case "creating":
      return "Creating in Kargo";
    case "error":
      return "Failed";
    case "expired":
      return "Expired";
    case "pending":
      return "Pending review";
    case "preparing":
      return "Preparing diffs";
    case "rejected":
      return "Rejected";
  }
}

function snapshotString(snapshot: Record<string, unknown>, key: string) {
  const value = snapshot[key];
  return typeof value === "string" ? value : undefined;
}

function setPage(url: URL, name: string, page: number) {
  if (page === 1) url.searchParams.delete(name);
  else url.searchParams.set(name, String(page));
}
