import { pageFromSearchParam } from "../../shared/search-params.ts";

export const promotionStageFreightPageSize = 10;

export type PromotionStageView = "freight" | "details" | "branch-drift";

export function promotionStagePath(stageId: string) {
  return `/promotion-stages/${encodeURIComponent(stageId)}`;
}

export function selectedPromotionStageView(searchParams: URLSearchParams): PromotionStageView {
  const requested = searchParams.get("view");
  return requested === "details" || requested === "branch-drift" ? requested : "freight";
}

export function promotionStageFreightPage(searchParams: URLSearchParams, total: number) {
  const requested = pageFromSearchParam(
    searchParams.get("freightPage"),
    promotionStageFreightPageSize,
  );
  const lastPage = Math.max(1, Math.ceil(total / promotionStageFreightPageSize));
  return Math.min(requested, lastPage);
}

export function updatePromotionStageView(current: URLSearchParams, view: PromotionStageView) {
  const next = new URLSearchParams(current);
  if (view === "freight") next.delete("view");
  else next.set("view", view);
  next.delete("freightPage");
  return next;
}

export function canonicalPromotionStageSearchParams(
  current: URLSearchParams,
  freightTotal: number,
) {
  const next = new URLSearchParams(current);
  const view = selectedPromotionStageView(current);

  if (view === "freight") {
    next.delete("view");
    const page = promotionStageFreightPage(current, freightTotal);
    if (page === 1) next.delete("freightPage");
    else next.set("freightPage", String(page));
  } else {
    next.set("view", view);
    next.delete("freightPage");
  }

  return next;
}
