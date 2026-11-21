import { Badge, Code, Stack, Table, Text } from "@mantine/core";
import { Fragment } from "react";

import type { GateRequirement, GateSummary } from "~/api/generated";
import {
  gateConditionLabel,
  gateRequirementBadges,
  gateSummaryBadges,
} from "~/features/gates/gate-presentation";
import { SectionHeader } from "~/shared/page";
import { RelativeTime } from "~/shared/display-values";

type GateSubject = "Promotion" | "Run";

export function ReleaseGateDetails({
  gates,
  subject,
}: Readonly<{ gates: GateSummary; subject: GateSubject }>) {
  if (gates.state === "not-required") return null;

  const badge = gateSummaryBadges[gates.state];
  const description = gateSummaryDescription(gates.state, subject);
  const showLastEvaluated = gates.state === "waiting";
  const showEvidence = gates.state !== "stopped";

  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Badge color={badge.color} variant="light">
            {badge.label}
          </Badge>
        }
        description={description}
        title={subject === "Promotion" ? "Promotion gates" : "Release gates"}
      />
      <Table.ScrollContainer
        minWidth={showLastEvaluated ? 600 : showEvidence ? 480 : 260}
        type="native"
      >
        <Table aria-label="Gate evaluations" verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Gate</Table.Th>
              <Table.Th>Status</Table.Th>
              {showEvidence && (
                <>
                  <Table.Th>{gates.state === "released" ? "Value at release" : "Current"}</Table.Th>
                  <Table.Th>Expected</Table.Th>
                </>
              )}
              {showLastEvaluated ? <Table.Th>Last evaluated</Table.Th> : null}
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {gates.requirements.map((requirement) => (
              <Fragment key={requirement.name}>
                <GateRequirementRow
                  requirement={requirement}
                  showLastEvaluated={showLastEvaluated}
                />
                {requirement.diagnostic ? (
                  <Table.Tr>
                    <Table.Td colSpan={showLastEvaluated ? 5 : 4}>
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
  if (subject === "Promotion") {
    return state === "released"
      ? "Every required gate passed when this Promotion was released."
      : undefined;
  }
  if (state === "stopped") return "Cancellation ended the gate wait before this Run was released.";
  if (state === "waiting") {
    return "Run assignment requires every attached Gate to pass in the same release check.";
  }
  return "Every required Gate passed together when this Run was released for assignment.";
}

function GateRequirementRow({
  requirement,
  showLastEvaluated,
}: Readonly<{ requirement: GateRequirement; showLastEvaluated: boolean }>) {
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
        <Stack align="flex-start" gap={2}>
          <Badge color={badge.color} variant="light">
            {badge.label}
          </Badge>
          {requirement.releasedAt ? (
            <RelativeTime c="dimmed" size="xs" value={requirement.releasedAt} />
          ) : null}
        </Stack>
      </Table.Td>
      {requirement.state !== "stopped" && (
        <>
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
        </>
      )}
      {showLastEvaluated ? (
        <Table.Td>
          {requirement.evaluatedAt ? (
            <RelativeTime size="sm" value={requirement.evaluatedAt} />
          ) : (
            <Text c="dimmed" size="sm">
              Not evaluated
            </Text>
          )}
        </Table.Td>
      ) : null}
    </Table.Tr>
  );
}
