import { Alert, Anchor, Group, Paper, Stack, Table, Text } from "@mantine/core";
import { Link } from "react-router";

import type { ClusterScheduling } from "~/api/generated";
import { EmptyState, PageHeaderFact, SectionHeader } from "~/shared/page";

export function ClusterSchedulingPanel({
  clusterName,
  scheduling,
}: Readonly<{ clusterName: string; scheduling?: ClusterScheduling }>) {
  if (!scheduling) {
    return (
      <Alert color="yellow" title="Scheduling details unavailable">
        Capacity and Placements could not be loaded for this cluster.
      </Alert>
    );
  }

  return (
    <>
      <Stack gap="md">
        <SectionHeader
          description="The Server counts assigned, preparing, and running Runs toward the cluster limit."
          title="Scheduling and capacity"
        />
        <Paper p="md" withBorder>
          <Group gap="xl">
            <PageHeaderFact label="Active Runs">
              {scheduling.activeRuns} of {scheduling.maximumActiveRuns}
            </PageHeaderFact>
            <PageHeaderFact label="Free slots">
              {Math.max(0, scheduling.maximumActiveRuns - scheduling.activeRuns)}
            </PageHeaderFact>
            <PageHeaderFact label="Queued Runs">
              <Anchor
                component={Link}
                inherit
                to={`/runs?cluster=${encodeURIComponent(clusterName)}&phase=queued`}
              >
                {scheduling.queuedRuns}
              </Anchor>
            </PageHeaderFact>
          </Group>
        </Paper>
      </Stack>
      <Stack gap="md">
        <SectionHeader title="Placements" />
        {scheduling.placements.length === 0 ? (
          <EmptyState description="No Placements are configured for this cluster." />
        ) : (
          <Paper withBorder>
            <Table.ScrollContainer minWidth={600} type="native">
              <Table verticalSpacing="sm">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Placement</Table.Th>
                    <Table.Th>Job namespace</Table.Th>
                    <Table.Th>Active Runs</Table.Th>
                    <Table.Th>Active Run limit</Table.Th>
                    <Table.Th>Queued Runs</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {scheduling.placements.map((placement) => (
                    <Table.Tr key={placement.name}>
                      <Table.Td>
                        <Anchor
                          component={Link}
                          fw={600}
                          to={`/placements/${encodeURIComponent(placement.name)}`}
                        >
                          {placement.name}
                        </Anchor>
                      </Table.Td>
                      <Table.Td>{placement.namespace}</Table.Td>
                      <Table.Td>{placement.activeRuns}</Table.Td>
                      <Table.Td>
                        {placement.maximumActiveRuns ?? (
                          <Text c="dimmed" size="sm">
                            Shared cluster limit
                          </Text>
                        )}
                      </Table.Td>
                      <Table.Td>{placement.queuedRuns}</Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </Paper>
        )}
      </Stack>
    </>
  );
}
