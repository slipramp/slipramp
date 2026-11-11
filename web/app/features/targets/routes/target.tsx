import { Alert, Anchor, Badge, Code, Stack, Tabs } from "@mantine/core";
import {
  IconAdjustmentsHorizontal,
  IconFileCode,
  IconHistory,
  IconListDetails,
} from "@tabler/icons-react";
import { Link, Outlet, useLocation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getTarget, type Target } from "~/api/generated";
import { CommitReference } from "~/features/commits/git-commit";
import { RunHistoryButton } from "~/features/runs/run-history-button";
import { targetDetailTab } from "~/features/targets/target-detail";
import { PageHeader, PageHeaderFact, PageHeaderFacts } from "~/shared/page";
import { PageBackLink } from "~/shared/page-back-link";
import { pageTitle } from "~/shared/page-title";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import type { Route } from "./+types/target";

export type TargetOutletContext = {
  target: Target;
};

export function meta({ loaderData }: Route.MetaArgs) {
  const targetName = loaderData?.target
    ? (loaderData.target.definition.metadata.title ?? loaderData.target.definition.metadata.name)
    : "Target";
  return [{ title: pageTitle(targetName) }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const result = await getTarget({ client, path: { targetId: params.targetId } });

  return {
    error: result.data
      ? undefined
      : (result.error?.detail ??
        result.error?.title ??
        "The target definition could not be loaded."),
    target: result.data,
  };
}

export default function TargetDetail({ loaderData }: Route.ComponentProps) {
  const location = useLocation();

  if (!loaderData.target) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to targets" to="/targets" />
        <Alert color="red" title="Target not found">
          {loaderData.error}
        </Alert>
      </Stack>
    );
  }

  const record = loaderData.target;
  const target = record.definition;
  const targetPath = `/targets/${record.id}`;
  const activeTab = targetDetailTab(location.pathname, targetPath);
  const actionCount = Object.keys(target.spec.actions).length;

  return (
    <Stack gap="lg">
      <PageHeader
        actions={<RunHistoryButton to={`/runs?targetId=${record.id}`} />}
        back={<PageBackLink label="Back to targets" to="/targets" />}
        badges={
          <>
            {record.archivedAt ? <Badge color="gray">Archived</Badge> : null}
            <Badge variant="outline">
              {actionCount} action{actionCount === 1 ? "" : "s"}
            </Badge>
          </>
        }
        description="Operational target"
        details={
          <PageHeaderFacts>
            {target.metadata.title ? (
              <PageHeaderFact label="Target">
                <Code>{target.metadata.name}</Code>
              </PageHeaderFact>
            ) : null}
            <PageHeaderFact label="Repository">
              <Anchor component={Link} to={`/repositories/${record.repositoryId}`}>
                {record.repositoryName}
              </Anchor>
            </PageHeaderFact>
            <PageHeaderFact label="Last changed">
              <CommitReference
                commit={target.status.desiredCommit}
                repositoryId={record.repositoryId}
              />
            </PageHeaderFact>
            <PageHeaderFact label="Catalog">
              <CommitReference commit={record.catalogCommit} repositoryId={record.repositoryId} />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={target.metadata.title ?? target.metadata.name}
      />

      {record.archivedAt ? (
        <Alert color="gray" title="Archived target">
          This definition is retained for Run history. It cannot start new Runs unless it becomes
          active in its repository catalog again.
        </Alert>
      ) : null}

      <Tabs value={activeTab} variant="outline">
        <Tabs.List
          aria-label="Target detail sections"
          style={{ flexWrap: "nowrap", overflowX: "auto" }}
        >
          <Tabs.Tab
            leftSection={<IconListDetails aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={targetPath} />}
            style={{ flexShrink: 0 }}
            value="overview"
          >
            Overview
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconHistory aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${targetPath}/runs`} />}
            style={{ flexShrink: 0 }}
            value="runs"
          >
            Recent runs
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconAdjustmentsHorizontal aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${targetPath}/defaults`} />}
            style={{ flexShrink: 0 }}
            value="defaults"
          >
            Target defaults
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconFileCode aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${targetPath}/definition`} />}
            style={{ flexShrink: 0 }}
            value="definition"
          >
            YAML definition
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel pt="lg" value={activeTab}>
          <Outlet context={{ target: record }} />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  );
}
