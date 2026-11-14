import {
  Alert,
  Anchor,
  Badge,
  Button,
  Group,
  Paper,
  Stack,
  Table,
  Tabs,
  Text,
} from "@mantine/core";
import { IconListDetails, IconRoute } from "@tabler/icons-react";
import { Link, useSearchParams } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listClusters,
  listPromotionStages,
  listRuns,
  type Cluster,
  type KargoIntegrationHealth,
  type PromotionStageDetail,
  type Run,
} from "~/api/generated";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import { RecentRuns, recentRunLimit } from "~/features/runs/recent-runs";
import { pageTitle } from "~/shared/page-title";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { KargoObservationTime } from "~/features/promotions/kargo-observation-time";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { promotionStagePolicyColor } from "~/features/promotions/promotion-policy";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import type { Route } from "./+types/cluster";

const stageLimit = 10;

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.cluster?.name ?? "Cluster") }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const clustersResult = await listClusters({ client, query: { limit: 200 } });
  const cluster = clustersResult.data?.items.find((item) => item.name === params.clusterName);

  if (!cluster) {
    return {
      cluster,
      clustersAvailable: clustersResult.data !== undefined,
      recentRuns: [],
      runsAvailable: false,
      stages: [] as PromotionStageDetail[],
      stagesAvailable: false,
    };
  }

  const [runsResult, stagesResult] = await Promise.all([
    listRuns({ client, query: { cluster: cluster.name, limit: recentRunLimit } }),
    listPromotionStages({
      client,
      query: { cluster: cluster.name, limit: stageLimit, offset: 0 },
    }),
  ]);

  return {
    cluster,
    clustersAvailable: true,
    recentRuns: runsResult.data?.items ?? [],
    runsAvailable: runsResult.data !== undefined,
    stages: stagesResult.data?.items ?? [],
    stagesAvailable: stagesResult.data !== undefined,
  };
}

export default function ClusterDetail({ loaderData }: Route.ComponentProps) {
  useActivityRefresh((loaderData.cluster?.kargoIntegrations.length ?? 0) > 0, 30_000);
  const [searchParams, setSearchParams] = useSearchParams();

  if (!loaderData.cluster) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to clusters" to="/clusters" />
        <Alert
          color="red"
          title={loaderData.clustersAvailable ? "Cluster not found" : "Clusters unavailable"}
        >
          {loaderData.clustersAvailable
            ? "No observed controller matches this cluster name."
            : "The cluster inventory could not be loaded."}
        </Alert>
      </Stack>
    );
  }

  const cluster = loaderData.cluster;
  const activeView =
    searchParams.get("view") === "kargo" && cluster.kargoIntegrations.length > 0
      ? "kargo"
      : "overview";

  function changeView(view: string | null) {
    if (!view) return;

    const next = new URLSearchParams(searchParams);
    if (view === "overview") next.delete("view");
    else next.set("view", view);
    setSearchParams(next, { preventScrollReset: true });
  }

  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label="Back to clusters" to="/clusters" />}
        badges={
          <Badge color={cluster.connectionState === "connected" ? "green" : "red"} variant="light">
            {cluster.connectionState}
          </Badge>
        }
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Registered">
              <RelativeTime value={cluster.firstSeenAt} />
            </PageHeaderFact>
            <PageHeaderFact label="Last observed">
              <RelativeTime value={cluster.lastSeenAt} />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={cluster.name}
      />

      {cluster.connectionState === "unreachable" ? (
        <Alert color="red" title="Controller is unreachable">
          Slipramp has not heard from this cluster within the connection window. Active Run
          observations may be stale until the controller reconnects.
        </Alert>
      ) : null}

      <Tabs keepMounted={false} onChange={changeView} value={activeView} variant="outline">
        <Tabs.List aria-label="Cluster views" style={{ flexWrap: "nowrap", overflowX: "auto" }}>
          <Tabs.Tab
            leftSection={<IconListDetails aria-hidden size={16} />}
            style={{ flexShrink: 0 }}
            value="overview"
          >
            Overview
          </Tabs.Tab>
          {cluster.kargoIntegrations.length > 0 ? (
            <Tabs.Tab
              leftSection={<IconRoute aria-hidden size={16} />}
              style={{ flexShrink: 0 }}
              value="kargo"
            >
              Kargo
            </Tabs.Tab>
          ) : null}
        </Tabs.List>
        <Tabs.Panel pt="lg" value="overview">
          <ClusterOverview
            cluster={cluster}
            recentRuns={loaderData.recentRuns}
            runsAvailable={loaderData.runsAvailable}
          />
        </Tabs.Panel>
        {cluster.kargoIntegrations.length > 0 ? (
          <Tabs.Panel pt="lg" value="kargo">
            <Stack gap="lg">
              <KargoIntegrationHealthPanel integrations={cluster.kargoIntegrations} />
              <PromotionStages
                available={loaderData.stagesAvailable}
                clusterName={cluster.name}
                stages={loaderData.stages}
              />
            </Stack>
          </Tabs.Panel>
        ) : null}
      </Tabs>
    </Stack>
  );
}

