import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, Link } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  listPromotionBranchDrift,
  type PromotionBranchDriftRepository,
  type PromotionBranchDriftStage,
} from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/promotion-branch-drift";

const repositoryLimit = 50;

export function meta() {
  return [{ title: pageTitle("Promotion Branch Drift") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const repository = optionalValue(url.searchParams.get("repository"));
  const result = await listPromotionBranchDrift({
    client: createServerAPIClient(request),
    query: { integration, limit: repositoryLimit, repository },
  });

  return {
    available: result.data !== undefined,
    error: result.error?.detail ?? result.error?.title,
    integration,
    repositories: result.data?.items ?? [],
    repository,
    total: result.data?.total ?? 0,
  };
}

export default function PromotionBranchDrift({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Title order={1}>Promotion Branch Drift</Title>
          <Text c="dimmed">
            Cached source-path and target-branch comparisons from successful Kargo renders.
          </Text>
        </Stack>
        <Group>
          <Anchor component={Link} to="/promotion-pipelines">
            Compare Freight
          </Anchor>
          <Anchor component={Link} to="/promotion-stages">
            Stage inventory
          </Anchor>
          <Anchor component={Link} to="/promotions">
            Promotions
          </Anchor>
        </Group>
      </Group>

      <Paper p="md" withBorder>
        <Form method="get">
          <Group align="flex-end">
            <TextInput
              defaultValue={loaderData.integration}
              label="Integration"
              name="integration"
            />
            <TextInput defaultValue={loaderData.repository} label="Repository" name="repository" />
            <Button type="submit" variant="light">
              Apply filters
            </Button>
          </Group>
        </Form>
      </Paper>

      {!loaderData.available ? (
        <Alert color="red" title="Promotion branch drift unavailable">
          {loaderData.error ?? "The Slipramp API could not be reached."}
        </Alert>
      ) : loaderData.repositories.length === 0 ? (
        <Paper p="md" withBorder>
          <Text c="dimmed">No active Kargo Stages match these filters.</Text>
        </Paper>
      ) : (
        loaderData.repositories.map((repository) => (
          <BranchDriftRepository key={repository.repository} repository={repository} />
        ))
      )}

      {loaderData.available && loaderData.total > loaderData.repositories.length ? (
        <Text c="dimmed" size="sm">
          Showing the first {loaderData.repositories.length} of {loaderData.total} matching
          repositories.
        </Text>
      ) : null}
    </Stack>
  );
}

function BranchDriftRepository({
  repository,
}: Readonly<{ repository: PromotionBranchDriftRepository }>) {
  return (
    <Paper withBorder>
      <Stack gap={0}>
        <Group justify="space-between" p="md">
          <Stack gap={0}>
            <Title order={2} size="h3">
              {repository.repository}
            </Title>
            <Text c="dimmed" size="sm">
              Source branch {repository.sourceBranch ?? "unavailable"}
            </Text>
          </Stack>
          <Text c="dimmed" component="time" dateTime={repository.scannedAt} size="sm">
            Scanned {formatTimestamp(repository.scannedAt)}
          </Text>
        </Group>
        {repository.error ? (
          <Alert color="red" m="md" mt={0} title="Repository scan failed">
            {repository.error}
          </Alert>
        ) : null}
        <Table.ScrollContainer minWidth={1100}>
          <Table horizontalSpacing="md" verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Stage</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Source branch</Table.Th>
                <Table.Th>Target branch</Table.Th>
                <Table.Th>Observed</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {repository.stages.map((stage) => (
                <BranchDriftRow
                  key={stage.id}
                  sourceBranch={repository.sourceBranch}
                  stage={stage}
                />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Stack>
    </Paper>
  );
}

function BranchDriftRow({
  sourceBranch,
  stage,
}: Readonly<{ sourceBranch?: string; stage: PromotionBranchDriftStage }>) {
  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={0}>
          <Text fw={600}>{stage.name}</Text>
          <Text size="sm">
            {stage.application} · {stage.environment}
          </Text>
          <Text c="dimmed" size="xs">
            {stage.integration} · {stage.cluster} · {stage.project}
          </Text>
          <Text c="dimmed" size="xs">
            {stage.sourcePaths.join(", ")}
          </Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Stack align="flex-start" gap="xs">
          <Badge color={statusColor(stage.status)} variant="light">
            {stage.status}
          </Badge>
          {stage.noOpRender ? (
            <Badge color="blue" size="sm" variant="outline">
              No-op render acknowledged
            </Badge>
          ) : null}
          {stage.message ? (
            <Text c="dimmed" maw={300} size="xs">
              {stage.message}
            </Text>
          ) : null}
        </Stack>
      </Table.Td>
      <Table.Td>
        <RevisionComparison
          branch={sourceBranch}
          current={stage.sourceBranchRevision}
          drift={stage.sourceDrift}
          expected={stage.acknowledgedSourceRevision}
        />
      </Table.Td>
      <Table.Td>
        <RevisionComparison
          branch={stage.targetBranch}
          current={stage.targetBranchRevision}
          drift={stage.targetDrift}
          expected={stage.acknowledgedRenderRevision}
        />
      </Table.Td>
      <Table.Td>
        <Text component="time" dateTime={stage.lastObservedAt} size="sm">
          {formatTimestamp(stage.lastObservedAt)}
        </Text>
      </Table.Td>
    </Table.Tr>
  );
}

function RevisionComparison({
  branch,
  current,
  drift,
  expected,
}: Readonly<{
  branch?: string;
  current?: string;
  drift?: boolean;
  expected?: string;
}>) {
  return (
    <Stack gap={2}>
      <Group gap="xs">
        <Text size="sm">{branch ?? "Unavailable"}</Text>
        {drift !== undefined ? (
          <Badge color={drift ? "yellow" : "green"} size="sm" variant="light">
            {drift ? "Drifted" : "Current"}
          </Badge>
        ) : null}
      </Group>
      <Text c="dimmed" size="xs">
        Current {current ? <Code title={current}>{shortRevision(current)}</Code> : "unknown"}
      </Text>
      <Text c="dimmed" size="xs">
        Acknowledged{" "}
        {expected ? <Code title={expected}>{shortRevision(expected)}</Code> : "unknown"}
      </Text>
    </Stack>
  );
}

function statusColor(status: PromotionBranchDriftStage["status"]) {
  switch (status) {
    case "current":
      return "green";
    case "drifted":
      return "yellow";
    default:
      return "gray";
  }
}

function shortRevision(revision: string) {
  return revision.slice(0, 12);
}

function optionalValue(value: string | null) {
  return value?.trim() || undefined;
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}
