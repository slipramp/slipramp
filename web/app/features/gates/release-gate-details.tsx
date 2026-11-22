import { Anchor, Badge, Button, Code, Group, Stack, Table, Text, Textarea } from "@mantine/core";
import { IconExternalLink } from "@tabler/icons-react";
import { Fragment } from "react";
import { Form, useNavigation } from "react-router";

import type { GateRequirement, GateSummary } from "~/api/generated";
import {
  gateCanBeOverridden,
  hasMatchingOverride,
  gateConditionLabel,
  gateEvidenceLabel,
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
              <Fragment key={requirement.key}>
                <GateRequirementRow
                  requirement={requirement}
                  showLastEvaluated={showLastEvaluated}
                />
                {requirement.overrides?.map((decision) => (
                  <Table.Tr key={`${decision.recordedAt}-${decision.version}`}>
                    <Table.Td colSpan={showLastEvaluated ? 5 : showEvidence ? 4 : 2}>
                      <Text size="sm">
                        {decision.actor.displayName} recorded an override
                        {requirement.origin === "server"
                          ? ` for version ${decision.version}`
                          : requirement.version && decision.version !== requirement.version
                            ? " for an earlier policy version"
                            : ""}{" "}
                        <RelativeTime size="sm" value={decision.recordedAt} />. {decision.reason}
                      </Text>
                      <Text size="xs" c="dimmed">
                        {gateRequirementBadges[decision.observedState].label} at decision time
                        {decision.observedEvidence
                          ? ` (${gateEvidenceLabel(decision.observedEvidence)})`
                          : ""}
                        {decision.diagnostic ? ` · ${decision.diagnostic}` : ""}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
                {canOverride && gates.state === "waiting" && gateCanBeOverridden(requirement) ? (
                  <Table.Tr key={requirement.version}>
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
            {requirement.origin === "server"
              ? `Policy version ${requirement.version}`
              : requirement.name}
          </Text>
        ) : null}
        {requirement.queryUrl ? (
          <Anchor
            href={requirement.queryUrl}
            rel="noreferrer"
            size="xs"
            target="_blank"
            aria-label={`Open in Prometheus for ${requirement.title ?? requirement.name}`}
          >
            <Group component="span" gap={4} wrap="nowrap">
              Open in Prometheus
              <IconExternalLink aria-hidden size={13} />
            </Group>
          </Anchor>
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
            {requirement.evidence === undefined ? (
              <Text c="dimmed" size="sm">
                {requirement.releasedAt && hasMatchingOverride(requirement)
                  ? "Operator override"
                  : "No observation"}
              </Text>
            ) : (
              <Stack gap={2} align="flex-start">
                <Code>{gateEvidenceLabel(requirement.evidence)}</Code>
                {"lock" in requirement.evidence ? (
                  <Text size="xs" c="dimmed">
                    Locked <RelativeTime value={requirement.evidence.lock.createdAt} />
                  </Text>
                ) : null}
                {"lock" in requirement.evidence && requirement.evidence.lock.expiresAt ? (
                  <Text size="xs" c="dimmed">
                    Lock expiry <RelativeTime value={requirement.evidence.lock.expiresAt} />
                  </Text>
                ) : null}
                {"window" in requirement.evidence &&
                requirement.evidence.window.observation.intervalStart ? (
                  <Text size="xs" c="dimmed">
                    Interval start{" "}
                    <RelativeTime value={requirement.evidence.window.observation.intervalStart} />
                  </Text>
                ) : null}
                {"window" in requirement.evidence &&
                requirement.evidence.window.observation.nextTransition ? (
                  <Text size="xs" c="dimmed">
                    Next transition{" "}
                    <RelativeTime value={requirement.evidence.window.observation.nextTransition} />
                  </Text>
                ) : null}
              </Stack>
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
          <input name="gateKey" type="hidden" value={requirement.key} />
          <input name="version" type="hidden" value={requirement.version} />
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
