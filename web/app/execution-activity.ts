import type { Promotion, Run } from "~/api/generated";

export type ExecutionAnimation = "waiting" | "phone" | "working" | "celebrating" | "frustrated";

export type ExecutionAnimationPose =
  | "phone-scroll"
  | "book-browse"
  | "iced-coffee-sip"
  | "magazine-load"
  | "rifle-clean"
  | "popcorn-munch"
  | "headphone-headbang"
  | "toaster-vigil"
  | "suspended-animation"
  | "bubblegum-blow"
  | "binocular-scan"
  | "hourglass-turn"
  | "duck-float"
  | "lava-lamp-gaze"
  | "snail-coach"
  | "beach-ball-roll"
  | "cat-petting"
  | "grass-touch"
  | "pinwheel-spin"
  | "skull-consult"
  | "candle-vigil"
  | "ghost-jar"
  | "laptop-typing"
  | "blueprint-review"
  | "clipboard-check"
  | "walkie-talkie"
  | "antenna-tune"
  | "drill-drive"
  | "weld-spark-pipe"
  | "grenade-throw"
  | "mortar-drop"
  | "jackhammer-ride"
  | "bubble-level-check"
  | "paint-roller-swipe"
  | "pumpkin-autopsy"
  | "breaker-bar-bounce"
  | "beer-sip"
  | "pennant-wave"
  | "check-badge"
  | "beer-toast"
  | "flower-offer"
  | "championship-belt"
  | "confetti-cannon"
  | "rifle-salute"
  | "trophy-hoist"
  | "cowbell-clang"
  | "chart-rise"
  | "laptop-slump"
  | "server-bite"
  | "rocket-boot-misfire"
  | "kaiju-server-stomp"
  | "wrench-fumble"
  | "button-panic"
  | "chart-plunge"
  | "play-dead"
  | "tombstone-peek"
  | "trash-can-hide"
  | "lever-regret"
  | "anvil-bonk"
  | "cable-cocoon"
  | "nuclear-vaporize"
  | "too-many-arms"
  | "trapdoor-deploy"
  | "steamroller-flat"
  | "under-desk-drag"
  | "fire-extinguisher-blast"
  | "explosion-walkaway";

export const executionAnimationPosePools = {
  waiting: [
    "phone-scroll",
    "book-browse",
    "iced-coffee-sip",
    "magazine-load",
    "rifle-clean",
    "popcorn-munch",
    "headphone-headbang",
    "toaster-vigil",
    "suspended-animation",
    "bubblegum-blow",
    "binocular-scan",
    "hourglass-turn",
    "duck-float",
    "lava-lamp-gaze",
    "snail-coach",
    "beach-ball-roll",
    "cat-petting",
    "grass-touch",
    "pinwheel-spin",
    "skull-consult",
    "candle-vigil",
    "ghost-jar",
  ],
  phone: ["phone-scroll"],
  working: [
    "laptop-typing",
    "blueprint-review",
    "clipboard-check",
    "walkie-talkie",
    "antenna-tune",
    "drill-drive",
    "weld-spark-pipe",
    "grenade-throw",
    "mortar-drop",
    "jackhammer-ride",
    "bubble-level-check",
    "paint-roller-swipe",
    "pumpkin-autopsy",
    "breaker-bar-bounce",
  ],
  celebrating: [
    "beer-sip",
    "pennant-wave",
    "check-badge",
    "beer-toast",
    "explosion-walkaway",
    "flower-offer",
    "championship-belt",
    "confetti-cannon",
    "rifle-salute",
    "trophy-hoist",
    "cowbell-clang",
    "chart-rise",
  ],
  frustrated: [
    "laptop-slump",
    "server-bite",
    "rocket-boot-misfire",
    "kaiju-server-stomp",
    "wrench-fumble",
    "button-panic",
    "chart-plunge",
    "play-dead",
    "tombstone-peek",
    "trash-can-hide",
    "lever-regret",
    "anvil-bonk",
    "cable-cocoon",
    "nuclear-vaporize",
    "too-many-arms",
    "trapdoor-deploy",
    "steamroller-flat",
    "under-desk-drag",
    "fire-extinguisher-blast",
  ],
} as const satisfies Record<ExecutionAnimation, readonly ExecutionAnimationPose[]>;

export type ExecutionActivity = {
  animation: ExecutionAnimation;
  title: string;
  description?: string;
  tone?: "success" | "error" | "neutral";
  result?: {
    reason?: string;
    exitCode?: number;
  };
};

function activityAnimationHash(animationKey: string): number {
  let hash = 2166136261;
  for (let index = 0; index < animationKey.length; index++) {
    hash = Math.imul(hash ^ animationKey.charCodeAt(index), 16777619);
  }

  return hash >>> 0;
}

export function activityAnimationDelay(animationKey: string): number {
  return -(activityAnimationHash(animationKey) % 10000);
}

function animationPoseFromPool(
  animation: ExecutionAnimation,
  animationKey: string,
  poses: readonly ExecutionAnimationPose[],
): ExecutionAnimationPose {
  const index = activityAnimationHash(`${animation}:${animationKey}`) % poses.length;

  return poses[index]!;
}

export function activityAnimationPose(
  animation: ExecutionAnimation,
  animationKey: string,
): ExecutionAnimationPose {
  return animationPoseFromPool(animation, animationKey, executionAnimationPosePools[animation]);
}

export function runActivity(
  run: Pick<Run, "phase" | "observationState" | "cancellationRequestedAt">,
): ExecutionActivity | undefined {
  if (run.phase === "finished" || run.observationState === "unknown") {
    return undefined;
  }

  if (run.cancellationRequestedAt) {
    return {
      animation: run.phase === "queued" ? "waiting" : "working",
      title: "Stopping your run",
      description: "Waiting for the action to stop.",
    };
  }

  switch (run.phase) {
    case "queued":
      return {
        animation: "waiting",
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

export function promotionDetailActivity(
  promotion: Pick<Promotion, "status" | "message">,
): ExecutionActivity | undefined {
  const activity = promotionActivity(promotion);
  if (activity) return activity;

  switch (promotion.status) {
    case "pending":
      return {
        animation: "waiting",
        title: "Ready when you are",
        description: "Review the prepared changes, then approve or reject this Promotion.",
      };
    case "approved":
      return {
        animation: "celebrating",
        tone: "success",
        title: "Promotion approved",
        description: "Kargo can continue with the approved candidate.",
      };
    case "rejected":
      return {
        animation: "frustrated",
        tone: "error",
        title: "Promotion rejected",
        description: "The candidate was not approved for this Stage.",
      };
    case "expired":
      return {
        animation: "phone",
        tone: "neutral",
        title: "Review window expired",
        description:
          promotion.message ?? "No decision was recorded before the review window closed.",
      };
    case "error":
      return {
        animation: "frustrated",
        tone: "error",
        title: "Promotion failed",
        description: promotion.message ?? "The Promotion could not be created or prepared.",
      };
  }
}
