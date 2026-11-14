import { Anchor, Badge, Box, Group, Paper, Stack, Text } from "@mantine/core";

import type { Artifact, StepReport, StepReportStep } from "~/api/generated";
import { ReportDetailNotice } from "~/features/runs/run-report-detail-notice";
import type { ReportLoad } from "~/features/runs/run-reports";
import {
  formatStepDuration,
  stepDurationPercentage,
  stepReportCountBadges,
  stepReportOutcomeColor,
  stepReportOutcomeLabel,
  type StepReportOutcomeValue,
} from "~/features/runs/step-report";
import { SectionHeader } from "~/shared/page";

export function StepReportView({
  artifact,
  canReadContents,
  embedded = false,
  load,
}: Readonly<{
  artifact: Artifact;
  canReadContents: boolean;
  embedded?: boolean;
  load: ReportLoad;
}>) {
  const report = artifact.report;
  if (!report || report.kind !== "step-report") return null;

  const detail =
    load.state === "available" && load.detail.kind === "step-report" ? load.detail : undefined;
  const badges = stepReportCountBadges(report.summary.steps);
  const content = (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Group align="center" gap="xs" style={{ alignSelf: "center" }} wrap="wrap">
            <Badge color="gray" variant="light">
              {report.tool} · {report.mode}
            </Badge>
            {canReadContents &&
            artifact.state === "sealed" &&
            artifact.retentionState === "retained" ? (
              <Anchor href={`/api/v1/artifacts/${artifact.id}/download`} size="sm">
                Download source JSON
              </Anchor>
            ) : null}
          </Group>
        }
        title={detail?.title ?? "Step report"}
      />

      <Stack gap="xs">
        <Group aria-label="Step report summary" component="section" gap="xs" wrap="wrap">
          <OutcomeBadge outcome={report.summary.outcome} />
          <Badge color="blue" variant="light">
            {formatStepDuration(report.summary.durationMs)} total
          </Badge>
          {badges.map((badge) => (
            <Badge color={badge.color} key={badge.outcome} variant="light">
              {badge.count.toLocaleString()} {badge.label.toLocaleLowerCase()}
            </Badge>
          ))}
        </Group>
        {report.summary.steps.total === 0 ? (
          <Text c="dimmed" size="sm">
            No leaf steps were reported.
          </Text>
        ) : null}
      </Stack>

      <ReportDetailNotice state={load.state} />
      {detail ? <StepReportDetail report={detail} /> : null}
    </Stack>
  );

  return embedded ? (
    content
  ) : (
    <Paper p="md" withBorder>
      {content}
    </Paper>
  );
}

function StepReportDetail({ report }: Readonly<{ report: StepReport }>) {
  if (report.steps.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        This report contains no step detail.
      </Text>
    );
  }

  return <StepList steps={report.steps} />;
}

function StepList({
  steps,
  nested = false,
}: Readonly<{ steps: StepReportStep[]; nested?: boolean }>) {
  const maximumDurationMs = Math.max(...steps.map((step) => step.durationMs));

  return (
    <Stack
      component="ol"
      gap={nested ? "xs" : "sm"}
      m={0}
      pl={nested ? "md" : 0}
      style={{ listStyle: "none" }}
    >
      {steps.map((step) => (
        <Box component="li" key={step.id}>
          <Paper p={nested ? "xs" : "sm"} withBorder>
            <Group align="flex-start" justify="space-between" wrap="nowrap">
              <Group gap="xs" wrap="wrap">
                <Text fw={nested ? 550 : 650} size={nested ? "sm" : undefined}>
                  {step.name}
                </Text>
                <OutcomeBadge outcome={step.outcome} />
              </Group>
              <Text c="dimmed" ff="monospace" size="sm">
                {formatStepDuration(step.durationMs)}
              </Text>
            </Group>

            <DurationBar
              durationMs={step.durationMs}
              maximumDurationMs={maximumDurationMs}
              outcome={step.outcome}
            />

            {step.steps && step.steps.length > 0 ? (
              <Box mt="sm">
                <StepList nested steps={step.steps} />
              </Box>
            ) : null}
          </Paper>
        </Box>
      ))}
    </Stack>
  );
}

function OutcomeBadge({ outcome }: Readonly<{ outcome: StepReportOutcomeValue }>) {
  return (
    <Badge color={stepReportOutcomeColor(outcome)} size="sm" variant="light">
      {stepReportOutcomeLabel(outcome)}
    </Badge>
  );
}

function DurationBar({
  durationMs,
  maximumDurationMs,
  outcome,
}: Readonly<{
  durationMs: number;
  maximumDurationMs: number;
  outcome: StepReportOutcomeValue;
}>) {
  const width = stepDurationPercentage(durationMs, maximumDurationMs);
  if (width === 0) return null;

  return (
    <Box
      aria-hidden
      bg="var(--mantine-color-default-hover)"
      h={4}
      mt={8}
      style={{ borderRadius: 2 }}
    >
      <Box
        bg={`var(--mantine-color-${stepReportOutcomeColor(outcome)}-6)`}
        h="100%"
        style={{ borderRadius: 2, width: `${width}%` }}
      />
    </Box>
  );
}
