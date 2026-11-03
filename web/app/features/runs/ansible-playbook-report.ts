import type {
  AnsibleOutcomeCounts,
  AnsiblePlay,
  AnsiblePlaybookSummary,
  AnsibleTask,
  AnsibleTaskResult,
} from "~/api/generated";

export const ANSIBLE_TASK_PAGE_SIZE = 100;

export type AnsibleOutcome = AnsibleTaskResult["outcome"];

export type AnsibleCountBadge = {
  color: string;
  count: number;
  label: string;
};

export function ansibleSummaryBadges(summary: AnsiblePlaybookSummary): AnsibleCountBadge[] {
  return (
    [
      { color: "yellow", label: "changed" },
      { color: "red", label: "failed" },
      { color: "orange", label: "unreachable" },
      { color: "gray", label: "skipped" },
      { color: "cyan", label: "rescued" },
      { color: "violet", label: "ignored" },
    ] as const
  )
    .map((badge) => ({
      ...badge,
      count: summary.hostTotals.reduce((sum, host) => sum + host[badge.label], 0),
    }))
    .filter((badge) => badge.count > 0);
}

export function ansibleOutcomeBadges(counts: AnsibleOutcomeCounts): AnsibleCountBadge[] {
  return [
    { color: "green", count: counts.ok, label: "ok" },
    { color: "yellow", count: counts.changed, label: "changed" },
    { color: "red", count: counts.failed, label: "failed" },
    { color: "orange", count: counts.unreachable, label: "unreachable" },
    { color: "gray", count: counts.skipped, label: "skipped" },
  ].filter((badge) => badge.count > 0);
}

export function countAnsibleOutcomes(results: AnsibleTaskResult[]): AnsibleOutcomeCounts {
  const counts: AnsibleOutcomeCounts = {
    changed: 0,
    failed: 0,
    ok: 0,
    skipped: 0,
    total: results.length,
    unreachable: 0,
  };
  for (const result of results) counts[result.outcome]++;

  return counts;
}

export function countAnsibleUnchangedResults(plays: AnsiblePlay[]) {
  return plays.reduce(
    (playTotal, play) =>
      playTotal +
      play.tasks.reduce(
        (taskTotal, task) =>
          taskTotal +
          (task.redacted ? 0 : task.results.filter((result) => result.outcome === "ok").length),
        0,
      ),
    0,
  );
}

export function filterAnsiblePlays(
  plays: AnsiblePlay[],
  options: Readonly<{
    keyword: string;
    outcomes: readonly AnsibleOutcome[];
    showUnchanged: boolean;
  }>,
) {
  const keywords = options.keyword.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const outcomes = new Set(options.outcomes);
  const showUnchanged = options.showUnchanged || outcomes.has("ok");
  const filteredPlays: AnsiblePlay[] = [];

  for (const play of plays) {
    const playMatches = matchesAnsibleKeywords([play.name], keywords);
    const tasks: AnsibleTask[] = [];

    for (const task of play.tasks) {
      const taskMatches =
        playMatches || matchesAnsibleKeywords([task.name, task.action, task.sourcePath], keywords);
      const results = task.results.filter((result) => {
        if (result.outcome === "ok" && !showUnchanged && !task.redacted) return false;
        if (outcomes.size > 0 && !outcomes.has(result.outcome)) return false;
        if (taskMatches || keywords.length === 0) return true;

        return matchesAnsibleKeywords(
          [
            result.host,
            result.outcome,
            result.ignoredFailure ? "ignored" : undefined,
            result.loopItem ? "loop item" : undefined,
          ],
          keywords,
        );
      });
      const taskHasNoResult = task.results.length === 0 && outcomes.size === 0;
      const keepRedactedTask = task.redacted && outcomes.size === 0;
      const metadataMatches = keywords.length === 0 || taskMatches;

      if (results.length > 0 || (metadataMatches && (taskHasNoResult || keepRedactedTask))) {
        tasks.push({ ...task, results });
      }
    }

    if (tasks.length > 0) filteredPlays.push({ ...play, tasks });
  }

  return filteredPlays;
}

export function paginateAnsiblePlays(
  plays: AnsiblePlay[],
  requestedPage: number,
  pageSize = ANSIBLE_TASK_PAGE_SIZE,
) {
  const totalTasks = plays.reduce((total, play) => total + play.tasks.length, 0);
  const totalPages = Math.max(1, Math.ceil(totalTasks / pageSize));
  const page = Math.min(Math.max(1, requestedPage), totalPages);
  const pageStart = (page - 1) * pageSize;
  const pageEnd = pageStart + pageSize;
  const pagedPlays: AnsiblePlay[] = [];
  let taskOffset = 0;

  for (const play of plays) {
    const playStart = taskOffset;
    const playEnd = playStart + play.tasks.length;
    taskOffset = playEnd;
    if (playEnd <= pageStart || playStart >= pageEnd) continue;

    const taskStart = Math.max(0, pageStart - playStart);
    const taskEnd = Math.min(play.tasks.length, pageEnd - playStart);
    pagedPlays.push({ ...play, tasks: play.tasks.slice(taskStart, taskEnd) });
  }

  return { page, plays: pagedPlays, totalPages, totalTasks };
}

export function isAnsibleOutcome(value: string): value is AnsibleOutcome {
  return ["ok", "changed", "failed", "unreachable", "skipped"].includes(value);
}

function matchesAnsibleKeywords(values: Array<string | undefined>, keywords: string[]) {
  if (keywords.length === 0) return true;
  const searchableText = values
    .filter((value): value is string => value !== undefined)
    .join(" ")
    .toLocaleLowerCase();

  return keywords.every((keyword) => searchableText.includes(keyword));
}
