import {
  Anchor,
  Badge,
  Box,
  Button,
  Code,
  Divider,
  Group,
  Stack,
  Text,
  Textarea,
  Title,
} from "@mantine/core";
import { IconExternalLink } from "@tabler/icons-react";
import { Fragment } from "react";
import { Form, useNavigation } from "react-router";

import type { GateOverride, GateRequirement, GateSummary } from "~/api/generated";
import {
  gateCanBeOverridden,
  hasMatchingOverride,
  gateConditionLabel,
  gateEvidenceFields,
  gateRequirementBadges,
  gateScopeDescription,
  gateSummaryBadges,
} from "~/features/gates/gate-presentation";
import { SectionHeader } from "~/shared/page";
import { Timestamp } from "~/shared/display-values";

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
  const summaryBadge = gateSummaryBadges[gates.state];
  return (
    <Stack component="section" gap="md">
      <SectionHeader
        title={subject === "Promotion" ? "Promotion gates" : "Release gates"}
        description={gateSummaryDescription(gates.state, subject)}
        actions={
          <Badge color={summaryBadge.color} variant="light">
            {summaryBadge.label}
          </Badge>
        }
      />
      {gates.requirements.map((gate, index) => {
        const matched = hasMatchingOverride(gate);
        const badge =
          matched && gate.state !== "stopped"
            ? {
                label: gate.releasedAt ? "Released by override" : "Override recorded",
                color: "orange",
              }
            : gateRequirementBadges[gate.state];
        return (
          <Fragment key={gate.key}>
            {index > 0 && <Divider />}
            <Stack component="section" gap="xs" style={{ overflowWrap: "anywhere" }}>
              <Group justify="space-between">
                <Title order={3} size="h5">
                  {gate.title ?? gate.name}
                </Title>
                <Badge color={badge.color} variant="light">
                  {badge.label}
                </Badge>
              </Group>
              <Text size="sm" c="dimmed">
                {gateScopeDescription(gate)}
                {gate.title && gate.origin === "repository" && <> · {gate.name}</>}
              </Text>
              {gate.version && (
                <Text size="xs">
                  Policy version <Code>{gate.version}</Code>
                </Text>
              )}
              {gate.state !== "stopped" && (
                <>
                  <Text size="sm">Release condition · {gateConditionLabel(gate)}</Text>
                  <Text size="sm" fw={600}>
                    {gate.releasedAt ? "Evidence at release" : "Latest observation"}
                  </Text>
                  {!gate.evidence && !gate.evaluatedAt && (
                    <Text size="sm" c="dimmed">
                      {gate.releasedAt && matched
                        ? "Released with an operator override."
                        : "No observation available."}
                    </Text>
                  )}
                  <EvidenceFields
                    fields={gateEvidenceFields(gate.evidence, gate.evaluatedAt, gate.validUntil)}
                  />
                  {gate.releasedAt && (
                    <Text size="sm">
                      Released <Timestamp value={gate.releasedAt} showSeconds />
                    </Text>
                  )}
                  {gate.diagnostic && <Text size="sm">{gate.diagnostic}</Text>}
                  {gate.queryUrl && (
                    <Anchor
                      href={gate.queryUrl}
                      rel="noreferrer"
                      size="sm"
                      target="_blank"
                      aria-label={`Open in Prometheus for ${gate.title ?? gate.name}`}
                    >
                      <Group component="span" gap={4}>
                        Open in Prometheus <IconExternalLink aria-hidden size={13} />
                      </Group>
                    </Anchor>
                  )}
                </>
              )}
              {gate.overrides?.map((decision) => (
                <OverrideDecision
                  key={decision.recordedAt + decision.version}
                  decision={decision}
                  earlierVersion={Boolean(gate.version && decision.version !== gate.version)}
                />
              ))}
              {canOverride && gates.state === "waiting" && gateCanBeOverridden(gate) && (
                <GateOverrideForm requirement={gate} subject={subject} subjectId={subjectId} />
              )}
            </Stack>
          </Fragment>
        );
      })}
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

function EvidenceFields({ fields }: Readonly<{ fields: ReturnType<typeof gateEvidenceFields> }>) {
  if (!fields.length) return null;
  return (
    <Box
      component="dl"
      m={0}
      style={{
        display: "grid",
        gridTemplateColumns: "minmax(6rem,max-content) minmax(0,1fr)",
        columnGap: "var(--mantine-spacing-md)",
        rowGap: 2,
      }}
    >
      {fields.map(({ label, value, format }) => (
        <Fragment key={label}>
          <Text component="dt" size="sm" c="dimmed">
            {label}
          </Text>
          <Text component="dd" m={0} size="sm">
            {format === "time" ? (
              <Timestamp value={value} showSeconds />
            ) : format === "code" ? (
              <Code>{value}</Code>
            ) : (
              value
            )}
          </Text>
        </Fragment>
      ))}
    </Box>
  );
}

function OverrideDecision({
  decision,
  earlierVersion,
}: Readonly<{ decision: GateOverride; earlierVersion: boolean }>) {
  return (
    <Stack gap={2}>
      <Text size="sm" fw={600}>
        Override by {decision.actor.displayName} ·{" "}
        <Timestamp value={decision.recordedAt} showSeconds />
      </Text>
      <Text size="sm">{decision.reason}</Text>
      <Text size="xs">
        {earlierVersion ? "Earlier policy version" : "Policy version"}{" "}
        <Code>{decision.version}</Code>
      </Text>
      <Text size="sm">{gateRequirementBadges[decision.observedState].label} at decision time</Text>
      <EvidenceFields
        fields={gateEvidenceFields(decision.observedEvidence, decision.evaluatedAt)}
      />
      {decision.diagnostic && <Text size="sm">{decision.diagnostic}</Text>}
    </Stack>
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
