import {
  Alert,
  Anchor,
  Badge,
  Card,
  Code,
  Divider,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  Title,
} from "@mantine/core";
import { Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getRepositoryCatalog,
  listRepositories,
  type Action,
  type CatalogRevision,
  type Repository,
  type Target,
} from "~/api/generated";

import type { Route } from "./+types/repository";

export async function loader({ params }: Route.LoaderArgs) {
  const client = createServerAPIClient();
  const [repositoriesResult, catalogResult] = await Promise.all([
    listRepositories({ client }),
    getRepositoryCatalog({ client, path: { repositoryId: params.repositoryId } }),
  ]);
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);

  return {
    apiAvailable: repositoriesResult.data !== undefined || catalogResult.data !== undefined,
    catalog: catalogResult.data,
    repository,
    repositoryListAvailable: repositoriesResult.data !== undefined,
  };
}

export default function RepositoryCatalog({ loaderData }: Route.ComponentProps) {
  if (!loaderData.apiAvailable) {
    return (
      <Alert color="red" title="API unavailable">
        The repository catalog could not be loaded.
      </Alert>
    );
  }

  if (loaderData.repositoryListAvailable && !loaderData.repository) {
    return (
      <Alert color="red" title="Repository not found">
        This repository is not in the active repository list.
      </Alert>
    );
  }

  const repository = loaderData.repository;

  return (
    <Stack gap="lg">
      <Stack gap="xs">
        <Anchor component={Link} size="sm" to="/repositories">
          ← Repositories
        </Anchor>
        <Group align="flex-start" justify="space-between">
          <Stack gap={2}>
            <Title order={1}>{repository?.name ?? "Repository"}</Title>
            {repository ? (
              <Text c="dimmed">
                {repository.cloneUrl} · {repository.branch}
              </Text>
            ) : null}
          </Stack>
          {repository ? <RefreshBadge repository={repository} /> : null}
        </Group>
      </Stack>

      {repository?.refresh.failureReason ? (
        <Alert color="red" title="Latest refresh failed">
          {repository.refresh.failureReason}
        </Alert>
      ) : null}

      {!loaderData.catalog ? (
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
          <CatalogSummary catalog={loaderData.catalog} />
          <Stack gap="md">
            <Title order={2}>Targets</Title>
            {loaderData.catalog.targets.length === 0 ? (
              <Paper p="md" withBorder>
                <Text c="dimmed">This catalog contains no targets.</Text>
              </Paper>
            ) : (
              <SimpleGrid cols={{ base: 1, lg: 2 }}>
                {loaderData.catalog.targets.map((target) => (
                  <TargetCard key={target.metadata.name} target={target} />
                ))}
              </SimpleGrid>
            )}
          </Stack>
        </>
      )}
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
    <Badge color={colors[repository.refresh.status]} size="lg" variant="light">
      Refresh {repository.refresh.status}
    </Badge>
  );
}

function CatalogSummary({ catalog }: Readonly<{ catalog: CatalogRevision }>) {
  return (
    <Paper p="md" withBorder>
      <Group gap="xl">
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Active commit
          </Text>
          <Code>{shortCommit(catalog.commitSha)}</Code>
        </Stack>
        <Stack gap={0}>
          <Text c="dimmed" size="xs" tt="uppercase">
            Accepted
          </Text>
          <Text component="time" dateTime={catalog.createdAt} size="sm">
            {formatTimestamp(catalog.createdAt)}
          </Text>
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

function TargetCard({ target }: Readonly<{ target: Target }>) {
  const actions = Object.entries(target.spec.actions).toSorted(([left], [right]) =>
    left.localeCompare(right),
  );

  return (
    <Card padding="lg" withBorder>
      <Stack gap="md">
        <Stack gap={2}>
          <Group justify="space-between">
            <Title order={3}>{target.metadata.title ?? target.metadata.name}</Title>
            <Badge variant="outline">{actions.length} actions</Badge>
          </Group>
          {target.metadata.title ? (
            <Text c="dimmed" size="sm">
              {target.metadata.name}
            </Text>
          ) : null}
          <Text c="dimmed" size="xs">
            {target.source.path} · document {target.source.document}
          </Text>
        </Stack>

        <Group gap="xs">
          {target.spec.runtime ? (
            <Badge variant="light">Runtime: {target.spec.runtime}</Badge>
          ) : null}
          {target.spec.placement ? (
            <Badge variant="light">Placement: {target.spec.placement}</Badge>
          ) : null}
          {target.spec.path ? <Badge variant="light">Path: {target.spec.path}</Badge> : null}
        </Group>

        <Divider />

        <Stack gap="md">
          {actions.map(([name, action]) => (
            <ActionSummary action={action} key={name} name={name} />
          ))}
        </Stack>
      </Stack>
    </Card>
  );
}

function ActionSummary({ action, name }: Readonly<{ action: Action; name: string }>) {
  const parameterCount = Object.keys(action.parameters ?? {}).length;
  const artifactCount = Object.keys(action.artifacts ?? {}).length;
  const inputCount = Object.keys(action.inputs ?? {}).length;

  return (
    <Stack gap={4}>
      <Group gap="xs">
        <Text fw={600}>{name}</Text>
        {action.intent ? <Badge variant="outline">{action.intent}</Badge> : null}
      </Group>
      <Code block>{action.command.join(" ")}</Code>
      <Group gap="xs">
        {parameterCount > 0 ? <Badge variant="light">{parameterCount} parameters</Badge> : null}
        {artifactCount > 0 ? <Badge variant="light">{artifactCount} artifacts</Badge> : null}
        {inputCount > 0 ? <Badge variant="light">{inputCount} inputs</Badge> : null}
        {action.timeout ? <Badge variant="light">Timeout: {action.timeout}</Badge> : null}
      </Group>
    </Stack>
  );
}

function shortCommit(commit: string) {
  return commit.slice(0, 12);
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
