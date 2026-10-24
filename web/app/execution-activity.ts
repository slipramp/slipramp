import type { Promotion, Run } from "~/api/generated";

export type ExecutionActivity = {
  title: string;
  description: string;
};

export function runActivity(
  run: Pick<Run, "phase" | "observationState" | "cancellationRequestedAt">,
): ExecutionActivity | undefined {
  if (run.phase === "queued" || run.phase === "finished" || run.observationState === "unknown") {
    return undefined;
  }

  if (run.cancellationRequestedAt) {
    return {
      title: "Stopping your run",
      description: "Waiting for the action to stop.",
    };
  }

  switch (run.phase) {
    case "assigned":
      return {
        title: "Getting your run ready",
        description: "The Controller has picked up your run.",
      };
    case "preparing":
      return {
        title: "Hard hat on. Getting set!",
        description: "Preparing the execution environment for your action.",
      };
    case "running":
      return {
        title: "A little axolotl. A lot of work.",
        description: "Your action is running. Follow along in the live logs below.",
      };
  }
}

export function promotionActivity(
  promotion: Pick<Promotion, "status">,
): ExecutionActivity | undefined {
  switch (promotion.status) {
    case "creating":
      return {
        title: "Making a little momentum",
        description: "Creating your Promotion in Kargo and waiting for its data.",
      };
    case "preparing":
      return {
        title: "Good things are in motion",
        description: "Preparing the Promotion and its diffs.",
      };
    default:
      return undefined;
  }
}
