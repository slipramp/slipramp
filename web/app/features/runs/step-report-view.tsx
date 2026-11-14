import { Anchor, Badge, Box, Group, Paper, Stack, Table, Text } from "@mantine/core";

import type { Artifact, StepReport } from "~/api/generated";
import { ReportDetailNotice } from "~/features/runs/run-report-detail-notice";
import type { ReportLoad } from "~/features/runs/run-reports";
import {
  formatStepDuration,
  flattenStepReportSteps,
  stepDurationPercentage,
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
  const content = (
    <Stack gap="md">
      <SectionHeader
        actions={
          canReadContents &&
          artifact.state === "sealed" &&
          artifact.retentionState === "retained" ? (
            <Anchor href={`/api/v1/artifacts/${artifact.id}/download`} size="sm">
              Download source JSON
            </Anchor>
          ) : null
        }
        title={detail?.title ?? "Step report"}
      />

      {report.summary.steps.total === 0 ? (
        <Text c="dimmed" size="sm">
          No leaf steps were reported.
        </Text>
      ) : null}

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

  return <StepTable report={report} />;
}

function StepTable({ report }: Readonly<{ report: StepReport }>) {
  const rows = flattenStepReportSteps(report.steps);
  const maximumDurationMs = Math.max(...rows.map(({ step }) => step.durationMs));

  return (
    <Table.ScrollContainer minWidth={620} type="native">
      <Table aria-label="Step report" highlightOnHover striped withRowBorders>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Step</Table.Th>
            <Table.Th miw={120}>Outcome</Table.Th>
            <Table.Th miw={190} ta="right">
              Duration
            </Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map(({ depth, key, step }) => (
            <Table.Tr key={key}>
              <Table.Th scope="row">
                <Text fw={depth === 0 ? 650 : 500} pl={depth * 20} size="sm" tt="none">
                  {step.name}
                </Text>
              </Table.Th>
              <Table.Td>
                <OutcomeBadge outcome={step.outcome} />
              </Table.Td>
              <Table.Td>
                <Group gap="sm" justify="flex-end" wrap="nowrap">
                  <DurationBar
                    durationMs={step.durationMs}
                    maximumDurationMs={maximumDurationMs}
                    outcome={step.outcome}
                  />
                  <Text c="dimmed" ff="monospace" miw={64} size="sm" ta="right">
                    {formatStepDuration(step.durationMs)}
                  </Text>
                </Group>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
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
      miw={72}
      style={{ borderRadius: 2, flex: 1 }}
    >
      <Box
        bg={`var(--mantine-color-${stepReportOutcomeColor(outcome)}-6)`}
        h="100%"
        style={{ borderRadius: 2, width: `${width}%` }}
      />
    </Box>
  );
}
