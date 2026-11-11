import { Alert, Anchor, Badge, Code, Stack, Tabs } from "@mantine/core";
import { IconActivityHeartbeat, IconFileCode, IconListDetails } from "@tabler/icons-react";
import { Link, Outlet, useLocation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getPlacement, type PlacementDetail } from "~/api/generated";
import { placementDetailTab } from "~/features/placements/placement-detail";
import { PageHeader, PageHeaderFact, PageHeaderFacts } from "~/shared/page";
import { PageBackLink } from "~/shared/page-back-link";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/placement";

export type PlacementOutletContext = {
  detail: PlacementDetail;
};

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
  const location = useLocation();

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

  const detail = loaderData.detail;
  const placement = detail.placement;
  const placementPath = `/placements/${encodeURIComponent(placement.name)}`;
  const activeTab = placementDetailTab(location.pathname, placementPath);

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
        description="Validated execution configuration currently loaded by the Server."
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

      <Tabs value={activeTab} variant="outline">
        <Tabs.List
          aria-label="Placement detail sections"
          style={{ flexWrap: "nowrap", overflowX: "auto" }}
        >
          <Tabs.Tab
            leftSection={<IconListDetails aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={placementPath} />}
            style={{ flexShrink: 0 }}
            value="overview"
          >
            Overview
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconActivityHeartbeat aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${placementPath}/preflight`} />}
            style={{ flexShrink: 0 }}
            value="preflight"
          >
            Preflight
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconFileCode aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${placementPath}/definition`} />}
            style={{ flexShrink: 0 }}
            value="definition"
          >
            YAML definition
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel pt="lg" value={activeTab}>
          <Outlet context={{ detail }} />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  );
}
