import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  MultiSelect,
  Pagination,
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
  AnsibleHostTotals,
  AnsiblePlaybookReport,
  AnsibleTask,
  AnsibleTaskResult,
  Artifact,
} from "~/api/generated";
import {
  ANSIBLE_TASK_PAGE_SIZE,
  ansibleOutcomeBadges,
  ansibleSummaryBadges,
  ansibleTaskPresentation,
  countAnsibleOutcomes,
  countAnsibleUnchangedResults,
  filterAnsiblePlays,
  isAnsibleOutcome,
  paginateAnsiblePlays,
  type AnsibleCountBadge,
  type AnsibleOutcome,
} from "~/features/runs/ansible-playbook-report";
import { ReportDetailNotice } from "~/features/runs/run-report-detail-notice";
import type { ReportLoad } from "~/features/runs/run-reports";
import { SectionHeader } from "~/shared/page";

const ansibleResultBatchSize = 100;

const ansibleOutcomeOptions: Array<{ label: string; value: AnsibleOutcome }> = [
  { label: "Changed", value: "changed" },
  { label: "Failed", value: "failed" },
  { label: "Unreachable", value: "unreachable" },
  { label: "Skipped", value: "skipped" },
  { label: "OK / unchanged", value: "ok" },
];

export function AnsiblePlaybookReportView({
  artifact,
  canReadContents,
  load,
}: Readonly<{
  artifact: Artifact;
  canReadContents: boolean;
  load: ReportLoad;
}>) {
  const report = artifact.report;
  if (!report || report.kind !== "ansible-playbook") return null;

  const detail =
    load.state === "available" && load.detail.kind === "ansible-playbook" ? load.detail : undefined;
  const summaryBadges = ansibleSummaryBadges(report.summary);

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <SectionHeader
          actions={
            <Group align="center" gap="xs" style={{ alignSelf: "center" }} wrap="wrap">
              <Badge color={report.mode === "apply" ? "green" : "blue"} variant="light">
                {report.mode === "apply" ? "Apply" : "Preview"}
              </Badge>
              <Badge color={report.summary.complete ? "green" : "orange"} variant="light">
                {report.summary.complete ? "Complete" : "Partial"}
              </Badge>
              {detail ? <RunnerStatusBadge report={detail} /> : null}
              <Badge color="gray" variant="light">
                Ansible{report.toolVersion ? ` ${report.toolVersion}` : ""}
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
          title="Ansible playbook report"
        />

        <Text c="dimmed" size="sm">
          {report.summary.hosts.toLocaleString()} hosts · {report.summary.plays.toLocaleString()}{" "}
          plays · {report.summary.tasks.toLocaleString()} tasks ·{" "}
          {report.summary.results.total.toLocaleString()} host-task results
        </Text>
        <AnsibleBadges
          badges={summaryBadges}
          empty="Ansible reported no changes, failures, unreachable hosts, skips, rescues, or ignored failures."
          label="Ansible result recap"
        />

        {report.mode === "preview" ? (
          <Alert color="blue" title="Ansible preview">
            Check-mode results describe predicted task outcomes. Some modules cannot predict
            changes, so this report is not proof of the exact apply result.
          </Alert>
        ) : null}
        {!report.summary.complete ? (
          <Alert color="orange" title="Partial playbook report">
            Ansible Runner did not produce a final play recap. The report shows only events observed
            before execution stopped.
          </Alert>
        ) : null}

        <AnsibleHostRecap totals={report.summary.hostTotals} />
        <ReportDetailNotice state={load.state} />
        {detail ? <AnsiblePlaybookDetail report={detail} /> : null}
      </Stack>
    </Paper>
  );
}

function RunnerStatusBadge({ report }: Readonly<{ report: AnsiblePlaybookReport }>) {
  const colors = {
    canceled: "orange",
    failed: "red",
    successful: "green",
    timeout: "orange",
  } as const;

  return (
    <Badge color={colors[report.runner.status]} variant="light">
      Runner{report.runner.version ? ` ${report.runner.version}` : ""} {report.runner.status} · exit{" "}
      {report.runner.exitCode}
    </Badge>
  );
}

function AnsibleBadges({
  badges,
  empty,
  label,
}: Readonly<{ badges: AnsibleCountBadge[]; empty: string; label: string }>) {
  if (badges.length === 0) return <Text c="dimmed">{empty}</Text>;

  return (
    <Group aria-label={label} component="section" gap="xs" wrap="wrap">
      {badges.map((badge) => (
        <Badge color={badge.color} key={badge.label} variant="light">
          {badge.count.toLocaleString()} {badge.label}
        </Badge>
      ))}
    </Group>
  );
}

