import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
import { IconSearch } from "@tabler/icons-react";
import { useDeferredValue, useMemo, useState } from "react";

import type { Artifact, PyinfraDeployReport, PyinfraOperation } from "~/api/generated";
import {
  filterPyinfraOperations,
  isPyinfraNoOp,
  previewPyinfraHosts,
  PYINFRA_HOST_PREVIEW_LIMIT,
  pyinfraPlanBadges,
  pyinfraResultBadges,
  type PyinfraCountBadge,
} from "~/features/runs/pyinfra-deploy-report";
import { ReportDetailNotice } from "~/features/runs/run-report-detail-notice";
import type { ReportLoad } from "~/features/runs/run-reports";
import { SectionHeader } from "~/shared/page";

export function PyinfraDeployReportView({
  artifact,
  canReadContents,
  load,
}: Readonly<{
  artifact: Artifact;
  canReadContents: boolean;
  load: ReportLoad;
}>) {
  const report = artifact.report;
  if (!report || report.kind !== "pyinfra-deploy") return null;

  const detail =
    load.state === "available" && load.detail.kind === "pyinfra-deploy" ? load.detail : undefined;
  const planBadges = pyinfraPlanBadges(report.summary.plan).filter(
    (badge) => badge.label !== "unchanged operations",
  );

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <SectionHeader
          actions={
            <Group align="center" gap="xs" style={{ alignSelf: "center" }} wrap="wrap">
              <Badge color={report.mode === "apply" ? "green" : "blue"} variant="light">
                {report.mode === "apply" ? "Apply" : "Preview"}
              </Badge>
              <Badge color="gray" variant="light">
                {report.tool}
                {report.toolVersion ? ` ${report.toolVersion}` : ""}
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
          title="pyinfra deploy report"
        />

        <SummaryBadges badges={planBadges} empty="No operation changes were planned." />
        {report.summary.results ? (
          <SummaryBadges
            badges={pyinfraResultBadges(report.summary.results)}
            empty="The apply completed without host changes or failures."
          />
        ) : null}

        <ReportDetailNotice state={load.state} />
        {detail ? <PyinfraDeployDetail report={detail} /> : null}
      </Stack>
    </Paper>
  );
}

function PyinfraDeployDetail({ report }: Readonly<{ report: PyinfraDeployReport }>) {
  const [keyword, setKeyword] = useState("");
  const [showNoOp, setShowNoOp] = useState(false);
  const deferredKeyword = useDeferredValue(keyword);
  const operations = useMemo(
    () =>
      filterPyinfraOperations(report.operations, {
        keyword: deferredKeyword,
        showNoOp,
      }),
    [deferredKeyword, report.operations, showNoOp],
  );
  const noOpRows = useMemo(
    () => report.operations.filter((operation) => isPyinfraNoOp(operation)).length,
    [report.operations],
  );

  return (
    <Stack gap="lg">
      {report.failedHosts.length > 0 ? (
        <Alert color="red" title="Failed hosts">
          <HostNames hosts={report.failedHosts} />
        </Alert>
      ) : null}
      <Stack gap={6}>
        <Group align="flex-end" gap="md" justify="space-between" wrap="wrap">
          <TextInput
            aria-label="Filter pyinfra operations by keyword"
            label="Filter operations"
            leftSection={<IconSearch aria-hidden size={15} />}
            onChange={(event) => setKeyword(event.currentTarget.value)}
            placeholder="Operation or host"
            style={{ flex: "1 1 24rem" }}
            value={keyword}
          />
          {noOpRows > 0 ? (
            <Switch
              checked={showNoOp}
              label={`Show no-op (${noOpRows.toLocaleString()})`}
              onChange={(event) => setShowNoOp(event.currentTarget.checked)}
            />
          ) : null}
        </Group>
        <Text c="dimmed" size="xs">
          Showing {operations.length.toLocaleString()} of{" "}
          {report.operations.length.toLocaleString()}
          {deferredKeyword.trim() ? " operations matching the keyword filter." : " operations."}
          {!showNoOp && noOpRows > 0 ? " No-op operations are hidden." : ""}
        </Text>
      </Stack>
      {operations.length === 0 ? (
        <Text c="dimmed" size="sm">
          {report.operations.length === 0
            ? "pyinfra reported no operations."
            : "No operations match the current filter."}
        </Text>
      ) : (
        <Table.ScrollContainer minWidth={900} type="native">
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Operation</Table.Th>
                <Table.Th>Planned changes</Table.Th>
                <Table.Th>Conditional changes</Table.Th>
                <Table.Th>{report.mode === "apply" ? "Result" : "Execution"}</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {operations.map((operation, index) => (
                <OperationRow key={`${operation.names.join(":")}:${index}`} operation={operation} />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  );
}

function OperationRow({ operation }: Readonly<{ operation: PyinfraOperation }>) {
  return (
    <Table.Tr>
      <Table.Td>
        <Stack gap={4}>
          {operation.names.map((name) => (
            <Code key={name}>{name}</Code>
          ))}
        </Stack>
      </Table.Td>
      <Table.Td>
        <HostOutcome color="yellow" hosts={operation.hostsWithChanges} label="planned" />
      </Table.Td>
      <Table.Td>
        <HostOutcome
          color="violet"
          hosts={operation.hostsWithConditionalChanges}
          label="conditional"
        />
      </Table.Td>
      <Table.Td>
        {operation.result ? (
          <Stack gap="xs">
            <HostOutcome color="green" hosts={operation.result.changedHosts} label="changed" />
            <HostOutcome color="red" hosts={operation.result.failedHosts} label="failed" />
            <HostOutcome color="gray" hosts={operation.result.unchangedHosts} label="unchanged" />
          </Stack>
        ) : (
          <Text c="dimmed" size="sm">
            Not executed in preview
          </Text>
        )}
      </Table.Td>
    </Table.Tr>
  );
}

function HostOutcome({
  color,
  hosts,
  label,
}: Readonly<{ color: string; hosts: string[]; label: string }>) {
  if (hosts.length === 0) return <Text c="dimmed">—</Text>;

  return (
    <Stack gap={4}>
      <Badge color={color} variant="light">
        {hosts.length.toLocaleString()} {label}
      </Badge>
      <HostNames hosts={hosts} />
    </Stack>
  );
}

function HostNames({ hosts }: Readonly<{ hosts: string[] }>) {
  const [expanded, setExpanded] = useState(false);
  const { hiddenCount, visibleHosts } = previewPyinfraHosts(hosts, expanded);
  const expandable = hosts.length > PYINFRA_HOST_PREVIEW_LIMIT;

  return (
    <Stack align="flex-start" gap={2}>
      <Text c="dimmed" size="xs" style={{ overflowWrap: "anywhere" }}>
        {visibleHosts.join(", ")}
      </Text>
      {expandable ? (
        <Button
          aria-expanded={expanded}
          onClick={() => setExpanded((value) => !value)}
          size="compact-xs"
          variant="subtle"
        >
          {expanded ? "Show less" : `Show ${hiddenCount.toLocaleString()} more`}
        </Button>
      ) : null}
    </Stack>
  );
}

function SummaryBadges({
  badges,
  empty,
}: Readonly<{ badges: PyinfraCountBadge[]; empty: string }>) {
  if (badges.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        {empty}
      </Text>
    );
  }

  return (
    <Group gap="xs" wrap="wrap">
      {badges.map((badge) => (
        <Badge color={badge.color} key={badge.label} variant="light">
          {badge.count.toLocaleString()} {badge.label}
        </Badge>
      ))}
    </Group>
  );
}
