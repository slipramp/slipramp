import type { Run, RunActivityFilter } from "~/api/generated";
import { gateWaitLabel } from "../gates/gate-presentation.ts";

export const runActivityOptions = [
  { value: "queued", label: "Queued" },
  { value: "starting", label: "Starting" },
  { value: "running", label: "Running" },
  { value: "canceling", label: "Canceling" },
] satisfies { value: RunActivityFilter; label: string }[];

export function readRunActivityFilter(url: URL) {
  const values = url.searchParams.getAll("activity");
  const activity = runActivityOptions.find((option) => option.value === values[0])?.value;
  if (values.length === 0 || (values.length === 1 && activity)) return { activity };

  const canonical = new URL(url);
  if (activity) canonical.searchParams.set("activity", activity);
  else canonical.searchParams.delete("activity");
  canonical.searchParams.delete("page");
  return { activity, redirectTo: canonical.pathname + canonical.search };
}

export function runStatusColor(run: Run) {
  if (
    run.phase === "queued" &&
    (run.review.state === "pending" ||
      ["awaiting-review", "awaiting-gates", "cluster-unreachable", "cluster-capacity"].includes(
        run.schedulingState ?? "",
      ))
  )
    return "yellow";
  if (run.observationState === "unknown") return "yellow";

  switch (run.outcome ?? run.phase) {
    case "succeeded":
      return "green";
    case "failed":
      return "red";
    case "timed-out":
      return "orange";
    case "running":
      return "blue";
    case "preparing":
      return "cyan";
    case "assigned":
      return "indigo";
    default:
      return "gray";
  }
}

export function runStatusLabel(
  run: Pick<Run, "outcome" | "phase" | "review" | "schedulingState"> & Partial<Pick<Run, "gates">>,
) {
  if (run.phase === "queued") {
    switch (run.schedulingState) {
      case "awaiting-review":
        return "Awaiting review";
      case "awaiting-gates":
        return gateWaitLabel(run.gates?.requirements ?? []);
      case "target-busy":
        return "Target busy";
      case "cluster-unreachable":
        return "Cluster unreachable";
      case "cluster-capacity":
        return "Cluster at capacity";
      case "eligible":
        return "Eligible";
    }
    if (run.review.state === "pending") return "Awaiting review";
  }

  return run.outcome ?? run.phase;
}
