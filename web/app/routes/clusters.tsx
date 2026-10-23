import {
  Anchor,
  Badge,
  Group,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getReadiness,
  listClusters,
  type DependencyStatus,
  type ReadinessResponse,
} from "~/api/generated";
import { Datestamp, RelativeTime } from "~/components/display";
import { ListPagination, SearchParamFilters } from "~/components/list-controls";
import { EmptyState, PageHeader } from "~/components/page";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/clusters";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Clusters") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const query = url.searchParams.get("q")?.trim() ?? "";
  const connection = url.searchParams.get("connection");
  const normalizedConnection =
    connection === "connected" || connection === "unreachable" ? connection : undefined;
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request);
  const [readinessResult, clustersResult] = await Promise.all([
    getReadiness({ client }),
    listClusters({
      client,
      query: {
        connection: normalizedConnection,
        limit: pageSize,
        offset: (page - 1) * pageSize,
        q: query || undefined,
      },
    }),
  ]);

  if (clustersResult.data) {
    const lastPage = Math.max(1, Math.ceil(clustersResult.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    clusters: clustersResult.data?.items ?? [],
    clustersAvailable: clustersResult.data !== undefined,
    connection: normalizedConnection,
    page,
    query,
    readiness: readinessReport(readinessResult.data ?? readinessResult.error),
    total: clustersResult.data?.total ?? 0,
  } as const;
}

export default function Clusters({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="lg">
      <PageHeader
        description="Server dependencies and Controller connectivity across execution destinations."
        title="Clusters"
      />

      <SimpleGrid cols={{ base: 1, sm: 2 }}>
        <DependencyCard label="PostgreSQL" status={loaderData.readiness?.database} />
        <DependencyCard label="Artifact storage" status={loaderData.readiness?.artifactStorage} />
      </SimpleGrid>

      <SearchParamFilters
        active={Boolean(loaderData.query || loaderData.connection)}
        clearTo="/clusters"
      >
        <TextInput
          defaultValue={loaderData.query}
          key={`query:${loaderData.query}`}
          label="Search clusters"
          name="q"
          placeholder="Cluster name"
          style={{ flex: "1 1 300px" }}
          type="search"
        />
        <NativeSelect
          data={[
            { label: "Any connection state", value: "" },
            { label: "Connected", value: "connected" },
            { label: "Unreachable", value: "unreachable" },
          ]}
          defaultValue={loaderData.connection ?? ""}
          key={`connection:${loaderData.connection ?? ""}`}
          label="Connection"
          name="connection"
          style={{ flex: "0 1 240px" }}
        />
      </SearchParamFilters>

      {!loaderData.clustersAvailable ? (
        <EmptyState description="Cluster observations are unavailable while the API is disconnected." />
      ) : loaderData.clusters.length === 0 ? (
        <EmptyState
          description={
            loaderData.query || loaderData.connection
              ? "No clusters match these filters."
              : "No controller has checked in yet."
          }
        />
      ) : (
        <Paper withBorder>
          <Table.ScrollContainer minWidth={560} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Cluster</Table.Th>
                  <Table.Th>Connection</Table.Th>
                  <Table.Th>Last check-in</Table.Th>
                  <Table.Th>Registered</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {loaderData.clusters.map((cluster) => (
                  <Table.Tr key={cluster.name}>
                    <Table.Td>
                      <Anchor
                        component={Link}
                        fw={600}
                        to={`/clusters/${encodeURIComponent(cluster.name)}`}
                      >
                        {cluster.name}
                      </Anchor>
                    </Table.Td>
                    <Table.Td>
                      <Badge
                        color={cluster.connectionState === "connected" ? "green" : "red"}
                        variant="light"
                      >
                        {cluster.connectionState}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <RelativeTime size="sm" value={cluster.lastSeenAt} />
                    </Table.Td>
                    <Table.Td>
                      <Datestamp c="dimmed" size="sm" value={cluster.firstSeenAt} />
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      )}
      {loaderData.clustersAvailable && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.clusters.length}
          noun="Clusters"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function DependencyCard({ label, status }: Readonly<{ label: string; status?: DependencyStatus }>) {
  const available = status === "available";

  return (
    <Paper p="md" withBorder>
      <Group justify="space-between">
        <Stack gap={2}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Server dependency
          </Text>
          <Text fw={650}>{label}</Text>
        </Stack>
        <Badge color={status === undefined ? "gray" : available ? "green" : "red"} variant="light">
          {status ?? "unknown"}
        </Badge>
      </Group>
    </Paper>
  );
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
