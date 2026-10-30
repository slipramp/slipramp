import type { Run } from "~/api/generated";

export function runStatusColor(run: Run) {
  if (
    run.phase === "queued" &&
    (run.review.state === "pending" ||
      ["awaiting-review", "cluster-unreachable", "cluster-capacity"].includes(
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

export function runStatusLabel(run: Pick<Run, "outcome" | "phase" | "review" | "schedulingState">) {
  if (run.phase === "queued") {
    switch (run.schedulingState) {
      case "awaiting-review":
        return "Awaiting review";
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
