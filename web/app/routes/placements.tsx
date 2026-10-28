import { Alert, Anchor, Badge, Paper, Stack, Table, Text } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPlacements, type Placement } from "~/api/generated";
import { RelativeTime } from "~/components/display";
import { ListPagination } from "~/components/list-controls";
import { EmptyState, PageHeader } from "~/components/page";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/placements";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Placements") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const result = await listPlacements({
    client: createServerAPIClient(request, context),
    query: { limit: pageSize, offset: (page - 1) * pageSize },
  });

  if (result.data) {
    const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    available: result.data !== undefined,
    error: result.error?.detail ?? result.error?.title,
    page,
    placements: result.data?.items ?? [],
    total: result.data?.total ?? 0,
  };
}

export default function Placements({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="lg">
      <PageHeader
        description="Sanitized execution configuration and Controller connectivity currently loaded by the Server."
        title="Placements"
      />

      {!loaderData.available ? (
        <Alert color="red" title="Placements unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.placements.length === 0 ? (
        <EmptyState description="No Placements are configured on this Server." />
      ) : (
        <PlacementTable placements={loaderData.placements} />
      )}

      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.placements.length}
          noun="Placements"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}

function PlacementTable({ placements }: Readonly<{ placements: Placement[] }>) {
  return (
    <Paper withBorder>
      <Table.ScrollContainer minWidth={680} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Placement</Table.Th>
              <Table.Th>Cluster</Table.Th>
              <Table.Th>Controller</Table.Th>
              <Table.Th>Active Targets</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {placements.map((placement) => (
              <PlacementRow key={placement.name} placement={placement} />
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}

function PlacementRow({ placement }: Readonly<{ placement: Placement }>) {
  return (
    <Table.Tr>
      <Table.Td>
        <Anchor component={Link} fw={650} to={`/placements/${encodeURIComponent(placement.name)}`}>
          {placement.name}
        </Anchor>
      </Table.Td>
      <Table.Td>
        {placement.controllerLastSeenAt ? (
          <Anchor
            component={Link}
            fw={600}
            to={`/clusters/${encodeURIComponent(placement.cluster)}`}
          >
            {placement.cluster}
          </Anchor>
        ) : (
          <Text fw={600}>{placement.cluster}</Text>
        )}
      </Table.Td>
      <Table.Td>
        <Stack align="flex-start" gap={2}>
          <Badge
            color={placement.controllerConnectionState === "connected" ? "green" : "red"}
            variant="light"
          >
            {placement.controllerConnectionState}
          </Badge>
          {placement.controllerLastSeenAt ? (
            <RelativeTime c="dimmed" size="xs" value={placement.controllerLastSeenAt} />
          ) : (
            <Text c="dimmed" size="xs">
              Never checked in
            </Text>
          )}
        </Stack>
      </Table.Td>
      <Table.Td>
        {placement.targetCount > 0 ? (
          <Anchor component={Link} to={`/targets?placement=${encodeURIComponent(placement.name)}`}>
            {placement.targetCount} Target{placement.targetCount === 1 ? "" : "s"}
          </Anchor>
        ) : (
          <Text c="dimmed" size="sm">
            None
          </Text>
        )}
      </Table.Td>
    </Table.Tr>
  );
}
