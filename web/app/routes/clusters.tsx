import { Badge, Group, Paper, Stack, Table, Text, Title } from "@mantine/core";

import { createServerAPIClient } from "~/api/client.server";
import { getHealth, listClusters } from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/clusters";

export function meta() {
  return [{ title: pageTitle("Clusters") }];
}

export async function loader() {
  const client = createServerAPIClient();
  const [healthResult, clustersResult] = await Promise.all([
    getHealth({ client }),
    listClusters({ client }),
  ]);

  return {
    apiStatus: healthResult.data?.status ?? "unavailable",
    clusters: clustersResult.data?.items ?? [],
    clustersAvailable: clustersResult.data !== undefined,
  } as const;
}

export default function Clusters({ loaderData }: Route.ComponentProps) {
  const apiAvailable = loaderData.apiStatus === "ok";

  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Clusters</Title>
          <Text c="dimmed">Controller connectivity across execution destinations.</Text>
        </Stack>
        <Badge color={apiAvailable ? "green" : "red"} size="lg" variant="light">
          {apiAvailable ? "API connected" : "API unavailable"}
        </Badge>
      </Group>

      <Paper withBorder>
        {!loaderData.clustersAvailable ? (
          <Text c="dimmed" p="md">
            Cluster observations are unavailable while the API is disconnected.
          </Text>
        ) : loaderData.clusters.length === 0 ? (
          <Text c="dimmed" p="md">
            No controller has checked in yet.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={560}>
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Cluster</Table.Th>
                  <Table.Th>Connection</Table.Th>
                  <Table.Th>Last seen</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {loaderData.clusters.map((cluster) => (
                  <Table.Tr key={cluster.name}>
                    <Table.Td>{cluster.name}</Table.Td>
                    <Table.Td>
                      <Badge
                        color={cluster.connectionState === "connected" ? "green" : "red"}
                        variant="light"
                      >
                        {cluster.connectionState}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text c="dimmed" component="time" dateTime={cluster.lastSeenAt} size="sm">
                        {formatTimestamp(cluster.lastSeenAt)}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Paper>
    </Stack>
  );
}

function formatTimestamp(value: string) {
  return `${new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value))} UTC`;
}
