import { Alert, Badge, Button, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { IconActivityHeartbeat } from "@tabler/icons-react";
import { useFetcher, useOutletContext } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  createPlacementPreflight,
  listPlacementPreflights,
  type PlacementPreflight,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { problemDetail } from "~/shared/form-data";
import { SectionHeader } from "~/shared/page";

import type { Route } from "./+types/placement-preflight";
import type { PlacementOutletContext } from "./placement";

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [preflights, principal] = await Promise.all([
    listPlacementPreflights({
      client,
      path: { placementName: params.placementName },
    }),
    getProductPrincipal(context),
  ]);

  return {
    available: preflights.data !== undefined || preflights.response?.status === 404,
    preflights: preflights.data?.items ?? [],
    principal,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const result = await createPlacementPreflight({
    client: createServerAPIClient(request, context),
    path: { placementName: params.placementName },
  });
  if (result.data) return { preflight: result.data };

  return { error: problemDetail(result.error, "The Placement preflight could not be started.") };
}

export default function PlacementPreflight({ actionData, loaderData }: Route.ComponentProps) {
  const { detail } = useOutletContext<PlacementOutletContext>();
  const preflightFetcher = useFetcher<typeof action>();
  const activePreflight = loaderData.preflights.find(
    (preflight) => preflight.state === "queued" || preflight.state === "running",
  );
  useActivityRefresh(activePreflight !== undefined);

  const canStartPreflight =
    loaderData.principal?.role === "operator" || loaderData.principal?.role === "admin";
  const operationData = preflightFetcher.data ?? actionData;
  const preflightSubmitting = preflightFetcher.state !== "idle";
  const latest = loaderData.preflights[0];

  return (
    <Stack gap="md">
      {operationData?.error ? (
        <Alert color="red" title="Preflight could not start">
          {operationData.error}
        </Alert>
      ) : null}

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            actions={
              canStartPreflight ? (
                <preflightFetcher.Form method="post">
                  <Button
                    disabled={
                      detail.placement.controllerConnectionState !== "connected" ||
                      !!activePreflight
                    }
                    leftSection={<IconActivityHeartbeat size={16} />}
                    loading={preflightSubmitting}
                    size="compact-md"
                    type="submit"
                    variant="light"
                  >
                    {activePreflight ? "Preflight active" : "Start preflight"}
                  </Button>
                </preflightFetcher.Form>
              ) : null
            }
            description="Active probe Jobs verify that this Placement can launch, observe, and clean up a workload. They do not run repository Actions or validate Action credentials."
            title="Placement preflight"
          />

          {!loaderData.available ? (
            <Alert color="red" title="Preflight history unavailable">
              The Server could not load preflight history.
            </Alert>
          ) : loaderData.preflights.length === 0 ? (
            <Text c="dimmed" size="sm">
              No preflight has been requested for this Placement.
            </Text>
          ) : (
            <Stack gap="md">
              {latest ? <LatestPreflight preflight={latest} /> : null}
              <Table.ScrollContainer minWidth={680} type="native">
                <Table verticalSpacing="sm">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>Result</Table.Th>
                      <Table.Th>Requested</Table.Th>
                      <Table.Th>Requested by</Table.Th>
                      <Table.Th>Cleanup</Table.Th>
                      <Table.Th>Reason</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {loaderData.preflights.map((preflight) => (
                      <Table.Tr key={preflight.id}>
                        <Table.Td>
                          <PreflightBadge state={preflight.state} />
                        </Table.Td>
                        <Table.Td>
                          <Timestamp size="sm" value={preflight.createdAt} />
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{preflight.requestedBy.displayName}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Badge
                            color={preflight.cleanupState === "failed" ? "red" : "gray"}
                            variant="default"
                          >
                            {preflight.cleanupState}
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Text c={preflight.reason ? undefined : "dimmed"} size="sm">
                            {preflight.reason ?? "—"}
                          </Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </Table.ScrollContainer>
            </Stack>
          )}
        </Stack>
      </Paper>
    </Stack>
  );
}

function LatestPreflight({ preflight }: Readonly<{ preflight: PlacementPreflight }>) {
  return (
    <Alert
      color={
        preflight.state === "succeeded" ? "green" : preflight.state === "failed" ? "red" : "blue"
      }
      title={
        <Group gap="xs">
          <PreflightBadge state={preflight.state} />
          <Text fw={700}>Latest preflight</Text>
          <RelativeTime c="dimmed" size="sm" value={preflight.finishedAt ?? preflight.createdAt} />
        </Group>
      }
    >
      <Stack gap="xs">
        <Text size="sm">
          {preflight.message ??
            (preflight.state === "queued"
              ? "Waiting for the responsible Controller to accept the probe."
              : "The Controller is running the probe Job.")}
        </Text>
        {preflight.diagnostics?.map((diagnostic, index) => (
          <Text key={`${diagnostic.reason}-${index}`} size="sm">
            <Text component="span" fw={700} inherit>
              {diagnostic.reason}:
            </Text>{" "}
            {diagnostic.message}
            {diagnostic.count > 1 ? ` (${diagnostic.count} times)` : ""}
          </Text>
        ))}
      </Stack>
    </Alert>
  );
}

function PreflightBadge({ state }: Readonly<{ state: PlacementPreflight["state"] }>) {
  const presentation = {
    queued: { color: "gray", label: "Queued" },
    running: { color: "blue", label: "Running" },
    succeeded: { color: "green", label: "Succeeded" },
    failed: { color: "red", label: "Failed" },
  }[state];

  return (
    <Badge color={presentation.color} variant="light">
      {presentation.label}
    </Badge>
  );
}
