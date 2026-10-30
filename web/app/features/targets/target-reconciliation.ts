import type { TargetReconciliationStatus, TargetSyncRunCheckpoint } from "~/api/generated";

export function reconciliationPresentation(status: TargetReconciliationStatus, archived: boolean) {
  if (archived) {
    return {
      color: "gray",
      label: "Archived",
      description: "Sync history is retained for this archived target.",
    };
  }

  switch (status.comparison) {
    case "matched":
      return {
        color: "green",
        label: "Synced",
        description:
          "The latest successful sync matches the target's current commit. This does not check for drift.",
      };
    case "different":
      return {
        color: "yellow",
        label: "Out of sync",
        description: "The latest successful sync used a different commit.",
      };
    default:
      return {
        color: "gray",
        label: "Unknown",
        description: "No successful sync commit is available for comparison.",
      };
  }
}

export function activeTargetReconciliationPresentation(
  actions: readonly string[],
  status: TargetReconciliationStatus,
) {
  return actions.includes("sync") ? reconciliationPresentation(status, false) : undefined;
}

export function showLastAttemptedSync(status: TargetReconciliationStatus) {
  return (
    status.lastAttemptedSync === undefined ||
    status.lastAttemptedSync.runId !== status.lastSuccessfulSync?.runId
  );
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
