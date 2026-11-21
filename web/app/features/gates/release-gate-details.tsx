import { Alert, Badge, Code, Group, Paper, SimpleGrid, Stack, Text } from "@mantine/core";
import type { ReactNode } from "react";

import type { GateRequirement, GateSummary } from "~/api/generated";
import {
  gateConditionLabel,
  gateRequirementBadges,
  gateSummaryBadges,
} from "~/features/gates/gate-presentation";
import { SectionHeader } from "~/shared/page";
import { Timestamp } from "~/shared/display-values";

type GateSubject = "Promotion" | "Run";

export function ReleaseGateDetails({
  gates,
  subject,
}: Readonly<{ gates: GateSummary; subject: GateSubject }>) {
  if (gates.state === "not-required") return null;

  const badge = gateSummaryBadges[gates.state];
  const description = gateSummaryDescription(gates.state, subject);

  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Badge color={badge.color} variant="light">
            {badge.label}
          </Badge>
        }
        description={description}
        title="Release gates"
      />
      {gates.requirements.map((requirement) => (
        <GateRequirementDetails key={requirement.name} requirement={requirement} />
      ))}
    </Stack>
  );
}

function gateSummaryDescription(state: GateSummary["state"], subject: GateSubject) {
  if (state === "waiting") {
    if (subject === "Run") {
      return "Run assignment requires every attached Gate to pass in the same release check.";
    }
    return "Kargo execution requires every attached Gate to pass in the same release check.";
  }

  if (subject === "Run") {
    return "Every required Gate passed together when this Run was released for assignment.";
  }
  return "Every required Gate passed together when this Promotion was released for Kargo execution.";
}

function GateRequirementDetails({ requirement }: Readonly<{ requirement: GateRequirement }>) {
  const badge = gateRequirementBadges[requirement.state];

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group align="flex-start" justify="space-between" wrap="nowrap">
          <Stack gap={2}>
            <Text fw={650}>{requirement.title ?? requirement.name}</Text>
            {requirement.title ? (
              <Text c="dimmed" size="xs">
                {requirement.name}
              </Text>
            ) : null}
          </Stack>
          <Badge color={badge.color} variant="light">
            {badge.label}
          </Badge>
        </Group>

        <SimpleGrid cols={{ base: 1, sm: requirement.releasedAt ? 4 : 3 }} spacing="lg">
          <GateFact label="Observed value">
            {requirement.value === undefined ? (
              <Text c="dimmed" size="sm">
                No observation
              </Text>
            ) : (
              <Code>{requirement.value}</Code>
            )}
          </GateFact>
          <GateFact label="Expected condition">
            <Text size="sm">{gateConditionLabel(requirement)}</Text>
          </GateFact>
          <GateFact label="Last evaluation">
            {requirement.evaluatedAt ? (
              <Timestamp size="sm" value={requirement.evaluatedAt} />
            ) : (
              <Text c="dimmed" size="sm">
                Not evaluated
              </Text>
            )}
          </GateFact>
          {requirement.releasedAt ? (
            <GateFact label="Released">
              <Timestamp size="sm" value={requirement.releasedAt} />
            </GateFact>
          ) : null}
        </SimpleGrid>

        {requirement.diagnostic ? (
          <Alert color="orange" title="Gate diagnostic">
            {requirement.diagnostic}
          </Alert>
        ) : null}
      </Stack>
    </Paper>
  );
}

function GateFact({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap={0}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      {children}
    </Stack>
  );
}
