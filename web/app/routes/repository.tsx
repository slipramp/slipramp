import {
  Alert,
  Badge,
  Button,
  Code,
  Divider,
  Group,
  NativeSelect,
  Paper,
  Stack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { IconGitCommit, IconListDetails, IconRefresh, IconTrash } from "@tabler/icons-react";
import { Form, Link, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getRepositoryCatalog,
  getCurrentPrincipal,
  listCommits,
  listRepositoryRefreshAttempts,
  listRepositories,
  listRuns,
  refreshRepository,
  type CatalogRevision,
  type Repository,
  type RepositoryCommit,
  type RepositoryCommitIndexingMode,
  type RepositoryRefreshAttempt,
  updateRepository,
} from "~/api/generated";
import { Timestamp } from "~/components/display";
import { CommitReference } from "~/components/git-commit";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/components/page";
import { RecentRuns, recentRunLimit } from "~/components/recent-runs";
import { pageTitle } from "~/page-title";
import { formValue, problemDetail } from "~/form-data";
import {
  formatRefreshDuration,
  refreshFailureTitle,
  refreshOutcomePresentation,
} from "~/repository-refresh";

import type { Route } from "./+types/repository";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.repository?.name ?? "Repository") }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [
    repositoriesResult,
    catalogResult,
    runsResult,
    commitsResult,
    attemptsResult,
    principalResult,
  ] = await Promise.all([
    listRepositories({ client, query: { limit: 200 } }),
    getRepositoryCatalog({ client, path: { repositoryId: params.repositoryId } }),
    listRuns({
      client,
      query: { limit: recentRunLimit, repositoryId: params.repositoryId },
    }),
    listCommits({
      client,
      query: { limit: 5, repositoryId: params.repositoryId },
    }),
    listRepositoryRefreshAttempts({
      client,
      path: { repositoryId: params.repositoryId },
      query: { limit: 50 },
    }),
    getCurrentPrincipal({ client }),
  ]);
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);

  return {
    apiAvailable: repositoriesResult.data !== undefined || catalogResult.data !== undefined,
    catalog: catalogResult.data,
    repository,
    repositoryId: params.repositoryId,
    repositoryListAvailable: repositoriesResult.data !== undefined,
    recentRuns: runsResult.data?.items ?? [],
    recentCommits: commitsResult.data?.items ?? [],
    refreshAttempts: attemptsResult.data?.items ?? [],
    refreshAttemptsAvailable: attemptsResult.data !== undefined,
    principal: principalResult.data,
    runsAvailable: runsResult.data !== undefined,
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
  if (intent !== "update-indexing") {
    return { error: "The requested repository operation is not supported.", intent };
  }

  const result = await updateRepository({
    body: {
      commitIndexing: formValue(formData, "commitIndexing") as RepositoryCommitIndexingMode,
    },
    client,
    path: { repositoryId: params.repositoryId },
  });

  if (result.data) return { intent, message: "Commit indexing was updated.", status: "updated" };

  return {
    error: problemDetail(result.error, "The repository could not be updated."),
    intent,
  };
}

