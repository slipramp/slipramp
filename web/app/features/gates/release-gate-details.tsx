import { Badge, Code, Stack, Table, Text } from "@mantine/core";
import { Fragment, type ReactNode } from "react";

import type { GateRequirement, GateSummary } from "~/api/generated";
import {
  gateConditionLabel,
  gateRequirementBadges,
  gateSummaryBadges,
} from "~/features/gates/gate-presentation";
import { SectionHeader } from "~/shared/page";
import { RelativeTime, Timestamp } from "~/shared/display-values";

type GateSubject = "Promotion" | "Run";

export function ReleaseGateDetails({
  gates,
  subject,
  title = "Release gates",
}: Readonly<{ gates: GateSummary; subject: GateSubject; title?: ReactNode }>) {
  if (gates.state === "not-required") {
    return subject === "Promotion" ? (
      <Text c="dimmed">No gates required for this Promotion.</Text>
    ) : null;
  }

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
        title={title}
      />
      <Table.ScrollContainer minWidth={600} type="native">
        <Table aria-label="Gate evaluations" verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Gate</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Observed value</Table.Th>
              <Table.Th>Expected condition</Table.Th>
              <Table.Th>Last evaluated</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {gates.requirements.map((requirement) => (
              <Fragment key={requirement.name}>
                <GateRequirementRow requirement={requirement} />
                {requirement.diagnostic ? (
                  <Table.Tr>
                    <Table.Td colSpan={5}>
                      <Text size="sm">{requirement.diagnostic}</Text>
                    </Table.Td>
                  </Table.Tr>
                ) : null}
              </Fragment>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
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

function GateRequirementRow({ requirement }: Readonly<{ requirement: GateRequirement }>) {
  const badge = gateRequirementBadges[requirement.state];

  return (
    <Table.Tr>
      <Table.Td>
        <Text fw={600} size="sm" style={{ overflowWrap: "anywhere" }}>
          {requirement.title ?? requirement.name}
        </Text>
        {requirement.title ? (
          <Text c="dimmed" size="xs" style={{ overflowWrap: "anywhere" }}>
            {requirement.name}
          </Text>
        ) : null}
      </Table.Td>
      <Table.Td>
        <Badge color={badge.color} variant="light">
          {badge.label}
        </Badge>
        {requirement.releasedAt ? (
          <Timestamp
            aria-label="Release time"
            c="dimmed"
            size="xs"
            value={requirement.releasedAt}
          />
        ) : null}
      </Table.Td>
      <Table.Td>
        {requirement.value === undefined ? (
          <Text c="dimmed" size="sm">
            No observation
          </Text>
        ) : (
          <Code>{requirement.value}</Code>
        )}
      </Table.Td>
      <Table.Td>{gateConditionLabel(requirement)}</Table.Td>
      <Table.Td>
        {requirement.evaluatedAt ? (
          <RelativeTime size="sm" value={requirement.evaluatedAt} />
        ) : (
          <Text c="dimmed" size="sm">
            Not evaluated
          </Text>
        )}
      </Table.Td>
    </Table.Tr>
  );
}
