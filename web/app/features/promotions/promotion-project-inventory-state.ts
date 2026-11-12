import type { ShouldRevalidateFunctionArgs } from "react-router";

import type { PromotionProjectDetail } from "~/api/generated";

export const promotionProjectInventoryPageSize = 8;
export const promotionStageInventoryPageSize = 25;

export function promotionProjectDetailPath(
  project: Pick<PromotionProjectDetail, "cluster" | "integration" | "project">,
) {
  return `/promotion-projects/${encodeURIComponent(project.integration)}/${encodeURIComponent(project.cluster)}/${encodeURIComponent(project.project)}`;
}

export function shouldRevalidatePromotionInventory({
  currentUrl,
  defaultShouldRevalidate,
  formMethod,
  nextUrl,
}: ShouldRevalidateFunctionArgs) {
  if (formMethod !== undefined || currentUrl.pathname !== nextUrl.pathname) {
    return defaultShouldRevalidate;
  }

  const currentDataSearch = dataSearchParams(currentUrl.searchParams);
  const nextDataSearch = dataSearchParams(nextUrl.searchParams);
  return currentDataSearch === nextDataSearch ? false : defaultShouldRevalidate;
}

function dataSearchParams(searchParams: URLSearchParams) {
  const result = new URLSearchParams(searchParams);
  for (const name of ["freight", "q", "pipeline", "pipelines", "policy"]) result.delete(name);
  return result.toString();
}
