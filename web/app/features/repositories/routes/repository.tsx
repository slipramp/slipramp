import { Alert, Badge, Button, Code, Group, Stack, Tabs } from "@mantine/core";
import {
  IconEdit,
  IconGitCommit,
  IconHistory,
  IconListDetails,
  IconRefresh,
  IconTrash,
} from "@tabler/icons-react";
import { Link, Outlet, useFetcher, useLocation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listRepositories,
  listRepositoryRefreshAttempts,
  refreshRepository,
  type Repository,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts } from "~/shared/page";
import { formValue, problemDetail } from "~/shared/form-data";
import { pageTitle } from "~/shared/page-title";
import { repositoryDetailTab } from "~/features/repositories/repository-detail";
import { refreshFailureTitle } from "~/features/repositories/repository-refresh";

import type { Route } from "./+types/repository";

export type RepositoryOutletContext = {
  repository: Repository;
};

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.repository?.name ?? "Repository") }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [repositoriesResult, principal] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
    getProductPrincipal(context),
  ]);
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);
  const attemptsResult = repository?.refresh.failureReason
    ? await listRepositoryRefreshAttempts({
        client,
        path: { repositoryId: params.repositoryId },
        query: { limit: 1 },
      })
    : undefined;

  return {
    apiAvailable: repositoriesResult.data !== undefined,
    latestRefreshOutcome: attemptsResult?.data?.items[0]?.outcome,
    principal,
    repository,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const intent = formValue(formData, "intent");
  const client = createServerAPIClient(request, context);
  if (intent === "refresh") {
    const result = await refreshRepository({
      client,
      path: { repositoryId: params.repositoryId },
    });
    if (result.data) {
      return {
        intent,
        message:
          result.data.outcome === "accepted"
            ? "The repository catalog was accepted."
            : result.data.outcome === "catalog-rejected"
              ? "Git was fetched, but the catalog was rejected."
              : "The Git fetch failed.",
        status: result.data.outcome,
      };
    }

    return {
      error: problemDetail(result.error, "The repository could not be refreshed."),
      intent,
    };
  }
  return { error: "The requested repository operation is not supported.", intent };
}

export default function RepositoryDetail({ actionData, loaderData }: Route.ComponentProps) {
  const location = useLocation();
  const refreshFetcher = useFetcher<typeof action>();

  if (!loaderData.apiAvailable) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to repositories" to="/repositories" />
        <Alert color="red" title="API unavailable">
          The repository could not be loaded.
        </Alert>
      </Stack>
    );
  }

  if (!loaderData.repository) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to repositories" to="/repositories" />
        <Alert color="red" title="Repository not found">
          This repository is not in the active repository list.
        </Alert>
      </Stack>
    );
  }

  const repository = loaderData.repository;
  const isAdmin = loaderData.principal?.role === "admin";
  const refreshSubmitting = refreshFetcher.state !== "idle";
  const operationData = refreshFetcher.data ?? actionData;
  const repositoryPath = `/repositories/${repository.id}`;
  const activeTab = repositoryDetailTab(location.pathname, repositoryPath);

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          isAdmin ? (
            <Group gap="xs" wrap="wrap">
              <refreshFetcher.Form action={repositoryPath} method="post">
                <input name="intent" type="hidden" value="refresh" />
                <Button
                  leftSection={<IconRefresh size={16} />}
                  loading={refreshSubmitting}
                  size="compact-md"
                  type="submit"
                  variant="light"
                >
                  Refresh now
                </Button>
              </refreshFetcher.Form>
              <Button
                component={Link}
                leftSection={<IconEdit size={16} />}
                size="compact-md"
                to={`${repositoryPath}/edit`}
                variant="light"
              >
                Edit
              </Button>
              <Button
                color="red"
                component={Link}
                leftSection={<IconTrash size={16} />}
                size="compact-md"
                to={`${repositoryPath}/delete`}
                variant="subtle"
              >
                Remove
              </Button>
            </Group>
          ) : null
        }
        back={<PageBackLink label="Back to repositories" to="/repositories" />}
        badges={<RefreshBadge repository={repository} />}
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="URL">
              <Code>{repository.cloneUrl}</Code>
            </PageHeaderFact>
            <PageHeaderFact label="Branch">
              <Code>{repository.branch}</Code>
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={repository.name}
      />

      {operationData?.error ? (
        <Alert color="red" title="Repository operation failed">
          {operationData.error}
        </Alert>
      ) : null}
      {operationData?.message ? (
        <Alert
          color={
            operationData.status === "accepted"
              ? "green"
              : operationData.status === "catalog-rejected"
                ? "orange"
                : "red"
          }
          title={actionResultTitle(operationData.status)}
        >
          {operationData.message}
        </Alert>
      ) : null}

      {repository.refresh.failureReason ? (
        <Alert
          color={loaderData.latestRefreshOutcome === "catalog-rejected" ? "orange" : "red"}
          title={refreshFailureTitle(loaderData.latestRefreshOutcome)}
        >
          {repository.refresh.failureReason}
        </Alert>
      ) : null}

      <Tabs value={activeTab} variant="outline">
        <Tabs.List
          aria-label="Repository detail sections"
          style={{ flexWrap: "nowrap", overflowX: "auto" }}
        >
          <Tabs.Tab
            leftSection={<IconListDetails aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={repositoryPath} />}
            style={{ flexShrink: 0 }}
            value="overview"
          >
            Overview
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconRefresh aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${repositoryPath}/refresh-attempts`} />}
            style={{ flexShrink: 0 }}
            value="refresh-attempts"
          >
            Refresh attempts
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconGitCommit aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${repositoryPath}/commits`} />}
            style={{ flexShrink: 0 }}
            value="commits"
          >
            Commits
          </Tabs.Tab>
          <Tabs.Tab
            leftSection={<IconHistory aria-hidden size={16} />}
            renderRoot={(props) => <Link {...props} to={`${repositoryPath}/runs`} />}
            style={{ flexShrink: 0 }}
            value="runs"
          >
            Recent runs
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel pt="lg" value={activeTab}>
          <Outlet context={{ repository }} />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  );
}

function actionResultTitle(status: string) {
  if (status === "accepted") return "Repository refreshed";
  if (status === "catalog-rejected") return "Catalog rejected";
  if (status === "fetch-failed") return "Git fetch failed";
  return "Repository updated";
}

function RefreshBadge({ repository }: Readonly<{ repository: Repository }>) {
  const colors = {
    failed: "red",
    pending: "yellow",
    succeeded: "green",
  } as const;

  return (
    <Badge color={colors[repository.refresh.status]} variant="light">
      Refresh {repository.refresh.status}
    </Badge>
  );
}
