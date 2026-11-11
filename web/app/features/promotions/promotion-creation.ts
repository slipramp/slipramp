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

export type PromotionCreationContext = "candidate" | "in-place" | "stage" | "upstream";

export type PromotionCreationURLSelection = {
  context: PromotionCreationContext;
  freightName?: string;
  sourceStageId?: string;
  stageId: string;
};

export const promotionCreationSearchParamNames = {
  context: "promote",
  freight: "promoteFreight",
  sourceStage: "promoteSourceStage",
  stage: "promoteStage",
} as const;

const promotionCreationContexts = new Set<PromotionCreationContext>([
  "candidate",
  "in-place",
  "stage",
  "upstream",
]);

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

export function promotionCreationContext(options: {
  inPlace?: boolean;
  initialFreightName?: string;
  sourceStageId?: string;
}): PromotionCreationContext {
  if (options.inPlace) return "in-place";
  if (options.sourceStageId) return "upstream";
  if (options.initialFreightName) return "candidate";
  return "stage";
}

export function promotionCreationURLSelection(
  searchParams: URLSearchParams,
): PromotionCreationURLSelection | undefined {
  const context = searchParams.get(promotionCreationSearchParamNames.context);
  const stageId = searchParams.get(promotionCreationSearchParamNames.stage);
  if (!stageId || !context || !promotionCreationContexts.has(context as PromotionCreationContext)) {
    return undefined;
  }

  return {
    context: context as PromotionCreationContext,
    freightName: searchParams.get(promotionCreationSearchParamNames.freight) || undefined,
    sourceStageId: searchParams.get(promotionCreationSearchParamNames.sourceStage) || undefined,
    stageId,
  };
}

export function updatePromotionCreationSearchParams(
  searchParams: URLSearchParams,
  selection?: PromotionCreationURLSelection,
) {
  const next = new URLSearchParams(searchParams);
  for (const name of Object.values(promotionCreationSearchParamNames)) next.delete(name);
  if (!selection) return next;

  next.set(promotionCreationSearchParamNames.context, selection.context);
  next.set(promotionCreationSearchParamNames.stage, selection.stageId);
  if (selection.freightName) {
    next.set(promotionCreationSearchParamNames.freight, selection.freightName);
  }
  if (selection.sourceStageId) {
    next.set(promotionCreationSearchParamNames.sourceStage, selection.sourceStageId);
  }
  return next;
}

export function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}
