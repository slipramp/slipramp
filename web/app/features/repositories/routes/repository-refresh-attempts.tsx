import { Badge, Paper, Stack, Table, Text } from "@mantine/core";

import { createServerAPIClient } from "~/api/client.server";
import { listRepositoryRefreshAttempts, type RepositoryRefreshAttempt } from "~/api/generated";
import { Timestamp } from "~/shared/display-values";
import { CommitReference } from "~/features/commits/git-commit";
import { SectionHeader } from "~/shared/page";
import { repositoryDetailListLimit } from "~/features/repositories/repository-detail";
import {
  formatRefreshDuration,
  refreshOutcomePresentation,
} from "~/features/repositories/repository-refresh";

import type { Route } from "./+types/repository-refresh-attempts";

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const result = await listRepositoryRefreshAttempts({
    client,
    path: { repositoryId: params.repositoryId },
    query: { limit: repositoryDetailListLimit },
  });

  return {
    attempts: result.data?.items ?? [],
    available: result.data !== undefined,
    repositoryId: params.repositoryId,
  };
}

export default function RepositoryRefreshAttempts({ loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="md">
      <SectionHeader
        description={`The latest ${repositoryDetailListLimit} periodic and requested attempts, newest first.`}
        title="Refresh attempts"
      />
      <Paper withBorder>
        {!loaderData.available ? (
          <Text c="dimmed" p="md">
            Refresh attempt history could not be loaded.
          </Text>
        ) : loaderData.attempts.length === 0 ? (
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
                {loaderData.attempts.map((attempt) => (
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
                        <CommitReference
                          commit={attempt.commit}
                          repositoryId={loaderData.repositoryId}
                        />
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
