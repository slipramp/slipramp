import type { PromotionStageCapabilities, PromotionStagePolicy } from "~/api/generated";

type PromotionStagePolicyPresentationInput = Readonly<{
  capabilities: Pick<PromotionStageCapabilities, "promotion" | "review">;
  policy: Pick<PromotionStagePolicy, "effectiveMode">;
}>;

export function promotionStagePolicyPresentation(stage: PromotionStagePolicyPresentationInput) {
  const editable = stage.capabilities.promotion.status === "ready";
  const reviewRequired = stage.policy.effectiveMode === "review-required";

  return {
    editable,
    label: editable ? (reviewRequired ? "Review required" : "Automatic policy") : "Not promotable",
    reviewRequiredDisabled: !editable || stage.capabilities.review.status !== "ready",
  };
}
