import { Alert, Anchor, Badge, Code, Group, Paper, Stack, Table, Text } from "@mantine/core";
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
    configurationChecksum: result.data?.configurationChecksum,
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

      {loaderData.configurationChecksum ? (
        <Paper p="md" withBorder>
          <Stack gap={4}>
            <Text c="dimmed" size="xs" tt="uppercase">
              Effective configuration checksum
            </Text>
            <Code style={{ overflowWrap: "anywhere" }}>{loaderData.configurationChecksum}</Code>
          </Stack>
        </Paper>
      ) : null}

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
      <Table.ScrollContainer minWidth={980} type="native">
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Placement</Table.Th>
              <Table.Th>Destination</Table.Th>
              <Table.Th>Controller</Table.Th>
              <Table.Th>Service account</Table.Th>
              <Table.Th>Defaults</Table.Th>
              <Table.Th>Credentials</Table.Th>
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
        <Stack gap={0}>
          <Text fw={650}>{placement.name}</Text>
          {placement.targetCount > 0 ? (
            <Anchor
              component={Link}
              size="xs"
              to={`/targets?placement=${encodeURIComponent(placement.name)}`}
            >
              {placement.targetCount} Target{placement.targetCount === 1 ? "" : "s"}
            </Anchor>
          ) : (
            <Text c="dimmed" size="xs">
              No active Targets
            </Text>
          )}
        </Stack>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
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
          <Text c="dimmed" ff="monospace" size="xs">
            {placement.namespace}
          </Text>
        </Stack>
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
        <Code>{placement.serviceAccount}</Code>
      </Table.Td>
      <Table.Td>
        <PlacementDefaults placement={placement} />
      </Table.Td>
      <Table.Td>
        {placement.credentialNames.length > 0 ? (
          <Group gap={4} wrap="wrap">
            {placement.credentialNames.map((name) => (
              <Badge color="gray" key={name} variant="light">
                {name}
              </Badge>
            ))}
          </Group>
        ) : (
          <Text c="dimmed" size="sm">
            None
          </Text>
        )}
      </Table.Td>
    </Table.Tr>
  );
}

function PlacementDefaults({ placement }: Readonly<{ placement: Placement }>) {
  const requests = resourceValues("requests", placement.resources.requests);
  const limits = resourceValues("limits", placement.resources.limits);
  if (!placement.timeout && requests.length === 0 && limits.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        None
      </Text>
    );
  }

  return (
    <Stack gap={1}>
      {placement.timeout ? (
        <Text size="xs">
          Timeout <Code>{placement.timeout}</Code>
        </Text>
      ) : null}
      {[...requests, ...limits].map((value) => (
        <Text ff="monospace" key={value} size="xs">
          {value}
        </Text>
      ))}
    </Stack>
  );
}

function resourceValues(kind: "limits" | "requests", values: Record<string, string>) {
  return Object.entries(values)
    .toSorted(([left], [right]) => left.localeCompare(right))
    .map(([name, value]) => `${kind} ${name}=${value}`);
}