function ClusterOverview({
  cluster,
  recentRuns,
  runsAvailable,
}: Readonly<{
  cluster: Cluster;
  recentRuns: Run[];
  runsAvailable: boolean;
}>) {
  return (
    <RecentRuns
      available={runsAvailable}
      description={`The latest ${recentRunLimit} executions assigned to this cluster.`}
      emptyMessage="No recent Runs were assigned to this cluster."
      runs={recentRuns}
      viewAllTo={`/runs?cluster=${encodeURIComponent(cluster.name)}`}
    />
  );
}

function KargoIntegrationHealthPanel({
  integrations,
}: Readonly<{ integrations: KargoIntegrationHealth[] }>) {
  const degraded = integrations.filter((integration) => integration.observationState !== "current");

  return (
    <Stack gap="md">
      <SectionHeader
        description="Controller observation health for each Kargo integration configured on this cluster."
        title="Kargo integrations"
      />
      {degraded.length > 0 ? (
        <Alert color="red" title="Kargo inventory is not current">
          {degraded.length === 1
            ? `${degraded[0]!.name} is configured but is not reporting a current complete Stage snapshot.`
            : `${degraded.length} Kargo integrations are configured but are not reporting current complete Stage snapshots.`}
        </Alert>
      ) : null}
      <Paper withBorder>
        <Table.ScrollContainer minWidth={620} type="native">
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Integration</Table.Th>
                <Table.Th>Observation</Table.Th>
                <Table.Th>Last complete snapshot</Table.Th>
                <Table.Th>Freshness window</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {integrations.map((integration) => (
                <Table.Tr key={integration.name}>
                  <Table.Td>
                    {integration.kargoUrl ? (
                      <Anchor href={integration.kargoUrl} rel="noreferrer" target="_blank">
                        {integration.name}
                      </Anchor>
                    ) : (
                      <Text fw={600} size="sm">
                        {integration.name}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Badge
                      color={integrationHealthColor(integration.observationState)}
                      variant="light"
                    >
                      {integrationHealthLabel(integration.observationState)}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    {integration.lastObservedAt ? (
                      <Group gap="xs" wrap="nowrap">
                        <RelativeTime size="sm" value={integration.lastObservedAt} />
                        <Timestamp c="dimmed" size="xs" value={integration.lastObservedAt} />
                      </Group>
                    ) : (
                      <Text c="dimmed" size="sm">
                        Never reported
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    {formatObservationWindow(integration.maximumObservationAgeSeconds)}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>
    </Stack>
  );
}

function integrationHealthColor(state: KargoIntegrationHealth["observationState"]) {
  return state === "current" ? "green" : "red";
}

function integrationHealthLabel(state: KargoIntegrationHealth["observationState"]) {
  switch (state) {
    case "current":
      return "Current";
    case "not-observed":
      return "Never reported";
    case "stale":
      return "Not reporting";
  }
}

function formatObservationWindow(seconds: number) {
  if (seconds % 3600 === 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}

function PromotionStages({
  available,
  clusterName,
  stages,
}: Readonly<{ available: boolean; clusterName: string; stages: PromotionStageDetail[] }>) {
  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Button
            component={Link}
            leftSection={<IconListDetails size={16} />}
            size="compact-md"
            to={`/promotion-projects?cluster=${encodeURIComponent(clusterName)}`}
            variant="light"
          >
            View Kargo projects
          </Button>
        }
        description={`Up to ${stageLimit} active Kargo Stages in this execution cluster, sorted by name.`}
        title="Promotion stages"
      />
      {!available ? (
        <Alert color="yellow" title="Promotion inventory unavailable">
          Promotion Stages could not be loaded for this cluster.
        </Alert>
      ) : stages.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No active Promotion Stages are associated with this cluster.</Text>
        </Paper>
      ) : (
        <Paper withBorder>
          <Table.ScrollContainer minWidth={760} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Stage</Table.Th>
                  <Table.Th>Application</Table.Th>
                  <Table.Th>Project</Table.Th>
                  <Table.Th>Policy</Table.Th>
                  <Table.Th>Last observed</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {stages.map((item) => (
                  <Table.Tr key={item.stage.id}>
                    <Table.Td>
                      <Stack gap={0}>
                        <PromotionStageLink stage={item.stage} />
                        <Text c="dimmed" size="xs">
                          {item.stage.enrichment?.environment}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>{item.stage.enrichment?.application}</Table.Td>
                    <Table.Td>{item.project}</Table.Td>
                    <Table.Td>
                      <Badge
                        color={promotionStagePolicyColor(item.stage.policy.effectiveMode)}
                        variant="light"
                      >
                        {item.stage.policy.effectiveMode}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <KargoObservationTime value={item.stage.lastObservedAt} />
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      )}
    </Stack>
  );
}
