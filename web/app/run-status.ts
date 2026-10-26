import type { Run } from "~/api/generated";

export function runStatusColor(run: Run) {
  if (run.phase === "queued" && run.review.state === "pending") return "yellow";
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

export function runStatusLabel(run: Pick<Run, "outcome" | "phase" | "review">) {
  if (run.phase === "queued" && run.review.state === "pending") {
    return "Awaiting review";
  }

  return run.outcome ?? run.phase;
}
