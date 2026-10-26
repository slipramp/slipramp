import type { TargetReconciliationStatus, TargetSyncRunCheckpoint } from "~/api/generated";

export function reconciliationPresentation(status: TargetReconciliationStatus, archived: boolean) {
  if (archived) {
    return {
      color: "gray",
      label: "Historical checkpoints",
      description:
        "Sync history is retained, but an archived target has no active convergence comparison.",
    };
  }

  switch (status.comparison) {
    case "matched":
      return {
        color: "green",
        label: "Desired commit matched",
        description:
          "The last successful sync used the current desired relevance commit. This is a checkpoint, not continuous drift detection.",
      };
    case "different":
      return {
        color: "yellow",
        label: "Desired commit differs",
        description: "The last successful sync used an older or different relevance commit.",
      };
    default:
      return {
        color: "gray",
        label: "Comparison unknown",
        description:
          "No trustworthy successful sync marker is available for the active target and its current sync action.",
      };
  }
}

export function syncCheckpointResult(checkpoint: TargetSyncRunCheckpoint) {
  return checkpoint.outcome ?? checkpoint.phase;
}

export function syncCheckpointColor(checkpoint: TargetSyncRunCheckpoint) {
  switch (syncCheckpointResult(checkpoint)) {
    case "succeeded":
      return "green";
    case "failed":
      return "red";
    case "timed-out":
      return "orange";
    case "canceled":
      return "gray";
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
