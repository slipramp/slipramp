import { Alert, Anchor, Badge, Code, Group, Paper, SimpleGrid, Stack, Text } from "@mantine/core";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getPlacement } from "~/api/generated";
import { RelativeTime, Timestamp } from "~/components/display";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/components/page";
import { PageBackLink } from "~/components/page-back-link";
import {
  CodeList,
  ResourceRequirementsTable,
  TechnicalValue,
} from "~/components/technical-definition";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/placement";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.detail?.placement.name ?? "Placement") }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const result = await getPlacement({
    client: createServerAPIClient(request, context),
    path: { placementName: params.placementName },
  });

  return {
    available: result.data !== undefined || result.response?.status === 404,
    detail: result.data,
    error:
      result.error?.detail ?? result.error?.title ?? "The Placement inventory could not be loaded.",
  };
}

export default function PlacementDetail({ loaderData }: Route.ComponentProps) {
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

  return (
    <Stack gap="lg">
      <PageHeader
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
