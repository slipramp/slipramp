export const promotionDetailSearchParamNames = {
  stage: "stage",
  view: "view",
} as const;

export const promotionDetailViews = ["git", "argo", "details"] as const;

export type PromotionDetailView = (typeof promotionDetailViews)[number];
export type PromotionPhaseStage = "delivery" | "preparation" | "review" | "execution";
export type PromotionLifecycleStage = PromotionPhaseStage | "cancellation";

export function defaultPromotionDetailView(
  gitDiffArtifactId?: string,
  argoDiffArtifactId?: string,
): PromotionDetailView {
  if (gitDiffArtifactId) return "git";
  if (argoDiffArtifactId) return "argo";
  return "details";
}

export function selectedPromotionDetailView(
  searchParams: URLSearchParams,
  fallback: PromotionDetailView,
): PromotionDetailView {
  const requested = searchParams.get(promotionDetailSearchParamNames.view);
  return promotionDetailViews.includes(requested as PromotionDetailView)
    ? (requested as PromotionDetailView)
    : fallback;
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
