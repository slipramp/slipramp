import {
  Anchor,
  Badge,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
import { IconSearch } from "@tabler/icons-react";
import { useDeferredValue, useMemo, useState } from "react";

import type {
  Artifact,
  InfrastructureResourceChange,
  OpenTofuPlanReport,
  OpenTofuPlanReportSummary,
  RunReport,
  RunReportSummary,
  TerraformPlanReport,
  TerraformPlanReportSummary,
} from "~/api/generated";
import { SectionHeader } from "~/shared/page";
import {
  filterInfrastructureResources,
  groupInfrastructureResources,
  infrastructureActionBadges,
  infrastructureCheckBadges,
  type CountBadge,
} from "~/features/runs/terraform-plan-report";
import type { ReportLoad } from "~/features/runs/run-reports";
import { ReportDetailNotice } from "~/features/runs/run-report-detail-notice";

type InfrastructurePlanDetailReport = OpenTofuPlanReport | TerraformPlanReport;
type InfrastructurePlanSummaryReport = OpenTofuPlanReportSummary | TerraformPlanReportSummary;

export function InfrastructurePlanReportView({
  artifact,
  canReadContents,
  load,
}: Readonly<{
  artifact: Artifact;
  canReadContents: boolean;
  load: ReportLoad;
}>) {
  const report = artifact.report;
  if (!report || !isInfrastructurePlanSummaryReport(report)) return null;

  const detail =
    load.state === "available" && isInfrastructurePlanDetailReport(load.detail)
      ? load.detail
      : undefined;
  const summaryBadges = infrastructureActionBadges(report.summary.resourceChanges).filter(
    (badge) => badge.label !== "No-op",
  );
  const toolName = infrastructureToolName(report.tool);

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <SectionHeader
          actions={
            <Group align="center" gap="xs" style={{ alignSelf: "center" }} wrap="wrap">
              <BooleanFact label="Applyable" value={report.summary.applyable} />
              <BooleanFact label="Complete" value={report.summary.complete} />
              <BooleanFact label="Errored" negative value={report.summary.errored} />
              <Badge color="gray" variant="light">
                {toolName}
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
          title={`${toolName} plan report`}
        />

        {summaryBadges.length > 0 || report.summary.driftedResources > 0 ? (
          <Group gap="xs" wrap="wrap">
            {summaryBadges.map((badge) => (
              <CountBadgeView badge={badge} key={badge.label} />
            ))}
            {report.summary.driftedResources > 0 ? (
              <Badge color="grape" variant="light">
                {report.summary.driftedResources} drifted
              </Badge>
            ) : null}
          </Group>
        ) : null}

        <ReportDetailNotice state={load.state} />

        {detail ? <InfrastructurePlanDetail report={detail} /> : null}
      </Stack>
    </Paper>
  );
}

function InfrastructurePlanDetail({
  report,
}: Readonly<{ report: InfrastructurePlanDetailReport }>) {
  const [keyword, setKeyword] = useState("");
  const [showNoOp, setShowNoOp] = useState(false);
  const deferredKeyword = useDeferredValue(keyword);
  const resourceChanges = useMemo(
    () =>
      filterInfrastructureResources(report.resourceChanges, {
        keyword: deferredKeyword,
        showNoOp,
      }),
    [deferredKeyword, report.resourceChanges, showNoOp],
  );
  const resourceDrift = useMemo(
    () =>
      filterInfrastructureResources(report.resourceDrift, {
        keyword: deferredKeyword,
        showNoOp,
      }),
    [deferredKeyword, report.resourceDrift, showNoOp],
  );
  const totalRows = report.resourceChanges.length + report.resourceDrift.length;
  const visibleRows = resourceChanges.length + resourceDrift.length;
  const noOpRows = useMemo(
    () =>
      [...report.resourceChanges, ...report.resourceDrift].filter(
        (resource) => resource.action === "no-op",
      ).length,
    [report.resourceChanges, report.resourceDrift],
  );

  return (
    <Stack gap="lg">
      <Stack gap={6}>
        <Group align="flex-end" gap="md" justify="space-between" wrap="wrap">
          <TextInput
            aria-label={`Filter ${infrastructureToolName(report.tool)} resources by keyword`}
            label="Filter resources"
            leftSection={<IconSearch aria-hidden size={15} />}
            onChange={(event) => setKeyword(event.currentTarget.value)}
            placeholder="Address, type, provider, or reason"
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
          Showing {visibleRows.toLocaleString()} of {totalRows.toLocaleString()} resource rows
          {deferredKeyword.trim() ? " matching the keyword filter" : ""}.
          {!showNoOp && noOpRows > 0 ? " No-op resources are hidden." : ""}
        </Text>
      </Stack>
      <ResourceSection
        changes={resourceChanges}
        empty={
          report.resourceChanges.length === 0
            ? `${infrastructureToolName(report.tool)} reported no resource changes.`
            : "No resource changes match the current filters."
        }
        title="Resource changes"
        total={report.resourceChanges.length}
      />
      {report.summary.driftedResources > 0 || report.resourceDrift.length > 0 ? (
        <ResourceSection
          changes={resourceDrift}
          empty={
            report.resourceDrift.length === 0
              ? `${infrastructureToolName(report.tool)} counted drift, but no drift detail was projected.`
              : "No resource drift matches the current filters."
          }
          title="Resource drift"
          total={report.resourceDrift.length}
        />
      ) : null}
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
        <AggregateSection
          badges={infrastructureCheckBadges(report.summary.checks)}
          empty="No checks were reported."
          title="Checks"
          total={report.summary.checks.total}
        />
        <AggregateSection
          badges={infrastructureActionBadges(report.summary.outputChanges)}
          empty="No output changes were reported."
          title="Output changes"
          total={report.summary.outputChanges.total}
        />
      </SimpleGrid>
    </Stack>
  );
}

function ResourceSection({
  changes,
  empty,
  title,
  total,
}: Readonly<{
  changes: InfrastructureResourceChange[];
  empty: string;
  title: string;
  total: number;
}>) {
  return (
    <Stack gap="sm">
      <Group justify="space-between" wrap="wrap">
        <Text fw={600} size="lg">
          {title}
        </Text>
        <Badge variant="light">
          {changes.length === total
            ? total.toLocaleString()
            : `${changes.length.toLocaleString()} of ${total.toLocaleString()}`}
        </Badge>
      </Group>
      {changes.length === 0 ? (
        <Text c="dimmed" size="sm">
          {empty}
        </Text>
      ) : (
        <Table.ScrollContainer minWidth={860} type="native">
          <Table verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Address</Table.Th>
                <Table.Th>Type</Table.Th>
                <Table.Th>Action</Table.Th>
                <Table.Th>Replacement</Table.Th>
                <Table.Th>Reason</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {groupInfrastructureResources(changes).map((group) => (
                <ResourceGroup
                  key={group.moduleAddress ?? "root"}
                  moduleAddress={group.moduleAddress}
                  resources={group.resources}
                />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  );
}

function ResourceGroup({
  moduleAddress,
  resources,
}: Readonly<{ moduleAddress?: string; resources: InfrastructureResourceChange[] }>) {
  return (
    <>
      <Table.Tr>
        <Table.Td colSpan={5}>
          <Text c="dimmed" fw={600} size="xs" tt="uppercase">
            {moduleAddress ?? "Root module"}
          </Text>
        </Table.Td>
      </Table.Tr>
      {resources.map((resource) => (
        <Table.Tr
          key={`${resource.address}:${resource.replacedObjectKey ? `replaced:${resource.replacedObjectKey}` : "current"}`}
        >
          <Table.Td>
            <Code>{resource.address}</Code>
            {resource.replacedObjectKey ? (
              <Text c="dimmed" size="xs">
                Replaced object <Code>{resource.replacedObjectKey}</Code>
              </Text>
            ) : null}
            {resource.previousAddress ? (
              <Text c="dimmed" size="xs">
                Previously <Code>{resource.previousAddress}</Code>
              </Text>
            ) : null}
          </Table.Td>
          <Table.Td>
            <Text>{resource.type}</Text>
            <Text c="dimmed" size="xs">
              {resource.mode} · {resource.providerName}
            </Text>
          </Table.Td>
          <Table.Td>
            <ActionBadge action={resource.action} />
          </Table.Td>
          <Table.Td>{replacementLabel(resource.replacementOrder)}</Table.Td>
          <Table.Td>{resource.actionReason ?? "—"}</Table.Td>
        </Table.Tr>
      ))}
    </>
  );
}

function AggregateSection({
  badges,
  empty,
  title,
  total,
}: Readonly<{ badges: CountBadge[]; empty: string; title: string; total: number }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Group justify="space-between">
          <Text fw={600}>{title}</Text>
          <Badge variant="light">{total}</Badge>
        </Group>
        {total === 0 ? (
          <Text c="dimmed" size="sm">
            {empty}
          </Text>
        ) : (
          <Group gap="xs" wrap="wrap">
            {badges.map((badge) => (
              <CountBadgeView badge={badge} key={badge.label} />
            ))}
          </Group>
        )}
      </Stack>
    </Paper>
  );
}

function BooleanFact({
  label,
  negative = false,
  value,
}: Readonly<{ label: string; negative?: boolean; value?: boolean }>) {
  const color = value === undefined ? "gray" : value === negative ? "red" : "green";
  const display = value === undefined ? "Not reported" : value ? "Yes" : "No";

  return (
    <Badge color={color} variant="light">
      {label}: {display}
    </Badge>
  );
}

function CountBadgeView({ badge }: Readonly<{ badge: CountBadge }>) {
  return (
    <Badge color={badge.color} variant="light">
      {badge.count} {badge.label}
    </Badge>
  );
}

function ActionBadge({ action }: Readonly<{ action: InfrastructureResourceChange["action"] }>) {
  const color = {
    create: "green",
    delete: "red",
    forget: "grape",
    "no-op": "gray",
    read: "blue",
    replace: "orange",
    update: "yellow",
  }[action];

  return (
    <Badge color={color} variant="light">
      {sentenceCase(action)}
    </Badge>
  );
}

function replacementLabel(order: InfrastructureResourceChange["replacementOrder"]) {
  switch (order) {
    case "create-before-destroy":
      return "Create before destroy";
    case "destroy-before-create":
      return "Destroy before create";
    default:
      return "—";
  }
}

function sentenceCase(value: string) {
  return `${value.slice(0, 1).toLocaleUpperCase()}${value.slice(1)}`;
}

function isInfrastructurePlanSummaryReport(
  report: RunReportSummary,
): report is InfrastructurePlanSummaryReport {
  return report.kind === "opentofu-plan" || report.kind === "terraform-plan";
}

function isInfrastructurePlanDetailReport(
  report: RunReport,
): report is InfrastructurePlanDetailReport {
  return report.kind === "opentofu-plan" || report.kind === "terraform-plan";
}

function infrastructureToolName(tool: string) {
  return tool === "opentofu" ? "OpenTofu" : sentenceCase(tool);
}
