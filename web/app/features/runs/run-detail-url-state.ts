import type { RunPhase } from "~/api/generated";
import type { ReportLoad } from "./run-reports";

export const runDetailSearchParamNames = {
  logQuery: "logQuery",
  output: "output",
  stage: "stage",
} as const;

export type RunDetailSearchParam =
  (typeof runDetailSearchParamNames)[keyof typeof runDetailSearchParamNames];

export function reportOutputValue(artifactId: string) {
  return `report:${artifactId}`;
}

export function defaultRunOutput(phase: RunPhase, reports: readonly ReportLoad[]) {
  if (phase === "finished") {
    const report = reports.find((load) => load.state === "available");
    return report ? reportOutputValue(report.artifactId) : "log";
  }
  return phase === "running" ? "log" : "details";
}

export function selectedRunDetailValue(
  searchParams: URLSearchParams,
  name: RunDetailSearchParam,
  validValues: readonly string[],
  fallback: string,
) {
  const value = searchParams.get(name);
  return value !== null && validValues.includes(value) ? value : fallback;
}

export function updateRunDetailSearchParams(
  searchParams: URLSearchParams,
  updates: Partial<Record<RunDetailSearchParam, string | undefined>>,
) {
  const next = new URLSearchParams(searchParams);
  for (const [name, value] of Object.entries(updates)) {
    if (value) next.set(name, value);
    else next.delete(name);
  }
  return next;
}
