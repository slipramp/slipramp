import { Alert, Badge, Button, Paper, SimpleGrid, Stack, Table, Text } from "@mantine/core";
import { IconListDetails } from "@tabler/icons-react";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listClusters, listPromotionStages, listRuns, type PromotionStage } from "~/api/generated";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, SectionHeader } from "~/shared/page";
import { RecentRuns, recentRunLimit } from "~/features/runs/recent-runs";
import { formatDuration } from "~/shared/display";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/cluster";

const stageLimit = 10;

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.cluster?.name ?? "Cluster") }];
}

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
      stages: [] as PromotionStage[],
      stagesAvailable: false,
    };
  }

  const [runsResult, stagesResult] = await Promise.all([
    listRuns({ client, query: { cluster: cluster.name, limit: recentRunLimit } }),
    listPromotionStages({
      client,
      query: { archived: false, cluster: cluster.name, limit: stageLimit, offset: 0 },
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
  const activeRuns = loaderData.recentRuns.filter((run) => run.phase !== "finished").length;

  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label="Back to clusters" to="/clusters" />}
        badges={
          <Badge color={cluster.connectionState === "connected" ? "green" : "red"} variant="light">
            {cluster.connectionState}
          </Badge>
        }
        description="Controller-backed Kubernetes execution destination."
        title={cluster.name}
      />

      {cluster.connectionState === "unreachable" ? (
        <Alert color="red" title="Controller is unreachable">
          Slipramp has not heard from this cluster within the connection window. Active Run
          observations may be stale until the controller reconnects.
        </Alert>
      ) : null}

      <SimpleGrid cols={{ base: 1, sm: 3 }}>
        <Paper p="md" withBorder>
          <Stack gap={2}>
            <Text c="dimmed" size="xs" tt="uppercase">
              Last check-in
            </Text>
            <RelativeTime fw={700} size="lg" value={cluster.lastSeenAt} />
            <Timestamp c="dimmed" size="xs" value={cluster.lastSeenAt} />
          </Stack>
        </Paper>
        <Paper p="md" withBorder>
          <Stack gap={2}>
            <Text c="dimmed" size="xs" tt="uppercase">
              First observed
            </Text>
            <Timestamp fw={700} size="lg" value={cluster.firstSeenAt} />
            <Text c="dimmed" size="xs">
              {formatDuration(cluster.firstSeenAt, cluster.lastSeenAt)} observed
            </Text>
          </Stack>
        </Paper>
        <Paper p="md" withBorder>
          <Stack gap={2}>
            <Text c="dimmed" size="xs" tt="uppercase">
              Recent execution
            </Text>
            <Text fw={700} size="lg">
              {loaderData.recentRuns.length} runs
            </Text>
            <Text c="dimmed" size="xs">
              {activeRuns} active in the latest {recentRunLimit} cluster Runs
            </Text>
          </Stack>
        </Paper>
      </SimpleGrid>

      <RecentRuns
        available={loaderData.runsAvailable}
        description={`The latest ${recentRunLimit} executions assigned to this cluster.`}
        emptyMessage="No recent Runs were assigned to this cluster."
        runs={loaderData.recentRuns}
        viewAllTo={`/runs?cluster=${encodeURIComponent(cluster.name)}`}
      />
      <PromotionStages
        available={loaderData.stagesAvailable}
        clusterName={cluster.name}
        stages={loaderData.stages}
      />
    </Stack>
  );
}

function PromotionStages({
  available,
  clusterName,
  stages,
}: Readonly<{ available: boolean; clusterName: string; stages: PromotionStage[] }>) {
  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Button
            component={Link}
            leftSection={<IconListDetails size={16} />}
            size="compact-md"
            to={`/promotion-stages?cluster=${encodeURIComponent(clusterName)}`}
            variant="light"
          >
            View all stages
          </Button>
        }
        description={`The latest ${stageLimit} active Kargo Stages observed in this execution cluster.`}
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
                {stages.map((stage) => (
                  <Table.Tr key={stage.id}>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text fw={600} size="sm">
                          {stage.name}
                        </Text>
                        <Text c="dimmed" size="xs">
                          {stage.environment}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>{stage.application}</Table.Td>
                    <Table.Td>{stage.project}</Table.Td>
                    <Table.Td>
                      <Badge
                        color={stage.policy.effectiveMode === "automatic" ? "green" : "yellow"}
                        variant="light"
                      >
                        {stage.policy.effectiveMode}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <RelativeTime size="sm" value={stage.lastObservedAt} />
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
