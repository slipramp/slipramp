import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
} from "@mantine/core";
import { IconActivityHeartbeat } from "@tabler/icons-react";
import { Link, useFetcher } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  createPlacementPreflight,
  getPlacement,
  listPlacementPreflights,
  type PlacementPreflight,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import { PageBackLink } from "~/shared/page-back-link";
import { problemDetail } from "~/shared/form-data";
import { CodeList, ResourceRequirementsTable, TechnicalValue } from "~/shared/technical-definition";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/placement";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.detail?.placement.name ?? "Placement") }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [result, preflights, principal] = await Promise.all([
    getPlacement({
      client,
      path: { placementName: params.placementName },
    }),
    listPlacementPreflights({
      client,
      path: { placementName: params.placementName },
      query: { limit: 10 },
    }),
    getProductPrincipal(context),
  ]);

  return {
    available: result.data !== undefined || result.response?.status === 404,
    detail: result.data,
    error:
      result.error?.detail ?? result.error?.title ?? "The Placement inventory could not be loaded.",
    preflights: preflights.data?.items ?? [],
    preflightsAvailable: preflights.data !== undefined || preflights.response?.status === 404,
    principal,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const result = await createPlacementPreflight({
    client: createServerAPIClient(request, context),
    path: { placementName: params.placementName },
  });
  if (result.data) {
    return { preflight: result.data };
  }

  return { error: problemDetail(result.error, "The Placement preflight could not be started.") };
}

export default function PlacementDetail({ actionData, loaderData }: Route.ComponentProps) {
  const preflightFetcher = useFetcher<typeof action>();
  const activePreflight = loaderData.preflights.find(
    (preflight) => preflight.state === "queued" || preflight.state === "running",
  );
  useActivityRefresh(activePreflight !== undefined);

  if (!loaderData.detail) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to Placements" to="/placements" />
        <Alert
          color={loaderData.available ? "yellow" : "red"}
          title={loaderData.available ? "Placement not found" : "Placement unavailable"}
        >
          {loaderData.available
            ? "This Placement is not configured on the Server."
            : loaderData.error}
        </Alert>
      </Stack>
    );
  }

  const { configurationChecksum, placement } = loaderData.detail;
  const canStartPreflight =
    loaderData.principal?.role === "operator" || loaderData.principal?.role === "admin";
  const operationData = preflightFetcher.data ?? actionData;
  const preflightSubmitting = preflightFetcher.state !== "idle";

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          canStartPreflight ? (
            <preflightFetcher.Form method="post">
              <Button
                disabled={placement.controllerConnectionState !== "connected" || !!activePreflight}
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
        back={<PageBackLink label="Back to Placements" to="/placements" />}
        badges={
          <Badge
            color={placement.controllerConnectionState === "connected" ? "green" : "red"}
            variant="light"
          >
            {placement.controllerConnectionState}
          </Badge>
        }
        description="Sanitized execution configuration currently loaded by the Server."
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Cluster">
              {placement.controllerLastSeenAt ? (
                <Anchor component={Link} to={`/clusters/${encodeURIComponent(placement.cluster)}`}>
                  {placement.cluster}
                </Anchor>
              ) : (
                placement.cluster
              )}
            </PageHeaderFact>
            <PageHeaderFact label="Namespace">
              <Code>{placement.namespace}</Code>
            </PageHeaderFact>
            <PageHeaderFact label="Active Targets">
              {placement.targetCount > 0 ? (
                <Anchor
                  component={Link}
                  to={`/targets?placement=${encodeURIComponent(placement.name)}`}
                >
                  {placement.targetCount}
                </Anchor>
              ) : (
                "None"
              )}
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={placement.name}
      />

      {placement.controllerConnectionState === "unreachable" ? (
        <Alert color="red" title="Controller is unreachable">
          {placement.controllerLastSeenAt
            ? "The Controller has not checked in within the connection window."
            : "The Controller for this Placement's Cluster has never checked in."}
        </Alert>
      ) : null}

      {operationData?.error ? (
        <Alert color="red" title="Preflight could not start">
          {operationData.error}
        </Alert>
      ) : null}

      <PlacementPreflightHistory
        available={loaderData.preflightsAvailable}
        preflights={loaderData.preflights}
      />

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            description="Defaults applied when a Target Action does not provide an override."
            title="Execution configuration"
          />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TechnicalValue label="Service account" value={placement.serviceAccount} />
            <TechnicalValue label="Default timeout" value={placement.timeout ?? "No default"} />
          </SimpleGrid>
          <ResourceRequirementsTable
            empty="No resource defaults"
            label="Resource defaults"
            limits={placement.resources.limits}
            requests={placement.resources.requests}
          />
          <CodeList
            empty="No logical credential sources"
            label="Credential names"
            values={placement.credentialNames}
          />
        </Stack>
      </Paper>

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            description="Observed connectivity and the effective configuration version behind this view."
            title="Server observation"
          />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <Stack gap={4}>
              <Text c="dimmed" fw={700} size="xs" tt="uppercase">
                Last Controller contact
              </Text>
              {placement.controllerLastSeenAt ? (
                <Group gap="xs" wrap="wrap">
                  <RelativeTime fw={650} value={placement.controllerLastSeenAt} />
                  <Timestamp c="dimmed" size="xs" value={placement.controllerLastSeenAt} />
                </Group>
              ) : (
                <Text c="dimmed" size="sm">
                  Never checked in
                </Text>
              )}
            </Stack>
            <TechnicalValue
              description="Identifies the complete validated Placement set loaded by this Server."
              label="Configuration checksum"
              value={configurationChecksum}
            />
          </SimpleGrid>
        </Stack>
      </Paper>
    </Stack>
  );
}

function PlacementPreflightHistory({
  available,
  preflights,
}: Readonly<{ available: boolean; preflights: PlacementPreflight[] }>) {
  const latest = preflights[0];

  return (
    <Paper p="lg" withBorder>
      <Stack gap="lg">
        <SectionHeader
          description="Active probe Jobs verify that this Placement can launch, observe, and clean up a workload. They do not run repository Actions or validate Action credentials."
          title="Placement preflight"
        />
        {!available ? (
          <Alert color="red" title="Preflight history unavailable">
            The Server could not load preflight history.
          </Alert>
        ) : preflights.length === 0 ? (
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
                  {preflights.map((preflight) => (
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
