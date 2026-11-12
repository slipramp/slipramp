import type {
  PromotionPolicyMode,
  PromotionStageCapabilities,
  PromotionStagePolicy,
} from "~/api/generated";

export function promotionStagePolicyColor(mode: PromotionPolicyMode) {
  return mode === "review-required" ? "yellow" : "gray";
}

type PromotionStagePolicyPresentationInput = Readonly<{
  capabilities: Pick<PromotionStageCapabilities, "promotion" | "review">;
  policy: Pick<PromotionStagePolicy, "effectiveMode">;
}>;

export function promotionStagePolicyPresentation(stage: PromotionStagePolicyPresentationInput) {
  const editable = stage.capabilities.promotion.status === "ready";
  const reviewRequired = stage.policy.effectiveMode === "review-required";

  return {
    editable,
    label: editable ? (reviewRequired ? "Review required" : "Auto") : "Not promotable",
    reviewRequiredDisabled: !editable || stage.capabilities.review.status !== "ready",
  };
}
