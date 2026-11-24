import type { Run } from "~/api/generated";

export function runActionHistoryPath(
  current: URLSearchParams,
  run: Pick<Run, "action" | "targetId">,
) {
  const filters = new URLSearchParams(current);
  filters.set("targetId", run.targetId);
  filters.set("action", run.action);
  filters.delete("page");
  return `/runs?${filters}`;
}
