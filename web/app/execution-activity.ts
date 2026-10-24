import type { Promotion, Run } from "~/api/generated";

export type ExecutionActivity = {
  animation: "phone" | "working" | "celebrating" | "frustrated";
  title: string;
  description?: string;
  tone?: "success" | "error" | "neutral";
  result?: {
    reason?: string;
    exitCode?: number;
  };
};

export function activityAnimationDelay(animationKey: string): number {
  let hash = 2166136261;
  for (let index = 0; index < animationKey.length; index++) {
    hash = Math.imul(hash ^ animationKey.charCodeAt(index), 16777619);
  }

  return -((hash >>> 0) % 10000);
}

export function runActivity(
  run: Pick<Run, "phase" | "observationState" | "cancellationRequestedAt">,
): ExecutionActivity | undefined {
  if (run.phase === "finished" || run.observationState === "unknown") {
    return undefined;
  }

  if (run.cancellationRequestedAt) {
    return {
      animation: run.phase === "queued" ? "phone" : "working",
      title: "Stopping your run",
      description: "Waiting for the action to stop.",
    };
  }

  switch (run.phase) {
    case "queued":
      return {
        animation: "phone",
        title: "Waiting for a turn",
        description: "Your run is queued and waiting to be picked up by the Controller.",
      };
    case "assigned":
      return {
        animation: "working",
        title: "Getting your run ready",
        description: "The Controller has picked up your run.",
      };
    case "preparing":
      return {
        animation: "working",
        title: "Hard hat on. Getting set!",
        description: "Preparing the execution environment for your action.",
      };
    case "running":
      return {
        animation: "working",
        title: "A little axolotl. A lot of work.",
        description: "Your action is running. Follow along in the live logs below.",
      };
  }
}

export function runResultActivity(
  run: Pick<Run, "phase" | "outcome" | "outcomeReason" | "outcomeMessage" | "exitCode">,
): ExecutionActivity | undefined {
  if (run.phase !== "finished") return undefined;

  let activity: ExecutionActivity;

  switch (run.outcome) {
    case "succeeded":
      return {
        animation: "celebrating",
        tone: "success",
        title: "Run succeeded",
      };
    case "failed":
      activity = {
        animation: "frustrated",
        tone: "error",
        title: "Run failed",
        description: "The action failed. Details are in the logs below.",
      };
      break;
    case "timed-out":
      activity = {
        animation: "frustrated",
        tone: "error",
        title: "Run timed out",
        description: "The action exceeded its time limit.",
      };
      break;
    case "canceled":
      activity = {
        animation: "phone",
        tone: "neutral",
        title: "Run canceled",
        description: "Your run was canceled.",
      };
      break;
    default:
      return undefined;
  }

  return {
    ...activity,
    description: run.outcomeMessage ?? activity.description,
    result: {
      reason: run.outcomeReason,
      exitCode: run.exitCode,
    },
  };
}

export function promotionActivity(
  promotion: Pick<Promotion, "status">,
): ExecutionActivity | undefined {
  switch (promotion.status) {
    case "creating":
      return {
        animation: "working",
        title: "Making a little momentum",
        description: "Creating your Promotion in Kargo and waiting for its data.",
      };
    case "preparing":
      return {
        animation: "working",
        title: "Good things are in motion",
        description: "Preparing the Promotion and its diffs.",
      };
    default:
      return undefined;
  }
}