export default function RepositoryCatalog({ actionData, loaderData }: Route.ComponentProps) {
  const navigation = useNavigation();
  if (!loaderData.apiAvailable) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to repositories" to="/repositories" />
        <Alert color="red" title="API unavailable">
          The repository catalog could not be loaded.
        </Alert>
      </Stack>
    );
  }

  if (loaderData.repositoryListAvailable && !loaderData.repository) {
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
  const catalog = loaderData.catalog;
  const isAdmin = loaderData.principal?.role === "admin";
  const refreshSubmitting =
    navigation.state === "submitting" && navigation.formData?.get("intent") === "refresh";

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          repository && isAdmin ? (
            <Group gap="xs" wrap="wrap">
              <Form method="post">
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
              </Form>
              <Button
                color="red"
                component={Link}
                leftSection={<IconTrash size={16} />}
                size="compact-md"
                to={`/repositories/${repository.id}/delete`}
                variant="subtle"
              >
                Remove
              </Button>
            </Group>
          ) : null
        }
        back={<PageBackLink label="Back to repositories" to="/repositories" />}
        badges={repository ? <RefreshBadge repository={repository} /> : null}
        details={
          repository ? (
            <PageHeaderFacts>
              <PageHeaderFact label="URL">
                <Code>{repository.cloneUrl}</Code>
              </PageHeaderFact>
              <PageHeaderFact label="Branch">
                <Code>{repository.branch}</Code>
              </PageHeaderFact>
            </PageHeaderFacts>
          ) : undefined
        }
        title={repository?.name ?? "Repository"}
      />

      {actionData?.error ? (
        <Alert color="red" title="Repository operation failed">
          {actionData.error}
        </Alert>
      ) : null}
      {actionData?.message ? (
        <Alert
          color={
            actionData.status === "accepted" || actionData.status === "updated"
              ? "green"
              : actionData.status === "catalog-rejected"
                ? "orange"
                : "red"
          }
          title={actionResultTitle(actionData.status)}
        >
          {actionData.message}
        </Alert>
      ) : null}

      {repository?.refresh.failureReason ? (
        <Alert
          color={loaderData.refreshAttempts[0]?.outcome === "catalog-rejected" ? "orange" : "red"}
          title={refreshFailureTitle(loaderData.refreshAttempts[0]?.outcome)}
        >
          {repository.refresh.failureReason}
        </Alert>
      ) : null}

      {!catalog ? (
        <Paper p="lg" withBorder>
          <Stack gap="xs">
            <Title order={2}>No active catalog</Title>
            <Text c="dimmed">
              Slipramp has not accepted a catalog revision for this repository yet. The previous
              refresh status above shows whether indexing is pending or failed.
            </Text>
          </Stack>
        </Paper>
      ) : (
        <>
          <CatalogSummary catalog={catalog} repositoryId={loaderData.repositoryId} />
          <Stack gap="md">
            <SectionHeader
              actions={
                catalog.targets.length > 0 ? (
                  <Button
                    component={Link}
                    leftSection={<IconListDetails size={16} />}
                    size="compact-md"
                    to={`/targets?repositoryId=${encodeURIComponent(loaderData.repositoryId)}`}
                    variant="light"
                  >
                    View targets
                  </Button>
                ) : null
              }
              title="Targets"
            />
            <Paper p="md" withBorder>
              <Stack gap={2}>
                <Text fw={600}>
                  {catalog.targets.length} active target
                  {catalog.targets.length === 1 ? "" : "s"}
                </Text>
                <Text c="dimmed" size="sm">
                  {catalog.targets.length === 0
                    ? "This catalog contains no targets."
                    : "Search and browse this repository’s targets in the main target index."}
                </Text>
              </Stack>
            </Paper>
          </Stack>
        </>
      )}

      <RefreshAttemptsSection
        attempts={loaderData.refreshAttempts}
        available={loaderData.refreshAttemptsAvailable}
        repositoryId={loaderData.repositoryId}
      />

      {repository ? (
        <CommitHistorySection
          commits={loaderData.recentCommits}
          editable={isAdmin}
          repository={repository}
        />
      ) : null}

      <RecentRuns
        available={loaderData.runsAvailable}
        description={`The latest ${recentRunLimit} executions from this repository.`}
        emptyMessage="No Runs have been created from this repository."
        runs={loaderData.recentRuns}
        viewAllTo={`/runs?repositoryId=${encodeURIComponent(loaderData.repositoryId)}`}
      />
    </Stack>
  );
}

