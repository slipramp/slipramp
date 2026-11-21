import { Badge, Button, Code, Stack, Table, Text, Textarea } from "@mantine/core";
import { Fragment } from "react";
import { Form, useNavigation } from "react-router";

import type { GateRequirement, GateSummary } from "~/api/generated";
import {
  gateCanBeOverridden,
  hasMatchingOverride,
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
  subjectId,
  canOverride,
}: Readonly<{
  gates: GateSummary;
  subject: GateSubject;
  subjectId: string;
  canOverride: boolean;
}>) {
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
                {requirement.overrides?.map((decision) => (
                  <Table.Tr key={`${decision.recordedAt}-${decision.definitionDigest}`}>
                    <Table.Td colSpan={showLastEvaluated ? 5 : showEvidence ? 4 : 2}>
                      <Text size="sm">
                        {decision.actor.displayName} recorded an override
                        {requirement.definitionDigest &&
                        decision.definitionDigest !== requirement.definitionDigest
                          ? " for an earlier Gate definition"
                          : ""}{" "}
                        <RelativeTime size="sm" value={decision.recordedAt} />. {decision.reason}
                      </Text>
                      <Text size="xs" c="dimmed">
                        {gateRequirementBadges[decision.observedState].label} at decision time
                        {decision.observedValue !== undefined ? ` (${decision.observedValue})` : ""}
                        {decision.diagnostic ? ` · ${decision.diagnostic}` : ""}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
                {canOverride && gates.state === "waiting" && gateCanBeOverridden(requirement) ? (
                  <Table.Tr key={requirement.definitionDigest}>
                    <Table.Td colSpan={5}>
                      <GateOverrideForm
                        requirement={requirement}
                        subject={subject}
                        subjectId={subjectId}
                      />
                    </Table.Td>
                  </Table.Tr>
                ) : null}
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
  if (state === "stopped")
    return `Cancellation ended the gate wait before this ${subject} was released.`;
  if (state === "waiting")
    return `Each Gate needs a fresh pass or an override before this ${subject} can proceed.`;
  return undefined;
}

function GateRequirementRow({
  requirement,
  showLastEvaluated,
}: Readonly<{ requirement: GateRequirement; showLastEvaluated: boolean }>) {
  const badge = hasMatchingOverride(requirement)
    ? {
        label: requirement.releasedAt ? "Released by override" : "Override recorded",
        color: "orange",
      }
    : gateRequirementBadges[requirement.state];

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
                {requirement.releasedAt && hasMatchingOverride(requirement)
                  ? "Operator override"
                  : "No observation"}
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

function GateOverrideForm({
  requirement,
  subject,
  subjectId,
}: Readonly<{
  requirement: GateRequirement;
  subject: GateSubject;
  subjectId: string;
}>) {
  const navigation = useNavigation();
  return (
    <details>
      <summary>Override gate</summary>
      <Form method="post">
        <Stack align="flex-start" gap="xs" mt="xs">
          <input name="intent" type="hidden" value="override-gate" />
          <input name="gateName" type="hidden" value={requirement.name} />
          <input name="definitionDigest" type="hidden" value={requirement.definitionDigest} />
          <Textarea
            required
            maxLength={1024}
            name="reason"
            label="Reason for override"
            description={`Waive ${requirement.name} for ${subject} ${subjectId.slice(0, 8)}. It can proceed once its other requirements are met.`}
          />
          <Button
            color="orange"
            type="submit"
            size="compact-md"
            disabled={navigation.state !== "idle"}
          >
            Record override
          </Button>
        </Stack>
      </Form>
    </details>
  );
}
