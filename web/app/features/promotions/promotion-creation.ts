import type {
  ProductPrincipal,
  PromotionFreightItem,
  PromotionProjectStage,
  PromotionStagePolicyUpdate,
} from "~/api/generated";

export type PromotionCreationActionResult = {
  error?: string;
  promotionId?: string;
};

export type PromotionReviewPolicySelection =
  | "unchanged"
  | "automatic"
  | "permanent"
  | "30m"
  | "1h"
  | "24h";

const promotionReviewPolicySelections = new Set<PromotionReviewPolicySelection>([
  "unchanged",
  "automatic",
  "permanent",
  "30m",
  "1h",
  "24h",
]);

export function parsePromotionReviewPolicySelection(
  value: string,
): PromotionReviewPolicySelection | undefined {
  return promotionReviewPolicySelections.has(value as PromotionReviewPolicySelection)
    ? (value as PromotionReviewPolicySelection)
    : undefined;
}

export function promotionReviewPolicyUpdate(
  selection: PromotionReviewPolicySelection,
  now = new Date(),
): PromotionStagePolicyUpdate | undefined {
  if (selection === "unchanged") return undefined;
  if (selection === "automatic") return { mode: "automatic" };
  if (selection === "permanent") return { mode: "review-required" };

  const durationMilliseconds = {
    "30m": 30 * 60 * 1000,
    "1h": 60 * 60 * 1000,
    "24h": 24 * 60 * 60 * 1000,
  }[selection];

  return {
    expiresAt: new Date(now.getTime() + durationMilliseconds).toISOString(),
    mode: "review-required",
  };
}

export function eligibleStageFreight(stage: PromotionProjectStage): PromotionFreightItem[] {
  if (stage.capabilities.promotion.status !== "ready") return [];

  return stage.requestedFreight.flatMap((request) =>
    request.candidates
      .filter((candidate) => candidate.readiness === "ready")
      .map((candidate) => candidate.freight),
  );
}

export function initialPromotionFreightName(
  freight: PromotionFreightItem[],
  requestedName?: string,
) {
  if (requestedName && freight.some((item) => item.name === requestedName)) return requestedName;
  return freight.length === 1 ? (freight[0]?.name ?? "") : "";
}

export function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}
