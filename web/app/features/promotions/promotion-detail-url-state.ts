export const promotionDetailSearchParamNames = {
  stage: "stage",
  view: "view",
} as const;

export const promotionDetailViews = ["git", "argo", "details"] as const;
export const promotionLifecycleStages = ["delivery", "preparation", "review", "execution"] as const;

export type PromotionDetailView = (typeof promotionDetailViews)[number];
export type PromotionLifecycleStage = (typeof promotionLifecycleStages)[number];

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
  fallback: PromotionLifecycleStage,
): PromotionLifecycleStage {
  const requested = searchParams.get(promotionDetailSearchParamNames.stage);
  return promotionLifecycleStages.includes(requested as PromotionLifecycleStage)
    ? (requested as PromotionLifecycleStage)
    : fallback;
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