function RefreshAttemptsSection({
  attempts,
  available,
  repositoryId,
}: Readonly<{
  attempts: RepositoryRefreshAttempt[];
  available: boolean;
  repositoryId: string;
}>) {
  return (
    <Stack gap="md">
      <SectionHeader
        description="Up to 50 retained periodic and requested attempts, newest first."
        title="Refresh attempts"
      />
      <Paper withBorder>
        {!available ? (
          <Text c="dimmed" p="md">
            Refresh attempt history could not be loaded.
          </Text>
        ) : attempts.length === 0 ? (
          <Text c="dimmed" p="md">
            No repository refresh has completed yet.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={760} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Result</Table.Th>
                  <Table.Th>Source</Table.Th>
                  <Table.Th>Commit</Table.Th>
                  <Table.Th>Started</Table.Th>
                  <Table.Th>Duration</Table.Th>
                  <Table.Th>Diagnostic</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {attempts.map((attempt) => (
                  <Table.Tr key={attempt.id}>
                    <Table.Td>
                      <RefreshOutcomeBadge attempt={attempt} />
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="default">
                        {attempt.source === "requested" ? "Requested" : "Periodic"}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      {attempt.commit ? (
                        <CommitReference commit={attempt.commit} repositoryId={repositoryId} />
                      ) : (
                        <Text c="dimmed" size="sm">
                          Not fetched
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Timestamp size="sm" value={attempt.startedAt} />
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{formatRefreshDuration(attempt.durationMilliseconds)}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Text c={attempt.diagnostic ? undefined : "dimmed"} maw={420} size="sm">
                        {attempt.diagnostic ?? "—"}
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

function RefreshOutcomeBadge({ attempt }: Readonly<{ attempt: RepositoryRefreshAttempt }>) {
  const result = refreshOutcomePresentation(attempt.outcome);

  return (
    <Badge color={result.color} variant="light">
      {result.label}
    </Badge>
  );
}

function actionResultTitle(status: string) {
  if (status === "accepted") return "Repository refreshed";
  if (status === "catalog-rejected") return "Catalog rejected";
  if (status === "fetch-failed") return "Git fetch failed";
  return "Repository updated";
}

function CommitHistorySection({
  commits,
  editable,
  repository,
}: Readonly<{
  commits: RepositoryCommit[];
  editable: boolean;
  repository: Repository;
}>) {
  const index = repository.commitHistoryIndex;
  const navigation = useNavigation();
  const colors = { failed: "red", pending: "yellow", succeeded: "green" } as const;
  const tracksHistory = repository.commitIndexing === "trackedBranchHistory";

  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          tracksHistory ? (
            <Button
              component={Link}
              leftSection={<IconGitCommit size={16} />}
              size="compact-md"
              to={`/commits?repositoryId=${encodeURIComponent(repository.id)}`}
              variant="light"
            >
              View all commits
            </Button>
          ) : null
        }
        title="Commit history"
      />
      <Paper p="md" withBorder>
        <Stack gap="md">
          {editable ? (
            <Form method="post">
              <input name="intent" type="hidden" value="update-indexing" />
              <Group align="flex-end" wrap="wrap">
                <NativeSelect
                  data={[
                    { label: "Disabled", value: "disabled" },
                    { label: "Observed branch heads", value: "observedHeads" },
                    { label: "Complete tracked branch history", value: "trackedBranchHistory" },
                  ]}
                  defaultValue={repository.commitIndexing}
                  description="Exact SHAs used by Runs and catalogs are retained in every mode. Disabling indexing does not erase metadata already collected."
                  label="Indexing mode"
                  name="commitIndexing"
                  style={{ flex: "1 1 28rem" }}
                />
                <Button
                  loading={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("intent") === "update-indexing"
                  }
                  type="submit"
                >
                  Save
                </Button>
              </Group>
            </Form>
          ) : null}

          {tracksHistory ? (
            <>
              <Divider />
              {index?.failureReason ? (
                <Alert color="red" title="History indexing failed">
                  {index.failureReason}
                </Alert>
              ) : null}
              <Group justify="space-between">
                <Stack gap={0}>
                  <Text fw={600}>
                    {index?.commitCount ?? 0} indexed commit
                    {(index?.commitCount ?? 0) === 1 ? "" : "s"}
                  </Text>
                  <Text c="dimmed" size="sm">
                    {index?.status === "pending"
                      ? "Catching up to the latest observed branch head."
                      : index?.status === "failed"
                        ? "The previous successful index remains available."
                        : "Reachable from the latest indexed branch head."}
                  </Text>
                </Stack>
                {index ? (
                  <Badge color={colors[index.status]} variant="light">
                    {index.status}
                  </Badge>
                ) : null}
              </Group>
              {commits.length === 0 ? (
                <Text c="dimmed">No commits have been indexed yet.</Text>
              ) : (
                <Table>
                  <Table.Tbody>
                    {commits.map((item) => (
                      <Table.Tr key={item.commit.sha}>
                        <Table.Td>
                          <Text fw={600} lineClamp={1} title={item.commit.title}>
                            {item.commit.title ?? "Untitled commit"}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <CommitReference commit={item.commit} repositoryId={item.repositoryId} />
                        </Table.Td>
                        <Table.Td>
                          {(item.commit.committedAt ?? item.commit.authoredAt) ? (
                            <Timestamp
                              c="dimmed"
                              size="sm"
                              value={item.commit.committedAt ?? item.commit.authoredAt ?? ""}
                            />
                          ) : null}
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              )}
            </>
          ) : null}
        </Stack>
      </Paper>
    </Stack>
  );
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

function CatalogSummary({
  catalog,
  repositoryId,
}: Readonly<{ catalog: CatalogRevision; repositoryId: string }>) {
  return (
    <Paper p="md" withBorder>
      <Group gap="xl">
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Active commit
          </Text>
          <CommitReference commit={catalog.commit} repositoryId={repositoryId} />
        </Stack>
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Accepted
          </Text>
          <Timestamp size="sm" value={catalog.createdAt} />
        </Stack>
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Definitions
          </Text>
          <Text size="sm">
            {catalog.targets.length} targets · {catalog.runtimes.length} runtimes
          </Text>
        </Stack>
      </Group>
    </Paper>
  );
}
