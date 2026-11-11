export const runReportSearchParamNames = {
  artifact: "report",
  outcome: "reportOutcome",
  page: "reportPage",
  query: "reportQuery",
  showNoOp: "reportShowNoOp",
  showUnchanged: "reportShowUnchanged",
  task: "reportTask",
} as const;

export type RunReportSearchParam = Exclude<
  (typeof runReportSearchParamNames)[keyof typeof runReportSearchParamNames],
  "report"
>;

const reportStateParamNames: RunReportSearchParam[] = [
  runReportSearchParamNames.outcome,
  runReportSearchParamNames.page,
  runReportSearchParamNames.query,
  runReportSearchParamNames.showNoOp,
  runReportSearchParamNames.showUnchanged,
  runReportSearchParamNames.task,
];

export function runReportSearchParam(
  searchParams: URLSearchParams,
  artifactId: string,
  name: RunReportSearchParam,
) {
  if (searchParams.get(runReportSearchParamNames.artifact) !== artifactId) return null;
  return searchParams.get(name);
}

export function runReportSearchParams(
  searchParams: URLSearchParams,
  artifactId: string,
  name: RunReportSearchParam,
) {
  if (searchParams.get(runReportSearchParamNames.artifact) !== artifactId) return [];
  return searchParams.getAll(name);
}

export function updateRunReportSearchParams(
  searchParams: URLSearchParams,
  artifactId: string,
  updates: Partial<Record<RunReportSearchParam, string | string[] | undefined>>,
) {
  const next = new URLSearchParams(searchParams);
  if (next.get(runReportSearchParamNames.artifact) !== artifactId) {
    for (const name of reportStateParamNames) next.delete(name);
  }
  next.set(runReportSearchParamNames.artifact, artifactId);

  for (const [name, value] of Object.entries(updates)) {
    next.delete(name);
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item) next.append(name, item);
      }
    } else if (value) {
      next.set(name, value);
    }
  }

  if (!reportStateParamNames.some((name) => next.has(name))) {
    next.delete(runReportSearchParamNames.artifact);
  }

  return next;
}
