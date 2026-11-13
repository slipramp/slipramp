import {
  ActionIcon,
  Alert,
  Anchor,
  Badge,
  Code,
  Group,
  Paper,
  Select,
  Spoiler,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Tooltip,
  UnstyledButton,
} from "@mantine/core";
import { IconChevronDown, IconChevronRight, IconSearch, IconTerminal2 } from "@tabler/icons-react";
import { useDeferredValue, useMemo, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";

import type { Artifact, PyinfraDeployReport, PyinfraOperation } from "~/api/generated";
import {
  filterPyinfraOperations,
  isPyinfraNoOp,
  pyinfraHosts,
  pyinfraPlanBadges,
  pyinfraResultBadges,
  type PyinfraCountBadge,
  type PyinfraOperationScope,
} from "~/features/runs/pyinfra-deploy-report";
import {
  runDetailSearchParamNames,
  updateRunDetailSearchParams,
} from "~/features/runs/run-detail-url-state";
import { ReportDetailNotice } from "~/features/runs/run-report-detail-notice";
import type { ReportLoad } from "~/features/runs/run-reports";
import {
  runReportSearchParam,
  runReportSearchParamNames,
  updateRunReportSearchParams,
} from "~/features/runs/run-report-url-state";
import { SectionHeader } from "~/shared/page";

const pyinfraScopes: PyinfraOperationScope[] = [
  "all",
  "attention",
  "planned",
  "observed",
  "failed",
];

const pyinfraScopeOptions = [
  { label: "All operations", value: "all" },
  { label: "Needs attention", value: "attention" },
  { label: "Planned changes", value: "planned" },
  { label: "Observed changes", value: "observed" },
  { label: "Failures", value: "failed" },
];

export function PyinfraDeployReportView({
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
  if (!report || report.kind !== "pyinfra-deploy") return null;

  const detail =
    load.state === "available" && load.detail.kind === "pyinfra-deploy" ? load.detail : undefined;
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
        title="pyinfra deploy report"
      />

      <ReportSummary report={report} />
      <ReportDetailNotice state={load.state} />
      {detail ? <PyinfraDeployDetail artifactId={artifact.id} report={detail} /> : null}
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

function ReportSummary({ report }: Readonly<{ report: NonNullable<Artifact["report"]> }>) {
  if (report.kind !== "pyinfra-deploy") return null;
  const planBadges = pyinfraPlanBadges(report.summary.plan);
  const resultBadges = report.summary.results
    ? pyinfraResultBadges(report.summary.results)
    : undefined;
  const entirelyUnchanged =
    report.summary.plan.changedOperations === 0 &&
    (report.summary.results?.changedHostOperations ?? 0) === 0 &&
    (report.summary.results?.failedHostOperations ?? 0) === 0;

  return (
    <Stack gap="xs">
      {entirelyUnchanged ? (
        <Alert color="green" title="No changes across this Run">
          Every reported operation was unchanged. All operations remain available for inspection.
        </Alert>
      ) : null}
      <SummaryBadges badges={planBadges} empty="No operation changes were planned." label="Plan" />
      {resultBadges ? (
        <SummaryBadges
          badges={resultBadges}
          empty="The apply completed without host changes or failures."
          label="Observed"
        />
      ) : (
        <Text c="dimmed" size="sm">
          Execution results are not available for a preview.
        </Text>
      )}
    </Stack>
  );
}

function PyinfraDeployDetail({
  artifactId,
  report,
}: Readonly<{ artifactId: string; report: PyinfraDeployReport }>) {
  const [searchParams, setSearchParams] = useSearchParams();
  const keyword =
    runReportSearchParam(searchParams, artifactId, runReportSearchParamNames.query) ?? "";
  const host = runReportSearchParam(searchParams, artifactId, runReportSearchParamNames.host) ?? "";
  const expandedOperation = Number(
    runReportSearchParam(searchParams, artifactId, runReportSearchParamNames.operation),
  );
  const allNoOp = report.operations.every((operation) => isPyinfraNoOp(operation));
  const defaultScope: PyinfraOperationScope = allNoOp ? "all" : "attention";
  const requestedScope = runReportSearchParam(
    searchParams,
    artifactId,
    runReportSearchParamNames.scope,
  );
  const scope = pyinfraScopes.includes(requestedScope as PyinfraOperationScope)
    ? (requestedScope as PyinfraOperationScope)
    : defaultScope;
  const requestedShowNoOp =
    runReportSearchParam(searchParams, artifactId, runReportSearchParamNames.showNoOp) === "true";
  const showNoOp = allNoOp || requestedShowNoOp;
  const deferredKeyword = useDeferredValue(keyword);
  const hosts = useMemo(() => pyinfraHosts(report.operations), [report.operations]);
  const operations = useMemo(() => {
    const filtered = new Set(
      filterPyinfraOperations(report.operations, {
        host,
        keyword: deferredKeyword,
        scope,
        showNoOp,
      }),
    );
    return report.operations
      .map((operation, index) => ({ index, operation }))
      .filter(({ operation }) => filtered.has(operation));
  }, [deferredKeyword, host, report.operations, scope, showNoOp]);
  const unchangedOperations = useMemo(
    () => report.operations.filter((operation) => isPyinfraNoOp(operation)).length,
    [report.operations],
  );

  function updateFilter(
    updates: Parameters<typeof updateRunReportSearchParams>[2],
    replace = true,
  ) {
    setSearchParams(updateRunReportSearchParams(searchParams, artifactId, updates), {
      preventScrollReset: true,
      replace,
    });
  }

  return (
    <Stack gap="md">
      {report.failedHosts.length > 0 ? (
        <Alert color="red" title="Failed hosts">
          <HostTags
            hosts={report.failedHosts}
            onSelect={(selectedHost) =>
              updateFilter(
                {
                  [runReportSearchParamNames.host]: selectedHost,
                  [runReportSearchParamNames.operation]: undefined,
                  [runReportSearchParamNames.scope]: "all",
                },
                false,
              )
            }
          />
        </Alert>
      ) : null}

      <Group align="flex-end" gap="sm" wrap="wrap">
        <TextInput
          aria-label="Filter pyinfra operations by keyword"
          label="Filter operations"
          leftSection={<IconSearch aria-hidden size={15} />}
          onChange={(event) =>
            updateFilter({
              [runReportSearchParamNames.operation]: undefined,
              [runReportSearchParamNames.query]: event.currentTarget.value,
            })
          }
          placeholder="Operation or host"
          style={{ flex: "1 1 18rem" }}
          value={keyword}
        />
        <Select
          allowDeselect={false}
          data={pyinfraScopeOptions}
          label="Outcome"
          onChange={(value) =>
            updateFilter(
              {
                [runReportSearchParamNames.operation]: undefined,
                [runReportSearchParamNames.scope]:
                  value === defaultScope ? undefined : (value ?? undefined),
              },
              false,
            )
          }
          value={scope}
          w={190}
        />
        <Select
          aria-label="Follow one host across pyinfra operations"
          clearable
          data={hosts}
          label="Host"
          nothingFoundMessage="No matching hosts"
          onChange={(value) =>
            updateFilter(
              {
                [runReportSearchParamNames.host]: value ?? undefined,
                [runReportSearchParamNames.operation]: undefined,
                [runReportSearchParamNames.scope]: value ? "all" : undefined,
              },
              false,
            )
          }
          placeholder={`All ${hosts.length.toLocaleString()} hosts`}
          searchable
          value={host || null}
          w={280}
        />
        {unchangedOperations > 0 && !allNoOp ? (
          <Switch
            checked={showNoOp}
            label={`Show unchanged (${unchangedOperations.toLocaleString()})`}
            mb={7}
            onChange={(event) =>
              updateFilter({
                [runReportSearchParamNames.operation]: undefined,
                [runReportSearchParamNames.showNoOp]: event.currentTarget.checked
                  ? "true"
                  : undefined,
              })
            }
          />
        ) : null}
      </Group>

      <Text c="dimmed" size="xs">
        Showing {operations.length.toLocaleString()} of {report.operations.length.toLocaleString()}
        {" operations"}
        {host ? ` for ${host}` : ""}
        {deferredKeyword.trim() ? " matching the keyword filter" : ""}.
      </Text>

      {operations.length === 0 ? (
        <Alert color="gray" title="No matching operations">
          Adjust the operation, outcome, or host filters to inspect other results.
        </Alert>
      ) : (
        <Table.ScrollContainer minWidth={640} type="native">
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Operation</Table.Th>
                <Table.Th>Host results</Table.Th>
                <Table.Th ta="center" w={56}>
                  Log
                </Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {operations.map(({ index, operation }) => {
                const operationNumber = index + 1;
                const expanded = expandedOperation === operationNumber;
                return (
                  <OperationRows
                    expanded={expanded}
                    host={host}
                    key={`${operation.names.join(":")}:${index}`}
                    onExpand={() =>
                      updateFilter(
                        {
                          [runReportSearchParamNames.operation]: expanded
                            ? undefined
                            : String(operationNumber),
                        },
                        false,
                      )
                    }
                    onHostSelect={(selectedHost) =>
                      updateFilter(
                        {
                          [runReportSearchParamNames.host]: selectedHost,
                          [runReportSearchParamNames.scope]: "all",
                        },
                        false,
                      )
                    }
                    operation={operation}
                    searchParams={searchParams}
                  />
                );
              })}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  );
}

function OperationRows({
  expanded,
  host,
  onExpand,
  onHostSelect,
  operation,
  searchParams,
}: Readonly<{
  expanded: boolean;
  host: string;
  onExpand: () => void;
  onHostSelect: (host: string) => void;
  operation: PyinfraOperation;
  searchParams: URLSearchParams;
}>) {
  const visible = host ? operationForHost(operation, host) : operation;
  const logSearchParams = updateRunDetailSearchParams(searchParams, {
    [runDetailSearchParamNames.logQuery]: operation.names[0],
    [runDetailSearchParamNames.output]: "log",
  });

  return (
    <>
      <Table.Tr data-expanded={expanded || undefined}>
        <Table.Td>
          <UnstyledButton
            aria-expanded={expanded}
            aria-label={`${expanded ? "Hide" : "Show"} host results for ${operation.names.join(", ")}`}
            onClick={onExpand}
            style={{ textAlign: "left", width: "100%" }}
          >
            <Group align="center" gap="xs" wrap="nowrap">
              {expanded ? (
                <IconChevronDown aria-hidden size={15} />
              ) : (
                <IconChevronRight aria-hidden size={15} />
              )}
              <Stack gap={3}>
                {operation.names.map((name) => (
                  <Code key={name}>{name}</Code>
                ))}
              </Stack>
            </Group>
          </UnstyledButton>
        </Table.Td>
        <Table.Td>
          <OperationCounts operation={visible} />
        </Table.Td>
        <Table.Td ta="center">
          <Tooltip label="Find in Run log">
            <ActionIcon
              aria-label="Find in Run log"
              color="lilac"
              component={Link}
              preventScrollReset
              to={`?${logSearchParams.toString()}`}
              variant="subtle"
            >
              <IconTerminal2 aria-hidden size={16} />
            </ActionIcon>
          </Tooltip>
        </Table.Td>
      </Table.Tr>
      {expanded ? (
        <Table.Tr>
          <Table.Td bg="var(--mantine-color-default-hover)" colSpan={3}>
            <OperationHostBreakdown operation={visible} onHostSelect={onHostSelect} />
          </Table.Td>
        </Table.Tr>
      ) : null}
    </>
  );
}

function OperationCounts({ operation }: Readonly<{ operation: PyinfraOperation }>) {
  const counts = [
    { color: "yellow", count: operation.hostsWithChanges.length, label: "planned" },
    {
      color: "violet",
      count: operation.hostsWithConditionalChanges.length,
      label: "conditional",
    },
    { color: "red", count: operation.result?.failedHosts.length ?? 0, label: "failed" },
    { color: "green", count: operation.result?.changedHosts.length ?? 0, label: "changed" },
    { color: "gray", count: operation.result?.unchangedHosts.length ?? 0, label: "unchanged" },
  ].filter(({ count }) => count > 0);

  if (counts.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        {operation.result ? "No host results" : "Not executed in preview"}
      </Text>
    );
  }

  return (
    <Group gap={6} wrap="wrap">
      {counts.map(({ color, count, label }) => (
        <Badge color={color} key={label} size="sm" variant="light">
          {count.toLocaleString()} {label}
        </Badge>
      ))}
    </Group>
  );
}

function OperationHostBreakdown({
  onHostSelect,
  operation,
}: Readonly<{ onHostSelect: (host: string) => void; operation: PyinfraOperation }>) {
  const groups = [
    { color: "red", hosts: operation.result?.failedHosts ?? [], label: "Observed failed" },
    { color: "green", hosts: operation.result?.changedHosts ?? [], label: "Observed changed" },
    { color: "yellow", hosts: operation.hostsWithChanges, label: "Planned change" },
    {
      color: "violet",
      hosts: operation.hostsWithConditionalChanges,
      label: "Conditional change",
    },
    { color: "gray", hosts: operation.result?.unchangedHosts ?? [], label: "Observed unchanged" },
  ].filter(({ hosts }) => hosts.length > 0);

  if (groups.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        No host names were recorded for this operation.
      </Text>
    );
  }

  return (
    <Stack gap="sm">
      {groups.map(({ color, hosts, label }) => (
        <Stack gap={5} key={label}>
          <Text c={color} fw={650} size="xs">
            {hosts.length.toLocaleString()} {label.toLocaleLowerCase()}{" "}
            {hosts.length === 1 ? "host" : "hosts"}
          </Text>
          {label === "Observed unchanged" && hosts.length > 6 ? (
            <Spoiler
              hideLabel="Collapse unchanged hosts"
              maxHeight={40}
              showLabel={`Show all ${hosts.length.toLocaleString()} unchanged hosts`}
            >
              <HostTags hosts={hosts} onSelect={onHostSelect} />
            </Spoiler>
          ) : (
            <HostTags hosts={hosts} onSelect={onHostSelect} />
          )}
        </Stack>
      ))}
    </Stack>
  );
}

function HostTags({
  hosts,
  onSelect,
}: Readonly<{ hosts: string[]; onSelect: (host: string) => void }>) {
  return (
    <Group gap={6} wrap="wrap">
      {hosts.map((host) => (
        <Badge
          aria-label={`Show operations for ${host}`}
          component="button"
          key={host}
          onClick={() => onSelect(host)}
          style={{ cursor: "pointer", overflowWrap: "anywhere" }}
          tt="none"
          type="button"
          variant="outline"
        >
          {host}
        </Badge>
      ))}
    </Group>
  );
}

function operationForHost(operation: PyinfraOperation, host: string): PyinfraOperation {
  const onlyHost = (hosts: string[]) => hosts.filter((candidate) => candidate === host);
  return {
    ...operation,
    hostsWithChanges: onlyHost(operation.hostsWithChanges),
    hostsWithConditionalChanges: onlyHost(operation.hostsWithConditionalChanges),
    result: operation.result
      ? {
          changedHosts: onlyHost(operation.result.changedHosts),
          failedHosts: onlyHost(operation.result.failedHosts),
          unchangedHosts: onlyHost(operation.result.unchangedHosts),
        }
      : undefined,
  };
}

function SummaryBadges({
  badges,
  empty,
  label,
}: Readonly<{ badges: PyinfraCountBadge[]; empty: string; label: string }>) {
  let contents: ReactNode;
  if (badges.length === 0) {
    contents = (
      <Text c="dimmed" size="sm">
        {empty}
      </Text>
    );
  } else {
    contents = (
      <Group gap="xs" wrap="wrap">
        {badges.map((badge) => (
          <Badge color={badge.color} key={badge.label} variant="light">
            {badge.count.toLocaleString()} {badge.label}
          </Badge>
        ))}
      </Group>
    );
  }

  return (
    <Group align="flex-start" gap="sm" wrap="wrap">
      <Text c="dimmed" fw={700} size="xs" tt="uppercase" w={64}>
        {label}
      </Text>
      {contents}
    </Group>
  );
}
