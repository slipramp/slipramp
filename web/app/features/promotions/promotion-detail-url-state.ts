import type { Promotion } from "~/api/generated";

export const promotionDetailSearchParamNames = {
  stage: "stage",
  view: "view",
} as const;

export const promotionDetailViews = ["git", "argo", "details"] as const;

export type PromotionDetailView = (typeof promotionDetailViews)[number];
export type PromotionPhaseStage = "queued" | "delivery" | "preparation" | "review" | "execution";
export type PromotionLifecycleStage = PromotionPhaseStage | "cancellation";

type PromotionReviewViews = Pick<
  Promotion,
  "reviewRequired" | "gitDiffArtifactId" | "argoDiffArtifactId"
>;

export function defaultPromotionDetailView(promotion: PromotionReviewViews): PromotionDetailView {
  if (!promotion.reviewRequired) return "details";
  if (promotion.gitDiffArtifactId) return "git";
  if (promotion.argoDiffArtifactId) return "argo";
  return "details";
}

export function selectedPromotionDetailView(
  searchParams: URLSearchParams,
  promotion: PromotionReviewViews,
): PromotionDetailView {
  if (!promotion.reviewRequired) return "details";
  const requested = searchParams.get(promotionDetailSearchParamNames.view);
  return (
    promotionDetailViews.find((view) => view === requested) ?? defaultPromotionDetailView(promotion)
  );
}

export function updatePromotionDetailSearchParams(
  current: URLSearchParams,
  view: PromotionDetailView,
  defaultView: PromotionDetailView,
) {
  const next = new URLSearchParams(current);
  if (view === defaultView) next.delete(promotionDetailSearchParamNames.view);
  else next.set(promotionDetailSearchParamNames.view, view);
  return next;
}

export function selectedPromotionLifecycleStage(
  searchParams: URLSearchParams,
  visibleSteps: readonly PromotionPhaseStage[],
  fallback: PromotionLifecycleStage,
): PromotionLifecycleStage {
  const requested = searchParams.get(promotionDetailSearchParamNames.stage);
  if (requested === "cancellation" && fallback === "cancellation") return requested;
  return visibleSteps.find((step) => step === requested) ?? fallback;
}

export function updatePromotionLifecycleSearchParams(
  current: URLSearchParams,
  stage?: PromotionLifecycleStage,
) {
  const next = new URLSearchParams(current);
  if (stage) next.set(promotionDetailSearchParamNames.stage, stage);
  else next.delete(promotionDetailSearchParamNames.stage);
  return next;
}
