import {
  Alert,
  Anchor,
  Badge,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
} from "@mantine/core";
import type { ReactNode } from "react";

import type { Artifact, Run, TerraformPlanReport, TerraformResourceChange } from "~/api/generated";
import { SectionHeader } from "~/components/page";
import { Timestamp } from "~/components/display";
import {
  groupTerraformResources,
  terraformActionBadges,
  terraformCheckBadges,
  type CountBadge,
} from "~/terraform-plan-report";
import type { ReportLoad } from "~/run-reports";
import { runStatusColor, runStatusLabel } from "~/run-status";

export function TerraformPlanReportView({
  artifact,
  canReadContents,
  load,
  run,
}: Readonly<{
  artifact: Artifact;
  canReadContents: boolean;
  load: ReportLoad;
  run: Run;
}>) {
  const report = artifact.report;
  if (!report || report.kind !== "terraform-plan") return null;

  const detail =
    load.state === "available" && load.detail.kind === "terraform-plan" ? load.detail : undefined;
  const summaryBadges = terraformActionBadges(report.summary.resourceChanges);

  return (
    <Paper p="md" withBorder>
      <Stack gap="lg">
        <SectionHeader
          actions={
            canReadContents &&
            artifact.state === "sealed" &&
            artifact.retentionState === "retained" ? (
              <Anchor href={`/api/v1/artifacts/${artifact.id}/download`} size="sm">
                Download source JSON
              </Anchor>
            ) : undefined
          }
          description={
            <>
              Structured, value-free preview produced from {report.tool}
              {report.toolVersion ? ` ${report.toolVersion}` : ""}. Report facts are descriptive and
              do not determine the Run outcome.
            </>
          }
          title="Terraform plan report"
        />

        <Group gap="xs" wrap="wrap">
          <Badge variant="light">
            {report.summary.resourceChanges.total}{" "}
            {report.summary.resourceChanges.total === 1 ? "resource change" : "resource changes"}
          </Badge>
          {summaryBadges.map((badge) => (
            <CountBadgeView badge={badge} key={badge.label} />
          ))}
          {report.summary.driftedResources > 0 ? (
            <Badge color="grape" variant="light">
              {report.summary.driftedResources} drifted
            </Badge>
          ) : null}
        </Group>

        <SimpleGrid cols={{ base: 1, md: 3 }} spacing="sm">
          <ReportFact label="Run outcome">
            <Badge color={runStatusColor(run)} variant="light">
              {runStatusLabel(run)}
            </Badge>
            <Text c="dimmed" size="xs">
              Authoritative execution result
            </Text>
          </ReportFact>
          <ReportFact label="Report status">
            <Group gap="xs" wrap="wrap">
              <BooleanFact label="Applyable" value={report.summary.applyable} />
              <BooleanFact label="Complete" value={report.summary.complete} />
              <BooleanFact label="Errored" negative value={report.summary.errored} />
            </Group>
          </ReportFact>
          <ReportFact label="Source Artifact">
            <Group gap="xs" wrap="wrap">
              <Badge color={artifact.state === "sealed" ? "green" : "gray"} variant="light">
                {sentenceCase(artifact.state)}
              </Badge>
              <Badge
                color={artifact.retentionState === "retained" ? "blue" : "orange"}
                variant="light"
              >
                {sentenceCase(artifact.retentionState)}
              </Badge>
            </Group>
            <Text c="dimmed" size="xs">
              Projection schema {report.schemaVersion} · adapter {report.adapterVersion} · created{" "}
              <Timestamp inherit value={report.createdAt} />
            </Text>
          </ReportFact>
        </SimpleGrid>

        <ReportDetailNotice state={load.state} />

        {detail ? <TerraformPlanDetail report={detail} /> : null}
      </Stack>
    </Paper>
  );
}

function TerraformPlanDetail({ report }: Readonly<{ report: TerraformPlanReport }>) {
  return (
    <Stack gap="lg">
      <ResourceSection
        changes={report.resourceChanges}
        empty="Terraform reported no resource changes."
        title="Resource changes"
      />
      {report.summary.driftedResources > 0 || report.resourceDrift.length > 0 ? (
        <ResourceSection
          changes={report.resourceDrift}
          empty="Terraform counted drift, but no drift detail was projected."
          title="Resource drift"
        />
      ) : null}
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
        <AggregateSection
          badges={terraformCheckBadges(report.summary.checks)}
          empty="No checks were reported."
          title="Checks"
          total={report.summary.checks.total}
        />
        <AggregateSection
          badges={terraformActionBadges(report.summary.outputChanges)}
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
}: Readonly<{ changes: TerraformResourceChange[]; empty: string; title: string }>) {
  return (
    <Stack gap="sm">
      <Group justify="space-between" wrap="wrap">
        <Text fw={600} size="lg">
          {title}
        </Text>
        <Badge variant="light">{changes.length}</Badge>
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
              {groupTerraformResources(changes).map((group) => (
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
}: Readonly<{ moduleAddress?: string; resources: TerraformResourceChange[] }>) {
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

function ReportFact({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Paper p="sm" withBorder>
      <Stack gap={6}>
        <Text c="dimmed" fw={600} size="xs" tt="uppercase">
          {label}
        </Text>
        {children}
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

function ActionBadge({ action }: Readonly<{ action: TerraformResourceChange["action"] }>) {
  const color = {
    create: "green",
    delete: "red",
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

function ReportDetailNotice({ state }: Readonly<{ state: ReportLoad["state"] }>) {
  switch (state) {
    case "available":
      return null;
    case "restricted":
      return (
        <Alert color="yellow" title="Report detail restricted">
          The aggregate summary is visible, but an operator or administrator is required to read
          this sensitive report or download its source Artifact.
        </Alert>
      );
    case "expired":
      return (
        <Alert color="orange" title="Report detail expired">
          The durable summary remains available, but the normalized detail and source Artifact have
          expired.
        </Alert>
      );
    case "deleted":
      return (
        <Alert color="orange" title="Report content deleted">
          The durable summary remains available, but the normalized detail and source Artifact have
          been deleted.
        </Alert>
      );
    case "unavailable":
      return (
        <Alert color="orange" title="Report detail unavailable">
          The report projection is no longer available. Its durable aggregate summary is shown.
        </Alert>
      );
    case "error":
      return (
        <Alert color="red" title="Report detail could not be loaded">
          Slipramp could not read the normalized report projection. Try refreshing this page.
        </Alert>
      );
  }
}

function replacementLabel(order: TerraformResourceChange["replacementOrder"]) {
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