function AnsibleHostRecap({ totals }: Readonly<{ totals: AnsibleHostTotals[] }>) {
  if (totals.length === 0) {
    return (
      <Stack gap={4}>
        <Text fw={600}>Host recap</Text>
        <Text c="dimmed" size="sm">
          No host outcomes were observed.
        </Text>
      </Stack>
    );
  }

  return (
    <Stack gap="xs">
      <Text fw={600}>Host recap</Text>
      <Table.ScrollContainer minWidth={760} type="native">
        <Table striped withRowBorders>
          <Table.Caption>Aggregate Ansible outcomes by host</Table.Caption>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Host</Table.Th>
              <Table.Th>OK</Table.Th>
              <Table.Th>Changed</Table.Th>
              <Table.Th>Failed</Table.Th>
              <Table.Th>Unreachable</Table.Th>
              <Table.Th>Skipped</Table.Th>
              <Table.Th>Rescued</Table.Th>
              <Table.Th>Ignored</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {totals.map((host) => (
              <Table.Tr key={host.host}>
                <Table.Th scope="row">{host.host}</Table.Th>
                <HostCount value={host.ok} />
                <HostCount value={host.changed} />
                <HostCount value={host.failed} />
                <HostCount value={host.unreachable} />
                <HostCount value={host.skipped} />
                <HostCount value={host.rescued} />
                <HostCount value={host.ignored} />
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Stack>
  );
}

function HostCount({ value }: Readonly<{ value: number }>) {
  return <Table.Td c={value === 0 ? "dimmed" : undefined}>{value.toLocaleString()}</Table.Td>;
}

function AnsiblePlaybookDetail({ report }: Readonly<{ report: AnsiblePlaybookReport }>) {
  const [keyword, setKeyword] = useState("");
  const [outcomes, setOutcomes] = useState<AnsibleOutcome[]>([]);
  const [page, setPage] = useState(1);
  const [showUnchanged, setShowUnchanged] = useState(false);
  const deferredKeyword = useDeferredValue(keyword);
  const unchangedResults = useMemo(
    () => countAnsibleUnchangedResults(report.plays),
    [report.plays],
  );
  const filteredPlays = useMemo(
    () =>
      filterAnsiblePlays(report.plays, {
        keyword: deferredKeyword,
        outcomes,
        showUnchanged,
      }),
    [deferredKeyword, outcomes, report.plays, showUnchanged],
  );
  const paginated = useMemo(() => paginateAnsiblePlays(filteredPlays, page), [filteredPlays, page]);

  return (
    <Stack gap="lg">
      <Stack gap={6}>
        <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
          <TextInput
            aria-label="Filter Ansible plays, tasks, modules, and hosts"
            label="Filter playbook detail"
            leftSection={<IconSearch aria-hidden size={15} />}
            onChange={(event) => {
              setKeyword(event.currentTarget.value);
              setPage(1);
            }}
            placeholder="Play, task, module, host, or outcome"
            value={keyword}
          />
          <MultiSelect
            aria-label="Filter Ansible task outcomes"
            clearable
            data={ansibleOutcomeOptions}
            label="Outcomes"
            onChange={(values) => {
              setOutcomes(values.filter(isAnsibleOutcome));
              setPage(1);
            }}
            placeholder="All outcomes"
            searchable
            value={outcomes}
          />
        </SimpleGrid>
        {unchangedResults > 0 ? (
          <Switch
            checked={showUnchanged}
            label={`Show OK / unchanged results (${unchangedResults.toLocaleString()})`}
            onChange={(event) => {
              setShowUnchanged(event.currentTarget.checked);
              setPage(1);
            }}
          />
        ) : null}
        <Text c="dimmed" size="xs">
          Showing {paginated.totalTasks.toLocaleString()} of {report.summary.tasks.toLocaleString()}{" "}
          tasks.
          {!showUnchanged && unchangedResults > 0 && !outcomes.includes("ok")
            ? " OK / unchanged results are hidden."
            : ""}
          {paginated.totalTasks > ANSIBLE_TASK_PAGE_SIZE
            ? ` Up to ${ANSIBLE_TASK_PAGE_SIZE.toLocaleString()} tasks are shown per page.`
            : ""}
        </Text>
      </Stack>

      {paginated.totalTasks === 0 ? (
        <Text c="dimmed" size="sm">
          No playbook tasks match the current filters.
        </Text>
      ) : (
        <Stack gap="xl">
          {paginated.plays.map((play) => (
            <AnsiblePlayGroup key={play.id} play={play} />
          ))}
          {paginated.totalPages > 1 ? (
            <Pagination
              aria-label="Ansible task pages"
              onChange={setPage}
              total={paginated.totalPages}
              value={paginated.page}
              withEdges
            />
          ) : null}
        </Stack>
      )}
    </Stack>
  );
}

function AnsiblePlayGroup({ play }: Readonly<{ play: AnsiblePlaybookReport["plays"][number] }>) {
  const [expandedTasks, setExpandedTasks] = useState<string[]>([]);

  return (
    <Stack gap="xs">
      <Group justify="space-between" wrap="wrap">
        <Text component="h3" fw={600} m={0}>
          {play.name}
        </Text>
        <Badge color="gray" variant="light">
          {play.tasks.length.toLocaleString()} {play.tasks.length === 1 ? "task" : "tasks"}
        </Badge>
      </Group>
      <Accordion multiple onChange={setExpandedTasks} value={expandedTasks} variant="separated">
        {play.tasks.map((task) => (
          <AnsibleTaskItem expanded={expandedTasks.includes(task.id)} key={task.id} task={task} />
        ))}
      </Accordion>
    </Stack>
  );
}

function AnsibleTaskItem({ expanded, task }: Readonly<{ expanded: boolean; task: AnsibleTask }>) {
  const presentation = ansibleTaskPresentation(task);
  const badges = ansibleOutcomeBadges(countAnsibleOutcomes(task.results));
  const ignoredFailures = task.results.filter((result) => result.ignoredFailure).length;

  return (
    <Accordion.Item value={task.id}>
      <Accordion.Control>
        <Group align="flex-start" justify="space-between" pr="sm" wrap="wrap">
          <Stack gap={3} style={{ flex: "1 1 20rem", minWidth: 0 }}>
            <Group gap="xs" wrap="wrap">
              <Text fw={600}>{presentation.name}</Text>
              {task.redacted ? (
                <Badge color="yellow" variant="light">
                  no_log redacted
                </Badge>
              ) : null}
            </Group>
            <Group gap="xs" wrap="wrap">
              <Code>{presentation.action}</Code>
              {presentation.sourcePath ? (
                <Text c="dimmed" size="xs" style={{ overflowWrap: "anywhere" }}>
                  {presentation.sourcePath}
                </Text>
              ) : null}
            </Group>
          </Stack>
          <Group gap={6} wrap="wrap">
            {badges.map((badge) => (
              <Badge color={badge.color} key={badge.label} variant="light">
                {badge.count.toLocaleString()} {badge.label}
              </Badge>
            ))}
            {ignoredFailures > 0 ? (
              <Badge color="violet" variant="light">
                {ignoredFailures.toLocaleString()} ignored
              </Badge>
            ) : null}
            {task.results.length === 0 ? (
              <Badge color="orange" variant="light">
                No result
              </Badge>
            ) : null}
          </Group>
        </Group>
      </Accordion.Control>
      <Accordion.Panel>{expanded ? <AnsibleTaskResults task={task} /> : null}</Accordion.Panel>
    </Accordion.Item>
  );
}

function AnsibleTaskResults({ task }: Readonly<{ task: AnsibleTask }>) {
  const [limit, setLimit] = useState(ansibleResultBatchSize);
  const visibleResults = task.results.slice(0, limit);
  const hiddenResults = task.results.length - visibleResults.length;

  if (task.results.length === 0) {
    return (
      <Text c="dimmed" size="sm">
        No host result was observed before the report ended.
      </Text>
    );
  }

  return (
    <Stack gap="sm">
      {task.redacted ? (
        <Alert color="yellow" title="Task details redacted">
          Ansible marked this task as no_log. Only normalized host outcomes are shown.
        </Alert>
      ) : null}
      <Table.ScrollContainer minWidth={560} type="native">
        <Table withRowBorders>
          <Table.Caption>Normalized Ansible host outcomes for this task</Table.Caption>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Host</Table.Th>
              <Table.Th>Outcome</Table.Th>
              <Table.Th>Context</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {visibleResults.map((result) => (
              <AnsibleResultRow key={`${result.order}:${result.host}`} result={result} />
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      {hiddenResults > 0 ? (
        <Button
          onClick={() => setLimit((value) => value + ansibleResultBatchSize)}
          size="compact-sm"
          style={{ alignSelf: "flex-start" }}
          variant="subtle"
        >
          Show next {Math.min(ansibleResultBatchSize, hiddenResults).toLocaleString()} results
        </Button>
      ) : null}
    </Stack>
  );
}

function AnsibleResultRow({ result }: Readonly<{ result: AnsibleTaskResult }>) {
  const colors = {
    changed: "yellow",
    failed: "red",
    ok: "green",
    skipped: "gray",
    unreachable: "orange",
  } as const;

  return (
    <Table.Tr>
      <Table.Th scope="row">{result.host}</Table.Th>
      <Table.Td>
        <Badge color={colors[result.outcome]} variant="light">
          {result.outcome}
        </Badge>
      </Table.Td>
      <Table.Td>
        <Group gap={6} wrap="wrap">
          {result.loopItem ? (
            <Badge color="blue" variant="outline">
              Loop item
            </Badge>
          ) : null}
          {result.ignoredFailure ? (
            <Badge color="violet" variant="outline">
              Ignored failure
            </Badge>
          ) : null}
          {!result.loopItem && !result.ignoredFailure ? <Text c="dimmed">—</Text> : null}
        </Group>
      </Table.Td>
    </Table.Tr>
  );
}
